// Package terminal runs the integrated terminal: the user's own interactive
// shell in a pseudo-terminal (ConPTY on Windows, /dev/ptmx on Linux).
//
// This is the one deliberate exception to "never run a shell" (see
// CLAUDE.md, rules 3 and 6). It is bounded: only shells this package detects
// can start, by absolute path and with fixed arguments, never with a command
// string (-c, /c, -Command); the bridge only opens one when the user has
// switched the terminal on in Settings, confirmed in a native dialog.
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Shell is an interactive shell the terminal can start.
type Shell struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Path string   `json:"path"`
	args []string // fixed; never a command to run
}

// Chunk is terminal output (or its end) sent to the UI.
type Chunk struct {
	ID     string `json:"id"`
	Data   string `json:"data"`
	Exited bool   `json:"exited"`
	Code   int    `json:"code"`
	Error  string `json:"error"`
}

// EventName is the UI event that carries Chunks.
const EventName = "kupi:terminal"

// Limits.
const (
	MaxSessions = 12
	MaxInput    = 64 << 10
	maxDim      = 1000
)

// ErrNotFound is returned for an unknown or closed terminal.
var ErrNotFound = errors.New("this terminal is closed")

// process is a running shell attached to a pseudo-terminal.
type process interface {
	io.ReadWriter
	Resize(cols, rows uint16) error
	// Wait blocks until the shell exits and returns its exit code.
	Wait() (int, error)
	// Close stops the shell (if it's still running) and frees the terminal.
	Close() error
}

// OpenRequest starts a terminal.
type OpenRequest struct {
	Shell Shell
	Cols  int
	Rows  int
	Dir   string
	// Env is the full environment for the shell.
	Env []string
	// OnExit runs once after the shell has exited and the terminal is closed.
	OnExit func()
}

type session struct {
	id     string
	proc   process
	onExit func()
	mu     sync.Mutex // serialises writes
}

// Manager owns the open terminals.
type Manager struct {
	emit  func(Chunk)
	start func(sh Shell, dir string, env []string, cols, rows uint16) (process, error)

	mu       sync.Mutex
	sessions map[string]*session
}

// NewManager returns a manager that sends output through emit.
func NewManager(emit func(Chunk)) *Manager {
	return &Manager{emit: emit, start: startShell, sessions: map[string]*session{}}
}

func dim(n int, def uint16) uint16 {
	if n <= 0 {
		return def
	}
	if n > maxDim {
		return maxDim
	}
	return uint16(n)
}

// Open starts req.Shell and streams its output. It returns the terminal id.
func (m *Manager) Open(req OpenRequest) (string, error) {
	m.mu.Lock()
	if len(m.sessions) >= MaxSessions {
		m.mu.Unlock()
		return "", fmt.Errorf("at most %d terminals can be open; close one first", MaxSessions)
	}
	m.mu.Unlock()

	proc, err := m.start(req.Shell, req.Dir, req.Env, dim(req.Cols, 80), dim(req.Rows, 24))
	if err != nil {
		return "", fmt.Errorf("start %s: %w", req.Shell.Name, err)
	}
	s := &session{id: newID(), proc: proc, onExit: req.OnExit}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	go m.pump(s)
	return s.id, nil
}

// pump forwards output until the shell exits, then reports the exit.
func (m *Manager) pump(s *session) {
	buf := make([]byte, 32<<10)
	var carry []byte
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			data := append(carry, buf[:n]...)
			whole, rest := splitUTF8(data)
			carry = append([]byte(nil), rest...)
			if len(whole) > 0 {
				m.emit(Chunk{ID: s.id, Data: string(whole)})
			}
		}
		if err != nil {
			break
		}
	}
	if len(carry) > 0 {
		m.emit(Chunk{ID: s.id, Data: strings.ToValidUTF8(string(carry), "�")})
	}
	code, werr := waitWithTimeout(s.proc, 5*time.Second)
	_ = s.proc.Close()
	m.mu.Lock()
	delete(m.sessions, s.id)
	m.mu.Unlock()
	if s.onExit != nil {
		s.onExit()
	}
	c := Chunk{ID: s.id, Exited: true, Code: code}
	if werr != nil {
		c.Error = werr.Error()
	}
	m.emit(c)
}

func waitWithTimeout(p process, d time.Duration) (int, error) {
	type result struct {
		code int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		code, err := p.Wait()
		ch <- result{code, err}
	}()
	select {
	case r := <-ch:
		return r.code, r.err
	case <-time.After(d):
		_ = p.Close()
		return -1, nil
	}
}

func (m *Manager) get(id string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// Write sends keystrokes or pasted text to a terminal.
func (m *Manager) Write(id, data string) error {
	if len(data) > MaxInput {
		return fmt.Errorf("input is larger than %d bytes", MaxInput)
	}
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = io.WriteString(s.proc, data)
	return err
}

// Resize changes a terminal's size.
func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.proc.Resize(dim(cols, 80), dim(rows, 24))
}

// Close stops a terminal's shell. Its exit is reported like any other.
func (m *Manager) Close(id string) {
	if s, err := m.get(id); err == nil {
		_ = s.proc.Close()
	}
}

// CloseAll stops every terminal, for example when the app quits or the
// terminal is switched off.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		_ = s.proc.Close()
	}
}

// Count returns how many terminals are open.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// splitUTF8 returns the longest prefix of b that doesn't end inside a
// multi-byte character, and the incomplete rest (at most 3 bytes).
func splitUTF8(b []byte) (whole, rest []byte) {
	for i := 1; i <= 3 && i <= len(b); i++ {
		c := b[len(b)-i]
		if c < 0x80 {
			break // ASCII: nothing pending
		}
		if c >= 0xC0 { // start byte of a multi-byte rune
			need := 2
			switch {
			case c >= 0xF0:
				need = 4
			case c >= 0xE0:
				need = 3
			}
			if i < need && !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i], b[len(b)-i:]
			}
			break
		}
	}
	return b, nil
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
