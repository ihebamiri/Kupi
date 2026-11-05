// Package graph builds the Observe canvas: how the objects in a namespace
// connect. Pods are folded into the workload that owns them, and edges come
// from real relationships: routes to Services, Service selectors to
// workloads, workloads to the ConfigMaps, Secrets and claims they use, and
// autoscalers to their targets.
package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Health of a node.
const (
	Good    = "good"
	Warn    = "warn"
	Bad     = "bad"
	Unknown = "unknown"
)

// Security and exposure flags.
const (
	FlagPublic      = "public"
	FlagPrivileged  = "privileged"
	FlagHostNetwork = "hostNetwork"
	FlagHostPath    = "hostPath"
	FlagRoot        = "mayRunAsRoot"
	FlagNoLimits    = "noMemoryLimit"
	FlagMissing     = "missing"
)

// MaxPods caps how many pods one graph reads.
const MaxPods = 3000

// Node is one box on the canvas.
type Node struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Group     string            `json:"group"`
	Version   string            `json:"version"`
	Resource  string            `json:"resource"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Created   string            `json:"created"`
	Labels    map[string]string `json:"labels"`
	Health    string            `json:"health"`
	Summary   string            `json:"summary"`
	Pods      int               `json:"pods"`
	Ready     int               `json:"ready"`
	Restarts  int64             `json:"restarts"`
	Flags     []string          `json:"flags"`

	// Internal: the labels of the pods this node runs, and what they use.
	templateLabels map[string]string
	refs           []ref
}

// Edge is one relationship. Kind is routes, selects, uses, mounts or scales.
type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// Graph is the canvas for one namespace, or all of them.
type Graph struct {
	Namespace string   `json:"namespace"`
	Nodes     []Node   `json:"nodes"`
	Edges     []Edge   `json:"edges"`
	Warnings  []string `json:"warnings"`
	Truncated bool     `json:"truncated"`
}

type gvrKind struct {
	gvr  schema.GroupVersionResource
	kind string
}

var (
	podsT         = gvrKind{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "Pod"}
	servicesT     = gvrKind{schema.GroupVersionResource{Version: "v1", Resource: "services"}, "Service"}
	configMapsT   = gvrKind{schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, "ConfigMap"}
	secretsT      = gvrKind{schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, "Secret"}
	pvcsT         = gvrKind{schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, "PersistentVolumeClaim"}
	deploymentsT  = gvrKind{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment"}
	statefulSetsT = gvrKind{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, "StatefulSet"}
	daemonSetsT   = gvrKind{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, "DaemonSet"}
	replicaSetsT  = gvrKind{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, "ReplicaSet"}
	jobsT         = gvrKind{schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, "Job"}
	cronJobsT     = gvrKind{schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, "CronJob"}
	ingressesT    = gvrKind{schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, "Ingress"}
	httpRoutesT   = gvrKind{schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, "HTTPRoute"}
	hpasT         = gvrKind{schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, "HorizontalPodAutoscaler"}
)

// ID returns the node id for an object.
func ID(kind, namespace, name string) string { return kind + "/" + namespace + "/" + name }

type builder struct {
	r     kube.Reader
	ns    string
	g     *Graph
	nodes map[string]*Node
	edges map[string]bool
}

// Build reads a namespace ("" for all) and returns its graph.
func Build(ctx context.Context, r kube.Reader, namespace string) (*Graph, error) {
	b := &builder{r: r, ns: namespace, g: &Graph{Namespace: namespace}, nodes: map[string]*Node{}, edges: map[string]bool{}}

	pods, err := b.list(ctx, podsT, true)
	if err != nil {
		return nil, err
	}
	if len(pods) > MaxPods {
		pods = pods[:MaxPods]
		b.g.Truncated = true
	}
	deployments, _ := b.list(ctx, deploymentsT, false)
	statefulSets, _ := b.list(ctx, statefulSetsT, false)
	daemonSets, _ := b.list(ctx, daemonSetsT, false)
	replicaSets, _ := b.list(ctx, replicaSetsT, false)
	jobs, _ := b.list(ctx, jobsT, false)
	cronJobs, _ := b.list(ctx, cronJobsT, false)
	services, _ := b.list(ctx, servicesT, false)
	ingresses, _ := b.list(ctx, ingressesT, false)
	routes, _ := b.list(ctx, httpRoutesT, false)
	hpas, _ := b.list(ctx, hpasT, false)
	pvcs, _ := b.list(ctx, pvcsT, false)
	configMaps := b.names(ctx, configMapsT)
	secrets := b.names(ctx, secretsT)

	// Workloads, and which workload each ReplicaSet or Job belongs to.
	owner := map[string]string{} // "ReplicaSet/ns/name" -> workload node id
	for _, list := range []struct {
		items []unstructured.Unstructured
		t     gvrKind
	}{{deployments, deploymentsT}, {statefulSets, statefulSetsT}, {daemonSets, daemonSetsT}, {cronJobs, cronJobsT}} {
		for i := range list.items {
			b.workload(&list.items[i], list.t)
		}
	}
	for i := range replicaSets {
		rs := &replicaSets[i]
		if c := controller(rs); c != nil && c.Kind == "Deployment" {
			owner[ID("ReplicaSet", rs.GetNamespace(), rs.GetName())] = ID("Deployment", rs.GetNamespace(), c.Name)
			continue
		}
		b.workload(rs, replicaSetsT)
	}
	for i := range jobs {
		j := &jobs[i]
		if c := controller(j); c != nil && c.Kind == "CronJob" {
			owner[ID("Job", j.GetNamespace(), j.GetName())] = ID("CronJob", j.GetNamespace(), c.Name)
			continue
		}
		b.workload(j, jobsT)
	}

	// Fold pods into their workload; pods without one become their own node.
	podStatus := map[string][]podInfo{}
	for i := range pods {
		p := &pods[i]
		target := ""
		if c := controller(p); c != nil {
			id := ID(c.Kind, p.GetNamespace(), c.Name)
			if w, ok := owner[id]; ok {
				id = w
			}
			if _, ok := b.nodes[id]; ok {
				target = id
			}
		}
		if target == "" {
			target = b.workload(p, podsT).ID
		}
		podStatus[target] = append(podStatus[target], inspectPod(p))
	}
	for id, infos := range podStatus {
		b.applyPods(b.nodes[id], infos)
	}

	for i := range services {
		b.service(&services[i])
	}
	for i := range ingresses {
		b.ingress(&ingresses[i])
	}
	for i := range routes {
		b.httpRoute(&routes[i])
	}
	for i := range hpas {
		b.hpa(&hpas[i])
	}
	b.configRefs(configMaps, secrets, pvcs)

	for _, n := range b.nodes {
		sort.Strings(n.Flags)
		b.g.Nodes = append(b.g.Nodes, *n)
	}
	sort.Slice(b.g.Nodes, func(i, j int) bool { return b.g.Nodes[i].ID < b.g.Nodes[j].ID })
	sort.Slice(b.g.Edges, func(i, j int) bool { return b.g.Edges[i].ID < b.g.Edges[j].ID })
	if b.g.Nodes == nil {
		b.g.Nodes = []Node{}
	}
	if b.g.Edges == nil {
		b.g.Edges = []Edge{}
	}
	return b.g, nil
}

// list reads one type. Missing types (no Gateway API, say) and forbidden ones
// are skipped with a warning unless required.
func (b *builder) list(ctx context.Context, t gvrKind, required bool) ([]unstructured.Unstructured, error) {
	l, err := b.r.List(ctx, t.gvr, b.ns, metav1.ListOptions{})
	if err != nil {
		if required {
			return nil, fmt.Errorf("list %s: %w", t.gvr.Resource, err)
		}
		if apierrors.IsForbidden(err) {
			b.g.Warnings = append(b.g.Warnings, fmt.Sprintf("You can't list %s, so they're missing from the map.", t.gvr.Resource))
		} else if !apierrors.IsNotFound(err) && !meta.IsNoMatchError(err) {
			b.g.Warnings = append(b.g.Warnings, fmt.Sprintf("Couldn't read %s: %v", t.gvr.Resource, err))
		}
		return nil, err
	}
	return l.Items, nil
}

// names lists only object names, through the table API, so Secret values
// are never read.
func (b *builder) names(ctx context.Context, t gvrKind) map[string]bool {
	out := map[string]bool{}
	table, err := b.r.Table(ctx, t.gvr, true, b.ns, kube.TableOptions{})
	if err != nil {
		return out
	}
	for _, row := range table.Rows {
		var m metav1.PartialObjectMetadata
		if err := json.Unmarshal(row.Object.Raw, &m); err == nil {
			out[m.Namespace+"/"+m.Name] = true
		}
	}
	return out
}

func (b *builder) node(obj *unstructured.Unstructured, t gvrKind) *Node {
	id := ID(t.kind, obj.GetNamespace(), obj.GetName())
	if n, ok := b.nodes[id]; ok {
		return n
	}
	n := &Node{
		ID: id, Kind: t.kind, Group: t.gvr.Group, Version: t.gvr.Version, Resource: t.gvr.Resource,
		Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: string(obj.GetUID()), Labels: obj.GetLabels(),
		Health: Unknown,
	}
	if ts := obj.GetCreationTimestamp(); !ts.IsZero() {
		n.Created = ts.UTC().Format("2006-01-02T15:04:05Z")
	}
	b.nodes[id] = n
	return n
}

func (b *builder) placeholder(t gvrKind, namespace, name string) *Node {
	id := ID(t.kind, namespace, name)
	if n, ok := b.nodes[id]; ok {
		return n
	}
	n := &Node{ID: id, Kind: t.kind, Group: t.gvr.Group, Version: t.gvr.Version, Resource: t.gvr.Resource,
		Name: name, Namespace: namespace, Health: Bad, Summary: "doesn't exist", Flags: []string{FlagMissing}}
	b.nodes[id] = n
	return n
}

func (b *builder) edge(source, target, kind string) {
	id := source + "->" + target
	if b.edges[id] {
		return
	}
	b.edges[id] = true
	b.g.Edges = append(b.g.Edges, Edge{ID: id, Source: source, Target: target, Kind: kind})
}

// workload adds a node for an object that runs pods, with its pod template.
func (b *builder) workload(obj *unstructured.Unstructured, t gvrKind) *Node {
	n := b.node(obj, t)
	spec, labels := podTemplate(obj, t.kind)
	n.templateLabels = labels
	n.refs = podRefs(spec)
	n.Flags = append(n.Flags, securityFlags(spec)...)
	switch t.kind {
	case "Deployment", "StatefulSet", "ReplicaSet":
		want, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if !found {
			want = 1
		}
		ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
		n.Summary = fmt.Sprintf("%d/%d ready", ready, want)
		n.Health = ratioHealth(ready, want)
	case "DaemonSet":
		want, _, _ := unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
		ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "numberReady")
		n.Summary = fmt.Sprintf("%d/%d ready", ready, want)
		n.Health = ratioHealth(ready, want)
	case "CronJob":
		schedule, _, _ := unstructured.NestedString(obj.Object, "spec", "schedule")
		n.Summary = "schedule " + schedule
		n.Health = Good
	case "Job":
		succeeded, _, _ := unstructured.NestedInt64(obj.Object, "status", "succeeded")
		failed, _, _ := unstructured.NestedInt64(obj.Object, "status", "failed")
		switch {
		case failed > 0:
			n.Health, n.Summary = Bad, fmt.Sprintf("%d failed", failed)
		case succeeded > 0:
			n.Health, n.Summary = Good, "complete"
		default:
			n.Health, n.Summary = Warn, "running"
		}
	}
	return n
}

func ratioHealth(ready, want int64) string {
	switch {
	case want == 0:
		return Unknown
	case ready >= want:
		return Good
	case ready == 0:
		return Bad
	}
	return Warn
}

type podInfo struct {
	ready    bool
	restarts int64
	health   string
	reason   string
}

func inspectPod(p *unstructured.Unstructured) podInfo {
	info := podInfo{health: Good}
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	allReady := len(statuses) > 0
	for _, s := range statuses {
		sm, _ := s.(map[string]interface{})
		if r, _ := sm["ready"].(bool); !r {
			allReady = false
		}
		rc, _, _ := unstructured.NestedInt64(sm, "restartCount")
		info.restarts += rc
		if reason, ok, _ := unstructured.NestedString(sm, "state", "waiting", "reason"); ok {
			info.reason = reason
		}
	}
	info.ready = allReady && phase == "Running"
	switch {
	case phase == "Failed" || info.reason == "CrashLoopBackOff" || info.reason == "ImagePullBackOff" || info.reason == "ErrImagePull" || info.reason == "CreateContainerConfigError":
		info.health = Bad
	case phase == "Succeeded":
		info.health = Good
	case phase == "Pending" || !info.ready:
		info.health = Warn
	}
	return info
}

func (b *builder) applyPods(n *Node, infos []podInfo) {
	worst := Good
	reason := ""
	for _, p := range infos {
		n.Pods++
		if p.ready {
			n.Ready++
		}
		n.Restarts += p.restarts
		if rank(p.health) > rank(worst) {
			worst, reason = p.health, p.reason
		}
	}
	if n.Kind == "Pod" {
		n.Health = worst
		n.Summary = "running"
		if reason != "" {
			n.Summary = reason
		} else if worst == Warn {
			n.Summary = "not ready"
		} else if worst == Good && infos[0].health == Good && !infos[0].ready {
			n.Summary = "completed"
		}
		return
	}
	if rank(worst) > rank(n.Health) || n.Health == Unknown {
		n.Health = worst
	}
	if reason != "" {
		n.Summary += " · " + reason
	}
}

func rank(h string) int {
	switch h {
	case Bad:
		return 3
	case Warn:
		return 2
	case Good:
		return 1
	}
	return 0
}

func (b *builder) service(svc *unstructured.Unstructured) {
	n := b.node(svc, servicesT)
	typ, _, _ := unstructured.NestedString(svc.Object, "spec", "type")
	if typ == "" {
		typ = "ClusterIP"
	}
	n.Summary = typ
	if typ == "LoadBalancer" || typ == "NodePort" {
		n.Flags = append(n.Flags, FlagPublic)
	}
	selector, _, _ := unstructured.NestedStringMap(svc.Object, "spec", "selector")
	if len(selector) == 0 {
		n.Health = Unknown
		n.Summary += " · no selector"
		return
	}
	matched := 0
	for _, w := range b.nodes {
		if w.Namespace != svc.GetNamespace() || !runsPods(w.Kind) {
			continue
		}
		if matches(selector, b.templateLabels(w)) {
			b.edge(n.ID, w.ID, "selects")
			matched++
			if rank(w.Health) > rank(n.Health) || n.Health == Unknown {
				n.Health = w.Health
			}
		}
	}
	if matched == 0 {
		n.Health = Warn
		n.Summary += " · selects no pods"
	}
}

// templateLabels are the labels of the pods a node runs.
func (b *builder) templateLabels(n *Node) map[string]string {
	if n.Kind == "Pod" {
		return n.Labels
	}
	return n.templateLabels
}

func runsPods(kind string) bool {
	switch kind {
	case "Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		return true
	}
	return false
}

func matches(selector, labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func (b *builder) ingress(ing *unstructured.Unstructured) {
	n := b.node(ing, ingressesT)
	n.Flags = append(n.Flags, FlagPublic)
	n.Health = Good
	hosts := map[string]bool{}
	var backends []string
	if name, ok, _ := unstructured.NestedString(ing.Object, "spec", "defaultBackend", "service", "name"); ok {
		backends = append(backends, name)
	}
	rules, _, _ := unstructured.NestedSlice(ing.Object, "spec", "rules")
	for _, r := range rules {
		rm, _ := r.(map[string]interface{})
		if h, ok := rm["host"].(string); ok && h != "" {
			hosts[h] = true
		}
		paths, _, _ := unstructured.NestedSlice(rm, "http", "paths")
		for _, p := range paths {
			pm, _ := p.(map[string]interface{})
			if name, ok, _ := unstructured.NestedString(pm, "backend", "service", "name"); ok {
				backends = append(backends, name)
			}
		}
	}
	n.Summary = hostSummary(hosts)
	b.linkServices(n, ing.GetNamespace(), backends)
}

func (b *builder) httpRoute(route *unstructured.Unstructured) {
	n := b.node(route, httpRoutesT)
	n.Flags = append(n.Flags, FlagPublic)
	n.Health = Good
	hosts := map[string]bool{}
	hostnames, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "hostnames")
	for _, h := range hostnames {
		hosts[h] = true
	}
	var backends []string
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	for _, r := range rules {
		rm, _ := r.(map[string]interface{})
		refs, _, _ := unstructured.NestedSlice(rm, "backendRefs")
		for _, ref := range refs {
			m, _ := ref.(map[string]interface{})
			kind, _ := m["kind"].(string)
			ns, _ := m["namespace"].(string)
			if (kind != "" && kind != "Service") || (ns != "" && ns != route.GetNamespace()) {
				continue
			}
			if name, ok := m["name"].(string); ok {
				backends = append(backends, name)
			}
		}
	}
	n.Summary = hostSummary(hosts)
	b.linkServices(n, route.GetNamespace(), backends)
}

func (b *builder) linkServices(n *Node, namespace string, names []string) {
	for _, name := range names {
		id := ID("Service", namespace, name)
		if _, ok := b.nodes[id]; !ok {
			b.placeholder(servicesT, namespace, name)
			n.Health = Bad
			n.Summary += " · missing Service " + name
		}
		b.edge(n.ID, id, "routes")
	}
}

func hostSummary(hosts map[string]bool) string {
	if len(hosts) == 0 {
		return "any host"
	}
	list := make([]string, 0, len(hosts))
	for h := range hosts {
		list = append(list, h)
	}
	sort.Strings(list)
	if len(list) > 2 {
		return fmt.Sprintf("%s +%d", strings.Join(list[:2], ", "), len(list)-2)
	}
	return strings.Join(list, ", ")
}

func (b *builder) hpa(h *unstructured.Unstructured) {
	n := b.node(h, hpasT)
	kind, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "kind")
	name, _, _ := unstructured.NestedString(h.Object, "spec", "scaleTargetRef", "name")
	minR, found, _ := unstructured.NestedInt64(h.Object, "spec", "minReplicas")
	if !found {
		minR = 1
	}
	maxR, _, _ := unstructured.NestedInt64(h.Object, "spec", "maxReplicas")
	n.Summary = fmt.Sprintf("%d to %d copies", minR, maxR)
	n.Health = Good
	target := ID(kind, h.GetNamespace(), name)
	if _, ok := b.nodes[target]; !ok {
		n.Health = Bad
		n.Summary += " · target missing"
		return
	}
	b.edge(n.ID, target, "scales")
}

// configRefs links workloads to the ConfigMaps, Secrets and claims their pod
// templates use. References to objects that don't exist become red nodes.
func (b *builder) configRefs(configMaps, secrets map[string]bool, pvcs []unstructured.Unstructured) {
	claims := map[string]*unstructured.Unstructured{}
	for i := range pvcs {
		claims[pvcs[i].GetNamespace()+"/"+pvcs[i].GetName()] = &pvcs[i]
	}
	workloads := make([]*Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		if runsPods(n.Kind) {
			workloads = append(workloads, n)
		}
	}
	sort.Slice(workloads, func(i, j int) bool { return workloads[i].ID < workloads[j].ID })
	for _, w := range workloads {
		for _, ref := range w.refs {
			key := w.Namespace + "/" + ref.name
			switch ref.kind {
			case "ConfigMap":
				t := b.refNode(configMapsT, w.Namespace, ref.name, configMaps[key] || ref.optional)
				b.edge(w.ID, t.ID, "uses")
			case "Secret":
				t := b.refNode(secretsT, w.Namespace, ref.name, secrets[key] || ref.optional)
				b.edge(w.ID, t.ID, "uses")
			case "PersistentVolumeClaim":
				t := b.refNode(pvcsT, w.Namespace, ref.name, claims[key] != nil)
				if pvc := claims[key]; pvc != nil {
					phase, _, _ := unstructured.NestedString(pvc.Object, "status", "phase")
					size, _, _ := unstructured.NestedString(pvc.Object, "spec", "resources", "requests", "storage")
					t.Summary = strings.TrimSpace(size + " · " + phase)
					t.Health = map[bool]string{true: Good, false: Warn}[phase == "Bound"]
				}
				b.edge(w.ID, t.ID, "mounts")
			}
		}
	}
}

func (b *builder) refNode(t gvrKind, namespace, name string, exists bool) *Node {
	if !exists {
		return b.placeholder(t, namespace, name)
	}
	id := ID(t.kind, namespace, name)
	if n, ok := b.nodes[id]; ok {
		return n
	}
	n := &Node{ID: id, Kind: t.kind, Group: t.gvr.Group, Version: t.gvr.Version, Resource: t.gvr.Resource,
		Name: name, Namespace: namespace, Health: Good}
	b.nodes[id] = n
	return n
}

func controller(obj *unstructured.Unstructured) *metav1.OwnerReference {
	for _, o := range obj.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller {
			ref := o
			return &ref
		}
	}
	return nil
}
