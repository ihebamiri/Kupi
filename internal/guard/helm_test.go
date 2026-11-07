package guard

import (
	"context"
	"errors"
	"strings"
	"testing"

	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/profile"
)

// fakeHelm keeps release revisions in memory. Upgrades render a manifest
// whose replica count is the "replicas" value.
type fakeHelm struct {
	revs      []*rspb.Release // oldest first
	upgrades  []map[string]any
	rolled    []int
	removed   bool
	installed []kube.HelmInstall
}

func (f *fakeCluster) Helm() kube.HelmReader         { return f.helm() }
func (f *fakeCluster) HelmMutator() kube.HelmMutator { return f.helm() }

var helms = map[*fakeCluster]*fakeHelm{}

func (f *fakeCluster) helm() *fakeHelm {
	if helms[f] == nil {
		helms[f] = &fakeHelm{}
	}
	return helms[f]
}

func manifest(replicas string) string {
	return "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: shop\nspec:\n  replicas: " + replicas +
		"\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: shop-db\nstringData:\n  password: hunter2-" + replicas + "\n"
}

func (h *fakeHelm) add(replicas, chartVersion string, status common.Status) {
	h.revs = append(h.revs, &rspb.Release{
		Name: "shop", Namespace: "shop", Version: len(h.revs) + 1, Manifest: manifest(replicas),
		Info:  &rspb.Info{Status: status},
		Chart: &chartv2.Chart{Metadata: &chartv2.Metadata{Name: "shop", Version: chartVersion}},
	})
}

func (h *fakeHelm) HelmReleases(context.Context, string) ([]*rspb.Release, error) { return h.revs, nil }
func (h *fakeHelm) HelmHistory(context.Context, string, string) ([]*rspb.Release, error) {
	return h.revs, nil
}

func (h *fakeHelm) HelmRelease(_ context.Context, _, name string, revision int) (*rspb.Release, error) {
	if len(h.revs) == 0 || h.removed {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "releases"}, name)
	}
	if revision == 0 {
		return h.revs[len(h.revs)-1], nil
	}
	if revision > len(h.revs) {
		return nil, errors.New("no such revision")
	}
	return h.revs[revision-1], nil
}

func (h *fakeHelm) HelmUpgrade(_ context.Context, req kube.HelmUpgrade, dryRun bool) (*rspb.Release, error) {
	if req.FromRevision != len(h.revs) {
		return nil, kube.ErrReleaseChanged
	}
	replicas, _ := req.Values["replicas"].(string)
	if replicas == "" {
		return nil, errors.New("values don't meet the schema: replicas is required")
	}
	out := &rspb.Release{Name: "shop", Namespace: "shop", Version: len(h.revs) + 1, Manifest: manifest(replicas), Info: &rspb.Info{Status: common.StatusDeployed}}
	if !dryRun {
		h.upgrades = append(h.upgrades, req.Values)
		h.revs = append(h.revs, out)
	}
	return out, nil
}

// HelmInstall renders a Deployment, a Secret and a ClusterRole, plus a hook.
func (h *fakeHelm) HelmInstall(_ context.Context, req kube.HelmInstall, dryRun bool) (*rspb.Release, error) {
	if _, bad := req.Values["broken"]; bad {
		return nil, errors.New("template: shop/templates/deploy.yaml:3: nil pointer")
	}
	out := &rspb.Release{
		Name: req.Name, Namespace: req.Namespace, Version: 1, Chart: req.Chart,
		Manifest: manifest("2") + "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: shop-reader\n",
		Info:     &rspb.Info{Status: common.StatusDeployed},
		Hooks:    []*rspb.Hook{{Name: "shop-migrate", Kind: "Job", Events: []rspb.HookEvent{rspb.HookPreInstall}}},
	}
	if !dryRun {
		h.installed = append(h.installed, req)
		h.revs = append(h.revs, out)
	}
	return out, nil
}

func (h *fakeHelm) HelmRollback(_ context.Context, _, _ string, revision, from int) error {
	if from != len(h.revs) {
		return kube.ErrReleaseChanged
	}
	h.rolled = append(h.rolled, revision)
	return nil
}

func (h *fakeHelm) HelmUninstall(_ context.Context, _, _ string, from int) error {
	if from != len(h.revs) {
		return kube.ErrReleaseChanged
	}
	h.removed = true
	return nil
}

func TestHelmUpgradeReviewHidesSecretsAndChecksRevision(t *testing.T) {
	f := newFake(profile.Local)
	h := f.helm()
	h.add("1", "1.0.0", common.StatusDeployed)
	g, log := newGuard(t, f)
	target := HelmTarget{Context: "test", Namespace: "shop", Name: "shop"}

	plan, err := g.PrepareHelmUpgrade(context.Background(), target, map[string]any{"replicas": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("denied: %s", plan.DeniedReason)
	}
	if strings.Contains(plan.Diff, "hunter2") || !strings.Contains(plan.Diff, "+  replicas: 3") || !strings.Contains(plan.Diff, "(changed)") {
		t.Errorf("diff:\n%s", plan.Diff)
	}
	if _, err := g.Commit(context.Background(), plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if len(h.upgrades) != 1 {
		t.Fatalf("upgrades = %v", h.upgrades)
	}
	if entries, _ := log.Recent(1); entries[0].Action != "helm-upgrade" || entries[0].Resource != "Helm release shop/shop" {
		t.Errorf("audit = %+v", entries[0])
	}

	// A preview goes stale when the release moves on.
	stale, _ := g.PrepareHelmUpgrade(context.Background(), target, map[string]any{"replicas": "4"})
	h.add("5", "1.0.0", common.StatusDeployed)
	if _, err := g.Commit(context.Background(), stale.ID, Confirmation{}); !errors.Is(err, kube.ErrReleaseChanged) {
		t.Errorf("stale upgrade: %v", err)
	}

	bad, _ := g.PrepareHelmUpgrade(context.Background(), target, map[string]any{})
	if bad.Allowed || !strings.Contains(bad.DeniedReason, "schema") {
		t.Errorf("invalid values allowed: %+v", bad)
	}
}

func TestHelmRollbackAndUninstall(t *testing.T) {
	f := newFake(profile.Production)
	h := f.helm()
	h.add("1", "1.0.0", common.StatusSuperseded)
	h.add("2", "1.1.0", common.StatusDeployed)
	g, _ := newGuard(t, f)
	if err := g.SetEditing("test", true, "rollback after bad deploy"); err != nil {
		t.Fatal(err)
	}
	target := HelmTarget{Context: "test", Namespace: "shop", Name: "shop"}

	if _, err := g.PrepareHelmRollback(context.Background(), target, 2); err == nil {
		t.Error("rollback to the current revision accepted")
	}
	rb, err := g.PrepareHelmRollback(context.Background(), target, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rb.Summary[0], "revision 2 (shop 1.1.0) to revision 1 (shop 1.0.0)") || !strings.Contains(rb.Diff, "-  replicas: 2") {
		t.Errorf("rollback plan: %v\n%s", rb.Summary, rb.Diff)
	}
	if _, err := g.Commit(context.Background(), rb.ID, Confirmation{Reason: "bad deploy"}); err != nil {
		t.Fatal(err)
	}
	if len(h.rolled) != 1 || h.rolled[0] != 1 {
		t.Errorf("rolled = %v", h.rolled)
	}

	un, err := g.PrepareHelmUninstall(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if un.ConfirmName != "shop" || !strings.Contains(un.Summary[0], "deletes its 2 objects") {
		t.Errorf("uninstall plan: %+v", un)
	}
	if _, err := g.Commit(context.Background(), un.ID, Confirmation{Reason: "retire"}); !errors.Is(err, ErrConfirmName) {
		t.Errorf("uninstall without typed name: %v", err)
	}
	if _, err := g.Commit(context.Background(), un.ID, Confirmation{Reason: "retire", TypedName: "shop"}); err != nil {
		t.Fatal(err)
	}
	if !h.removed {
		t.Error("release not removed")
	}
}

func TestHelmPendingReleaseIsDenied(t *testing.T) {
	f := newFake(profile.Local)
	f.helm().add("1", "1.0.0", common.StatusPendingUpgrade)
	g, _ := newGuard(t, f)
	p, err := g.PrepareHelmRollback(context.Background(), HelmTarget{Context: "test", Namespace: "shop", Name: "shop"}, 0)
	if err == nil && p.Allowed {
		t.Error("action allowed on a release with a pending operation")
	}
	p, _ = g.PrepareHelmUninstall(context.Background(), HelmTarget{Context: "test", Namespace: "shop", Name: "shop"})
	if p.Allowed {
		t.Error("uninstall allowed during a pending upgrade")
	}
}

func shopChart(version string, deprecated bool) *chartv2.Chart {
	return &chartv2.Chart{Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "shop", Version: version, Deprecated: deprecated}}
}

func TestHelmInstallReview(t *testing.T) {
	f := newFake(profile.Local)
	f.put(namespacesGVR, obj("v1", "Namespace", "", "shop"))
	g, log := newGuard(t, f)
	ctx := context.Background()
	req := HelmInstallRequest{Context: "test", Namespace: "shop", Name: "shop", Chart: shopChart("2.0.0", false), Source: "https://charts.example.com", Values: map[string]any{"replicas": "2"}}

	plan, err := g.PrepareHelmInstall(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("denied: %s", plan.DeniedReason)
	}
	if !strings.Contains(plan.Summary[0], "shop 2.0.0 from https://charts.example.com") || !strings.Contains(plan.Summary[0], "3 objects") {
		t.Errorf("summary = %v", plan.Summary)
	}
	if strings.Contains(plan.Diff, "hunter2") || !strings.Contains(plan.Diff, "kind: Deployment") {
		t.Errorf("diff must show the objects without Secret values:\n%s", plan.Diff)
	}
	warn := strings.Join(plan.Warnings, "\n")
	if !strings.Contains(warn, "ClusterRole shop-reader") || !strings.Contains(warn, "Job shop-migrate (pre-install)") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
	if len(f.helm().installed) != 0 {
		t.Fatal("prepare must not install")
	}
	if _, err := g.Commit(ctx, plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	if len(f.helm().installed) != 1 {
		t.Fatalf("installed %d times", len(f.helm().installed))
	}
	if entries, _ := log.Recent(1); entries[0].Action != "helm-install" || entries[0].Resource != "Helm release shop/shop" {
		t.Errorf("audit = %+v", entries[0])
	}

	// The name is now taken.
	again, _ := g.PrepareHelmInstall(ctx, req)
	if again.Allowed || !strings.Contains(again.DeniedReason, "already exists") {
		t.Errorf("duplicate install: %v %q", again.Allowed, again.DeniedReason)
	}
}

func TestHelmInstallRefusals(t *testing.T) {
	f := newFake(profile.Local)
	g, _ := newGuard(t, f)
	ctx := context.Background()
	base := HelmInstallRequest{Context: "test", Namespace: "fresh", Name: "web", Chart: shopChart("1.0.0", true)}

	if _, err := g.PrepareHelmInstall(ctx, HelmInstallRequest{Context: "test", Namespace: "fresh", Name: "Web_App", Chart: shopChart("1.0.0", false)}); err == nil {
		t.Error("an invalid release name was accepted")
	}
	plan, _ := g.PrepareHelmInstall(ctx, base)
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "doesn't exist") {
		t.Errorf("missing namespace: %v %q", plan.Allowed, plan.DeniedReason)
	}
	base.CreateNamespace = true
	plan, _ = g.PrepareHelmInstall(ctx, base)
	if !plan.Allowed {
		t.Errorf("create namespace: %q", plan.DeniedReason)
	}
	if !strings.Contains(strings.Join(plan.Warnings, "\n"), "deprecated") {
		t.Errorf("deprecated chart not flagged: %v", plan.Warnings)
	}
	base.Values = map[string]any{"broken": true}
	plan, _ = g.PrepareHelmInstall(ctx, base)
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "couldn't render") {
		t.Errorf("broken chart: %v %q", plan.Allowed, plan.DeniedReason)
	}
	f.denyVerbs["create"] = true
	base.Values = nil
	plan, _ = g.PrepareHelmInstall(ctx, base)
	if plan.Allowed || !strings.Contains(plan.DeniedReason, "can't create") {
		t.Errorf("no permission: %v %q", plan.Allowed, plan.DeniedReason)
	}
}

func TestHelmUpgradeToAnotherChartVersion(t *testing.T) {
	f := newFake(profile.Local)
	h := f.helm()
	h.add("1", "1.0.0", common.StatusDeployed)
	g, _ := newGuard(t, f)
	target := HelmTarget{Context: "test", Namespace: "shop", Name: "shop"}
	plan, err := g.PrepareHelmUpgradeChart(context.Background(), target, map[string]any{"replicas": "1"}, shopChart("1.4.0", false), "oci://ghcr.io/example/charts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Summary[0], "from shop 1.0.0 to shop 1.4.0 (oci://ghcr.io/example/charts)") {
		t.Errorf("summary = %v", plan.Summary)
	}
	other, _ := g.PrepareHelmUpgradeChart(context.Background(), target, map[string]any{"replicas": "1"}, &chartv2.Chart{Metadata: &chartv2.Metadata{Name: "store", Version: "1.0.0"}}, "")
	if !strings.Contains(strings.Join(other.Warnings, "\n"), "different chart") {
		t.Errorf("chart swap not flagged: %v", other.Warnings)
	}
}
