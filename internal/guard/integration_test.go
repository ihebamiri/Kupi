//go:build integration

package guard_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/trust"
)

// testContext is created by `wails3 task cluster:up`.
const testContext = "k3d-kupi-test"

func setup(t *testing.T) (*kube.Manager, *guard.Guard, *audit.Log, *profile.Store) {
	t.Helper()
	dir := t.TempDir()
	trustStore, err := trust.Open(filepath.Join(dir, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	log, err := audit.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := kube.NewManager(kubeconfig.NewLoader(), trustStore, profiles)
	g := guard.New(guard.SessionsFunc(func(name string) (guard.Session, bool) {
		s, ok := mgr.Session(name)
		if !ok {
			return nil, false
		}
		return s, true
	}), log)
	return mgr, g, log, profiles
}

func TestGuardAgainstK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	mgr, g, log, profiles := setup(t)

	sess, err := mgr.Connect(ctx, testContext)
	if err != nil {
		t.Fatalf("connect to %s (run `wails3 task cluster:up`): %v", testContext, err)
	}
	if sess.Profile() != profile.Local {
		t.Fatalf("k3d cluster detected as %s, want local", sess.Profile())
	}
	name := fmt.Sprintf("kupi-it-%d", time.Now().UnixNano())
	cmGVR := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

	commit := func(p *guard.Plan, c guard.Confirmation) {
		t.Helper()
		if !p.Allowed {
			t.Fatalf("plan for %s not allowed: %s %v", p.Resource(), p.DeniedReason, p.Conflicts)
		}
		if _, err := g.Commit(ctx, p.ID, c); err != nil {
			t.Fatalf("commit %s: %v", p.Resource(), err)
		}
	}

	// Create.
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": name, "namespace": "default"},
		"data":     map[string]interface{}{"color": "blue"},
	}}
	plan, err := g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: cm})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Reader().Get(ctx, cmGVR, "default", name); !apierrors.IsNotFound(err) {
		t.Fatalf("dry run must not create the object, got %v", err)
	}
	commit(plan, guard.Confirmation{})

	// Update: the diff shows exactly the changed value.
	cm.Object["data"] = map[string]interface{}{"color": "red"}
	plan, err = g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: cm})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Diff, "-  color: blue") || !strings.Contains(plan.Diff, "+  color: red") {
		t.Errorf("update diff:\n%s", plan.Diff)
	}
	commit(plan, guard.Confirmation{})
	live, err := sess.Reader().Get(ctx, cmGVR, "default", name)
	if err != nil {
		t.Fatal(err)
	}
	if color, _, _ := unstructured.NestedString(live.Object, "data", "color"); color != "red" {
		t.Errorf("color = %q after apply", color)
	}

	// Secrets: values never reach the plan.
	secret := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata":   map[string]interface{}{"name": name, "namespace": "default"},
		"stringData": map[string]interface{}{"password": "hunter2-integration"},
	}}
	plan, err = g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: secret})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.Diff, "hunter2") || strings.Contains(plan.Diff, "aHVudGVyMi1pbnRlZ3JhdGlvbg") {
		t.Fatalf("secret value leaked into the diff:\n%s", plan.Diff)
	}
	commit(plan, guard.Confirmation{})

	// A stale plan fails instead of overwriting someone else's change.
	cm.Object["data"] = map[string]interface{}{"color": "green"}
	stale, err := g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: cm})
	if err != nil {
		t.Fatal(err)
	}
	cm.Object["data"] = map[string]interface{}{"color": "yellow"}
	plan, err = g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: cm})
	if err != nil {
		t.Fatal(err)
	}
	commit(plan, guard.Confirmation{})
	if _, err := g.Commit(ctx, stale.ID, guard.Confirmation{}); err == nil {
		t.Error("a plan prepared before another change must fail to commit")
	}

	// Delete both objects.
	for _, gvr := range []schema.GroupVersionResource{cmGVR, {Version: "v1", Resource: "secrets"}} {
		plan, err = g.PrepareDelete(ctx, guard.TargetRequest{Context: testContext, GVR: gvr, Namespace: "default", Name: name})
		if err != nil {
			t.Fatal(err)
		}
		commit(plan, guard.Confirmation{})
	}
	if _, err := sess.Reader().Get(ctx, cmGVR, "default", name); !apierrors.IsNotFound(err) {
		t.Errorf("ConfigMap still exists after delete: %v", err)
	}

	if v, err := log.Verify(); err != nil || !v.OK || v.Entries < 6 {
		t.Errorf("audit verify = %+v, %v", v, err)
	}

	// The same cluster labelled production opens read-only.
	if err := profiles.Set(testContext, profile.Production); err != nil {
		t.Fatal(err)
	}
	g.Forget(testContext)
	if _, err := mgr.Connect(ctx, testContext); err != nil {
		t.Fatal(err)
	}
	plan, err = g.PrepareApply(ctx, guard.ApplyRequest{Context: testContext, Object: cm})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !plan.NeedsReason {
		t.Errorf("production plan: allowed=%v needsReason=%v", plan.Allowed, plan.NeedsReason)
	}
}
