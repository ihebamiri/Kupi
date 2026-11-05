package profile

import (
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		context, cluster, server string
		want                     Kind
	}{
		{"k3d-kupi-test", "k3d-kupi-test", "https://0.0.0.0:6550", Local},
		{"kind-dev", "kind-dev", "https://127.0.0.1:41234", Local},
		{"minikube", "minikube", "https://192.168.49.2:8443", Local},
		{"docker-desktop", "docker-desktop", "https://kubernetes.docker.internal:6443", Local},
		{"my-cluster", "my-cluster", "https://localhost:6443", Local},
		{"my-cluster", "my-cluster", "https://[::1]:6443", Local},
		{"shop-prod", "eks", "https://abc.eks.amazonaws.com", Production},
		{"payments-staging", "gke", "https://10.0.0.1", Staging},
		{"team-dev", "aks", "https://dev.example.com", Dev},
		{"devops-prod", "x", "https://10.0.0.1", Production},
		{"product-api", "x", "https://10.0.0.1", Production},
		{"mystery", "cluster-1", "https://10.0.0.1", Production},
	}
	for _, c := range cases {
		if got := Detect(c.context, c.cluster, c.server); got != c.want {
			t.Errorf("Detect(%q, %q, %q) = %s, want %s", c.context, c.cluster, c.server, got, c.want)
		}
	}
}

func TestRules(t *testing.T) {
	if r := RulesFor(Production); r.EditingOnByDefault || !r.RequireReason || !r.ConfirmByName {
		t.Errorf("production rules too loose: %+v", r)
	}
	if r := RulesFor("weird"); r != RulesFor(Production) {
		t.Error("unknown profiles must get production rules")
	}
	if r := RulesFor(Local); !r.EditingOnByDefault || r.RequireReason {
		t.Errorf("local rules: %+v", r)
	}
}

func TestStoreOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("mystery", Dev); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("x", "bogus"); err == nil {
		t.Error("invalid kind accepted")
	}
	s, _ = OpenStore(path)
	if k, overridden := s.Resolve("mystery", "c", "https://10.0.0.1"); k != Dev || !overridden {
		t.Errorf("resolve = %s, %v", k, overridden)
	}
	_ = s.Set("mystery", "")
	if k, overridden := s.Resolve("mystery", "c", "https://10.0.0.1"); k != Production || overridden {
		t.Errorf("after clearing: %s, %v", k, overridden)
	}
}
