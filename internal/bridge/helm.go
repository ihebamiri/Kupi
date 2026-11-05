package bridge

import (
	"context"
	"fmt"
	"sort"

	"github.com/iheb-amiri/kupi/internal/guard"
	"github.com/iheb-amiri/kupi/internal/helmrel"
	"github.com/iheb-amiri/kupi/internal/validate"
)

const maxValuesBytes = 1 << 20

// ReleaseDetail is one release with its history. Values are not included:
// they often hold passwords, so they come only from Values.
type ReleaseDetail struct {
	Release   helmrel.Release    `json:"release"`
	History   []helmrel.Release  `json:"history"`
	Notes     string             `json:"notes"`
	Manifest  string             `json:"manifest"`
	Resources []helmrel.Resource `json:"resources"`
	// Defaults are the chart's own default values (not secret).
	Defaults           string `json:"defaults"`
	HasSchema          bool   `json:"hasSchema"`
	ValuesNeedReason   bool   `json:"valuesNeedReason"`
	ChartDescription   string `json:"chartDescription"`
	ChartHome          string `json:"chartHome"`
	UserValuesSupplied bool   `json:"userValuesSupplied"`
}

// HelmService shows Helm releases and prepares upgrades, rollbacks and
// uninstalls through the Guard.
type HelmService struct{ d Deps }

// NewHelmService returns the Helm service.
func NewHelmService(d Deps) *HelmService { return &HelmService{d: d} }

func helmTarget(contextName, namespace, name string) (guard.HelmTarget, error) {
	if err := validate.ContextName(contextName); err != nil {
		return guard.HelmTarget{}, err
	}
	if err := validate.Namespace(namespace); err != nil {
		return guard.HelmTarget{}, err
	}
	if err := validate.Name(name); err != nil {
		return guard.HelmTarget{}, err
	}
	return guard.HelmTarget{Context: contextName, Namespace: namespace, Name: name}, nil
}

// Releases lists the latest revision of every release in a namespace ("" for all).
func (s *HelmService) Releases(ctx context.Context, contextName, namespace string) ([]helmrel.Release, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return nil, err
	}
	rels, err := sess.Helm().HelmReleases(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]helmrel.Release, 0, len(rels))
	for _, r := range rels {
		out = append(out, helmrel.View(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name })
	return out, nil
}

// Release returns a release's details and history.
func (s *HelmService) Release(ctx context.Context, contextName, namespace, name string) (ReleaseDetail, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return ReleaseDetail{}, err
	}
	if _, err := helmTarget(contextName, namespace, name); err != nil {
		return ReleaseDetail{}, err
	}
	cur, err := sess.Helm().HelmRelease(ctx, namespace, name, 0)
	if err != nil {
		return ReleaseDetail{}, err
	}
	hist, err := sess.Helm().HelmHistory(ctx, namespace, name)
	if err != nil {
		return ReleaseDetail{}, err
	}
	d := ReleaseDetail{
		Release:            helmrel.View(cur),
		History:            make([]helmrel.Release, 0, len(hist)),
		Manifest:           helmrel.SafeManifest(cur.Manifest, namespace),
		Resources:          helmrel.Resources(cur.Manifest, namespace),
		ValuesNeedReason:   s.d.Guard.AccessNeedsReason(contextName),
		UserValuesSupplied: len(cur.Config) > 0,
	}
	for _, h := range hist {
		d.History = append(d.History, helmrel.View(h))
	}
	if cur.Info != nil {
		d.Notes = cur.Info.Notes
	}
	if ch := cur.Chart; ch != nil {
		d.Defaults, _ = helmrel.ValuesYAML(ch.Values)
		d.HasSchema = len(ch.Schema) > 0
		if ch.Metadata != nil {
			d.ChartDescription, d.ChartHome = ch.Metadata.Description, ch.Metadata.Home
		}
	}
	return d, nil
}

// Values returns the values the release was installed or upgraded with,
// after recording the access: they often hold passwords.
func (s *HelmService) Values(ctx context.Context, contextName, namespace, name, reason string) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	if _, err := helmTarget(contextName, namespace, name); err != nil {
		return "", err
	}
	cur, err := sess.Helm().HelmRelease(ctx, namespace, name, 0)
	if err != nil {
		return "", err
	}
	if err := s.d.Guard.RecordAccess(contextName, "helm-values-revealed", fmt.Sprintf("Helm release %s/%s", namespace, name), reason); err != nil {
		return "", err
	}
	return helmrel.ValuesYAML(cur.Config)
}

// PrepareUpgrade previews upgrading a release with edited values and the
// same chart. valuesYAML is the complete set of user-supplied values.
func (s *HelmService) PrepareUpgrade(ctx context.Context, contextName, namespace, name, valuesYAML string) (PlanView, error) {
	t, err := helmTarget(contextName, namespace, name)
	if err != nil {
		return PlanView{}, err
	}
	if len(valuesYAML) > maxValuesBytes {
		return PlanView{}, fmt.Errorf("values are limited to 1 MiB")
	}
	values, err := helmrel.ParseValues(valuesYAML)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareHelmUpgrade(ctx, t, values)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareRollback previews rolling a release back to an earlier revision.
func (s *HelmService) PrepareRollback(ctx context.Context, contextName, namespace, name string, revision int) (PlanView, error) {
	t, err := helmTarget(contextName, namespace, name)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareHelmRollback(ctx, t, revision)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareUninstall previews uninstalling a release.
func (s *HelmService) PrepareUninstall(ctx context.Context, contextName, namespace, name string) (PlanView, error) {
	t, err := helmTarget(contextName, namespace, name)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareHelmUninstall(ctx, t)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}
