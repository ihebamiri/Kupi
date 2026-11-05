package trust

import (
	"path/filepath"
	"testing"
)

func TestTrustIsPinnedToHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config")
	if s.IsTrusted(file, "aaa") {
		t.Fatal("nothing should be trusted yet")
	}
	if err := s.Trust(file, "aaa", []string{"aws eks get-token"}); err != nil {
		t.Fatal(err)
	}
	if !s.IsTrusted(file, "aaa") {
		t.Error("trusted file not recognised")
	}
	if s.IsTrusted(file, "bbb") {
		t.Error("changed file must not stay trusted")
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.IsTrusted(file, "aaa") {
		t.Error("trust not persisted")
	}
	if err := reopened.Revoke(file); err != nil {
		t.Fatal(err)
	}
	if reopened.IsTrusted(file, "aaa") {
		t.Error("revoked file still trusted")
	}
}
