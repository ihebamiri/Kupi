package kube

import (
	"path/filepath"
	"testing"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

// TestDemoChartLoads checks the demo chart in docs/testdata stays valid.
func TestDemoChartLoads(t *testing.T) {
	ch, err := loader.Load(filepath.Join("..", "..", "docs", "testdata", "charts", "demo-web"))
	if err != nil {
		t.Fatal(err)
	}
	if ch.Metadata.Name != "demo-web" || len(ch.Templates) != 4 || len(ch.Schema) == 0 || ch.Values["replicas"] == nil {
		t.Errorf("chart = %+v, %d templates", ch.Metadata, len(ch.Templates))
	}
}
