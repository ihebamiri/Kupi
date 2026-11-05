package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChainVerifiesAndDetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"apply", "delete", "scale"} {
		if _, err := log.Append(Entry{Context: "k3d-dev", Action: action, Resource: "ConfigMap default/demo", Outcome: OutcomeApplied}); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := log.Verify(); err != nil || !v.OK || v.Entries != 3 {
		t.Fatalf("verify = %+v, %v", v, err)
	}

	// Reopening continues the same chain.
	log, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := log.Append(Entry{Action: "restart", Outcome: OutcomeApplied}); err != nil || e.Seq != 4 {
		t.Fatalf("append after reopen: seq %d, %v", e.Seq, err)
	}

	data, _ := os.ReadFile(path)
	tampered := strings.Replace(string(data), `"action":"delete"`, `"action":"apply"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := log.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || v.BrokenAt != 2 {
		t.Errorf("tampered log: %+v, want broken at 2", v)
	}

	recent, _ := log.Recent(2)
	if len(recent) != 2 || recent[0].Seq != 4 {
		t.Errorf("recent = %+v, want newest first", recent)
	}
}

func TestDeletedEntryBreaksChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	log, _ := Open(path)
	for i := 0; i < 3; i++ {
		_, _ = log.Append(Entry{Action: "apply", Outcome: OutcomeApplied})
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, path)), "\n")
	_ = os.WriteFile(path, []byte(lines[0]+"\n"+lines[2]+"\n"), 0o600)
	if v, _ := log.Verify(); v.OK {
		t.Error("removing an entry should break the chain")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
