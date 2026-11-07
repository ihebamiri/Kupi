package guard

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	ActionHelmInstall   Action = "helm-install"
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
	return g.PrepareHelmUpgradeChart(ctx, t, values, nil, "")
}

// PrepareHelmUpgradeChart is PrepareHelmUpgrade with another chart (for
// example a newer version from the chart's repository). source describes
// where the chart came from, for the review and the audit trail.
func (g *Guard) PrepareHelmUpgradeChart(ctx context.Context, t HelmTarget, values map[string]any, ch *chartv2.Chart, source string) (*Plan, error) {
	s, p, cur, err := g.helmBegin(ctx, t, ActionHelmUpgrade)
	if err != nil {
		return nil, err
	}
	req := kube.HelmUpgrade{Namespace: t.Namespace, Name: t.Name, Values: values, FromRevision: cur.Version, Chart: ch}
	next, err := s.HelmMutator().HelmUpgrade(ctx, req, true)
	if err != nil {
		p.deny("Helm rejected this upgrade: " + err.Error())
	} else {
		p.Diff = helmrel.Diff(cur.Manifest, next.Manifest, t.Namespace)
		if p.Diff == "" {
			p.Warnings = append(p.Warnings, "This renders exactly the same objects. Helm will still record a new revision.")
		}
		p.Warnings = append(p.Warnings, hookWarnings(next)...)
	}
	what := fmt.Sprintf("with the edited values (%s, same chart)", chartLabel(cur))
	if ch != nil && ch.Metadata != nil {
		what = fmt.Sprintf("from %s to %s %s", chartLabel(cur), ch.Metadata.Name, ch.Metadata.Version)
		if source != "" {
			what += " (" + source + ")"
		}
		if ch.Metadata.Name != cur.Chart.Metadata.Name {
			p.Warnings = append(p.Warnings, fmt.Sprintf("This replaces the chart %s with a different chart, %s. Check the diff closely.", cur.Chart.Metadata.Name, ch.Metadata.Name))
		}
		if ch.Metadata.Deprecated {
			p.Warnings = append(p.Warnings, "The chart's maintainers marked this version as deprecated.")
		}
	}
	p.Summary = append([]string{fmt.Sprintf("Upgrades %s from revision %d %s.", p.Resource(), cur.Version, what)}, manifestSummaryOf(cur, next, t.Namespace)...)
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

// HelmInstallRequest installs a chart as a new release.
type HelmInstallRequest struct {
	Context   string
	Namespace string
	Name      string
	Chart     *chartv2.Chart
	// Source describes where the chart came from, for example
	// "https://charts.example.com" or "oci://ghcr.io/org/charts".
	Source          string
	Values          map[string]any
	CreateNamespace bool
}

// releaseNameRe is Helm's own rule: a DNS label of at most 53 characters.
var releaseNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,51}[a-z0-9])?$`)

// PrepareHelmInstall previews installing a chart: Helm renders it and
// validates it against the cluster without creating anything. The review
// lists every object it would create, the cluster-wide ones and the hooks it
// would run, after checking you may create each kind.
func (g *Guard) PrepareHelmInstall(ctx context.Context, req HelmInstallRequest) (*Plan, error) {
	if err := validate.Namespace(req.Namespace); err != nil {
		return nil, err
	}
	if !releaseNameRe.MatchString(req.Name) {
		return nil, fmt.Errorf("a release name is lowercase letters, digits and dashes, at most 53 characters")
	}
	if req.Chart == nil || req.Chart.Metadata == nil {
		return nil, fmt.Errorf("no chart to install")
	}
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	p := basePlan(s, rules, editing, ActionHelmInstall)
	p.Kind, p.Namespace, p.Name = "Helm release", req.Namespace, req.Name
	reader := s.Reader()

	if _, err := s.Helm().HelmRelease(ctx, req.Namespace, req.Name, 0); err == nil {
		p.deny(fmt.Sprintf("A release named %s already exists in %s. Upgrade it instead, or pick another name.", req.Name, req.Namespace))
	}
	if _, err := reader.Get(ctx, namespacesGVR, "", req.Namespace); apierrors.IsNotFound(err) {
		if !req.CreateNamespace {
			p.deny(fmt.Sprintf("The namespace %s doesn't exist. Choose to create it, or pick another namespace.", req.Namespace))
		} else {
			g.checkRBAC(ctx, p, reader, "create", namespacesGVR, "", "")
		}
	}
	g.checkRBAC(ctx, p, reader, "create", helmStorage, req.Namespace, "")

	label := req.Chart.Metadata.Name + " " + req.Chart.Metadata.Version
	if req.Source != "" {
		label += " from " + req.Source
	}
	inst := kube.HelmInstall{Namespace: req.Namespace, Name: req.Name, Chart: req.Chart, Values: req.Values, CreateNamespace: req.CreateNamespace}
	next, err := s.HelmMutator().HelmInstall(ctx, inst, true)
	if err != nil {
		p.deny("Helm couldn't render or validate this chart: " + err.Error())
		p.Summary = []string{"Installs " + label + " as " + p.Resource() + "."}
	} else {
		objs, _ := helmrel.Objects(next.Manifest, req.Namespace)
		p.Diff = helmrel.Diff("", next.Manifest, req.Namespace)
		p.Summary = append([]string{fmt.Sprintf("Installs %s as %s, creating %d objects.", label, p.Resource(), len(objs))}, manifestSummary("", next.Manifest, req.Namespace)...)
		p.Warnings = append(p.Warnings, g.installChecks(ctx, p, reader, objs)...)
		p.Warnings = append(p.Warnings, hookWarnings(next)...)
	}
	if req.Chart.Metadata.Deprecated {
		p.Warnings = append(p.Warnings, "The chart's maintainers marked this version as deprecated.")
	}
	p.helmExec = func(ctx context.Context, m kube.HelmMutator) error {
		_, err := m.HelmInstall(ctx, inst, false)
		return err
	}
	return g.store(p), nil
}

// installChecks checks you may create each kind of object the chart renders,
// and lists the cluster-wide ones.
func (g *Guard) installChecks(ctx context.Context, p *Plan, r kube.Reader, objs []*unstructured.Unstructured) []string {
	type key struct {
		gvr schema.GroupVersionResource
		ns  string
	}
	checked := map[key]bool{}
	var clusterWide []string
	for _, o := range objs {
		gvr, namespaced, err := r.Resolve(o.GroupVersionKind())
		if err != nil {
			// A type the chart itself defines (CRD) can't be resolved yet;
			// Helm installs CRDs first.
			continue
		}
		ns := ""
		if namespaced {
			ns = o.GetNamespace()
		} else {
			clusterWide = append(clusterWide, o.GetKind()+" "+o.GetName())
		}
		k := key{gvr, ns}
		if checked[k] {
			continue
		}
		checked[k] = true
		g.checkRBAC(ctx, p, r, "create", gvr, ns, "")
	}
	if len(clusterWide) == 0 {
		return nil
	}
	sort.Strings(clusterWide)
	if len(clusterWide) > 8 {
		clusterWide = append(clusterWide[:8], fmt.Sprintf("and %d more", len(clusterWide)-8))
	}
	return []string{"This chart creates cluster-wide objects, which affect every namespace: " + strings.Join(clusterWide, ", ") + "."}
}

// hookWarnings lists the hooks (usually Jobs) Helm runs around the change.
func hookWarnings(r *rspb.Release) []string {
	if r == nil || len(r.Hooks) == 0 {
		return nil
	}
	var names []string
	for _, h := range r.Hooks {
		var events []string
		for _, e := range h.Events {
			events = append(events, string(e))
		}
		names = append(names, fmt.Sprintf("%s %s (%s)", h.Kind, h.Name, strings.Join(events, ", ")))
	}
	sort.Strings(names)
	return []string{fmt.Sprintf("Helm will also run %d hook(s), which aren't in the diff: %s.", len(names), strings.Join(names, "; "))}
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
