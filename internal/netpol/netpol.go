// Package netpol answers "can pod A reach pod B on this port?" from the
// cluster's NetworkPolicies, the way Kubernetes defines them:
//
//   - Traffic must be allowed twice: as egress from A (by A's namespace's
//     policies) and as ingress to B (by B's namespace's policies).
//   - A pod that no Policy of a direction selects is open in that direction.
//   - Once selected, only what some rule allows gets through.
//   - A rule allows a peer (pod selector, namespace selector, both, or an IP
//     block) on a list of ports; an empty list means everything.
//
// It reads policies as data and never contacts pods. Whether the cluster
// enforces policies depends on its network plugin; the verdict says so.
package netpol

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// Pod is what the evaluator needs to know about a pod.
type Pod struct {
	Name      string
	Namespace string
	Labels    map[string]string
	IP        string
	// Ports maps container port names to numbers, for named ports.
	Ports map[string]int32
}

// Namespace is a namespace and its labels.
type Namespace struct {
	Name   string
	Labels map[string]string
}

// Query asks whether From can open a connection to To on Port/Protocol.
// Port 0 means "any port".
type Query struct {
	From     Pod
	To       Pod
	Port     int32
	Protocol string
}

// Side is the verdict for one direction.
type Side struct {
	Allowed bool `json:"allowed"`
	// Isolated: at least one Policy selects the pod in this direction.
	Isolated bool `json:"isolated"`
	// Policies lists the policies that select the pod in this direction.
	Policies []string `json:"policies"`
	// Reason explains the verdict in a sentence.
	Reason string `json:"reason"`
}

// Verdict is the answer, with both directions.
type Verdict struct {
	Allowed bool `json:"allowed"`
	Egress  Side `json:"egress"`
	Ingress Side `json:"ingress"`
}

// Policy is the part of a NetworkPolicy the evaluator uses.
type Policy struct {
	Name, Namespace string
	Selector        labels.Selector
	Ingress, Egress bool
	IngressRules    []rule
	EgressRules     []rule
}

type rule struct {
	Peers []peer // empty: any peer
	Ports []port // empty: any port
}

type peer struct {
	Pod       labels.Selector // nil: not set
	Namespace labels.Selector // nil: not set
	CIDR      *net.IPNet
	Except    []*net.IPNet
}

type port struct {
	Protocol string
	Number   int32
	Name     string
	End      int32
}

// Parse reads NetworkPolicy objects. Bad selectors make a policy match
// nothing, which errs towards "blocked" and is reported.
func Parse(objs []unstructured.Unstructured) ([]Policy, []string) {
	var out []Policy
	var problems []string
	for i := range objs {
		var np struct {
			Spec struct {
				PodSelector metav1.LabelSelector `json:"podSelector"`
				PolicyTypes []string             `json:"policyTypes"`
				Ingress     []struct {
					From  []rawPeer `json:"from"`
					Ports []rawPort `json:"ports"`
				} `json:"ingress"`
				Egress []struct {
					To    []rawPeer `json:"to"`
					Ports []rawPort `json:"ports"`
				} `json:"egress"`
			} `json:"spec"`
		}
		o := &objs[i]
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, &np); err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: %v", o.GetNamespace(), o.GetName(), err))
			continue
		}
		sel, err := metav1.LabelSelectorAsSelector(&np.Spec.PodSelector)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: %v", o.GetNamespace(), o.GetName(), err))
			sel = labels.Nothing()
		}
		p := Policy{Name: o.GetName(), Namespace: o.GetNamespace(), Selector: sel}
		_, hasEgress, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "egress")
		if len(np.Spec.PolicyTypes) == 0 {
			p.Ingress, p.Egress = true, hasEgress
		}
		for _, t := range np.Spec.PolicyTypes {
			switch t {
			case "Ingress":
				p.Ingress = true
			case "Egress":
				p.Egress = true
			}
		}
		for _, r := range np.Spec.Ingress {
			p.IngressRules = append(p.IngressRules, rule{Peers: parsePeers(r.From, &problems, o), Ports: parsePorts(r.Ports)})
		}
		for _, r := range np.Spec.Egress {
			p.EgressRules = append(p.EgressRules, rule{Peers: parsePeers(r.To, &problems, o), Ports: parsePorts(r.Ports)})
		}
		out = append(out, p)
	}
	return out, problems
}

type rawPeer struct {
	PodSelector       *metav1.LabelSelector `json:"podSelector"`
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`
	IPBlock           *struct {
		CIDR   string   `json:"cidr"`
		Except []string `json:"except"`
	} `json:"ipBlock"`
}

type rawPort struct {
	Protocol string `json:"protocol"`
	Port     any    `json:"port"`
	EndPort  int32  `json:"endPort"`
}

func parsePeers(in []rawPeer, problems *[]string, o *unstructured.Unstructured) []peer {
	var out []peer
	for _, r := range in {
		var p peer
		if r.PodSelector != nil {
			s, err := metav1.LabelSelectorAsSelector(r.PodSelector)
			if err != nil {
				*problems = append(*problems, fmt.Sprintf("%s/%s: %v", o.GetNamespace(), o.GetName(), err))
				s = labels.Nothing()
			}
			p.Pod = s
		}
		if r.NamespaceSelector != nil {
			s, err := metav1.LabelSelectorAsSelector(r.NamespaceSelector)
			if err != nil {
				*problems = append(*problems, fmt.Sprintf("%s/%s: %v", o.GetNamespace(), o.GetName(), err))
				s = labels.Nothing()
			}
			p.Namespace = s
		}
		if r.IPBlock != nil {
			_, cidr, err := net.ParseCIDR(r.IPBlock.CIDR)
			if err == nil {
				p.CIDR = cidr
			}
			for _, e := range r.IPBlock.Except {
				if _, n, err := net.ParseCIDR(e); err == nil {
					p.Except = append(p.Except, n)
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func parsePorts(in []rawPort) []port {
	var out []port
	for _, r := range in {
		p := port{Protocol: strings.ToUpper(r.Protocol), End: r.EndPort}
		if p.Protocol == "" {
			p.Protocol = "TCP"
		}
		switch v := r.Port.(type) {
		case int64:
			p.Number = int32(v)
		case float64:
			p.Number = int32(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				p.Number = int32(n)
			} else {
				p.Name = v
			}
		}
		out = append(out, p)
	}
	return out
}

// Evaluate answers q against the policies.
func Evaluate(policies []Policy, namespaces map[string]Namespace, q Query) Verdict {
	if q.Protocol == "" {
		q.Protocol = "TCP"
	}
	q.Protocol = strings.ToUpper(q.Protocol)
	egress := evaluateSide(policies, namespaces, q, true)
	ingress := evaluateSide(policies, namespaces, q, false)
	return Verdict{Allowed: egress.Allowed && ingress.Allowed, Egress: egress, Ingress: ingress}
}

func evaluateSide(policies []Policy, namespaces map[string]Namespace, q Query, egress bool) Side {
	subject, other := q.To, q.From
	dir := "ingress to"
	if egress {
		subject, other = q.From, q.To
		dir = "egress from"
	}
	var selecting []Policy
	for _, p := range policies {
		if p.Namespace != subject.Namespace || !p.Selector.Matches(labels.Set(subject.Labels)) {
			continue
		}
		if (egress && p.Egress) || (!egress && p.Ingress) {
			selecting = append(selecting, p)
		}
	}
	side := Side{Policies: []string{}}
	for _, p := range selecting {
		side.Policies = append(side.Policies, p.Name)
	}
	sort.Strings(side.Policies)
	if len(selecting) == 0 {
		side.Allowed = true
		side.Reason = fmt.Sprintf("No NetworkPolicy limits %s %s, so it's open.", dir, subject.Name)
		return side
	}
	side.Isolated = true
	for _, p := range selecting {
		rules := p.IngressRules
		if egress {
			rules = p.EgressRules
		}
		for i, r := range rules {
			if !peersMatch(r.Peers, p.Namespace, other, namespaces) {
				continue
			}
			// Named ports are looked up on the destination pod.
			if !portsMatch(r.Ports, q, q.To) {
				continue
			}
			side.Allowed = true
			side.Reason = fmt.Sprintf("Allowed by %s, %s rule %d.", p.Name, map[bool]string{true: "egress", false: "ingress"}[egress], i+1)
			return side
		}
	}
	if len(side.Policies) == 1 {
		side.Reason = fmt.Sprintf("The policy %s limits %s %s, and none of its rules allows %s%s.", side.Policies[0], dir, subject.Name, other.Name, portLabel(q))
	} else {
		side.Reason = fmt.Sprintf("The policies %s limit %s %s, and none of their rules allows %s%s.", strings.Join(side.Policies, ", "), dir, subject.Name, other.Name, portLabel(q))
	}
	return side
}

func portLabel(q Query) string {
	if q.Port == 0 {
		return ""
	}
	return fmt.Sprintf(" on %d/%s", q.Port, q.Protocol)
}

func peersMatch(peers []peer, policyNS string, other Pod, namespaces map[string]Namespace) bool {
	if len(peers) == 0 {
		return true
	}
	for _, p := range peers {
		if p.CIDR != nil {
			ip := net.ParseIP(other.IP)
			if ip == nil || !p.CIDR.Contains(ip) {
				continue
			}
			excluded := false
			for _, e := range p.Except {
				if e.Contains(ip) {
					excluded = true
				}
			}
			if !excluded {
				return true
			}
			continue
		}
		nsOK := false
		if p.Namespace == nil {
			// Only the policy's own namespace.
			nsOK = other.Namespace == policyNS
		} else {
			ns := namespaces[other.Namespace]
			nsLabels := ns.Labels
			if nsLabels == nil {
				nsLabels = map[string]string{}
			}
			// Kubernetes labels every namespace with its own name.
			nsLabels = withName(nsLabels, other.Namespace)
			nsOK = p.Namespace.Matches(labels.Set(nsLabels))
		}
		podOK := p.Pod == nil || p.Pod.Matches(labels.Set(other.Labels))
		if nsOK && podOK {
			return true
		}
	}
	return false
}

func withName(in map[string]string, name string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	out["kubernetes.io/metadata.name"] = name
	return out
}

func portsMatch(ports []port, q Query, dest Pod) bool {
	if len(ports) == 0 {
		return true
	}
	if q.Port == 0 {
		// "Any port" can't be proven allowed by a rule limited to ports.
		return false
	}
	for _, p := range ports {
		if p.Protocol != q.Protocol {
			continue
		}
		n := p.Number
		if p.Name != "" {
			n = dest.Ports[p.Name]
			if n == 0 {
				continue
			}
		}
		if n == 0 || (p.End == 0 && n == q.Port) || (p.End != 0 && q.Port >= n && q.Port <= p.End) {
			return true
		}
	}
	return false
}
