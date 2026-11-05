package engine

import (
	"errors"
	"testing"
)

func TestNewRefusesTCP(t *testing.T) {
	for _, ep := range []string{"tcp://0.0.0.0:2375", "http://localhost:2375", "https://example.com"} {
		if _, err := New(ep); !errors.Is(err, ErrTCPRefused) {
			t.Errorf("New(%q) = %v, want ErrTCPRefused", ep, err)
		}
	}
	if _, err := New("unix:///var/run/docker.sock"); err != nil {
		t.Errorf("unix endpoint refused: %v", err)
	}
	if _, err := New("nonsense"); err == nil {
		t.Error("malformed endpoint accepted")
	}
}

func TestSplitTag(t *testing.T) {
	cases := map[string][2]string{
		"nginx":                        {"nginx", "latest"},
		"nginx:1.27":                   {"nginx", "1.27"},
		"localhost:5000/app":           {"localhost:5000/app", "latest"},
		"localhost:5000/app:v2":        {"localhost:5000/app", "v2"},
		"ghcr.io/org/app@sha256:abc12": {"ghcr.io/org/app", "sha256:abc12"},
	}
	for in, want := range cases {
		img, tag := splitTag(in)
		if img != want[0] || tag != want[1] {
			t.Errorf("splitTag(%q) = %q, %q; want %q, %q", in, img, tag, want[0], want[1])
		}
	}
}
