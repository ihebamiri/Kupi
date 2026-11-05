// Package clipboard copies sensitive text to the system clipboard and wipes
// it again after a delay, unless the user has copied something else since.
// The text goes from Go straight to the clipboard; the UI never holds it.
package clipboard

import (
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

// Backend is the system clipboard.
type Backend interface {
	SetText(text string) bool
	Text() (string, bool)
}

// ErrUnavailable is returned when no clipboard is attached or it refused.
var ErrUnavailable = errors.New("the clipboard isn't available")

// Secure is the clipboard for secrets. Its zero value is ready to use once a
// backend is attached.
type Secure struct {
	mu      sync.Mutex
	backend Backend
	timer   *time.Timer
	digest  [32]byte
	armed   bool
}

// Attach sets the system clipboard. It is called once the app exists.
func (s *Secure) Attach(b Backend) {
	s.mu.Lock()
	s.backend = b
	s.mu.Unlock()
}

// Copy puts text on the clipboard and clears it after clearAfter. Only a
// hash of the text is kept, to recognise it when clearing.
//
// The lock is never held while the backend runs: the desktop clipboard
// waits for the UI thread, and the UI thread calls Close on shutdown.
func (s *Secure) Copy(text string, clearAfter time.Duration) error {
	s.mu.Lock()
	b := s.backend
	s.mu.Unlock()
	if b == nil || !b.SetText(text) {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.digest = sha256.Sum256([]byte(text))
	s.armed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(clearAfter, s.clear)
	return nil
}

// Close stops the timer and clears the clipboard now if it still holds a
// copied secret. Called on shutdown.
func (s *Secure) Close() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	s.clear()
}

func (s *Secure) clear() {
	s.mu.Lock()
	if !s.armed || s.backend == nil {
		s.mu.Unlock()
		return
	}
	b, digest := s.backend, s.digest
	s.armed, s.digest = false, [32]byte{}
	s.mu.Unlock()
	if current, ok := b.Text(); ok && sha256.Sum256([]byte(current)) == digest {
		b.SetText("")
	}
}
