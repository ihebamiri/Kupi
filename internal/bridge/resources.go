package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/iheb-amiri/kupi/internal/catalog"
	"github.com/iheb-amiri/kupi/internal/guard"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/live"
	"github.com/iheb-amiri/kupi/internal/redact"
	"github.com/iheb-amiri/kupi/internal/validate"
)

var (
	uidRe      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	streamIDRe = regexp.MustCompile(`^[0-9a-f]{24}$`)
	podsGVR    = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	eventsGVR  = schema.GroupVersionResource{Version: "v1", Resource: "events"}
	nsGVR      = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
)

const (
	maxLogTail     = 10000
	defaultLogTail = 500
	maxLogPods     = 20
)

// TypeRef names a resource type.
type TypeRef struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
}

// ResourceTypeView is a resource type for the sidebar.
type ResourceTypeView struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames"`
	Category   string   `json:"category"`
	Label      string   `json:"label"`
}

// TableColumn is one column of a server-side table.
type TableColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Priority    int32  `json:"priority"`
}

// TableRow is one object in a table.
type TableRow struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Created   string            `json:"created"`
	Labels    map[string]string `json:"labels"`
	OwnerKind string            `json:"ownerKind"`
	OwnerName string            `json:"ownerName"`
	Cells     []string          `json:"cells"`
}

// TableView is a list of objects with the server's columns.
type TableView struct {
	Columns         []TableColumn `json:"columns"`
	Rows            []TableRow    `json:"rows"`
	ResourceVersion string        `json:"resourceVersion"`
	Truncated       bool          `json:"truncated"`
}

// EventView is a Kubernetes event about an object.
type EventView struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int64  `json:"count"`
	LastSeen string `json:"lastSeen"`
	Source   string `json:"source"`
}

// ContainerView describes a container of a pod.
type ContainerView struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Init         bool   `json:"init"`
	Ready        bool   `json:"ready"`
	RestartCount int64  `json:"restartCount"`
	State        string `json:"state"`
}

// ResourceService lists and reads cluster objects.
type ResourceService struct{ d Deps }

// NewResourceService returns the resource service.
func NewResourceService(d Deps) *ResourceService { return &ResourceService{d: d} }

func (d Deps) session(contextName string) (*kube.Session, error) {
	if err := validate.ContextName(contextName); err != nil {
		return nil, err
	}
	s, ok := d.Kube.Session(contextName)
	if !ok {
		return nil, guard.ErrNotConnected
	}
	return s, nil
}

func checkType(t TypeRef) (schema.GroupVersionResource, error) {
	if err := validate.GroupVersionResource(t.Group, t.Version, t.Resource); err != nil {
		return schema.GroupVersionResource{}, err
	}
	return schema.GroupVersionResource{Group: t.Group, Version: t.Version, Resource: t.Resource}, nil
}

// Types lists the cluster's resource types, grouped for the sidebar.
func (s *ResourceService) Types(ctx context.Context, contextName string) ([]ResourceTypeView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	types, err := sess.Reader().ResourceTypes(ctx)
	if err != nil {
		return nil, err
	}
	entries := catalog.Build(types)
	out := make([]ResourceTypeView, 0, len(entries))
	for _, e := range entries {
		out = append(out, ResourceTypeView{
			Group: e.Group, Version: e.Version, Resource: e.Resource, Kind: e.Kind, Namespaced: e.Namespaced,
			Verbs: e.Verbs, ShortNames: e.ShortNames, Category: string(e.Category), Label: e.Label,
		})
	}
	return out, nil
}

// List returns objects of one type with the columns kubectl get shows.
func (s *ResourceService) List(ctx context.Context, contextName string, t TypeRef, namespace, labelSelector string) (TableView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return TableView{}, err
	}
	gvr, err := checkType(t)
	if err != nil {
		return TableView{}, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return TableView{}, err
	}
	if labelSelector != "" {
		if _, err := labels.Parse(labelSelector); err != nil {
			return TableView{}, fmt.Errorf("invalid label selector: %w", err)
		}
	}
	table, err := sess.Reader().Table(ctx, gvr, t.Namespaced, namespace, kube.TableOptions{LabelSelector: labelSelector})
	if err != nil {
		return TableView{}, err
	}
	return tableView(table), nil
}

func tableView(t *metav1.Table) TableView {
	out := TableView{ResourceVersion: t.ResourceVersion, Truncated: t.Continue != ""}
	for _, c := range t.ColumnDefinitions {
		out.Columns = append(out.Columns, TableColumn{Name: c.Name, Type: c.Type, Description: c.Description, Priority: c.Priority})
	}
	for _, r := range t.Rows {
		row := TableRow{Cells: make([]string, len(r.Cells))}
		for i, c := range r.Cells {
			row.Cells[i] = cellString(c)
		}
		var m metav1.PartialObjectMetadata
		if len(r.Object.Raw) > 0 && json.Unmarshal(r.Object.Raw, &m) == nil {
			row.Name, row.Namespace, row.UID = m.Name, m.Namespace, string(m.UID)
			row.Labels = m.Labels
			if !m.CreationTimestamp.IsZero() {
				row.Created = m.CreationTimestamp.UTC().Format(time.RFC3339)
			}
			if owner := metav1.GetControllerOfNoCopy(&m); owner != nil {
				row.OwnerKind, row.OwnerName = owner.Kind, owner.Name
			}
		}
		out.Rows = append(out.Rows, row)
	}
	if out.Columns == nil {
		out.Columns = []TableColumn{}
	}
	if out.Rows == nil {
		out.Rows = []TableRow{}
	}
	return out
}

func cellString(c interface{}) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

// GetYAML returns one object as YAML, with Secret values hidden.
func (s *ResourceService) GetYAML(ctx context.Context, contextName string, ref ResourceRef) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return "", err
	}
	obj, err := sess.Reader().Get(ctx, req.GVR, ref.Namespace, ref.Name)
	if err != nil {
		return "", err
	}
	safe := redact.Object(obj)
	unstructured.RemoveNestedField(safe.Object, "metadata", "managedFields")
	data, err := yaml.Marshal(safe.Object)
	if err != nil {
		return "", fmt.Errorf("render YAML: %w", err)
	}
	return string(data), nil
}

// Events lists the events about one object, newest first.
func (s *ResourceService) Events(ctx context.Context, contextName, namespace, uid string) ([]EventView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return nil, err
	}
	if !uidRe.MatchString(uid) {
		return nil, fmt.Errorf("invalid object id")
	}
	list, err := sess.Reader().List(ctx, eventsGVR, namespace, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + uid})
	if err != nil {
		return nil, err
	}
	out := make([]EventView, 0, len(list.Items))
	for _, e := range list.Items {
		out = append(out, eventView(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen > out[j].LastSeen })
	return out, nil
}

func eventView(e unstructured.Unstructured) EventView {
	str := func(path ...string) string { v, _, _ := unstructured.NestedString(e.Object, path...); return v }
	count, _, _ := unstructured.NestedInt64(e.Object, "count")
	last := str("lastTimestamp")
	if last == "" {
		last = str("eventTime")
	}
	if last == "" {
		last = e.GetCreationTimestamp().UTC().Format(time.RFC3339)
	}
	source := str("source", "component")
	if source == "" {
		source = str("reportingComponent")
	}
	return EventView{Type: str("type"), Reason: str("reason"), Message: str("message"), Count: max(count, 1), LastSeen: last, Source: source}
}

// Namespaces lists namespace names. If the user may not list namespaces it
// returns an empty list, and the UI falls back to the context's namespace.
func (s *ResourceService) Namespaces(ctx context.Context, contextName string) ([]string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	list, err := sess.Reader().List(ctx, nsGVR, "", metav1.ListOptions{})
	if err != nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(list.Items))
	for _, n := range list.Items {
		out = append(out, n.GetName())
	}
	sort.Strings(out)
	return out, nil
}

// Containers lists a pod's containers with their status.
func (s *ResourceService) Containers(ctx context.Context, contextName, namespace, pod string) ([]ContainerView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := validate.Namespace(namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(pod); err != nil {
		return nil, err
	}
	obj, err := sess.Reader().Get(ctx, podsGVR, namespace, pod)
	if err != nil {
		return nil, err
	}
	return containerViews(obj), nil
}

func containerViews(pod *unstructured.Unstructured) []ContainerView {
	statuses := map[string]map[string]interface{}{}
	for _, field := range []string{"containerStatuses", "initContainerStatuses"} {
		list, _, _ := unstructured.NestedSlice(pod.Object, "status", field)
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok {
					statuses[name] = m
				}
			}
		}
	}
	var out []ContainerView
	for _, spec := range []struct {
		field string
		init  bool
	}{{"initContainers", true}, {"containers", false}} {
		list, _, _ := unstructured.NestedSlice(pod.Object, "spec", spec.field)
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			c := ContainerView{Init: spec.init}
			c.Name, _ = m["name"].(string)
			c.Image, _ = m["image"].(string)
			if st, ok := statuses[c.Name]; ok {
				c.Ready, _ = st["ready"].(bool)
				c.RestartCount, _, _ = unstructured.NestedInt64(st, "restartCount")
				if state, ok := st["state"].(map[string]interface{}); ok {
					for k := range state {
						c.State = k
					}
				}
			}
			out = append(out, c)
		}
	}
	return out
}

// Watch tells the UI (event "kupi:changed") when a list changes.
func (s *ResourceService) Watch(contextName string, t TypeRef, namespace, resourceVersion string) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	gvr, err := checkType(t)
	if err != nil {
		return "", err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return "", err
	}
	if len(resourceVersion) > 64 {
		return "", fmt.Errorf("invalid resource version")
	}
	if !t.Namespaced {
		namespace = ""
	}
	return s.d.Live.Watch(contextName, sess.Reader(), gvr, namespace, resourceVersion), nil
}

// Unwatch stops a watch.
func (s *ResourceService) Unwatch(id string) {
	if streamIDRe.MatchString(id) {
		s.d.Live.Stop(id)
	}
}

// LogRequest selects logs to stream. Set Pod for one pod, or Workload for
// every pod of a Deployment, StatefulSet, DaemonSet, ReplicaSet or Job.
type LogRequest struct {
	Namespace  string       `json:"namespace"`
	Pod        string       `json:"pod"`
	Workload   *ResourceRef `json:"workload"`
	Container  string       `json:"container"`
	Follow     bool         `json:"follow"`
	Previous   bool         `json:"previous"`
	Timestamps bool         `json:"timestamps"`
	TailLines  int64        `json:"tailLines"`
}

// LogService streams container logs (event "kupi:logs").
type LogService struct{ d Deps }

// NewLogService returns the log service.
func NewLogService(d Deps) *LogService { return &LogService{d: d} }

// Start begins streaming and returns the stream's id.
func (s *LogService) Start(ctx context.Context, contextName string, req LogRequest) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	if err := validate.Namespace(req.Namespace); err != nil {
		return "", err
	}
	if req.Container != "" {
		if err := validate.Name(req.Container); err != nil {
			return "", err
		}
	}
	tail := req.TailLines
	if tail <= 0 {
		tail = defaultLogTail
	}
	tail = min(tail, maxLogTail)
	reader := sess.Reader()

	var pods []*unstructured.Unstructured
	switch {
	case req.Workload != nil:
		ref := *req.Workload
		ref.Namespace = req.Namespace
		target, err := targetRequest(contextName, ref)
		if err != nil {
			return "", err
		}
		workload, err := reader.Get(ctx, target.GVR, req.Namespace, ref.Name)
		if err != nil {
			return "", err
		}
		selector, err := workloadSelector(workload)
		if err != nil {
			return "", err
		}
		list, err := reader.List(ctx, podsGVR, req.Namespace, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			if len(pods) == maxLogPods {
				break
			}
			pods = append(pods, &list.Items[i])
		}
		if len(pods) == 0 {
			return "", fmt.Errorf("%s has no pods right now", ref.Name)
		}
	default:
		if err := validate.Name(req.Pod); err != nil {
			return "", err
		}
		pod, err := reader.Get(ctx, podsGVR, req.Namespace, req.Pod)
		if err != nil {
			return "", err
		}
		pods = []*unstructured.Unstructured{pod}
	}

	var targets []live.LogTarget
	for _, p := range pods {
		container := req.Container
		if container == "" {
			if cs := containerViews(p); len(cs) > 0 {
				for _, c := range cs {
					if !c.Init {
						container = c.Name
						break
					}
				}
			}
		}
		targets = append(targets, live.LogTarget{Namespace: req.Namespace, Pod: p.GetName(), Container: container})
	}
	opts := kube.LogOptions{Follow: req.Follow, Previous: req.Previous, Timestamps: req.Timestamps, TailLines: tail}
	return s.d.Live.Logs(contextName, reader, targets, opts), nil
}

// Stop ends a log stream.
func (s *LogService) Stop(id string) {
	if streamIDRe.MatchString(id) {
		s.d.Live.Stop(id)
	}
}

func workloadSelector(obj *unstructured.Unstructured) (string, error) {
	raw, found, err := unstructured.NestedMap(obj.Object, "spec", "selector")
	if err != nil || !found {
		return "", fmt.Errorf("%s %s has no pod selector", obj.GetKind(), obj.GetName())
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	var ls metav1.LabelSelector
	if err := json.Unmarshal(data, &ls); err != nil {
		return "", fmt.Errorf("read pod selector: %w", err)
	}
	sel, err := metav1.LabelSelectorAsSelector(&ls)
	if err != nil {
		return "", fmt.Errorf("read pod selector: %w", err)
	}
	return sel.String(), nil
}

// ShellService opens interactive shells in containers (event "kupi:shell").
type ShellService struct{ d Deps }

// NewShellService returns the shell service.
func NewShellService(d Deps) *ShellService { return &ShellService{d: d} }

// Prepare checks that a shell can be opened and what confirmation it needs.
func (s *ShellService) Prepare(ctx context.Context, contextName, namespace, pod, container string) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if container != "" {
		if err := validate.Name(container); err != nil {
			return PlanView{}, err
		}
	}
	plan, err := s.d.Guard.PrepareShell(ctx, guard.ShellRequest{Context: contextName, Namespace: namespace, Pod: pod, Container: container})
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// Open starts a prepared shell and returns the session id.
func (s *ShellService) Open(planID, reason string, cols, rows int) (string, error) {
	if !planIDRe.MatchString(planID) {
		return "", fmt.Errorf("invalid plan id")
	}
	contextName, run, err := s.d.Guard.OpenShell(planID, guard.Confirmation{Reason: reason})
	if err != nil {
		return "", err
	}
	return s.d.Live.Shell(contextName, run, termSize(cols), termSize(rows)), nil
}

// Input sends base64-encoded keystrokes to a session.
func (s *ShellService) Input(id, data string) error {
	if !streamIDRe.MatchString(id) {
		return fmt.Errorf("invalid session id")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("invalid input encoding")
	}
	return s.d.Live.ShellInput(id, raw)
}

// Resize tells a session the terminal's size.
func (s *ShellService) Resize(id string, cols, rows int) {
	if streamIDRe.MatchString(id) {
		s.d.Live.ShellResize(id, termSize(cols), termSize(rows))
	}
}

// Close ends a session.
func (s *ShellService) Close(id string) {
	if streamIDRe.MatchString(id) {
		s.d.Live.Stop(id)
	}
}

func termSize(n int) uint16 {
	return uint16(max(1, min(n, 1000)))
}
