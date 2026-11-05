package laptop

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleStopAndWake(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	var stopped []string
	busy := false
	path := filepath.Join(t.TempDir(), "laptop.json")
	m, err := Open(path, Hooks{
		Running: func(context.Context, string) (bool, error) { return true, nil },
		Stop:    func(_ context.Context, name string) error { stopped = append(stopped, name); return nil },
		Busy:    func(string) bool { return busy },
	})
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now }
	if err := m.SetIdleStop("dev", 30); err != nil {
		t.Fatal(err)
	}
	if err := m.SetIdleStop("dev", -1); err == nil {
		t.Error("negative idle time accepted")
	}

	now = now.Add(20 * time.Minute)
	m.Check(context.Background())
	m.Touch("k3d-dev")
	now = now.Add(29 * time.Minute)
	m.Check(context.Background())
	if len(stopped) != 0 {
		t.Fatalf("stopped too early: %v", stopped)
	}

	busy = true
	now = now.Add(2 * time.Minute)
	m.Check(context.Background())
	if len(stopped) != 0 {
		t.Fatal("stopped while a port-forward was open")
	}

	busy = false
	now = now.Add(31 * time.Minute)
	m.Check(context.Background())
	if len(stopped) != 1 || stopped[0] != "dev" {
		t.Fatalf("stopped = %v", stopped)
	}
	if name, ok := m.ShouldWake("k3d-dev"); !ok || name != "dev" {
		t.Error("idle-stopped cluster not marked to wake")
	}
	m.Woke("dev")
	if _, ok := m.ShouldWake("k3d-dev"); ok {
		t.Error("still marked after waking")
	}

	reopened, err := Open(path, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status("dev").IdleStopMinutes != 30 {
		t.Errorf("settings not saved: %+v", reopened.Status("dev"))
	}
}
