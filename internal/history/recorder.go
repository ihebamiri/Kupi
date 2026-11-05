package history

import (
	"context"
	"crypto/sha256"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/yaml"

	"github.com/iheb-amiri/kupi/internal/diff"
	"github.com/iheb-amiri/kupi/internal/kube"
)

const (
	// maxSnapshot is the largest object whose text is kept for diffs.
	maxSnapshot = 256 << 10
	maxDiff     = 64 << 10
	maxBackoff  = 2 * time.Minute
	// eventRepeat is how often a repeating warning is written again.
	eventRepeat = time.Minute
)

// recordedType is a resource type the recorder watches.
type recordedType struct {
	Kind string
	GVR  schema.GroupVersionResource
	// MetadataOnly types never transfer their contents (Secrets).
	MetadataOnly bool
}

// Types are the resource types Kupi records. Pods are left out on purpose:
// they change constantly, and their failures show up as events.
var Types = []recordedType{
	{"Deployment", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, false},
	{"StatefulSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, false},
	{"DaemonSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, false},
	{"CronJob", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, false},
	{"Service", schema.GroupVersionResource{Version: "v1", Resource: "services"}, false},
	{"Ingress", schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, false},
	{"NetworkPolicy", schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, false},
	{"ConfigMap", schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, false},
	{"Secret", schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true},
	{"HorizontalPodAutoscaler", schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, false},
	{"PersistentVolumeClaim", schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, false},
}

var eventsGVR = schema.GroupVersionResource{Version: "v1", Resource: "events"}

// Status describes a context's recorder for the UI.
type Status struct {
	Recording bool   `json:"recording"`
	Since     string `json:"since"`
	// Namespace is "" when recording every namespace.
	Namespace string   `json:"namespace"`
	Skipped   []string `json:"skipped"`
}

// Recorder runs one watch set per connected context.
type Recorder struct {
	store *Store
	now   func() time.Time

	mu   sync.Mutex
	runs map[string]*run
}

type run struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	status Status
}

// NewRecorder returns a recorder writing to store.
func NewRecorder(store *Store) *Recorder {
	return &Recorder{store: store, now: time.Now, runs: map[string]*run{}}
}

// Start records a context. It watches every namespace where the user may,
// and falls back to namespace otherwise. Calling it again is a no-op.
func (rec *Recorder) Start(contextName string, r kube.Reader, namespace string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if _, ok := rec.runs[contextName]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ru := &run{cancel: cancel, status: Status{Recording: true, Since: rec.now().UTC().Format(time.RFC3339), Skipped: []string{}}}
	rec.runs[contextName] = ru

	// The permission check is a network call, so it runs outside the lock.
	ru.wg.Add(1)
	go func() {
		defer ru.wg.Done()
		scope := ""
		if ok, _, err := r.CanI(ctx, "watch", Types[0].GVR, "", "", ""); err != nil || !ok {
			scope = namespace
		}
		if ctx.Err() != nil {
			return
		}
		ru.mu.Lock()
		ru.status.Namespace = scope
		ru.mu.Unlock()
		for _, t := range Types {
			ru.wg.Add(1)
			go func() {
				defer ru.wg.Done()
				w := &typeWatch{rec: rec, run: ru, contextName: contextName, r: r, t: t, ns: scope}
				w.loop(ctx)
			}()
		}
		ru.wg.Add(1)
		go func() {
			defer ru.wg.Done()
			(&eventWatch{rec: rec, run: ru, contextName: contextName, r: r, ns: scope, seen: map[string]seenEvent{}}).loop(ctx)
		}()
	}()
}

// Stop ends recording for a context and waits for its watches to end.
func (rec *Recorder) Stop(contextName string) {
	rec.mu.Lock()
	ru := rec.runs[contextName]
	delete(rec.runs, contextName)
	rec.mu.Unlock()
	if ru != nil {
		ru.cancel()
		ru.wg.Wait()
	}
}

// StopAll ends every recording.
func (rec *Recorder) StopAll() {
	rec.mu.Lock()
	names := make([]string, 0, len(rec.runs))
	for n := range rec.runs {
		names = append(names, n)
	}
	rec.mu.Unlock()
	for _, n := range names {
		rec.Stop(n)
	}
}

// Status reports whether a context is being recorded.
func (rec *Recorder) Status(contextName string) Status {
	rec.mu.Lock()
	ru := rec.runs[contextName]
	rec.mu.Unlock()
	if ru == nil {
		return Status{Skipped: []string{}}
	}
	ru.mu.Lock()
	defer ru.mu.Unlock()
	st := ru.status
	st.Skipped = append([]string{}, st.Skipped...)
	sort.Strings(st.Skipped)
	return st
}

func (ru *run) skip(kind, why string) {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	ru.status.Skipped = append(ru.status.Skipped, kind+" ("+why+")")
}

// snapshot is the last version of an object the recorder saw.
type snapshot struct {
	text string // normalized YAML; empty when too large
	hash [32]byte
	// rv is kept for metadata-only types, whose value changes show only as
	// a new resourceVersion.
	rv string
}

type typeWatch struct {
	rec         *Recorder
	run         *run
	contextName string
	r           kube.Reader
	t           recordedType
	ns          string
	snaps       map[string]snapshot
}

func (w *typeWatch) loop(ctx context.Context) {
	backoff := time.Second
	rv := ""
	for ctx.Err() == nil {
		if rv == "" {
			var err error
			rv, err = w.list(ctx)
			if err != nil {
				if apierrors.IsForbidden(err) {
					w.run.skip(w.t.Kind, "no permission to watch")
					return
				}
				if apierrors.IsNotFound(err) {
					return // not served by this cluster
				}
				if !sleep(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, maxBackoff)
				continue
			}
		}
		opts := metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true}
		var wi watch.Interface
		var err error
		if w.t.MetadataOnly {
			wi, err = w.r.WatchMetadata(ctx, w.t.GVR, w.ns, opts)
		} else {
			wi, err = w.r.Watch(ctx, w.t.GVR, w.ns, opts)
		}
		if err != nil {
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				rv = ""
				continue
			}
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		rv = w.drain(ctx, wi, rv)
		wi.Stop()
	}
}

// list loads the current objects. The first list only sets the baseline;
// a relist after a broken watch records what changed in between.
func (w *typeWatch) list(ctx context.Context) (string, error) {
	var items []*unstructured.Unstructured
	var rv string
	if w.t.MetadataOnly {
		table, err := w.r.Table(ctx, w.t.GVR, true, w.ns, kube.TableOptions{})
		if err != nil {
			return "", err
		}
		rv = table.ResourceVersion
		for _, row := range table.Rows {
			if obj, err := rawToUnstructured(row.Object); err == nil {
				items = append(items, obj)
			}
		}
	} else {
		list, err := w.r.List(ctx, w.t.GVR, w.ns, metav1.ListOptions{})
		if err != nil {
			return "", err
		}
		rv = list.GetResourceVersion()
		for i := range list.Items {
			items = append(items, &list.Items[i])
		}
	}
	first := w.snaps == nil
	seen := map[string]bool{}
	if first {
		w.snaps = map[string]snapshot{}
	}
	for _, obj := range items {
		k := key(obj)
		seen[k] = true
		if first {
			w.snaps[k] = w.snapshotOf(obj)
		} else {
			w.observe(watch.Modified, obj)
		}
	}
	if !first {
		for k := range w.snaps {
			if !seen[k] {
				ns, name, _ := strings.Cut(k, "/")
				obj := &unstructured.Unstructured{}
				obj.SetNamespace(ns)
				obj.SetName(name)
				w.observe(watch.Deleted, obj)
			}
		}
	}
	return rv, nil
}

func (w *typeWatch) drain(ctx context.Context, wi watch.Interface, rv string) string {
	for {
		select {
		case <-ctx.Done():
			return rv
		case ev, ok := <-wi.ResultChan():
			if !ok {
				return rv
			}
			switch ev.Type {
			case watch.Error:
				if status := apierrors.FromObject(ev.Object); apierrors.IsResourceExpired(status) || apierrors.IsGone(status) {
					return ""
				}
				return rv
			case watch.Bookmark:
				if obj, err := toUnstructured(ev.Object); err == nil {
					rv = obj.GetResourceVersion()
				}
			default:
				obj, err := toUnstructured(ev.Object)
				if err != nil {
					continue
				}
				rv = obj.GetResourceVersion()
				w.observe(ev.Type, obj)
			}
		}
	}
}

// observe compares an object with its snapshot and records any change.
func (w *typeWatch) observe(kind watch.EventType, obj *unstructured.Unstructured) {
	k := key(obj)
	prev, known := w.snaps[k]
	c := Change{Time: w.rec.now().UTC(), Kind: w.t.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
	if kind == watch.Deleted {
		if !known {
			return
		}
		delete(w.snaps, k)
		c.Action = ActionDeleted
		c.Diff = capDiff(diff.Unified("before", "deleted", prev.text, "", 3))
		_ = w.rec.store.AddChange(w.contextName, c)
		return
	}
	next := w.snapshotOf(obj)
	w.snaps[k] = next
	c.Manager = lastManager(obj)
	switch {
	case !known:
		c.Action = ActionCreated
		c.Diff = capDiff(diff.Unified("before", "created", "", next.text, 3))
	case prev.hash == next.hash:
		// Only status or server-managed fields changed, except for
		// Secrets: their values aren't watched, so a new resourceVersion
		// with the same metadata means the values changed.
		if !w.t.MetadataOnly || kind != watch.Modified || prev.rv == next.rv || prev.rv == "" {
			return
		}
		c.Action = ActionChanged
		c.Note = "Values changed. Kupi doesn't record Secret values."
	default:
		c.Action = ActionChanged
		if prev.text == "" || next.text == "" {
			c.Note = "The object is too large to keep a diff."
		} else {
			c.Diff = capDiff(diff.Unified("before", "after", prev.text, next.text, 3))
		}
	}
	_ = w.rec.store.AddChange(w.contextName, c)
}

func (w *typeWatch) snapshotOf(obj *unstructured.Unstructured) snapshot {
	text := normalizedYAML(obj)
	s := snapshot{hash: sha256.Sum256([]byte(text))}
	if len(text) <= maxSnapshot {
		s.text = text
	}
	if w.t.MetadataOnly {
		s.rv = obj.GetResourceVersion()
	}
	return s
}

func key(obj *unstructured.Unstructured) string { return obj.GetNamespace() + "/" + obj.GetName() }

// noisyAnnotations change without anyone changing the object's intent.
var noisyAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	"deployment.kubernetes.io/revision",
	"control-plane.alpha.kubernetes.io/leader",
}

// normalizedYAML renders an object without status and server-managed fields.
func normalizedYAML(obj *unstructured.Unstructured) string {
	o := obj.DeepCopy()
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(o.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(o.Object, "status")
	if ann := o.GetAnnotations(); ann != nil {
		for _, a := range noisyAnnotations {
			delete(ann, a)
		}
		if len(ann) == 0 {
			unstructured.RemoveNestedField(o.Object, "metadata", "annotations")
		} else {
			o.SetAnnotations(ann)
		}
	}
	// Metadata-only objects can't carry values, but drop them defensively.
	if o.GetKind() == "Secret" || o.GetKind() == "PartialObjectMetadata" {
		delete(o.Object, "data")
		delete(o.Object, "stringData")
	}
	delete(o.Object, "apiVersion")
	delete(o.Object, "kind")
	b, err := yaml.Marshal(o.Object)
	if err != nil {
		return ""
	}
	return string(b)
}

// lastManager returns the field manager with the newest write to the
// object itself (status writes by controllers don't count).
func lastManager(obj *unstructured.Unstructured) string {
	best, bestTime := "", time.Time{}
	for _, mf := range obj.GetManagedFields() {
		if mf.Subresource != "" || mf.Time == nil {
			continue
		}
		if mf.Time.Time.After(bestTime) || (mf.Time.Time.Equal(bestTime) && mf.Manager > best) {
			best, bestTime = mf.Manager, mf.Time.Time
		}
	}
	return best
}

func capDiff(d string) string {
	if len(d) <= maxDiff {
		return d
	}
	cut := strings.LastIndexByte(d[:maxDiff], '\n')
	if cut < 0 {
		cut = maxDiff
	}
	return d[:cut] + "\n… (diff shortened)\n"
}

func toUnstructured(o runtime.Object) (*unstructured.Unstructured, error) {
	if u, ok := o.(*unstructured.Unstructured); ok {
		return u, nil
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: m}, nil
}

func rawToUnstructured(raw runtime.RawExtension) (*unstructured.Unstructured, error) {
	if raw.Object != nil {
		return toUnstructured(raw.Object)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(raw.Raw); err != nil {
		return nil, err
	}
	return u, nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

type seenEvent struct {
	count   int64
	written time.Time
}

type eventWatch struct {
	rec         *Recorder
	run         *run
	contextName string
	r           kube.Reader
	ns          string
	seen        map[string]seenEvent
}

func (w *eventWatch) loop(ctx context.Context) {
	backoff := time.Second
	opts := metav1.ListOptions{FieldSelector: "type=Warning"}
	rv := ""
	for ctx.Err() == nil {
		if rv == "" {
			list, err := w.r.List(ctx, eventsGVR, w.ns, opts)
			if err != nil {
				if apierrors.IsForbidden(err) {
					w.run.skip("Event", "no permission to watch")
					return
				}
				if !sleep(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			rv = list.GetResourceVersion()
			for i := range list.Items {
				w.observe(&list.Items[i])
			}
		}
		o := opts
		o.ResourceVersion, o.AllowWatchBookmarks = rv, true
		wi, err := w.r.Watch(ctx, eventsGVR, w.ns, o)
		if err != nil {
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				rv = ""
				continue
			}
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		rv = w.drain(ctx, wi, rv)
		wi.Stop()
	}
}

func (w *eventWatch) drain(ctx context.Context, wi watch.Interface, rv string) string {
	prune := time.NewTicker(10 * time.Minute)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return rv
		case <-prune.C:
			cutoff := w.rec.now().Add(-2 * time.Hour)
			for uid, s := range w.seen {
				if s.written.Before(cutoff) {
					delete(w.seen, uid)
				}
			}
		case ev, ok := <-wi.ResultChan():
			if !ok {
				return rv
			}
			obj, err := toUnstructured(ev.Object)
			if err != nil {
				continue
			}
			switch ev.Type {
			case watch.Error:
				if status := apierrors.FromObject(ev.Object); apierrors.IsResourceExpired(status) || apierrors.IsGone(status) {
					return ""
				}
				return rv
			case watch.Bookmark:
				rv = obj.GetResourceVersion()
			case watch.Added, watch.Modified:
				rv = obj.GetResourceVersion()
				w.observe(obj)
			}
		}
	}
}

// observe writes a warning the first time it is seen, and again when it
// repeats, at most once a minute.
func (w *eventWatch) observe(obj *unstructured.Unstructured) {
	e := EventFrom(obj)
	if e.Time.IsZero() {
		e.Time = w.rec.now().UTC()
	}
	prev, ok := w.seen[e.UID]
	if ok && (e.Count <= prev.count || w.rec.now().Sub(prev.written) < eventRepeat) {
		return
	}
	w.seen[e.UID] = seenEvent{count: e.Count, written: w.rec.now()}
	_ = w.rec.store.AddEvent(w.contextName, e)
}

// EventFrom reads a core/v1 Event.
func EventFrom(obj *unstructured.Unstructured) Event {
	str := func(path ...string) string {
		v, _, _ := unstructured.NestedString(obj.Object, path...)
		return v
	}
	count, _, _ := unstructured.NestedInt64(obj.Object, "count")
	if series, ok, _ := unstructured.NestedInt64(obj.Object, "series", "count"); ok && series > count {
		count = series
	}
	if count == 0 {
		count = 1
	}
	var t time.Time
	for _, f := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		if ts, err := time.Parse(time.RFC3339Nano, str(f)); err == nil && !ts.IsZero() {
			t = ts.UTC()
			break
		}
	}
	if ts, err := time.Parse(time.RFC3339Nano, str("series", "lastObservedTime")); err == nil && ts.After(t) {
		t = ts.UTC()
	}
	ns := str("involvedObject", "namespace")
	if ns == "" {
		ns = obj.GetNamespace()
	}
	return Event{
		Time: t, UID: string(obj.GetUID()), Namespace: ns,
		Kind: str("involvedObject", "kind"), Name: str("involvedObject", "name"),
		Reason: str("reason"), Message: str("message"), Count: count,
	}
}
