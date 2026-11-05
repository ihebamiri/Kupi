package diff

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	if got := Unified("a", "b", "x\ny\n", "x\ny\n", 3); got != "" {
		t.Errorf("equal inputs should give no diff, got %q", got)
	}
	a := "apiVersion: apps/v1\nkind: Deployment\nspec:\n  replicas: 2\n  paused: false\n"
	b := "apiVersion: apps/v1\nkind: Deployment\nspec:\n  replicas: 3\n  paused: false\n"
	got := Unified("live", "after change", a, b, 1)
	want := "--- live\n+++ after change\n@@ -3,3 +3,3 @@\n spec:\n-  replicas: 2\n+  replicas: 3\n   paused: false\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedCreateAndDelete(t *testing.T) {
	created := Unified("live", "after", "", "a\nb\n", 3)
	if !strings.Contains(created, "@@ -1,0 +1,2 @@\n+a\n+b\n") {
		t.Errorf("create diff:\n%s", created)
	}
	deleted := Unified("live", "after", "a\nb\n", "", 3)
	if !strings.Contains(deleted, "-a\n-b\n") {
		t.Errorf("delete diff:\n%s", deleted)
	}
}

func TestSeparateHunks(t *testing.T) {
	var a, b []string
	for i := 0; i < 20; i++ {
		a = append(a, "line")
		b = append(b, "line")
	}
	a[2], b[2] = "old1", "new1"
	a[17], b[17] = "old2", "new2"
	got := Unified("a", "b", strings.Join(a, "\n"), strings.Join(b, "\n"), 2)
	if n := strings.Count(got, "@@ -"); n != 2 {
		t.Errorf("want 2 hunks, got %d:\n%s", n, got)
	}
}
