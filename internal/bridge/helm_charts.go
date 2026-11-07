package bridge

import (
	"context"
	"fmt"

	"github.com/ihebamiri/kupi/internal/charts"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/helmrel"
	"github.com/ihebamiri/kupi/internal/validate"
)

// SearchCharts asks Artifact Hub for Helm charts.
func (s *HelmService) SearchCharts(ctx context.Context, query string) ([]charts.Package, error) {
	out, err := s.d.Charts.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []charts.Package{}
	}
	return out, nil
}

// ChartVersions lists a chart's published versions, newest first.
func (s *HelmService) ChartVersions(ctx context.Context, repo, chart string) ([]charts.Version, error) {
	out, err := s.d.Charts.Versions(ctx, charts.Source{Repo: repo, Chart: chart})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []charts.Version{}
	}
	return out, nil
}

// ChartInfo downloads one version of a chart and describes it, with its
// default values for the values editor.
func (s *HelmService) ChartInfo(ctx context.Context, repo, chart, version string) (charts.Info, error) {
	_, info, err := s.d.Charts.Load(ctx, charts.Source{Repo: repo, Chart: chart}, version)
	return info, err
}

// InstallRequest installs a chart from a repository as a new release.
type InstallRequest struct {
	Context         string `json:"context"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Repo            string `json:"repo"`
	Chart           string `json:"chart"`
	Version         string `json:"version"`
	Values          string `json:"values"`
	CreateNamespace bool   `json:"createNamespace"`
}

// PrepareInstall downloads the chart and previews installing it.
func (s *HelmService) PrepareInstall(ctx context.Context, req InstallRequest) (PlanView, error) {
	if err := validate.ContextName(req.Context); err != nil {
		return PlanView{}, err
	}
	if err := validate.Namespace(req.Namespace); err != nil {
		return PlanView{}, err
	}
	values, err := parseValues(req.Values)
	if err != nil {
		return PlanView{}, err
	}
	src := charts.Source{Repo: req.Repo, Chart: req.Chart}
	ch, _, err := s.d.Charts.Load(ctx, src, req.Version)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareHelmInstall(ctx, guard.HelmInstallRequest{
		Context: req.Context, Namespace: req.Namespace, Name: req.Name, Chart: ch,
		Source: req.Repo, Values: values, CreateNamespace: req.CreateNamespace,
	})
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareUpgradeChart previews upgrading a release to another chart version
// (or another chart) from a repository, with the given values.
func (s *HelmService) PrepareUpgradeChart(ctx context.Context, contextName, namespace, name, repo, chart, version, valuesYAML string) (PlanView, error) {
	t, err := helmTarget(contextName, namespace, name)
	if err != nil {
		return PlanView{}, err
	}
	values, err := parseValues(valuesYAML)
	if err != nil {
		return PlanView{}, err
	}
	ch, _, err := s.d.Charts.Load(ctx, charts.Source{Repo: repo, Chart: chart}, version)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareHelmUpgradeChart(ctx, t, values, ch, repo)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

func parseValues(text string) (map[string]any, error) {
	if len(text) > maxValuesBytes {
		return nil, fmt.Errorf("values are limited to 1 MiB")
	}
	return helmrel.ParseValues(text)
}
