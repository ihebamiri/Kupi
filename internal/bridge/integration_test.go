//go:build integration

package bridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/kubeconfig"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/trust"
)

const testContext = "k3d-kupi-test"

func connectedDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	ts, _ := trust.Open(filepath.Join(dir, "trust.json"))
	ps, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	mgr := kube.NewManager(kubeconfig.NewLoader(), ts, ps)
	if _, err := mgr.Connect(context.Background(), testContext); err != nil {
		t.Fatalf("connect (run `wails3 task cluster:up`): %v", err)
	}
	return Deps{Kube: mgr, Profiles: ps, Trust: ts}
}

func TestFormServiceAgainstK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s := NewFormService(connectedDeps(t))

	yaml, err := s.Object(ctx, testContext, ResourceRef{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "kube-system", Name: "coredns"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yaml, "kind: Deployment") {
		t.Errorf("object YAML missing kind:\n%s", yaml)
	}
	for _, noise := range []string{"managedFields", "resourceVersion", "\nstatus:", "uid:"} {
		if strings.Contains(yaml, noise) {
			t.Errorf("editable YAML should not contain %q", noise)
		}
	}

	parsed := s.FromYAML(yaml)
	if parsed.Error != "" {
		t.Fatalf("round trip parse: %s", parsed.Error)
	}
	back, err := s.ToYAML(parsed.JSON)
	if err != nil || back != yaml {
		t.Errorf("YAML -> JSON -> YAML should be stable (err %v)", err)
	}

	if _, err := s.Object(ctx, testContext, ResourceRef{Version: "v1", Resource: "secrets", Namespace: "kube-system", Name: "anything"}); err == nil {
		t.Error("Secrets must not load into the plain editor")
	}

	n, err := s.MatchingPods(ctx, testContext, "kube-system", map[string]string{"k8s-app": "kube-dns"})
	if err != nil || n < 1 {
		t.Errorf("coredns selector matched %d pods (%v)", n, err)
	}
	if _, err := s.MatchingPods(ctx, testContext, "kube-system", map[string]string{"bad key!": "x"}); err == nil {
		t.Error("invalid label key accepted")
	}

	tree, err := s.Schema(ctx, testContext, "", "v1", "Service")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(tree), &root); err != nil || root["type"] != "object" {
		t.Errorf("Service schema: %v %v", root["type"], err)
	}
	if _, err := s.Schema(ctx, testContext, "", "v1", "not-a-kind"); err == nil {
		t.Error("invalid kind accepted")
	}
}
