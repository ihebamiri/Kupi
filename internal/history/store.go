// Package history records what changes in a cluster while Kupi is connected:
// every change to the main resource types, with the tool that made it and a
// diff, and every warning event. Kubernetes forgets events after about an
// hour and keeps no history of changes at all, so this is what the incident
// view and the History page read.
//
// Records go to append-only JSON lines files, one per context per day, in
// the user's config dir, and are kept for 14 days.
package history

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iheb-amiri/kupi/internal/appdir"
)

const (
	// DefaultRetention is how long records are kept unless set otherwise.
	DefaultRetention = 14 * 24 * time.Hour
	// maxDayBytes caps one context's file for one day; past it, changes are
	// still recorded but without their diff.
	maxDayBytes = 64 << 20
	// maxLine is the longest record line read back.
	maxLine = 1 << 20
)

// Actions of a change.
const (
	ActionCreated = "created"
	ActionChanged = "changed"
	ActionDeleted = "deleted"
)

// Change is one observed change to an object.
type Change struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Action    string    `json:"action"`
	// Manager is the field manager that made the change, such as
	// kubectl-edit, helm, argocd-controller or kupi. Empty when unknown.
	Manager string `json:"manager"`
	// Diff is a unified YAML diff without status or server-managed fields.
	Diff string `json:"diff"`
	// Note explains a change without a diff.
	Note string `json:"note,omitempty"`
}

// Event is a warning event Kubernetes reported.
type Event struct {
	Time      time.Time `json:"time"`
	UID       string    `json:"uid"`
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Count     int64     `json:"count"`
}

type record struct {
	Change *Change `json:"change,omitempty"`
	Event  *Event  `json:"event,omitempty"`
}

// Query filters records. Empty fields match everything.
type Query struct {
	Since     time.Time
	Namespace string
	Kind      string
	Name      string
	Limit     int
}

func (q Query) matches(t time.Time, kind, ns, name string) bool {
	return !t.Before(q.Since) &&
		(q.Namespace == "" || q.Namespace == ns) &&
		(q.Kind == "" || strings.EqualFold(q.Kind, kind)) &&
		(q.Name == "" || q.Name == name)
}

// Store reads and writes the record files.
type Store struct {
	dir       string
	now       func() time.Time
	retention time.Duration

	mu    sync.Mutex
	files map[string]*dayFile
}

type dayFile struct {
	day  string
	f    *os.File
	size int64
}

// Open returns the store kept in dir and removes records past the retention.
func Open(dir string) *Store {
	s := &Store{dir: dir, now: time.Now, retention: DefaultRetention, files: map[string]*dayFile{}}
	s.Prune()
	return s
}

// contextDir names a context's folder by a hash, so any context name is safe.
func (s *Store) contextDir(contextName string) string {
	sum := sha256.Sum256([]byte(contextName))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:6]))
}

func dayOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

// AddChange records a change.
func (s *Store) AddChange(contextName string, c Change) error {
	return s.append(contextName, c.Time, record{Change: &c})
}

// AddEvent records a warning event.
func (s *Store) AddEvent(contextName string, e Event) error {
	return s.append(contextName, e.Time, record{Event: &e})
}

func (s *Store) append(contextName string, t time.Time, r record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := dayOf(t)
	df := s.files[contextName]
	if df == nil || df.day != day {
		if df != nil {
			_ = df.f.Close()
		}
		f, err := appdir.OpenAppend(filepath.Join(s.contextDir(contextName), day+".jsonl"))
		if err != nil {
			return err
		}
		size := int64(0)
		if fi, err := f.Stat(); err == nil {
			size = fi.Size()
		}
		df = &dayFile{day: day, f: f, size: size}
		s.files[contextName] = df
	}
	if df.size > maxDayBytes && r.Change != nil && r.Change.Diff != "" {
		c := *r.Change
		c.Diff, c.Note = "", "Diff not kept: today's history for this cluster is over 64 MB."
		r.Change = &c
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n, err := df.f.Write(line)
	df.size += int64(n)
	if err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}

// Changes returns matching changes, newest first.
func (s *Store) Changes(contextName string, q Query) ([]Change, error) {
	out := []Change{}
	err := s.scan(contextName, q, func(r record) bool {
		if c := r.Change; c != nil && q.matches(c.Time, c.Kind, c.Namespace, c.Name) {
			out = append(out, *c)
		}
		return true
	})
	// Records are read oldest first; reverse before the stable sort so
	// changes with the same timestamp still come out newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, err
}

// Events returns matching warning events, newest first, one per event (the
// latest sighting, with its highest count).
func (s *Store) Events(contextName string, q Query) ([]Event, error) {
	latest := map[string]Event{}
	err := s.scan(contextName, q, func(r record) bool {
		if e := r.Event; e != nil && q.matches(e.Time, e.Kind, e.Namespace, e.Name) {
			if prev, ok := latest[e.UID]; !ok || e.Time.After(prev.Time) {
				latest[e.UID] = *e
			}
		}
		return true
	})
	out := make([]Event, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, err
}

// scan reads every record of the days the query covers.
func (s *Store) scan(contextName string, q Query, fn func(record) bool) error {
	since := q.Since
	if oldest := s.now().Add(-s.Retention()); since.Before(oldest) {
		since = oldest
	}
	dir := s.contextDir(contextName)
	for d := since.UTC().Truncate(24 * time.Hour); !d.After(s.now().UTC()); d = d.Add(24 * time.Hour) {
		f, err := os.Open(filepath.Join(dir, dayOf(d)+".jsonl"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read history: %w", err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), maxLine)
		for sc.Scan() {
			var r record
			// A line being written right now may be incomplete; skip it.
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			if !fn(r) {
				break
			}
		}
		_ = f.Close()
	}
	return nil
}

// Prune deletes day files older than the retention.
func (s *Store) Prune() {
	cutoff := dayOf(s.now().Add(-s.Retention()))
	contexts, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, c := range contexts {
		if !c.IsDir() {
			continue
		}
		days, _ := os.ReadDir(filepath.Join(s.dir, c.Name()))
		for _, d := range days {
			if day := strings.TrimSuffix(d.Name(), ".jsonl"); day != d.Name() && day < cutoff {
				_ = os.Remove(filepath.Join(s.dir, c.Name(), d.Name()))
			}
		}
	}
}

// Close closes open files.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, df := range s.files {
		_ = df.f.Close()
		delete(s.files, k)
	}
}

// Retention returns how long records are kept.
func (s *Store) Retention() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retention
}

// SetRetention changes how long records are kept (at least a day) and
// removes what's now too old.
func (s *Store) SetRetention(d time.Duration) {
	if d < 24*time.Hour {
		d = 24 * time.Hour
	}
	s.mu.Lock()
	s.retention = d
	s.mu.Unlock()
	s.Prune()
}
