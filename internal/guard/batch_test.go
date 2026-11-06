package guard

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/profile"
)

var widgetsGVR = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}

func obj(apiVersion, kind, ns, name string) *unstructured.Unstructured {
	meta := map[string]interface{}{"name": name}
	if ns != "" {
		meta["namespace"] = ns
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": apiVersion, "kind": kind, "metadata": meta}}
}

func appliedNames(f *fakeCluster) []string {
	var out []string
	for _, o := range f.applied {
		out = append(out, o.GetKind()+"/"+o.GetName())
	}
	return out
}

func TestBatchAppliesInDependencyOrder(t *testing.T) {
	f := newFake(profile.Local)
	f.put(configMaps, configMap("existing", map[string]interface{}{"a": "1"}))
	g, log := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		obj("apps/v1", "Deployment", "default", "web"),
		configMap("existing", map[string]interface{}{"a": "2"}),
		configMap("fresh", map[string]interface{}{"b": "1"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("plan not allowed: %s", plan.DeniedReason)
	}
	var states []string
	for _, it := range plan.Items {
		states = append(states, it.Kind+"/"+it.Name+"="+it.State)
	}
	want := "ConfigMap/existing=update ConfigMap/fresh=create Deployment/web=create"
	if got := strings.Join(states, " "); got != want {
		t.Errorf("items = %s, want %s", got, want)
	}
	if plan.Resource() != "3 objects" || len(f.applied) != 0 {
		t.Fatalf("resource %q, applied %d during prepare", plan.Resource(), len(f.applied))
	}

	res, err := g.Commit(ctx, plan.ID, Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(appliedNames(f), " "); got != "ConfigMap/existing ConfigMap/fresh Deployment/web" {
		t.Errorf("applied in order %s", got)
	}
	if f.lastApplyRV != "" {
		t.Errorf("new Deployment applied with resourceVersion %q", f.lastApplyRV)
	}
	if f.applied[0].GetResourceVersion() != "41" {
		t.Errorf("update of an existing object must carry its resourceVersion precondition")
	}
	entries, _ := log.Recent(10)
	if len(entries) != 3 || entries[0].Seq != res.AuditSeq {
		t.Fatalf("want one audit entry per object, got %+v", entries)
	}
	if entries[2].Resource != "ConfigMap default/existing" || entries[2].DiffSHA256 == "" {
		t.Errorf("first audit entry = %+v", entries[2])
	}
}

func TestBatchDefersObjectsInANewNamespace(t *testing.T) {
	f := newFake(profile.Local)
	f.strictNamespaces = true
	f.put(namespacesGVR, obj("v1", "Namespace", "", "default"))
	g, _ := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		obj("v1", "ConfigMap", "shop", "settings"),
		obj("v1", "Namespace", "", "shop"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("plan not allowed: %s", plan.DeniedReason)
	}
	if plan.Items[0].Kind != "Namespace" || plan.Items[1].State != ItemDeferred || !strings.Contains(plan.Items[1].Note, "shop") {
		t.Fatalf("items = %+v", plan.Items)
	}
	if len(plan.Warnings) == 0 {
		t.Error("a deferred object should come with a warning")
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(appliedNames(f), " "); got != "Namespace/shop ConfigMap/settings" {
		t.Errorf("applied %s", got)
	}
}

func TestBatchWaitsForACustomTypeItDefines(t *testing.T) {
	f := newFake(profile.Local)
	g, _ := newGuard(t, f)
	ctx := context.Background()

	crd := obj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "widgets.example.com")
	crd.Object["spec"] = map[string]interface{}{"group": "example.com", "scope": "Namespaced", "names": map[string]interface{}{"kind": "Widget", "plural": "widgets"}}
	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		obj("example.com/v1", "Widget", "", "blue"),
		crd,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("plan not allowed: %s", plan.DeniedReason)
	}
	w := plan.Items[1]
	if w.Kind != "Widget" || w.State != ItemDeferred || w.Namespace != "default" {
		t.Fatalf("widget item = %+v", w)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.objects[key(widgetsGVR, "default", "blue")]; !ok {
		t.Error("the widget was not applied after its CRD")
	}
}

func TestBatchBlocksUnknownTypesAndDuplicates(t *testing.T) {
	f := newFake(profile.Local)
	g, _ := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		configMap("one", nil), obj("example.com/v1", "Widget", "default", "x"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "Widget") || plan.Items[1].State != ItemDenied {
		t.Errorf("unknown type: allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}

	plan, _ = g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		configMap("one", nil), configMap("one", map[string]interface{}{"a": "1"}),
	}})
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "more than once") {
		t.Errorf("duplicate: allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err == nil {
		t.Error("a blocked plan must not commit")
	}
	if len(f.applied) != 0 {
		t.Errorf("applied %v", appliedNames(f))
	}
}

func TestBatchRespectsPermissionsAndProfiles(t *testing.T) {
	f := newFake(profile.Production)
	f.denyVerbs["create"] = true
	g, _ := newGuard(t, f)
	ctx := context.Background()
	if err := g.SetEditing("test", true, "incident 42"); err != nil {
		t.Fatal(err)
	}
	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{configMap("a", nil), configMap("b", nil)}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "can't create") {
		t.Errorf("allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}
	if !plan.NeedsReason {
		t.Error("production plans need a reason")
	}
}

func TestBatchStopsAtTheFirstFailure(t *testing.T) {
	f := newFake(profile.Local)
	f.failName = "b"
	g, log := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApplyBatch(ctx, ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		configMap("a", nil), configMap("b", nil), configMap("c", nil),
	}})
	if err != nil || !plan.Allowed {
		t.Fatalf("prepare: %v %s", err, plan.DeniedReason)
	}
	_, err = g.Commit(ctx, plan.ID, Confirmation{})
	if err == nil || !strings.Contains(err.Error(), "applied 1 of 3") {
		t.Fatalf("commit error = %v", err)
	}
	if got := strings.Join(appliedNames(f), " "); got != "ConfigMap/a" {
		t.Errorf("applied %s", got)
	}
	entries, _ := log.Recent(10)
	if len(entries) != 2 || entries[0].Outcome != audit.OutcomeFailed || entries[1].Outcome != audit.OutcomeApplied {
		t.Errorf("audit = %+v", entries)
	}
}

func TestBatchWithNothingToChange(t *testing.T) {
	f := newFake(profile.Local)
	f.put(configMaps, configMap("same", map[string]interface{}{"a": "1"}))
	g, _ := newGuard(t, f)
	plan, err := g.PrepareApplyBatch(context.Background(), ApplyBatchRequest{Context: "test", Objects: []*unstructured.Unstructured{
		configMap("same", map[string]interface{}{"a": "1"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || plan.Items[0].State != ItemUnchanged {
		t.Errorf("allowed=%v items=%+v", plan.Allowed, plan.Items)
	}
}
