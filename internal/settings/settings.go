// Package settings keeps the user's preferences in settings.json in Kupi's
// config dir. Every value is checked and clamped to a safe range when it's
// loaded or saved, so a hand-edited file can't switch safety features off.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"

	"github.com/ihebamiri/kupi/internal/appdir"
)

// Snippet is a saved terminal command. It's typed into the terminal when
// picked, never run on its own.
type Snippet struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Settings are the user's preferences.
type Settings struct {
	// Appearance.
	Theme   string `json:"theme"`   // system, dark or light
	UIScale int    `json:"uiScale"` // percent

	// Logs.
	LogTailLines  int  `json:"logTailLines"`
	LogTimestamps bool `json:"logTimestamps"`

	// Map.
	MapRefreshSeconds int  `json:"mapRefreshSeconds"`
	MapShowPods       bool `json:"mapShowPods"`

	// Safety.
	ClipboardClearSeconds int `json:"clipboardClearSeconds"`
	VaultAutoLockMinutes  int `json:"vaultAutoLockMinutes"`

	// History.
	HistoryRetentionDays int `json:"historyRetentionDays"`

	// Terminal. TerminalEnabled can only be switched on through
	// EnableTerminal, which asks in a native dialog.
	TerminalEnabled       bool      `json:"terminalEnabled"`
	TerminalShell         string    `json:"terminalShell"`
	TerminalFontSize      int       `json:"terminalFontSize"`
	TerminalScrollback    int       `json:"terminalScrollback"`
	TerminalPasteGuard    bool      `json:"terminalPasteGuard"`
	TerminalFollowCluster bool      `json:"terminalFollowCluster"`
	TerminalCopyOnSelect  bool      `json:"terminalCopyOnSelect"`
	Snippets              []Snippet `json:"snippets"`
}

// Defaults returns the settings of a fresh install.
func Defaults() Settings {
	return Settings{
		Theme:                 "system",
		UIScale:               100,
		LogTailLines:          500,
		LogTimestamps:         false,
		MapRefreshSeconds:     15,
		MapShowPods:           true,
		ClipboardClearSeconds: 30,
		VaultAutoLockMinutes:  15,
		HistoryRetentionDays:  14,
		TerminalEnabled:       false,
		TerminalShell:         "",
		TerminalFontSize:      13,
		TerminalScrollback:    10000,
		TerminalPasteGuard:    true,
		TerminalFollowCluster: true,
		TerminalCopyOnSelect:  false,
		Snippets:              []Snippet{},
	}
}

// Limits for snippets.
const (
	MaxSnippets       = 100
	MaxSnippetName    = 60
	MaxSnippetCommand = 2000
)

var shellID = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)

func clamp(v, lo, hi, def int) int {
	switch {
	case v == 0:
		return def
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// Normalize fixes out-of-range values and returns an error for values that
// can't be fixed (bad snippets, unknown theme).
func (s *Settings) Normalize() error {
	d := Defaults()
	switch s.Theme {
	case "system", "dark", "light":
	case "":
		s.Theme = d.Theme
	default:
		return fmt.Errorf("unknown theme %q", s.Theme)
	}
	s.UIScale = clamp(s.UIScale, 80, 150, d.UIScale)
	s.LogTailLines = clamp(s.LogTailLines, 50, 20000, d.LogTailLines)
	s.MapRefreshSeconds = clamp(s.MapRefreshSeconds, 5, 300, d.MapRefreshSeconds)
	// Copied secrets must clear: never longer than 5 minutes.
	s.ClipboardClearSeconds = clamp(s.ClipboardClearSeconds, 5, 300, d.ClipboardClearSeconds)
	// The vault must lock on its own: never longer than 8 hours.
	s.VaultAutoLockMinutes = clamp(s.VaultAutoLockMinutes, 1, 480, d.VaultAutoLockMinutes)
	s.HistoryRetentionDays = clamp(s.HistoryRetentionDays, 1, 90, d.HistoryRetentionDays)
	s.TerminalFontSize = clamp(s.TerminalFontSize, 9, 28, d.TerminalFontSize)
	s.TerminalScrollback = clamp(s.TerminalScrollback, 500, 200000, d.TerminalScrollback)
	if !shellID.MatchString(s.TerminalShell) {
		return fmt.Errorf("invalid shell id")
	}
	if s.Snippets == nil {
		s.Snippets = []Snippet{}
	}
	if len(s.Snippets) > MaxSnippets {
		return fmt.Errorf("at most %d snippets", MaxSnippets)
	}
	for _, sn := range s.Snippets {
		if sn.Name == "" || len(sn.Name) > MaxSnippetName {
			return fmt.Errorf("a snippet name must be 1 to %d characters", MaxSnippetName)
		}
		if sn.Command == "" || len(sn.Command) > MaxSnippetCommand {
			return fmt.Errorf("a snippet command must be 1 to %d characters", MaxSnippetCommand)
		}
	}
	return nil
}

// Store keeps settings on disk.
type Store struct {
	path string
	mu   sync.Mutex
	cur  Settings
}

// Open loads settings from path, starting from defaults when the file is
// missing. A file that can't be read is replaced by defaults on next save.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cur: Defaults()}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read settings: %w", err)
	}
	loaded := Defaults()
	if err := json.Unmarshal(data, &loaded); err != nil {
		return s, nil
	}
	if err := loaded.Normalize(); err != nil {
		// Keep the good parts: snippets and shell are the only fields that
		// fail outright, so drop them.
		loaded.Snippets, loaded.TerminalShell = []Snippet{}, ""
		if loaded.Normalize() != nil {
			return s, nil
		}
	}
	s.cur = loaded
	return s, nil
}

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.cur
	out.Snippets = append([]Snippet(nil), s.cur.Snippets...)
	return out
}

// Update changes the settings with fn, checks the result and saves it.
func (s *Store) Update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur
	next.Snippets = append([]Snippet(nil), s.cur.Snippets...)
	fn(&next)
	if err := next.Normalize(); err != nil {
		return s.cur, err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return s.cur, err
	}
	if err := appdir.WriteFileAtomic(s.path, data); err != nil {
		return s.cur, fmt.Errorf("save settings: %w", err)
	}
	s.cur = next
	return next, nil
}
