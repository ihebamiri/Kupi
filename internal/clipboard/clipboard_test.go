package clipboard

import (
	"sync"
	"testing"
	"time"
)

type memBoard struct {
	mu   sync.Mutex
	text string
}

func (m *memBoard) SetText(t string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.text = t
	return true
}

func (m *memBoard) Text() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, true
}

func TestClearsOnlyItsOwnText(t *testing.T) {
	board := &memBoard{}
	var s Secure
	if err := s.Copy("x", time.Second); err != ErrUnavailable {
		t.Fatalf("copy without backend: %v", err)
	}
	s.Attach(board)
	if err := s.Copy("hunter2", 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if got, _ := board.Text(); got != "" {
		t.Errorf("clipboard not cleared: %q", got)
	}

	_ = s.Copy("hunter2", 20*time.Millisecond)
	board.SetText("something the user copied")
	time.Sleep(80 * time.Millisecond)
	if got, _ := board.Text(); got != "something the user copied" {
		t.Errorf("user's own copy was wiped")
	}

	_ = s.Copy("on-shutdown", time.Hour)
	s.Close()
	if got, _ := board.Text(); got != "" {
		t.Errorf("Close left the secret: %q", got)
	}
}
