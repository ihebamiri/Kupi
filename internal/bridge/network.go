package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/graph"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/netpol"
	"github.com/ihebamiri/kupi/internal/validate"
)

var (
	netpolGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}
)

// Endpoint is one side of a traffic check: a pod, a workload or a Service.
type Endpoint struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TrafficCheck is the answer to "can From reach To on Port?".
type TrafficCheck struct {
	netpol.Verdict
	FromPod string `json:"fromPod"`
	ToPod   string `json:"toPod"`
	// Port is the pod port checked (a Service port is translated).
	Port     int32    `json:"port"`
	Protocol string   `json:"protocol"`
	Notes    []string `json:"notes"`
}

// NetworkService checks traffic against NetworkPolicies.
type NetworkService struct{ d Deps }

// NewNetworkService returns the network service.
func NewNetworkService(d Deps) *NetworkService { return &NetworkService{d: d} }

// CanReach works out whether from can open a connection to to on port (0
// for any) and protocol (TCP, UDP or SCTP), from the NetworkPolicies of
// both namespaces. It reads only; nothing is sent to the pods.
func (s *NetworkService) CanReach(ctx context.Context, contextName string, from, to Endpoint, port int, protocol string) (TrafficCheck, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return TrafficCheck{}, err
	}
	for _, e := range []Endpoint{from, to} {
		if err := validate.Namespace(e.Namespace); err != nil {
			return TrafficCheck{}, err
		}
		if err := validate.Name(e.Name); err != nil {
			return TrafficCheck{}, err
		}
	}
	protocol = strings.ToUpper(protocol)
	if protocol == "" {
		protocol = "TCP"
	}
	if protocol != "TCP" && protocol != "UDP" && protocol != "SCTP" {
		return TrafficCheck{}, fmt.Errorf("protocol must be TCP, UDP or SCTP")
	}
	if port < 0 || port > 65535 {
		return TrafficCheck{}, fmt.Errorf("port must be 0 to 65535")
	}
	r := sess.Reader()
	src, _, err := podFor(ctx, r, from, 0)
	if err != nil {
		return TrafficCheck{}, fmt.Errorf("source: %w", err)
	}
	dst, dstPort, err := podFor(ctx, r, to, int32(port))
	if err != nil {
		return TrafficCheck{}, fmt.Errorf("destination: %w", err)
	}

	var objs []unstructured.Unstructured
	seen := map[string]bool{}
	for _, ns := range []string{src.Namespace, dst.Namespace} {
		if seen[ns] {
			continue
		}
		seen[ns] = true
		l, err := r.List(ctx, netpolGVR, ns, metav1.ListOptions{})
		if err != nil {
			return TrafficCheck{}, fmt.Errorf("read NetworkPolicies in %s: %w", ns, err)
		}
		objs = append(objs, l.Items...)
	}
	policies, problems := netpol.Parse(objs)

	namespaces := map[string]netpol.Namespace{}
	if l, err := r.List(ctx, nsGVR, "", metav1.ListOptions{}); err == nil {
		for _, n := range l.Items {
			namespaces[n.GetName()] = netpol.Namespace{Name: n.GetName(), Labels: n.GetLabels()}
		}
	}

	v := netpol.Evaluate(policies, namespaces, netpol.Query{From: src, To: dst, Port: dstPort, Protocol: protocol})
	out := TrafficCheck{Verdict: v, FromPod: src.Namespace + "/" + src.Name, ToPod: dst.Namespace + "/" + dst.Name, Port: dstPort, Protocol: protocol, Notes: []string{}}
	if dstPort != int32(port) && port != 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("Service port %d goes to pod port %d.", port, dstPort))
	}
	out.Notes = append(out.Notes, problems...)
	out.Notes = append(out.Notes, "NetworkPolicies only work when the cluster's network plugin enforces them (Calico, Cilium, k3s' built-in controller and most others do; some, like plain Flannel, don't).")
	return out, nil
}

// podFor picks a pod to stand for an endpoint: the pod itself, a running
// pod of a workload, or one the Service selects. For a Service it also
// translates the Service port to the pod's port.
func podFor(ctx context.Context, r kube.Reader, e Endpoint, port int32) (netpol.Pod, int32, error) {
	var name string
	switch e.Kind {
	case "Pod":
		name = e.Name
	default:
		if !graph.HasPods(e.Kind) {
			return netpol.Pod{}, 0, fmt.Errorf("%s doesn't run pods", e.Kind)
		}
		res, err := graph.PodsOf(ctx, r, e.Kind, e.Namespace, e.Name)
		if err != nil {
			return netpol.Pod{}, 0, err
		}
		for _, p := range res.Pods {
			if p.Status == "Running" {
				name = p.Name
				break
			}
		}
		if name == "" && len(res.Pods) > 0 {
			name = res.Pods[0].Name
		}
		if name == "" {
			return netpol.Pod{}, 0, fmt.Errorf("%s %s has no pods right now", e.Kind, e.Name)
		}
	}
	obj, err := r.Get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, e.Namespace, name)
	if err != nil {
		return netpol.Pod{}, 0, err
	}
	p := netpol.Pod{Name: obj.GetName(), Namespace: obj.GetNamespace(), Labels: obj.GetLabels(), Ports: map[string]int32{}}
	p.IP, _, _ = unstructured.NestedString(obj.Object, "status", "podIP")
	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "containers")
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		ports, _, _ := unstructured.NestedSlice(cm, "ports")
		for _, pt := range ports {
			pm, _ := pt.(map[string]any)
			n, _ := pm["name"].(string)
			num, _, _ := unstructured.NestedInt64(pm, "containerPort")
			if n != "" {
				p.Ports[n] = int32(num)
			}
		}
	}
	if e.Kind == "Service" && port != 0 {
		svc, err := r.Get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "services"}, e.Namespace, e.Name)
		if err != nil {
			return netpol.Pod{}, 0, err
		}
		target, err := servicePort(svc, port, p)
		if err != nil {
			return netpol.Pod{}, 0, err
		}
		return p, target, nil
	}
	return p, port, nil
}

// servicePort maps a Service port to the target port on a pod.
func servicePort(svc *unstructured.Unstructured, port int32, pod netpol.Pod) (int32, error) {
	ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	for _, sp := range ports {
		m, _ := sp.(map[string]any)
		n, _, _ := unstructured.NestedInt64(m, "port")
		if int32(n) != port {
			continue
		}
		switch t := m["targetPort"].(type) {
		case int64:
			return int32(t), nil
		case float64:
			return int32(t), nil
		case string:
			if v, ok := pod.Ports[t]; ok {
				return v, nil
			}
			return 0, fmt.Errorf("the pod has no port named %s", t)
		default:
			return port, nil
		}
	}
	return 0, errors.New("the Service has no such port")
}
