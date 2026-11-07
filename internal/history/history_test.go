package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/ihebamiri/kupi/internal/kube"
)

func deploy(image string, replicas int64, manager string, rv string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]interface{}{"name": "api", "namespace": "shop", "resourceVersion": rv},
		"spec": map[string]interface{}{"replicas": replicas, "template": map[string]interface{}{"spec": map[string]interface{}{
			"containers": []interface{}{map[string]interface{}{"name": "api", "image": image}},
		}}},
		"status": map[string]interface{}{"readyReplicas": int64(1)},
	}}
	now := metav1.NewTime(time.Now())
	u.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: "kube-controller-manager", Operation: metav1.ManagedFieldsOperationUpdate, Subresource: "status", Time: &now},
		{Manager: manager, Operation: metav1.ManagedFieldsOperationUpdate, Time: &now},
	})
	return u
}

func TestObserveRecordsOnlyRealChanges(t *testing.T) {
	store := Open(t.TempDir())
	defer store.Close()
	rec := NewRecorder(store)
	w := &typeWatch{rec: rec, run: &run{}, contextName: "c", t: Types[0], snaps: map[string]snapshot{}}

	w.snaps["shop/api"] = w.snapshotOf(deploy("api:1", 2, "kubectl-client-side-apply", "1"))
	status := deploy("api:1", 2, "kubectl-client-side-apply", "2")
	unstructured.SetNestedField(status.Object, int64(2), "status", "readyReplicas")
	w.observe(watch.Modified, status)
	w.observe(watch.Modified, deploy("api:2", 2, "kubectl-edit", "3"))
	w.observe(watch.Deleted, deploy("api:2", 2, "", "4"))

	changes, err := store.Changes("c", Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	edit := changes[1]
	if edit.Action != ActionChanged || edit.Manager != "kubectl-edit" || !strings.Contains(edit.Diff, "+      - image: api:2") {
		t.Errorf("edit = %+v\n%s", edit, edit.Diff)
	}
	if strings.Contains(edit.Diff, "readyReplicas") || strings.Contains(edit.Diff, "managedFields") {
		t.Errorf("diff has status or server fields:\n%s", edit.Diff)
	}
	if changes[0].Action != ActionDeleted {
		t.Errorf("newest = %+v", changes[0])
	}
}

// kubectl scale and autoscalers write through the scale subresource, whose
// managedFields entry has no time (as seen on k3s 1.35).
func TestScaleWritesAreCreditedToTheScaler(t *testing.T) {
	store := Open(t.TempDir())
	defer store.Close()
	w := &typeWatch{rec: NewRecorder(store), run: &run{}, contextName: "c", t: Types[0], snaps: map[string]snapshot{}}

	applied := metav1.NewTime(time.Date(2026, 11, 7, 18, 15, 9, 0, time.UTC))
	obj := func(image string, replicas int64, rv string, entries ...metav1.ManagedFieldsEntry) *unstructured.Unstructured {
		u := deploy(image, replicas, "", rv)
		u.SetManagedFields(append([]metav1.ManagedFieldsEntry{
			{Manager: "k3s", Operation: metav1.ManagedFieldsOperationUpdate, Subresource: "status", Time: &applied},
		}, entries...))
		return u
	}
	apply := metav1.ManagedFieldsEntry{Manager: "kubectl-client-side-apply", Operation: metav1.ManagedFieldsOperationUpdate, Time: &applied}
	scale := metav1.ManagedFieldsEntry{Manager: "kubectl.exe", Operation: metav1.ManagedFieldsOperationUpdate, Subresource: "scale"}

	w.snaps["shop/api"] = w.snapshotOf(obj("api:1", 2, "1", apply))
	w.observe(watch.Modified, obj("api:1", 3, "2", scale, apply))
	later := metav1.NewTime(applied.Add(time.Minute))
	reapply := apply
	reapply.Time = &later
	w.observe(watch.Modified, obj("api:2", 3, "3", scale, reapply))

	changes, err := store.Changes("c", Query{})
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	if changes[1].Manager != "kubectl.exe" {
		t.Errorf("scale credited to %q", changes[1].Manager)
	}
	if changes[0].Manager != "kubectl-client-side-apply" {
		t.Errorf("later apply credited to %q", changes[0].Manager)
	}
}

func TestSecretValueChangesAreNotedWithoutValues(t *testing.T) {
	store := Open(t.TempDir())
	defer store.Close()
	w := &typeWatch{rec: NewRecorder(store), run: &run{}, contextName: "c", t: Types[8], snaps: map[string]snapshot{}}
	if w.t.Kind != "Secret" || !w.t.MetadataOnly {
		t.Fatalf("Types[8] = %+v", w.t)
	}
	meta := func(rv string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetName("db")
		u.SetNamespace("shop")
		u.SetResourceVersion(rv)
		u.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"pw":"aHVudGVyMg=="}}`})
		return u
	}
	w.snaps["shop/db"] = w.snapshotOf(meta("10"))
	w.observe(watch.Modified, meta("10")) // relist, nothing new
	w.observe(watch.Modified, meta("11"))
	changes, _ := store.Changes("c", Query{})
	if len(changes) != 1 || !strings.Contains(changes[0].Note, "Values changed") {
		t.Fatalf("changes = %+v", changes)
	}
	raw, _ := os.ReadFile(filepath.Join(store.contextDir("c"), dayOf(time.Now())+".jsonl"))
	if strings.Contains(string(raw), "aHVudGVyMg") {
		t.Error("history file holds a Secret value from the annotation")
	}
}

func TestStoreQueriesAndPrune(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	now := time.Now().UTC()
	_ = s.AddChange("c", Change{Time: now.Add(-40 * time.Minute), Kind: "ConfigMap", Namespace: "a", Name: "x", Action: ActionChanged})
	_ = s.AddChange("c", Change{Time: now.Add(-5 * time.Minute), Kind: "Deployment", Namespace: "a", Name: "api", Action: ActionChanged})
	_ = s.AddChange("other", Change{Time: now, Kind: "Deployment", Namespace: "a", Name: "api"})
	_ = s.AddEvent("c", Event{Time: now.Add(-3 * time.Minute), UID: "u1", Reason: "BackOff", Count: 3})
	_ = s.AddEvent("c", Event{Time: now.Add(-1 * time.Minute), UID: "u1", Reason: "BackOff", Count: 7})

	recent, _ := s.Changes("c", Query{Since: now.Add(-30 * time.Minute)})
	if len(recent) != 1 || recent[0].Name != "api" {
		t.Errorf("recent = %+v", recent)
	}
	byKind, _ := s.Changes("c", Query{Kind: "configmap"})
	if len(byKind) != 1 {
		t.Errorf("by kind = %+v", byKind)
	}
	events, _ := s.Events("c", Query{})
	if len(events) != 1 || events[0].Count != 7 {
		t.Errorf("events = %+v", events)
	}
	s.Close()

	old := filepath.Join(s.contextDir("c"), dayOf(now.Add(-20*24*time.Hour))+".jsonl")
	_ = os.WriteFile(old, []byte("{}\n"), 0o600)
	Open(dir).Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old history file not pruned")
	}
}

// fakeReader serves one list and then the events pushed into its watcher.
type fakeReader struct {
	kube.Reader
	mu       sync.Mutex
	watchers map[string]*watch.FakeWatcher
}

func (f *fakeReader) CanI(context.Context, string, schema.GroupVersionResource, string, string, string) (bool, string, error) {
	return true, "", nil
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	l := &unstructured.UnstructuredList{}
	l.SetResourceVersion("1")
	if gvr.Resource == "deployments" {
		l.Items = []unstructured.Unstructured{*deploy("api:1", 1, "helm", "1")}
	}
	return l, nil
}

func (f *fakeReader) Table(context.Context, schema.GroupVersionResource, bool, string, kube.TableOptions) (*metav1.Table, error) {
	return &metav1.Table{ListMeta: metav1.ListMeta{ResourceVersion: "1"}}, nil
}

func (f *fakeReader) watcher(resource string) *watch.FakeWatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watchers[resource] == nil {
		f.watchers[resource] = watch.NewFakeWithChanSize(10, false)
	}
	return f.watchers[resource]
}

func (f *fakeReader) Watch(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (watch.Interface, error) {
	return f.watcher(gvr.Resource), nil
}

func (f *fakeReader) WatchMetadata(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (watch.Interface, error) {
	return f.watcher(gvr.Resource), nil
}

func TestRecorderEndToEnd(t *testing.T) {
	store := Open(t.TempDir())
	defer store.Close()
	rec := NewRecorder(store)
	r := &fakeReader{watchers: map[string]*watch.FakeWatcher{}}
	rec.Start("c", r, "default")
	r.watcher("deployments").Modify(deploy("api:2", 1, "helm", "2"))
	r.watcher("events").Add(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Event", "type": "Warning", "reason": "BackOff", "message": "Back-off restarting failed container",
		"metadata":       map[string]interface{}{"name": "e1", "namespace": "shop", "uid": "e1", "resourceVersion": "5"},
		"involvedObject": map[string]interface{}{"kind": "Pod", "name": "api-1", "namespace": "shop"},
		"count":          int64(4), "lastTimestamp": time.Now().UTC().Format(time.RFC3339),
	}})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, _ := store.Changes("c", Query{})
		e, _ := store.Events("c", Query{})
		if len(c) == 1 && len(e) == 1 {
			if c[0].Manager != "helm" || e[0].Reason != "BackOff" || e[0].Name != "api-1" {
				t.Errorf("change %+v event %+v", c[0], e[0])
			}
			st := rec.Status("c")
			if !st.Recording || st.Namespace != "" {
				t.Errorf("status = %+v", st)
			}
			rec.StopAll()
			if rec.Status("c").Recording {
				t.Error("still recording after StopAll")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec.StopAll()
	t.Fatal("recorder didn't record the change and the event")
}
