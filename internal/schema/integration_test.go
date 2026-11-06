//go:build integration

package schema_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/profile"
	kschema "github.com/ihebamiri/kupi/internal/schema"
	"github.com/ihebamiri/kupi/internal/trust"
)

func find(n *kschema.Node, name string) *kschema.Node {
	for _, f := range n.Fields {
		if f.Name == name {
			return f.Node
		}
	}
	return nil
}

func TestDeploymentSchemaFromK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dir := t.TempDir()
	ts, _ := trust.Open(filepath.Join(dir, "trust.json"))
	ps, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	s, err := kube.NewManager(kubeconfig.NewLoader(), ts, ps).Connect(ctx, "k3d-kupi-test")
	if err != nil {
		t.Fatalf("connect (run `wails3 task cluster:up`): %v", err)
	}
	doc, err := s.Reader().OpenAPI(ctx, schema.GroupVersion{Group: "apps", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := kschema.Build(doc, "apps", "v1", "Deployment")
	if err != nil {
		t.Fatal(err)
	}
	spec := find(root, "spec")
	if spec == nil || find(spec, "replicas") == nil || find(spec, "replicas").Type != "integer" {
		t.Fatal("spec.replicas missing or not an integer")
	}
	podSpec := find(find(find(spec, "template"), "spec"), "containers")
	if podSpec == nil || podSpec.Type != "array" || find(podSpec.Items, "image") == nil {
		t.Fatal("spec.template.spec.containers[].image missing")
	}
	limits := find(find(podSpec.Items, "resources"), "limits")
	if limits == nil || limits.Type != "map" || limits.Values.Type != "quantity" {
		t.Errorf("resources.limits = %+v", limits)
	}
	data, _ := json.Marshal(root)
	t.Logf("Deployment form tree: %d KB of JSON", len(data)/1024)
	if len(data) > 4<<20 {
		t.Errorf("form tree is %d bytes; too large to send to the UI", len(data))
	}

	// Cached on the second call.
	start := time.Now()
	if _, err := s.Reader().OpenAPI(ctx, schema.GroupVersion{Group: "apps", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("second schema fetch should come from the cache")
	}
}
