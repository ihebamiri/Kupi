// Package trust records which kubeconfig files the user has approved.
//
// A kubeconfig credential plugin runs an arbitrary command when a client
// connects, so loading a kubeconfig someone sent you can run their code.
// Kupi shows the command first and only connects once the user trusts that
// exact file. Trust is pinned to the file's SHA-256: any change to the file
// asks again.
package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ihebamiri/kupi/internal/appdir"
)

// Entry is one trusted kubeconfig file.
type Entry struct {
	File      string    `json:"file"`
	SHA256    string    `json:"sha256"`
	Commands  []string  `json:"commands"`
	TrustedAt time.Time `json:"trustedAt"`
}

// Store is the on-disk list of trusted kubeconfig files.
type Store struct {
	path    string
	mu      sync.Mutex
	entries map[string]Entry
}

// Open loads the store from path, starting empty if the file does not exist.
func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read trust store: %w", err)
	}
	var list []Entry
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse trust store: %w", err)
	}
	for _, e := range list {
		s.entries[key(e.File)] = e
	}
	return s, nil
}

// IsTrusted reports whether file is trusted with exactly this content hash.
func (s *Store) IsTrusted(file, sha string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key(file)]
	return ok && e.SHA256 == sha
}

// Trust records file as trusted at the given content hash.
func (s *Store) Trust(file, sha string, commands []string) error {
	if file == "" || sha == "" {
		return fmt.Errorf("file and hash are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key(file)] = Entry{File: file, SHA256: sha, Commands: commands, TrustedAt: time.Now().UTC()}
	return s.saveLocked()
}

// Revoke forgets a trusted file.
func (s *Store) Revoke(file string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key(file))
	return s.saveLocked()
}

// Entries returns all trusted files.
func (s *Store) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out
}

func (s *Store) saveLocked() error {
	list := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trust store: %w", err)
	}
	return appdir.WriteFileAtomic(s.path, data)
}

// key normalises a path so the same file always maps to one entry. Windows
// paths are case-insensitive.
func key(file string) string {
	clean := filepath.Clean(file)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}
