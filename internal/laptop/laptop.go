// Package laptop keeps local clusters from draining a laptop: it stops a
// k3d cluster after a while without Kupi using it, and remembers which
// clusters it stopped so connecting starts them again.
package laptop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/iheb-amiri/kupi/internal/appdir"
)

// MaxIdleMinutes is the longest idle timeout offered (one day).
const MaxIdleMinutes = 24 * 60

// Settings are one cluster's laptop-mode settings.
type Settings struct {
	// IdleStopMinutes stops the cluster after this long without use; 0 is off.
	IdleStopMinutes int `json:"idleStopMinutes"`
}

// Status is a cluster's settings plus what Kupi knows right now.
type Status struct {
	Settings
	// LastUsed is when Kupi last used the cluster (RFC 3339), if it has.
	LastUsed string `json:"lastUsed"`
	// StoppedIdle: Kupi stopped it for being idle and will start it again
	// when it's next connected.
	StoppedIdle bool `json:"stoppedIdle"`
}

// Hooks connect the manager to the rest of Kupi.
type Hooks struct {
	// Running reports whether a k3d cluster is running.
	Running func(ctx context.Context, name string) (bool, error)
	// Stop stops a k3d cluster.
	Stop func(ctx context.Context, name string) error
	// Busy reports work that counts as use even without clicks, such as
	// open port-forwards.
	Busy func(contextName string) bool
	// Stopped is told after an idle stop, for the audit log.
	Stopped func(name string, err error)
}

// Manager applies laptop mode.
type Manager struct {
	path  string
	hooks Hooks
	now   func() time.Time
	start time.Time

	mu       sync.Mutex
	settings map[string]Settings
	lastUsed map[string]time.Time
	stopped  map[string]bool

	cancel context.CancelFunc
	done   chan struct{}
}

// Open loads settings from path.
func Open(path string, hooks Hooks) (*Manager, error) {
	m := &Manager{path: path, hooks: hooks, now: time.Now, settings: map[string]Settings{}, lastUsed: map[string]time.Time{}, stopped: map[string]bool{}}
	m.start = m.now()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read laptop settings: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m.settings); err != nil {
			return nil, fmt.Errorf("read laptop settings: %w", err)
		}
	}
	return m, nil
}

func contextOf(name string) string { return "k3d-" + name }

// Touch records that Kupi used a context.
func (m *Manager) Touch(contextName string) {
	m.mu.Lock()
	m.lastUsed[contextName] = m.now()
	m.mu.Unlock()
}

// Status returns a cluster's settings and state.
func (m *Manager) Status(name string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Settings: m.settings[name], StoppedIdle: m.stopped[name]}
	if t, ok := m.lastUsed[contextOf(name)]; ok {
		st.LastUsed = t.UTC().Format(time.RFC3339)
	}
	return st
}

// SetIdleStop sets a cluster's idle timeout in minutes; 0 turns it off.
func (m *Manager) SetIdleStop(name string, minutes int) error {
	if minutes < 0 || minutes > MaxIdleMinutes {
		return fmt.Errorf("the idle time must be between 0 and %d minutes", MaxIdleMinutes)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings[name]
	s.IdleStopMinutes = minutes
	if minutes == 0 {
		delete(m.settings, name)
	} else {
		m.settings[name] = s
	}
	// Count from now, so turning it on never stops a cluster at once.
	m.lastUsed[contextOf(name)] = m.now()
	data, err := json.MarshalIndent(m.settings, "", "  ")
	if err != nil {
		return err
	}
	return appdir.WriteFileAtomic(m.path, data)
}

// ShouldWake reports whether connecting to a context should start its
// cluster first: Kupi stopped it for being idle.
func (m *Manager) ShouldWake(contextName string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.stopped {
		if contextOf(name) == contextName {
			return name, true
		}
	}
	return "", false
}

// Woke records that a cluster was started again.
func (m *Manager) Woke(name string) {
	m.mu.Lock()
	delete(m.stopped, name)
	m.lastUsed[contextOf(name)] = m.now()
	m.mu.Unlock()
}

// Run checks once a minute until Close.
func (m *Manager) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel, m.done = cancel, make(chan struct{})
	done := m.done
	m.mu.Unlock()
	go func() {
		defer close(done)
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Check(ctx)
			}
		}
	}()
}

// Check stops every cluster idle past its timeout. Run calls it each minute.
func (m *Manager) Check(ctx context.Context) {
	now := m.now()
	m.mu.Lock()
	var due []string
	for name, s := range m.settings {
		if s.IdleStopMinutes == 0 || m.stopped[name] {
			continue
		}
		last, ok := m.lastUsed[contextOf(name)]
		if !ok {
			last = m.start
		}
		if now.Sub(last) >= time.Duration(s.IdleStopMinutes)*time.Minute {
			due = append(due, name)
		}
	}
	m.mu.Unlock()
	for _, name := range due {
		if m.hooks.Busy != nil && m.hooks.Busy(contextOf(name)) {
			m.Touch(contextOf(name))
			continue
		}
		running, err := m.hooks.Running(ctx, name)
		if err != nil || !running {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err = m.hooks.Stop(sctx, name)
		cancel()
		if err == nil {
			m.mu.Lock()
			m.stopped[name] = true
			m.mu.Unlock()
		}
		if m.hooks.Stopped != nil {
			m.hooks.Stopped(name, err)
		}
	}
}

// Close stops the background check.
func (m *Manager) Close() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
