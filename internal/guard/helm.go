package guard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/helmrel"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/validate"
)

// Helm actions, as recorded in the audit log.
const (
	ActionHelmUpgrade   Action = "helm-upgrade"
	ActionHelmRollback  Action = "helm-rollback"
	ActionHelmUninstall Action = "helm-uninstall"
)

// helmStorage is where Helm keeps release records; changing a release means
// writing a new one.
var helmStorage = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// HelmTarget names a release.
type HelmTarget struct {
	Context   string
	Namespace string
	Name      string
}

func (g *Guard) helmBegin(ctx context.Context, t HelmTarget, action Action) (Session, *Plan, *rspb.Release, error) {
	if err := validate.Namespace(t.Namespace); err != nil {
		return nil, nil, nil, err
	}
	if err := validate.Name(t.Name); err != nil {
		return nil, nil, nil, err
	}
	s, rules, editing, err := g.begin(t.Context)
	if err != nil {
		return nil, nil, nil, err
	}
	cur, err := s.Helm().HelmRelease(ctx, t.Namespace, t.Name, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	p := basePlan(s, rules, editing, action)
	p.Kind, p.Namespace, p.Name = "Helm release", t.Namespace, t.Name
	if cur.Info != nil && strings.HasPrefix(string(cur.Info.Status), "pending") {
		p.deny(fmt.Sprintf("Another Helm operation is in progress on this release (%s).", cur.Info.Status))
	}
	g.checkRBAC(ctx, p, s.Reader(), "create", helmStorage, t.Namespace, "")
	p.Warnings = append(p.Warnings, helmOwnerWarnings(cur)...)
	return s, p, cur, nil
}

// helmOwnerWarnings flags releases that Flux or Argo CD manage.
func helmOwnerWarnings(r *rspb.Release) []string {
	objs, _ := helmrel.Objects(r.Manifest, r.Namespace)
	for _, o := range objs {
		if w := ownerWarnings(o); len(w) > 0 {
			for _, line := range w {
				if !strings.HasPrefix(line, "Managed by the Helm release") {
					return []string{line}
				}
			}
		}
	}
	return nil
}

func chartLabel(r *rspb.Release) string {
	v := helmrel.View(r)
	if v.Chart == "" {
		return "unknown chart"
	}
	return v.Chart + " " + v.ChartVersion
}

// manifestSummary counts objects added, removed and changed.
func manifestSummary(before, after, namespace string) []string {
	a, _ := helmrel.Objects(before, namespace)
	b, _ := helmrel.Objects(after, namespace)
	key := func(kind, ns, name string) string { return kind + " " + ns + "/" + name }
	am := map[string]bool{}
	for _, o := range a {
		am[key(o.GetKind(), o.GetNamespace(), o.GetName())] = true
	}
	bm := map[string]bool{}
	var added, removed []string
	for _, o := range b {
		k := key(o.GetKind(), o.GetNamespace(), o.GetName())
		bm[k] = true
		if !am[k] {
			added = append(added, k)
		}
	}
	for k := range am {
		if !bm[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	var out []string
	for _, k := range added {
		out = append(out, "Adds "+k+".")
	}
	for _, k := range removed {
		out = append(out, "Removes "+k+".")
	}
	if len(out) > maxSummary {
		out = append(out[:maxSummary], fmt.Sprintf("…and %d more.", len(out)-maxSummary))
	}
	return out
}

// PrepareHelmUpgrade previews upgrading a release with its current chart and
// new values. The preview is a server-side dry run, so the cluster's
// admission checks and the chart's values schema both apply.
func (g *Guard) PrepareHelmUpgrade(ctx context.Context, t HelmTarget, values map[string]any) (*Plan, error) {
	s, p, cur, err := g.helmBegin(ctx, t, ActionHelmUpgrade)
	if err != nil {
		return nil, err
	}
	req := kube.HelmUpgrade{Namespace: t.Namespace, Name: t.Name, Values: values, FromRevision: cur.Version}
	next, err := s.HelmMutator().HelmUpgrade(ctx, req, true)
	if err != nil {
		p.deny("Helm rejected these values: " + err.Error())
	} else {
		p.Diff = helmrel.Diff(cur.Manifest, next.Manifest, t.Namespace)
		if p.Diff == "" {
			p.Warnings = append(p.Warnings, "These values render exactly the same objects. Helm will still record a new revision.")
		}
	}
	p.Summary = append([]string{fmt.Sprintf("Upgrades %s from revision %d with the edited values (%s, same chart).", p.Resource(), cur.Version, chartLabel(cur))}, manifestSummaryOf(cur, next, t.Namespace)...)
	p.Warnings = append(p.Warnings, "Kupi checked you may write Helm's release records. The upgrade can still fail if you can't change some of the release's objects.")
	p.helmExec = func(ctx context.Context, m kube.HelmMutator) error {
		_, err := m.HelmUpgrade(ctx, req, false)
		return err
	}
	return g.store(p), nil
}

func manifestSummaryOf(cur, next *rspb.Release, namespace string) []string {
	if next == nil {
		return nil
	}
	return manifestSummary(cur.Manifest, next.Manifest, namespace)
}

// PrepareHelmRollback previews rolling a release back to an earlier revision.
func (g *Guard) PrepareHelmRollback(ctx context.Context, t HelmTarget, revision int) (*Plan, error) {
	s, p, cur, err := g.helmBegin(ctx, t, ActionHelmRollback)
	if err != nil {
		return nil, err
	}
	if revision <= 0 || revision >= cur.Version {
		return nil, fmt.Errorf("choose an earlier revision than %d", cur.Version)
	}
	target, err := s.Helm().HelmRelease(ctx, t.Namespace, t.Name, revision)
	if err != nil {
		return nil, err
	}
	p.Diff = helmrel.Diff(cur.Manifest, target.Manifest, t.Namespace)
	p.Summary = append([]string{fmt.Sprintf("Rolls %s back from revision %d (%s) to revision %d (%s). Helm records this as revision %d.",
		p.Resource(), cur.Version, chartLabel(cur), revision, chartLabel(target), cur.Version+1)}, manifestSummaryOf(cur, target, t.Namespace)...)
	from := cur.Version
	p.helmExec = func(ctx context.Context, m kube.HelmMutator) error {
		return m.HelmRollback(ctx, t.Namespace, t.Name, revision, from)
	}
	return g.store(p), nil
}

// PrepareHelmUninstall previews uninstalling a release and deleting its objects.
func (g *Guard) PrepareHelmUninstall(ctx context.Context, t HelmTarget) (*Plan, error) {
	_, p, cur, err := g.helmBegin(ctx, t, ActionHelmUninstall)
	if err != nil {
		return nil, err
	}
	res := helmrel.Resources(cur.Manifest, t.Namespace)
	p.Diff = helmrel.Diff(cur.Manifest, "", t.Namespace)
	p.Summary = []string{fmt.Sprintf("Uninstalls %s (%s) and deletes its %d objects.", p.Resource(), chartLabel(cur), len(res))}
	p.Warnings = append(p.Warnings, "Volumes that StatefulSets created through volume claim templates are kept; delete them separately if you no longer need the data.")
	if profile.RulesFor(p.Profile).ConfirmByName {
		p.ConfirmName = t.Name
	}
	from := cur.Version
	p.helmExec = func(ctx context.Context, m kube.HelmMutator) error {
		return m.HelmUninstall(ctx, t.Namespace, t.Name, from)
	}
	return g.store(p), nil
}
