package bridge

import (
	"errors"
	"testing"
)

func TestRegistryOf(t *testing.T) {
	for ref, want := range map[string]string{
		"nginx:1.27":                   "docker.io",
		"library/nginx":                "docker.io",
		"bitnami/redis:7":              "docker.io",
		"ghcr.io/org/app:1.0":          "ghcr.io",
		"registry.example.com:5000/x":  "registry.example.com:5000",
		"localhost/app":                "localhost",
		"localhost:5000/app@sha256:ab": "localhost:5000",
	} {
		if got := registryOf(ref); got != want {
			t.Errorf("registryOf(%q) = %q, want %q", ref, got, want)
		}
	}
	if normalizeHost(" https://Index.Docker.io/ ") != "docker.io" {
		t.Error("Docker Hub aliases should become docker.io")
	}
	if got := registryItem("registry.example.com:5000"); got != "registry-registry.example.com-5000" {
		t.Errorf("item = %s", got)
	}
	if hostFromItem("registry-registry.example.com-5000", "Registry login for registry.example.com:5000") != "registry.example.com:5000" {
		t.Error("host lost its port")
	}
	for _, bad := range []string{"", "ghcr.io/org", "-bad.io", "a b", "host:999999"} {
		if registryHostRe.MatchString(normalizeHost(bad)) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAuthFailure(t *testing.T) {
	// Seen from Docker 29 pulling from a registry that wants a login.
	if !authFailure(errors.New(`failed to resolve reference "localhost:5000/kupi/busybox:1.36": pull access denied, repository does not exist or may require authorization: authorization failed: no basic auth credentials`)) {
		t.Error("a missing login wasn't recognised")
	}
	if authFailure(errors.New("dial tcp 127.0.0.1:5000: connection refused")) {
		t.Error("a network error was taken for a missing login")
	}
}
