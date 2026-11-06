package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/settings"
	"github.com/ihebamiri/kupi/internal/terminal"
)

func settingsDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	log, err := audit.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Settings: st, Audit: log, ConfigDir: dir, Terminals: terminal.NewManager(func(terminal.Chunk) {}), Native: &Native{}}
}

func TestTerminalIsOffUntilConfirmedNatively(t *testing.T) {
	d := settingsDeps(t)
	term := NewTerminalService(d)
	if _, err := term.Open("", "", "", 80, 24); !errors.Is(err, ErrTerminalOff) {
		t.Fatalf("open while off: %v", err)
	}

	svc := NewSettingsService(d)
	next := d.Settings.Get()
	next.TerminalEnabled = true
	saved, err := svc.Save(next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TerminalEnabled {
		t.Fatal("Save switched the terminal on; only EnableTerminal may")
	}

	// No native dialog (or the user said no): stays off.
	if on, err := svc.EnableTerminal(); on || err != nil {
		t.Fatalf("enable without a dialog: %v %v", on, err)
	}
	asked := ""
	d.Native.Set(func(title, _, _ string) bool { asked = title; return true }, nil)
	if on, err := svc.EnableTerminal(); !on || err != nil {
		t.Fatalf("enable after yes: %v %v", on, err)
	}
	if !strings.Contains(asked, "terminal") || !d.Settings.Get().TerminalEnabled {
		t.Errorf("asked %q, enabled %v", asked, d.Settings.Get().TerminalEnabled)
	}

	// Switching it off works through Save, and is audited.
	next = d.Settings.Get()
	next.TerminalEnabled = false
	if saved, _ := svc.Save(next); saved.TerminalEnabled {
		t.Error("couldn't switch the terminal off")
	}
	entries, _ := d.Audit.Recent(10)
	var actions []string
	for _, e := range entries {
		actions = append(actions, e.Action)
	}
	if got := strings.Join(actions, ","); !strings.Contains(got, "terminal-enabled") || !strings.Contains(got, "terminal-disabled") {
		t.Errorf("audit = %s", got)
	}
}

func TestTerminalOverlayHoldsNoCredentials(t *testing.T) {
	d := settingsDeps(t)
	s := NewTerminalService(d)
	path, err := s.writeOverlay("k3d-kupi-test", "k3d-kupi-test", "admin@k3d-kupi-test", "kupi-demo")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Current  string `json:"current-context"`
		Contexts []struct {
			Name    string            `json:"name"`
			Context map[string]string `json:"context"`
		} `json:"contexts"`
		Clusters []interface{} `json:"clusters"`
		Users    []interface{} `json:"users"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Current != "k3d-kupi-test" || len(cfg.Contexts) != 1 || cfg.Contexts[0].Context["namespace"] != "kupi-demo" || cfg.Contexts[0].Context["user"] != "admin@k3d-kupi-test" {
		t.Errorf("overlay = %s", data)
	}
	if len(cfg.Clusters) != 0 || len(cfg.Users) != 0 {
		t.Errorf("the overlay must not copy clusters or credentials:\n%s", data)
	}
	if filepath.Dir(path) != filepath.Join(d.ConfigDir, "terminal") {
		t.Errorf("overlay written to %s", path)
	}
	CleanTerminalFiles(d.ConfigDir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("CleanTerminalFiles left the overlay")
	}
}

func TestTerminalInputValidation(t *testing.T) {
	s := NewTerminalService(settingsDeps(t))
	if err := s.Input("../../etc", "ls"); err == nil {
		t.Error("bad id accepted")
	}
	if err := s.Input(strings.Repeat("a", 24), "ls"); err == nil {
		t.Error("input to a terminal that doesn't exist accepted")
	}
	if _, err := s.Open("../evil", "", "", 80, 24); err == nil {
		t.Error("bad shell id accepted")
	}
}

func TestVersionSkew(t *testing.T) {
	if versionSkew("v1.36.1", "v1.35.5+k3s1") != "" {
		t.Error("one minor version apart is supported")
	}
	if msg := versionSkew("v1.31.0", "v1.35.5+k3s1"); !strings.Contains(msg, "1.31") {
		t.Errorf("skew message = %q", msg)
	}
	if versionSkew("", "v1.35") != "" {
		t.Error("unknown version must not warn")
	}
}

func TestBaseEnvDropsKupiAndKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/x")
	t.Setenv("KUPI_DEV_AUTOCONFIRM", "1")
	t.Setenv("KUPI_CONFIG_DIR", "/tmp/y")
	for _, kv := range baseEnv() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "KUBECONFIG" || k == "KUPI_DEV_AUTOCONFIRM" || k == "KUPI_CONFIG_DIR" {
			t.Errorf("%s leaked into the terminal", k)
		}
	}
}
