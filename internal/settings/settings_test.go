package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsWhenMissing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if got.Theme != "system" || got.ClipboardClearSeconds != 30 || got.TerminalEnabled || !got.TerminalPasteGuard {
		t.Errorf("defaults = %+v", got)
	}
}

func TestUpdateClampsAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, _ := Open(path)
	got, err := s.Update(func(x *Settings) {
		x.Theme = "dark"
		x.ClipboardClearSeconds = 100000 // must still clear
		x.VaultAutoLockMinutes = -5
		x.UIScale = 300
		x.Snippets = []Snippet{{Name: "pods", Command: "kubectl get pods"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ClipboardClearSeconds != 300 || got.VaultAutoLockMinutes != 1 || got.UIScale != 150 {
		t.Errorf("not clamped: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("settings file mode %v", mode)
	}
	again, _ := Open(path)
	if g := again.Get(); g.Theme != "dark" || len(g.Snippets) != 1 {
		t.Errorf("reloaded = %+v", g)
	}
}

func TestBadValuesAreRejected(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "settings.json"))
	for name, fn := range map[string]func(*Settings){
		"theme":   func(x *Settings) { x.Theme = "neon" },
		"shell":   func(x *Settings) { x.TerminalShell = `C:\Windows\System32\cmd.exe` },
		"snippet": func(x *Settings) { x.Snippets = []Snippet{{Name: "", Command: "ls"}} },
		"long": func(x *Settings) {
			x.Snippets = []Snippet{{Name: "x", Command: strings.Repeat("a", MaxSnippetCommand+1)}}
		},
	} {
		if _, err := s.Update(fn); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if s.Get().Theme != "system" {
		t.Error("a rejected update must not change the settings")
	}
}

func TestHandEditedFileIsClamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"clipboardClearSeconds": 999999, "terminalShell": "../../evil", "theme": "light"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g := s.Get()
	if g.ClipboardClearSeconds != 300 || g.TerminalShell != "" || g.Theme != "light" {
		t.Errorf("loaded = %+v", g)
	}
}
