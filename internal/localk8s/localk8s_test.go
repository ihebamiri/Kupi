package localk8s

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTargetOf(t *testing.T) {
	k3d, err := TargetOf("k3d-dev")
	if err != nil || k3d.Tool != "k3d" || k3d.NodeLabel != "k3d.cluster=dev" {
		t.Errorf("k3d: %+v %v", k3d, err)
	}
	kind, err := TargetOf("kind-test")
	if err != nil || kind.Tool != "kind" || kind.NodeLabel != "io.x-k8s.kind.cluster=test" {
		t.Errorf("kind: %+v %v", kind, err)
	}
	if _, err := TargetOf("prod-eks"); !errors.Is(err, ErrNotLocal) {
		t.Errorf("remote cluster: %v", err)
	}
	if _, err := TargetOf("k3d--evil"); err == nil {
		t.Error("invalid cluster name accepted")
	}
}

func TestNormalizeRef(t *testing.T) {
	cases := map[string]string{
		"busybox":                "docker.io/library/busybox:latest",
		"kupi-local/demo:1":      "docker.io/kupi-local/demo:1",
		"ghcr.io/org/app:v2":     "ghcr.io/org/app:v2",
		"localhost:5000/app":     "localhost:5000/app:latest",
		"nginx@sha256:" + "ab12": "docker.io/library/nginx@sha256:ab12",
	}
	for in, want := range cases {
		if got := normalizeRef(in); got != want {
			t.Errorf("normalizeRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidationRejectsInjection(t *testing.T) {
	for _, bad := range []string{"--help", "-c=other", "nginx; rm -rf /", "nginx latest", "Nginx", "a/../b", ""} {
		if ValidImage(bad) == nil {
			t.Errorf("image %q accepted", bad)
		}
	}
	for _, good := range []string{"nginx", "nginx:1.27", "ghcr.io/org/app:v1.0.0", "localhost:5000/team/app"} {
		if err := ValidImage(good); err != nil {
			t.Errorf("image %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"-x", "Dev", "a_b", strings.Repeat("a", 40), ""} {
		if ValidName(bad) == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := CreateK3d(context.Background(), CreateOptions{Name: "ok", Image: "evil/image:latest"}); err == nil {
		t.Error("arbitrary k3s image accepted")
	}
}
