package bridge

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/forward"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/metrics"
	"github.com/ihebamiri/kupi/internal/prom"
	"github.com/ihebamiri/kupi/internal/validate"
)

var forwardIDRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// Resources is CPU in millicores and memory in bytes.
type Resources struct {
	CPU int64 `json:"cpu"`
	Mem int64 `json:"mem"`
}

// PodMetrics is a pod's history with its requests and limits.
type PodMetrics struct {
	Status   metrics.Status   `json:"status"`
	Samples  []metrics.Sample `json:"samples"`
	Requests Resources        `json:"requests"`
	Limits   Resources        `json:"limits"`
}

// NodeMetrics is a node's history.
type NodeMetrics struct {
	Status  metrics.Status   `json:"status"`
	Samples []metrics.Sample `json:"samples"`
}

// MetricsService serves CPU and memory data.
type MetricsService struct{ d Deps }

// NewMetricsService returns the metrics service.
func NewMetricsService(d Deps) *MetricsService { return &MetricsService{d: d} }

// Overview returns node usage and the busiest pods.
func (s *MetricsService) Overview(contextName, namespace string) (metrics.Overview, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return metrics.Overview{}, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return metrics.Overview{}, err
	}
	return s.d.Metrics.Overview(contextName, sess.Reader(), namespace, 10), nil
}

// Pod returns one pod's history plus its summed requests and limits.
func (s *MetricsService) Pod(ctx context.Context, contextName, namespace, pod string) (PodMetrics, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return PodMetrics{}, err
	}
	if err := validate.Namespace(namespace); err != nil {
		return PodMetrics{}, err
	}
	if err := validate.Name(pod); err != nil {
		return PodMetrics{}, err
	}
	status, samples := s.d.Metrics.PodSeries(contextName, sess.Reader(), namespace, pod)
	out := PodMetrics{Status: status, Samples: samples}
	if p, err := sess.Reader().Get(ctx, podsGVR, namespace, pod); err == nil {
		out.Requests, out.Limits = podResources(p)
	}
	return out, nil
}

// Node returns one node's history.
func (s *MetricsService) Node(contextName, node string) (NodeMetrics, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return NodeMetrics{}, err
	}
	if err := validate.Name(node); err != nil {
		return NodeMetrics{}, err
	}
	status, samples := s.d.Metrics.NodeSeries(contextName, sess.Reader(), node)
	return NodeMetrics{Status: status, Samples: samples}, nil
}

func podResources(pod *unstructured.Unstructured) (req, lim Resources) {
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		r, _, _ := unstructured.NestedStringMap(cm, "resources", "requests")
		l, _, _ := unstructured.NestedStringMap(cm, "resources", "limits")
		req.CPU += qty(r["cpu"], true)
		req.Mem += qty(r["memory"], false)
		lim.CPU += qty(l["cpu"], true)
		lim.Mem += qty(l["memory"], false)
	}
	return req, lim
}

func qty(s string, milli bool) int64 {
	if s == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	if milli {
		return q.MilliValue()
	}
	return q.Value()
}

// PortOption is a port the UI offers to forward.
type PortOption struct {
	Port     int    `json:"port"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}

// ForwardService manages port-forwards (event "kupi:forwards").
type ForwardService struct{ d Deps }

// NewForwardService returns the forward service.
func NewForwardService(d Deps) *ForwardService { return &ForwardService{d: d} }

var forwardKinds = map[string]schema.GroupVersionResource{
	"Pod":         podsGVR,
	"Service":     {Version: "v1", Resource: "services"},
	"Deployment":  {Group: "apps", Version: "v1", Resource: "deployments"},
	"StatefulSet": {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"DaemonSet":   {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"ReplicaSet":  {Group: "apps", Version: "v1", Resource: "replicasets"},
}

// Ports lists the ports a Pod, Service or workload exposes.
func (s *ForwardService) Ports(ctx context.Context, contextName, kind, namespace, name string) ([]PortOption, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	gvr, ok := forwardKinds[kind]
	if !ok {
		return nil, fmt.Errorf("can't forward to a %s", kind)
	}
	if err := validate.Namespace(namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(name); err != nil {
		return nil, err
	}
	obj, err := sess.Reader().Get(ctx, gvr, namespace, name)
	if err != nil {
		return nil, err
	}
	var ports []PortOption
	if kind == "Service" {
		list, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
		for _, p := range list {
			pm, _ := p.(map[string]interface{})
			n, _, _ := unstructured.NestedInt64(pm, "port")
			nm, _, _ := unstructured.NestedString(pm, "name")
			proto, _, _ := unstructured.NestedString(pm, "protocol")
			ports = append(ports, PortOption{Port: int(n), Name: nm, Protocol: proto})
		}
		return ports, nil
	}
	path := []string{"spec", "template", "spec", "containers"}
	if kind == "Pod" {
		path = []string{"spec", "containers"}
	}
	containers, _, _ := unstructured.NestedSlice(obj.Object, path...)
	for _, c := range containers {
		cm, _ := c.(map[string]interface{})
		list, _, _ := unstructured.NestedSlice(cm, "ports")
		for _, p := range list {
			pm, _ := p.(map[string]interface{})
			n, _, _ := unstructured.NestedInt64(pm, "containerPort")
			nm, _, _ := unstructured.NestedString(pm, "name")
			proto, _, _ := unstructured.NestedString(pm, "protocol")
			ports = append(ports, PortOption{Port: int(n), Name: nm, Protocol: proto})
		}
	}
	return ports, nil
}

// Start begins a forward on 127.0.0.1. localPort 0 picks one.
func (s *ForwardService) Start(ctx context.Context, contextName string, t forward.Target, localPort int) (forward.Forward, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return forward.Forward{}, err
	}
	if _, ok := forwardKinds[t.Kind]; !ok {
		return forward.Forward{}, fmt.Errorf("can't forward to a %s", t.Kind)
	}
	if err := validate.Namespace(t.Namespace); err != nil {
		return forward.Forward{}, err
	}
	if err := validate.Name(t.Name); err != nil {
		return forward.Forward{}, err
	}
	allowed, reason, err := sess.Reader().CanI(ctx, "create", podsGVR, "portforward", t.Namespace, "")
	if err == nil && !allowed {
		return forward.Forward{}, fmt.Errorf("your account can't forward ports in namespace %s. %s", t.Namespace, reason)
	}
	f, err := s.d.Forwards.Start(contextName, forwardSession{sess}, t, localPort)
	if err != nil {
		return forward.Forward{}, err
	}
	_, _ = s.d.Audit.Append(audit.Entry{
		Context:  contextName,
		Profile:  string(sess.Profile()),
		KubeUser: sess.UserName(),
		Action:   "port-forward",
		Resource: fmt.Sprintf("%s %s/%s port %d -> 127.0.0.1:%d", t.Kind, t.Namespace, t.Name, t.Port, f.LocalPort),
		Outcome:  audit.OutcomeInfo,
	})
	return f, nil
}

// Stop ends a forward.
func (s *ForwardService) Stop(id string) {
	if forwardIDRe.MatchString(id) {
		s.d.Forwards.Stop(id)
	}
}

// List returns all forwards.
func (s *ForwardService) List() []forward.Forward {
	return s.d.Forwards.List()
}

// forwardSession adapts a kube.Session to forward.Session.
type forwardSession struct{ s *kube.Session }

func (f forwardSession) Reader() kube.Reader { return f.s.Reader() }

func (f forwardSession) PortForward(ctx context.Context, ns, pod string, local, remote int, onReady func(int)) error {
	return f.s.PortForward(ctx, ns, pod, local, remote, onReady)
}

// PrometheusStatus says whether the cluster has a Prometheus Kupi can read.
type PrometheusStatus struct {
	Found  bool   `json:"found"`
	Where  string `json:"where"`
	Reason string `json:"reason"`
}

// Prometheus looks for a Prometheus Service in the cluster.
func (s *MetricsService) Prometheus(ctx context.Context, contextName string) PrometheusStatus {
	sess, err := s.d.session(contextName)
	if err != nil {
		return PrometheusStatus{Reason: err.Error()}
	}
	ep, err := prom.Find(ctx, sess.Reader())
	if err != nil {
		return PrometheusStatus{Reason: err.Error()}
	}
	return PrometheusStatus{Found: true, Where: ep.String()}
}

// PodHistory reads a pod's CPU and memory over the last hours from
// Prometheus (1 hour to 31 days).
func (s *MetricsService) PodHistory(ctx context.Context, contextName, namespace, pod string, hours int) ([]metrics.Sample, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if hours < 1 || hours > 31*24 {
		return nil, fmt.Errorf("choose between 1 hour and 31 days")
	}
	ep, err := prom.Find(ctx, sess.Reader())
	if err != nil {
		return nil, err
	}
	out, err := prom.PodHistory(ctx, sess.Reader(), ep, namespace, pod, time.Duration(hours)*time.Hour)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []metrics.Sample{}
	}
	return out, nil
}
