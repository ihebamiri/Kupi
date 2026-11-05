package guard

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/profile"
)

var (
	configMaps  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	secrets     = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	deployments = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// fakeCluster implements Session, kube.Reader and kube.Mutator in memory.
type fakeCluster struct {
	kind        profile.Kind
	objects     map[string]*unstructured.Unstructured
	denyVerbs   map[string]bool
	conflict    bool
	applied     []*unstructured.Unstructured
	deleted     []string
	deletedRV   []string
	patched     [][]byte
	lastApplyRV string
}

func newFake(kind profile.Kind) *fakeCluster {
	return &fakeCluster{kind: kind, objects: map[string]*unstructured.Unstructured{}, denyVerbs: map[string]bool{}}
}

func key(gvr schema.GroupVersionResource, ns, name string) string {
	return gvr.String() + "|" + ns + "|" + name
}

func (f *fakeCluster) put(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	obj.SetResourceVersion("41")
	f.objects[key(gvr, obj.GetNamespace(), obj.GetName())] = obj
}

func (f *fakeCluster) ContextName() string      { return "test" }
func (f *fakeCluster) Profile() profile.Kind    { return f.kind }
func (f *fakeCluster) UserName() string         { return "tester" }
func (f *fakeCluster) DefaultNamespace() string { return "default" }
func (f *fakeCluster) Reader() kube.Reader      { return f }
func (f *fakeCluster) Mutator() kube.Mutator    { return f }

func (f *fakeCluster) Get(_ context.Context, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	if obj, ok := f.objects[key(gvr, ns, name)]; ok {
		return obj.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
}

func (f *fakeCluster) List(context.Context, schema.GroupVersionResource, string, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (f *fakeCluster) Watch(context.Context, schema.GroupVersionResource, string, metav1.ListOptions) (watch.Interface, error) {
	return watch.NewEmptyWatch(), nil
}

func (f *fakeCluster) Resolve(gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool, error) {
	switch gvk.Kind {
	case "ConfigMap":
		return configMaps, true, nil
	case "Secret":
		return secrets, true, nil
	case "Deployment":
		return deployments, true, nil
	}
	return schema.GroupVersionResource{}, false, errors.New("unknown kind")
}

func (f *fakeCluster) CanI(_ context.Context, verb string, _ schema.GroupVersionResource, _, _ string) (bool, string, error) {
	return !f.denyVerbs[verb], "", nil
}

func (f *fakeCluster) Apply(_ context.Context, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, opts kube.ApplyOptions) (*unstructured.Unstructured, error) {
	if f.conflict && !opts.Force {
		return nil, apierrors.NewApplyConflict([]metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "kubectl": .data.color`}}, "conflict")
	}
	out := obj.DeepCopy()
	if !opts.DryRun {
		f.applied = append(f.applied, out)
		f.lastApplyRV = obj.GetResourceVersion()
		f.objects[key(gvr, ns, obj.GetName())] = out
	}
	return out, nil
}

func (f *fakeCluster) MergePatch(_ context.Context, gvr schema.GroupVersionResource, ns, name string, patch []byte, dryRun bool) (*unstructured.Unstructured, error) {
	obj, ok := f.objects[key(gvr, ns, name)]
	if !ok {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
	}
	out := obj.DeepCopy()
	if strings.Contains(string(patch), `"replicas"`) {
		_ = unstructured.SetNestedField(out.Object, int64(0), "spec", "replicas")
	}
	if !dryRun {
		f.patched = append(f.patched, patch)
	}
	return out, nil
}

func (f *fakeCluster) Delete(_ context.Context, gvr schema.GroupVersionResource, ns, name, rv string, dryRun bool) error {
	if !dryRun {
		f.deleted = append(f.deleted, key(gvr, ns, name))
		f.deletedRV = append(f.deletedRV, rv)
	}
	return nil
}

func newGuard(t *testing.T, f *fakeCluster) (*Guard, *audit.Log) {
	t.Helper()
	log, err := audit.Open(filepath.Join(t.TempDir(), "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	g := New(SessionsFunc(func(name string) (Session, bool) {
		if name != "test" {
			return nil, false
		}
		return f, true
	}), log)
	return g, log
}

func configMap(name string, data map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": name, "namespace": "default"},
		"data":       data,
	}}
}

func TestApplyCreateIsAudited(t *testing.T) {
	f := newFake(profile.Local)
	g, log := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("demo", map[string]interface{}{"a": "1"})})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("plan not allowed: %s", plan.DeniedReason)
	}
	if plan.NeedsReason || plan.ConfirmName != "" {
		t.Errorf("local cluster should not need a reason or typed confirmation")
	}
	if len(plan.Summary) == 0 || plan.Summary[0] != "Creates ConfigMap default/demo." {
		t.Errorf("summary = %v", plan.Summary)
	}
	if !strings.Contains(plan.Diff, "+data:") {
		t.Errorf("diff does not show the new object:\n%s", plan.Diff)
	}
	if len(f.applied) != 0 {
		t.Fatal("Prepare must not change the cluster")
	}

	res, err := g.Commit(ctx, plan.ID, Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 {
		t.Fatalf("applied %d objects, want 1", len(f.applied))
	}
	entries, _ := log.Recent(10)
	if len(entries) != 1 || entries[0].Action != "apply" || entries[0].Outcome != audit.OutcomeApplied || entries[0].Seq != res.AuditSeq {
		t.Errorf("audit entries = %+v", entries)
	}
	if v, _ := log.Verify(); !v.OK {
		t.Errorf("audit chain broken: %+v", v)
	}
}

func TestPlansAreSingleUseAndExpire(t *testing.T) {
	f := newFake(profile.Local)
	g, _ := newGuard(t, f)
	ctx := context.Background()

	plan, _ := g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("one", map[string]interface{}{"a": "1"})})
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("second commit: got %v, want ErrPlanNotFound", err)
	}

	plan, _ = g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("two", map[string]interface{}{"a": "1"})})
	g.now = func() time.Time { return time.Now().Add(PlanTTL + time.Minute) }
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("expired commit: got %v, want ErrPlanNotFound", err)
	}
}

func TestProductionRules(t *testing.T) {
	f := newFake(profile.Production)
	f.put(configMaps, configMap("live", map[string]interface{}{"a": "1"}))
	g, log := newGuard(t, f)
	ctx := context.Background()
	target := TargetRequest{Context: "test", GVR: configMaps, Namespace: "default", Name: "live"}

	plan, err := g.PrepareDelete(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "Editing is off") {
		t.Fatalf("production should start read-only, got allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{Reason: "x", TypedName: "live"}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("commit of a denied plan: got %v, want ErrNotAllowed", err)
	}

	if err := g.SetEditing("test", true, ""); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("enabling editing without a reason: got %v", err)
	}
	if err := g.SetEditing("test", true, "rotate config"); err != nil {
		t.Fatal(err)
	}

	plan, err = g.PrepareDelete(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed || !plan.NeedsReason || plan.ConfirmName != "live" {
		t.Fatalf("plan = allowed %v, needsReason %v, confirm %q", plan.Allowed, plan.NeedsReason, plan.ConfirmName)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{TypedName: "live"}); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("commit without reason: got %v", err)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{Reason: "cleanup", TypedName: "lvie"}); !errors.Is(err, ErrConfirmName) {
		t.Errorf("commit with wrong name: got %v", err)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{Reason: "cleanup", TypedName: "live"}); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 || f.deletedRV[0] != "41" {
		t.Errorf("deleted = %v with rv %v, want one delete guarded by rv 41", f.deleted, f.deletedRV)
	}
	entries, _ := log.Recent(10)
	if entries[0].Reason != "cleanup" || entries[0].Profile != "production" {
		t.Errorf("latest audit entry = %+v", entries[0])
	}
}

func TestRBACDenial(t *testing.T) {
	f := newFake(profile.Local)
	f.denyVerbs["create"] = true
	g, _ := newGuard(t, f)
	plan, err := g.PrepareApply(context.Background(), ApplyRequest{Context: "test", Object: configMap("demo", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "can't create configmaps") {
		t.Errorf("allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}
}

func TestConflictsAreShownNotForced(t *testing.T) {
	f := newFake(profile.Local)
	f.put(configMaps, configMap("demo", map[string]interface{}{"color": "blue"}))
	f.conflict = true
	g, _ := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("demo", map[string]interface{}{"color": "red"})})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0], ".data.color") {
		t.Fatalf("allowed=%v conflicts=%v", plan.Allowed, plan.Conflicts)
	}

	plan, err = g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("demo", map[string]interface{}{"color": "red"}), ForceConflicts: true})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed || len(plan.Warnings) == 0 {
		t.Fatalf("forced plan: allowed=%v warnings=%v", plan.Allowed, plan.Warnings)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if f.lastApplyRV != "41" {
		t.Errorf("commit applied with resourceVersion %q, want 41", f.lastApplyRV)
	}
}

func TestSecretValuesNeverReachThePlan(t *testing.T) {
	f := newFake(profile.Local)
	f.put(secrets, &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]interface{}{"name": "db", "namespace": "default"},
		"data":     map[string]interface{}{"password": "c2VjcmV0LW9sZA==", "user": "YWRtaW4="},
	}})
	g, _ := newGuard(t, f)
	desired := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]interface{}{"name": "db", "namespace": "default"},
		"data":     map[string]interface{}{"password": "c2VjcmV0LW5ldw==", "user": "YWRtaW4="},
	}}
	plan, err := g.PrepareApply(context.Background(), ApplyRequest{Context: "test", Object: desired})
	if err != nil {
		t.Fatal(err)
	}
	everything := plan.Diff + strings.Join(plan.Summary, "\n")
	for _, v := range []string{"c2VjcmV0LW9sZA==", "c2VjcmV0LW5ldw==", "YWRtaW4="} {
		if strings.Contains(everything, v) {
			t.Errorf("plan leaks secret value %q:\n%s", v, everything)
		}
	}
	if !strings.Contains(plan.Diff, "(changed)") || !strings.Contains(plan.Diff, "(unchanged)") {
		t.Errorf("diff should mark changed and unchanged keys:\n%s", plan.Diff)
	}
}

func TestScaleToZeroInProductionNeedsName(t *testing.T) {
	f := newFake(profile.Production)
	f.put(deployments, &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]interface{}{"name": "api", "namespace": "shop"},
		"spec":     map[string]interface{}{"replicas": int64(3)},
	}})
	g, _ := newGuard(t, f)
	if err := g.SetEditing("test", true, "incident 42"); err != nil {
		t.Fatal(err)
	}
	plan, err := g.PrepareScale(context.Background(), TargetRequest{Context: "test", GVR: deployments, Namespace: "shop", Name: "api"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ConfirmName != "api" || !strings.HasPrefix(plan.Summary[0], "Scales Deployment shop/api from 3 to 0") {
		t.Errorf("confirm=%q summary=%v", plan.ConfirmName, plan.Summary)
	}
	if _, err := g.PrepareScale(context.Background(), TargetRequest{Context: "test", GVR: configMaps, Namespace: "shop", Name: "api"}, 1); err == nil {
		t.Error("scaling a ConfigMap should be refused")
	}
}

func TestUnknownContextIsRefused(t *testing.T) {
	g, _ := newGuard(t, newFake(profile.Local))
	if _, err := g.PrepareApply(context.Background(), ApplyRequest{Context: "other", Object: configMap("x", nil)}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("got %v, want ErrNotConnected", err)
	}
}
