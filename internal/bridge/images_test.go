package bridge

import "testing"

func TestLookalike(t *testing.T) {
	official := []string{"nginx", "redis", "postgres"}
	cases := map[string]string{
		"nginx":           "",      // the official image itself
		"bitnami/nginx":   "nginx", // same name under another publisher
		"someone/nignx":   "nginx", // transposed letters
		"someone/postgre": "postgres",
		"someone/myapp":   "",
		"someone/reds":    "redis",
	}
	for name, want := range cases {
		if got := lookalike(name, official); got != want {
			t.Errorf("lookalike(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidImageOrID(t *testing.T) {
	if err := validImageOrID("sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err != nil {
		t.Errorf("image id rejected: %v", err)
	}
	if err := validImageOrID("sha256:not hex!"); err == nil {
		t.Error("bad id accepted")
	}
	if err := validImageOrID("--force"); err == nil {
		t.Error("flag-like reference accepted")
	}
}
