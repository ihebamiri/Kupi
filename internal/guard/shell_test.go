package guard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/profile"
)

func runningPod(phase string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]interface{}{"name": "api-1", "namespace": "shop"},
		"spec": map[string]interface{}{"containers": []interface{}{
			map[string]interface{}{"name": "app"},
			map[string]interface{}{"name": "sidecar"},
		}},
		"status": map[string]interface{}{"phase": phase},
	}}
}

func TestShellInProductionNeedsReasonAndIsAudited(t *testing.T) {
	f := newFake(profile.Production)
	f.put(podsGVR, runningPod("Running"))
	g, log := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareShell(ctx, ShellRequest{Context: "test", Namespace: "shop", Pod: "api-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed || !plan.NeedsReason {
		t.Fatalf("shell plan in read-only production: allowed=%v needsReason=%v (%s)", plan.Allowed, plan.NeedsReason, plan.DeniedReason)
	}
	if err := g.StartShell(ctx, plan.ID, Confirmation{}, kube.ExecStreams{}); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("shell without reason: got %v", err)
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{Reason: "x"}); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("committing a shell plan as a change must be refused, got %v", err)
	}
	if err := g.StartShell(ctx, plan.ID, Confirmation{Reason: "debug incident 7"}, kube.ExecStreams{}); err != nil {
		t.Fatal(err)
	}
	if len(f.execs) != 1 || !strings.HasPrefix(f.execs[0], "shop/api-1/app:/bin/sh -c") {
		t.Errorf("execs = %v", f.execs)
	}
	entries, _ := log.Recent(1)
	if entries[0].Action != "exec" || entries[0].Reason != "debug incident 7" || !strings.Contains(entries[0].Resource, "container app") {
		t.Errorf("audit entry = %+v", entries[0])
	}
}

func TestShellChecks(t *testing.T) {
	f := newFake(profile.Local)
	f.put(podsGVR, runningPod("Pending"))
	g, _ := newGuard(t, f)
	ctx := context.Background()

	plan, err := g.PrepareShell(ctx, ShellRequest{Context: "test", Namespace: "shop", Pod: "api-1", Container: "sidecar"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "isn't running") {
		t.Errorf("pending pod: allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}
	if _, err := g.PrepareShell(ctx, ShellRequest{Context: "test", Namespace: "shop", Pod: "api-1", Container: "nope"}); err == nil {
		t.Error("unknown container accepted")
	}

	f.put(podsGVR, runningPod("Running"))
	f.denyVerbs["create/exec"] = true
	plan, _ = g.PrepareShell(ctx, ShellRequest{Context: "test", Namespace: "shop", Pod: "api-1"})
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "can't open shells") {
		t.Errorf("RBAC denial: allowed=%v reason=%q", plan.Allowed, plan.DeniedReason)
	}

	apply, _ := g.PrepareApply(ctx, ApplyRequest{Context: "test", Object: configMap("x", nil)})
	if err := g.StartShell(ctx, apply.ID, Confirmation{}, kube.ExecStreams{}); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("starting a shell from an apply plan must be refused, got %v", err)
	}
}
