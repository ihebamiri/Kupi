//go:build integration

package vault

import "testing"

// TestOSKeyStore stores, reads and removes a throwaway entry in the real
// password store (Windows Credential Manager on the maintainer's machine).
func TestOSKeyStore(t *testing.T) {
	s := OSKeyStore{Key: "integration-test"}
	name, ok := s.Available()
	if !ok {
		t.Skip("no password store on this machine")
	}
	t.Logf("using %s", name)
	t.Cleanup(func() { _ = s.Delete() })
	if err := s.Set("AGE-SECRET-KEY-TEST"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get()
	if err != nil || got != "AGE-SECRET-KEY-TEST" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(); err == nil {
		t.Error("entry still present after Delete")
	}
}
