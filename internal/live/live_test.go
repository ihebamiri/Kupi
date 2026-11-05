package live

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/iheb-amiri/kupi/internal/kube"
)

type recorder struct {
	mu     sync.Mutex
	events []any
}

func (r *recorder) Emit(_ string, data any) {
	r.mu.Lock()
	r.events = append(r.events, data)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]any(nil), r.events...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// fakeReader serves logs and a scripted watch.
type fakeReader struct {
	kube.Reader
	logs  map[string]string
	watch *watch.FakeWatcher
}

func (f *fakeReader) Logs(_ context.Context, _, pod string, _ kube.LogOptions) (io.ReadCloser, error) {
	text, ok := f.logs[pod]
	if !ok {
		return nil, errors.New("pod not found")
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func (f *fakeReader) WatchMetadata(context.Context, schema.GroupVersionResource, string, metav1.ListOptions) (watch.Interface, error) {
	return f.watch, nil
}

func TestLogsMergeAndFinish(t *testing.T) {
	rec := &recorder{}
	m := New(rec)
	r := &fakeReader{logs: map[string]string{"a": "one\ntwo\r\n", "b": "three"}}
	m.Logs("ctx", r, []LogTarget{{Pod: "a"}, {Pod: "b"}, {Pod: "missing"}}, kube.LogOptions{})

	var done LogBatch
	waitFor(t, func() bool {
		for _, e := range rec.snapshot() {
			if b, ok := e.(LogBatch); ok && b.Done {
				done = b
				return true
			}
		}
		return false
	})
	var texts []string
	for _, e := range rec.snapshot() {
		if b, ok := e.(LogBatch); ok {
			for _, l := range b.Lines {
				texts = append(texts, l.Pod+":"+l.Text)
			}
		}
	}
	joined := strings.Join(texts, ",")
	for _, want := range []string{"a:one", "a:two", "b:three"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	if !strings.Contains(done.Error, "missing") {
		t.Errorf("done error = %q, want the failing pod named", done.Error)
	}
	if m.Active() != 0 {
		t.Error("finished log stream should be removed")
	}
}

func TestReadLinesTruncatesHugeLines(t *testing.T) {
	huge := strings.Repeat("x", maxLogLineSize*3)
	var got []string
	if err := readLines(strings.NewReader(huge+"\nnext\n"), func(s string) bool { got = append(got, s); return true }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasSuffix(got[0], "[line truncated]") || got[1] != "next" {
		t.Errorf("got %d lines, first ends %q", len(got), got[0][max(0, len(got[0])-20):])
	}
}

func TestWatchThrottlesAndStops(t *testing.T) {
	rec := &recorder{}
	m := New(rec)
	fw := watch.NewFake()
	id := m.Watch("ctx", &fakeReader{watch: fw}, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "", "")

	obj := func(rv string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetResourceVersion(rv)
		return u
	}
	for i := 0; i < 20; i++ {
		fw.Add(obj("1"))
	}
	waitFor(t, func() bool { return len(rec.snapshot()) >= 1 })
	time.Sleep(watchThrottle + 200*time.Millisecond)
	if n := len(rec.snapshot()); n != 1 {
		t.Errorf("20 rapid changes produced %d events, want 1", n)
	}
	m.StopContext("ctx")
	if m.Active() != 0 {
		t.Error("StopContext should stop the watch")
	}
	_ = id
}

func TestShellRoundTrip(t *testing.T) {
	rec := &recorder{}
	m := New(rec)
	id := m.Shell("ctx", func(ctx context.Context, s kube.ExecStreams) error {
		buf := make([]byte, 5)
		if _, err := io.ReadFull(s.Stdin, buf); err != nil {
			return err
		}
		_, _ = s.Stdout.Write([]byte("echo:" + string(buf)))
		return nil
	}, 80, 24)
	if err := m.ShellInput(id, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, e := range rec.snapshot() {
			if c, ok := e.(ShellChunk); ok && c.Closed {
				return true
			}
		}
		return false
	})
	if err := m.ShellInput(id, []byte("x")); err == nil {
		t.Error("input after the session ended should fail")
	}
}
