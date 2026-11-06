package terminal

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSplitUTF8(t *testing.T) {
	euro := []byte("€") // 3 bytes
	for _, tc := range []struct {
		in          []byte
		whole, rest string
	}{
		{[]byte("plain"), "plain", ""},
		{append([]byte("a"), euro[:1]...), "a", string(euro[:1])},
		{append([]byte("a"), euro[:2]...), "a", string(euro[:2])},
		{append([]byte("a"), euro...), "a€", ""},
		{[]byte("é"), "é", ""},
	} {
		w, r := splitUTF8(tc.in)
		if string(w) != tc.whole || string(r) != tc.rest {
			t.Errorf("splitUTF8(%q) = %q, %q", tc.in, w, r)
		}
	}
}

// fakeProc is a process whose output is fed by the test.
type fakeProc struct {
	out    *io.PipeReader
	feed   *io.PipeWriter
	mu     sync.Mutex
	input  bytes.Buffer
	cols   uint16
	closed chan struct{}
	once   sync.Once
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()
	return &fakeProc{out: r, feed: w, closed: make(chan struct{})}
}

func (f *fakeProc) Read(b []byte) (int, error) { return f.out.Read(b) }
func (f *fakeProc) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.input.Write(b)
}
func (f *fakeProc) Resize(cols, _ uint16) error { f.cols = cols; return nil }
func (f *fakeProc) Wait() (int, error)          { <-f.closed; return 3, nil }
func (f *fakeProc) Close() error {
	f.once.Do(func() { close(f.closed); f.feed.Close() })
	return nil
}

func TestManagerStreamsAndReportsExit(t *testing.T) {
	var mu sync.Mutex
	var chunks []Chunk
	done := make(chan struct{})
	m := NewManager(func(c Chunk) {
		mu.Lock()
		chunks = append(chunks, c)
		mu.Unlock()
		if c.Exited {
			close(done)
		}
	})
	fp := newFakeProc()
	m.start = func(Shell, string, []string, uint16, uint16) (process, error) { return fp, nil }
	exited := false
	id, err := m.Open(OpenRequest{Shell: Shell{ID: "x"}, OnExit: func() { exited = true }})
	if err != nil {
		t.Fatal(err)
	}
	euro := []byte("€")
	_, _ = fp.feed.Write(append([]byte("hi "), euro[:1]...))
	_, _ = fp.feed.Write(euro[1:])
	if err := m.Write(id, "ls\r"); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(id, 120, 40); err != nil || fp.cols != 120 {
		t.Errorf("resize: %v cols=%d", err, fp.cols)
	}
	if err := m.Write(id, strings.Repeat("x", MaxInput+1)); err == nil {
		t.Error("oversized input accepted")
	}
	m.Close(id)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no exit reported")
	}
	mu.Lock()
	defer mu.Unlock()
	var text strings.Builder
	for _, c := range chunks {
		text.WriteString(c.Data)
	}
	if text.String() != "hi €" {
		t.Errorf("output = %q (a character split across reads must arrive whole)", text.String())
	}
	if last := chunks[len(chunks)-1]; !last.Exited || last.Code != 3 || last.ID != id {
		t.Errorf("last chunk = %+v", last)
	}
	if !exited {
		t.Error("OnExit not called")
	}
	if fp.input.String() != "ls\r" {
		t.Errorf("input = %q", fp.input.String())
	}
	if err := m.Write(id, "x"); err != ErrNotFound {
		t.Errorf("write after exit: %v", err)
	}
}

func TestShellsAreAbsoluteAndFindable(t *testing.T) {
	shells := Shells()
	if len(shells) == 0 {
		t.Skip("no shell on this machine")
	}
	for _, s := range shells {
		if !strings.Contains(s.Path, string(os.PathSeparator)) {
			t.Errorf("%s: path %q is not absolute", s.ID, s.Path)
		}
		for _, a := range s.args {
			switch strings.ToLower(a) {
			case "-c", "/c", "/k", "-command", "-encodedcommand", "-file":
				t.Errorf("%s: shell started with %q", s.ID, a)
			}
		}
	}
	if s, err := Find(""); err != nil || s.ID != shells[0].ID {
		t.Errorf("Find default: %v %v", s, err)
	}
	if _, err := Find("not-a-shell"); err == nil {
		t.Error("unknown shell found")
	}
}

// TestRealShell starts the system's own shell in a pseudo-terminal, types a
// command and reads its output.
func TestRealShell(t *testing.T) {
	id := "sh"
	cmd := "echo kupi-$((40+2))\r"
	want := "kupi-42"
	if runtime.GOOS == "windows" {
		id, cmd, want = "cmd", "echo kupi-%NUMBER_OF_PROCESSORS%-ok\r", "-ok"
	}
	sh, err := Find(id)
	if err != nil {
		t.Skipf("%s not available: %v", id, err)
	}
	var mu sync.Mutex
	var out strings.Builder
	exited := make(chan Chunk, 1)
	m := NewManager(func(c Chunk) {
		mu.Lock()
		out.WriteString(c.Data)
		mu.Unlock()
		if c.Exited {
			exited <- c
		}
	})
	tid, err := m.Open(OpenRequest{Shell: sh, Cols: 100, Rows: 30, Env: os.Environ(), Dir: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := m.Write(tid, cmd); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := out.String()
		mu.Unlock()
		if strings.Count(got, want) >= 1 && strings.Contains(got, "kupi-") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := m.Resize(tid, 120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
	_ = m.Write(tid, "exit\r")
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		m.Close(tid)
		t.Fatal("shell didn't exit")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(out.String(), want) {
		t.Errorf("output never showed %q:\n%s", want, out.String())
	}
	if m.Count() != 0 {
		t.Errorf("%d terminals still open", m.Count())
	}
}

// TestRealShellClose closes a shell that's still running.
func TestRealShellClose(t *testing.T) {
	sh, err := Find("")
	if err != nil {
		t.Skip(err)
	}
	exited := make(chan Chunk, 1)
	m := NewManager(func(c Chunk) {
		if c.Exited {
			exited <- c
		}
	})
	id, err := m.Open(OpenRequest{Shell: sh, Env: os.Environ(), Dir: os.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	m.Close(id)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s didn't stop after Close", sh.Name)
	}
}
