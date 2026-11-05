// Package audit keeps an append-only, hash-chained log of every change Kupi
// makes to a cluster. Each entry records the hash of the previous one, so
// editing or deleting a past entry breaks the chain and Verify reports it.
//
// Entries never contain secret values: diffs are stored as a hash only.
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/iheb-amiri/kupi/internal/appdir"
)

// Outcome of an audited action.
const (
	OutcomeApplied = "applied"
	OutcomeFailed  = "failed"
	OutcomeInfo    = "info"
)

// Entry is one audited action.
type Entry struct {
	Seq        int64     `json:"seq"`
	Time       time.Time `json:"time"`
	Context    string    `json:"context"`
	Profile    string    `json:"profile"`
	OSUser     string    `json:"osUser"`
	KubeUser   string    `json:"kubeUser"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	Reason     string    `json:"reason,omitempty"`
	DiffSHA256 string    `json:"diffSha256,omitempty"`
	Outcome    string    `json:"outcome"`
	Error      string    `json:"error,omitempty"`
	Prev       string    `json:"prev"`
	Hash       string    `json:"hash"`
}

// Log is the audit log file.
type Log struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	lastSeq  int64
	lastHash string
}

// Open opens the audit log at path, reading the existing chain's tail.
func Open(path string) (*Log, error) {
	l := &Log{path: path, now: func() time.Time { return time.Now().UTC() }}
	entries, err := l.readAll()
	if err != nil {
		return nil, err
	}
	if n := len(entries); n > 0 {
		l.lastSeq = entries[n-1].Seq
		l.lastHash = entries[n-1].Hash
	}
	return l, nil
}

// Append adds an entry, filling in its sequence number, time and hashes.
func (l *Log) Append(e Entry) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Seq = l.lastSeq + 1
	e.Time = l.now()
	e.Prev = l.lastHash
	e.Hash = ""
	hash, err := hashEntry(e)
	if err != nil {
		return Entry{}, err
	}
	e.Hash = hash
	line, err := json.Marshal(e)
	if err != nil {
		return Entry{}, fmt.Errorf("encode audit entry: %w", err)
	}
	f, err := appdir.OpenAppend(l.path)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return Entry{}, fmt.Errorf("write audit entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Entry{}, fmt.Errorf("sync audit log: %w", err)
	}
	l.lastSeq = e.Seq
	l.lastHash = e.Hash
	return e, nil
}

// Recent returns up to limit entries, newest first.
func (l *Log) Recent(limit int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.readAll()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out, nil
}

// VerifyResult describes the state of the chain.
type VerifyResult struct {
	OK      bool
	Entries int
	// BrokenAt is the sequence number of the first entry that fails, or 0.
	BrokenAt int64
	Problem  string
}

// Verify recomputes every hash and checks every link in the chain.
func (l *Log) Verify() (VerifyResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := l.readAll()
	if err != nil {
		return VerifyResult{}, err
	}
	prev := ""
	for i, e := range entries {
		if e.Seq != int64(i+1) {
			return VerifyResult{Entries: len(entries), BrokenAt: e.Seq, Problem: "sequence numbers are not continuous"}, nil
		}
		if e.Prev != prev {
			return VerifyResult{Entries: len(entries), BrokenAt: e.Seq, Problem: "entry does not link to the previous one"}, nil
		}
		want := e.Hash
		e.Hash = ""
		got, err := hashEntry(e)
		if err != nil {
			return VerifyResult{}, err
		}
		if got != want {
			return VerifyResult{Entries: len(entries), BrokenAt: e.Seq, Problem: "entry was modified"}, nil
		}
		prev = want
	}
	return VerifyResult{OK: true, Entries: len(entries)}, nil
}

func (l *Log) readAll() ([]Entry, error) {
	f, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("audit log line %d is corrupt: %w", line, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	return entries, nil
}

func hashEntry(e Entry) (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("encode audit entry: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
