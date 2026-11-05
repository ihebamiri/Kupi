// Package live runs the long-lived streams behind the UI: resource watches,
// log streams and interactive shells. Each stream has an ID, belongs to one
// cluster context, and pushes its data to the UI as events. Disconnecting a
// context stops all of its streams.
package live

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Event names sent to the UI.
const (
	EventChanged = "kupi:changed"
	EventLogs    = "kupi:logs"
	EventShell   = "kupi:shell"
)

const (
	watchThrottle  = 750 * time.Millisecond
	maxBackoff     = 30 * time.Second
	logFlushEvery  = 100 * time.Millisecond
	logBatchMax    = 1000
	maxLogLineSize = 16 * 1024
	maxShellInput  = 64 * 1024
)

// Changed tells the UI that a watched list changed and should be reloaded.
// Reset means the watch lost its place and the list must be reloaded fully.
type Changed struct {
	ID    string `json:"id"`
	Reset bool   `json:"reset"`
}

// LogLine is one line of a container log.
type LogLine struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Text      string `json:"text"`
}

// LogBatch carries new log lines, or the end of the stream.
type LogBatch struct {
	ID    string    `json:"id"`
	Lines []LogLine `json:"lines"`
	Done  bool      `json:"done"`
	Error string    `json:"error"`
}

// ShellChunk carries terminal output (base64, because a chunk can split a
// UTF-8 character), or the end of the session.
type ShellChunk struct {
	ID     string `json:"id"`
	Data   string `json:"data"`
	Closed bool   `json:"closed"`
	Error  string `json:"error"`
}

// Emitter sends an event to the UI.
type Emitter interface {
	Emit(name string, data any)
}

// LateEmitter forwards to a function set after construction, because the
// Wails app that emits events is created after the services that use it.
type LateEmitter struct {
	mu sync.RWMutex
	f  func(name string, data any)
}

// Set installs the emit function.
func (e *LateEmitter) Set(f func(name string, data any)) {
	e.mu.Lock()
	e.f = f
	e.mu.Unlock()
}

// Emit sends an event if the function is set.
func (e *LateEmitter) Emit(name string, data any) {
	e.mu.RLock()
	f := e.f
	e.mu.RUnlock()
	if f != nil {
		f(name, data)
	}
}

type stream struct {
	context string
	cancel  context.CancelFunc
	stdin   *io.PipeWriter
	resize  chan kube.TerminalSize
}

// Manager owns every running stream.
type Manager struct {
	emit Emitter

	mu      sync.Mutex
	streams map[string]*stream
}

// New returns a manager that sends events through emit.
func New(emit Emitter) *Manager {
	return &Manager{emit: emit, streams: map[string]*stream{}}
}

func (m *Manager) register(contextName string, s *stream) string {
	id := newID()
	s.context = contextName
	m.mu.Lock()
	m.streams[id] = s
	m.mu.Unlock()
	return id
}

func (m *Manager) remove(id string) *stream {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.streams[id]
	delete(m.streams, id)
	return s
}

// Stop ends one stream.
func (m *Manager) Stop(id string) {
	if s := m.remove(id); s != nil {
		s.cancel()
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
	}
}

// StopContext ends every stream of a context.
func (m *Manager) StopContext(contextName string) {
	m.mu.Lock()
	var ids []string
	for id, s := range m.streams {
		if s.context == contextName {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// StopAll ends every stream, for shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	var ids []string
	for id := range m.streams {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// Active reports how many streams are running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.streams)
}

// Watch tells the UI when a list changes. It watches metadata only and sends
// at most one Changed event per watchThrottle; the UI then reloads the list.
func (m *Manager) Watch(contextName string, r kube.Reader, gvr schema.GroupVersionResource, namespace, resourceVersion string) string {
	ctx, cancel := context.WithCancel(context.Background())
	id := m.register(contextName, &stream{cancel: cancel})
	go m.runWatch(ctx, id, r, gvr, namespace, resourceVersion)
	return id
}

func (m *Manager) runWatch(ctx context.Context, id string, r kube.Reader, gvr schema.GroupVersionResource, namespace, rv string) {
	backoff := time.Second
	var timer *time.Timer
	var fire <-chan time.Time
	notify := func(reset bool) {
		if reset {
			m.emit.Emit(EventChanged, Changed{ID: id, Reset: true})
			return
		}
		if timer == nil {
			timer = time.NewTimer(watchThrottle)
			fire = timer.C
		}
	}
	for ctx.Err() == nil {
		w, err := r.WatchMetadata(ctx, gvr, namespace, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true})
		if err != nil {
			if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
				rv = ""
				notify(true)
			}
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		rv = m.drain(ctx, w, rv, notify, &fire, &timer, id)
		w.Stop()
	}
}

// drain reads one watch until it closes, returning the latest resourceVersion.
func (m *Manager) drain(ctx context.Context, w watch.Interface, rv string, notify func(bool), fire *<-chan time.Time, timer **time.Timer, id string) string {
	for {
		select {
		case <-ctx.Done():
			return rv
		case <-*fire:
			*timer = nil
			*fire = nil
			m.emit.Emit(EventChanged, Changed{ID: id})
		case ev, ok := <-w.ResultChan():
			if !ok {
				return rv
			}
			switch ev.Type {
			case watch.Error:
				if status := apierrors.FromObject(ev.Object); apierrors.IsResourceExpired(status) || apierrors.IsGone(status) {
					notify(true)
					return ""
				}
				return rv
			case watch.Bookmark:
				rv = resourceVersionOf(ev, rv)
			default:
				rv = resourceVersionOf(ev, rv)
				notify(false)
			}
		}
	}
}

func resourceVersionOf(ev watch.Event, fallback string) string {
	if acc, err := meta.Accessor(ev.Object); err == nil && acc.GetResourceVersion() != "" {
		return acc.GetResourceVersion()
	}
	return fallback
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// LogTarget is one container to stream.
type LogTarget struct {
	Namespace string
	Pod       string
	Container string
}

// Logs streams one or more container logs, merged, in batches.
func (m *Manager) Logs(contextName string, r kube.Reader, targets []LogTarget, opts kube.LogOptions) string {
	ctx, cancel := context.WithCancel(context.Background())
	id := m.register(contextName, &stream{cancel: cancel})
	lines := make(chan LogLine, 1024)
	errs := make(chan error, len(targets))
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t LogTarget) {
			defer wg.Done()
			o := opts
			o.Container = t.Container
			rc, err := r.Logs(ctx, t.Namespace, t.Pod, o)
			if err != nil {
				errs <- fmt.Errorf("%s/%s: %w", t.Pod, t.Container, err)
				return
			}
			defer rc.Close()
			err = readLines(rc, func(text string) bool {
				select {
				case lines <- LogLine{Pod: t.Pod, Container: t.Container, Text: text}:
					return true
				case <-ctx.Done():
					return false
				}
			})
			if err != nil && ctx.Err() == nil {
				errs <- fmt.Errorf("%s/%s: %w", t.Pod, t.Container, err)
			}
		}(t)
	}
	go func() {
		wg.Wait()
		close(lines)
		close(errs)
	}()
	go m.pumpLogs(ctx, id, lines, errs)
	return id
}

func (m *Manager) pumpLogs(ctx context.Context, id string, lines <-chan LogLine, errs <-chan error) {
	ticker := time.NewTicker(logFlushEvery)
	defer ticker.Stop()
	var batch []LogLine
	flush := func() {
		if len(batch) > 0 {
			m.emit.Emit(EventLogs, LogBatch{ID: id, Lines: batch})
			batch = nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flush()
		case line, ok := <-lines:
			if !ok {
				flush()
				var msgs []string
				for err := range errs {
					msgs = append(msgs, err.Error())
				}
				m.emit.Emit(EventLogs, LogBatch{ID: id, Done: true, Error: strings.Join(msgs, "; ")})
				m.remove(id)
				return
			}
			batch = append(batch, line)
			if len(batch) >= logBatchMax {
				flush()
			}
		}
	}
}

// readLines calls emit for each line, truncating very long lines. It stops
// when emit returns false.
func readLines(r io.Reader, emit func(string) bool) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var line []byte
	truncated := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 && !truncated {
			room := maxLogLineSize - len(line)
			if len(chunk) > room {
				line = append(line, chunk[:room]...)
				truncated = true
			} else {
				line = append(line, chunk...)
			}
		}
		if err != nil {
			if len(line) > 0 {
				emit(finishLine(line, truncated))
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if isPrefix {
			continue
		}
		if !emit(finishLine(line, truncated)) {
			return nil
		}
		line = line[:0]
		truncated = false
	}
}

func finishLine(b []byte, truncated bool) string {
	s := strings.ToValidUTF8(strings.TrimSuffix(string(b), "\r"), "�")
	if truncated {
		s += " …[line truncated]"
	}
	return s
}

// ShellRunner runs a shell attached to streams until it exits.
type ShellRunner func(ctx context.Context, streams kube.ExecStreams) error

// Shell starts an interactive terminal session.
func (m *Manager) Shell(contextName string, run ShellRunner, cols, rows uint16) string {
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	resize := make(chan kube.TerminalSize, 8)
	resize <- kube.TerminalSize{Width: cols, Height: rows}
	id := m.register(contextName, &stream{cancel: cancel, stdin: pw, resize: resize})
	out := &shellWriter{emit: m.emit, id: id}
	go func() {
		err := run(ctx, kube.ExecStreams{Stdin: pr, Stdout: out, TTY: true, Resize: resize})
		msg := ""
		if err != nil && ctx.Err() == nil {
			msg = err.Error()
		}
		m.emit.Emit(EventShell, ShellChunk{ID: id, Closed: true, Error: msg})
		_ = pr.Close()
		m.Stop(id)
	}()
	return id
}

// ShellInput sends keystrokes to a session.
func (m *Manager) ShellInput(id string, data []byte) error {
	if len(data) > maxShellInput {
		return fmt.Errorf("input is too large")
	}
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s == nil || s.stdin == nil {
		return fmt.Errorf("the shell session has ended")
	}
	_, err := s.stdin.Write(data)
	return err
}

// ShellResize tells a session the terminal's new size.
func (m *Manager) ShellResize(id string, cols, rows uint16) {
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s == nil || s.resize == nil {
		return
	}
	select {
	case s.resize <- kube.TerminalSize{Width: cols, Height: rows}:
	default:
	}
}

type shellWriter struct {
	emit Emitter
	id   string
}

func (w *shellWriter) Write(p []byte) (int, error) {
	w.emit.Emit(EventShell, ShellChunk{ID: w.id, Data: base64.StdEncoding.EncodeToString(p)})
	return len(p), nil
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
