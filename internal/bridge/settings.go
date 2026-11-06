package bridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/settings"
	"github.com/iheb-amiri/kupi/internal/terminal"
	"github.com/iheb-amiri/kupi/internal/trust"
)

// SettingsView is the settings plus the facts the Settings page shows.
type SettingsView struct {
	Settings        settings.Settings `json:"settings"`
	ConfigDir       string            `json:"configDir"`
	KubeconfigFiles []string          `json:"kubeconfigFiles"`
	Shells          []terminal.Shell  `json:"shells"`
	Trusted         []trust.Entry     `json:"trusted"`
}

// SettingsService reads and changes the user's preferences.
type SettingsService struct{ d Deps }

// NewSettingsService returns the settings service.
func NewSettingsService(d Deps) *SettingsService { return &SettingsService{d: d} }

// ApplySettings pushes settings into the parts of Kupi that use them.
func (d Deps) ApplySettings(st settings.Settings) {
	if d.Vault != nil {
		d.Vault.SetAutoLock(time.Duration(st.VaultAutoLockMinutes) * time.Minute)
	}
	if d.HistoryStore != nil {
		d.HistoryStore.SetRetention(time.Duration(st.HistoryRetentionDays) * 24 * time.Hour)
	}
}

// Get returns the settings and related facts.
func (s *SettingsService) Get() SettingsView {
	shells := terminal.Shells()
	if shells == nil {
		shells = []terminal.Shell{}
	}
	trusted := s.d.Trust.Entries()
	if trusted == nil {
		trusted = []trust.Entry{}
	}
	return SettingsView{
		Settings:        s.d.Settings.Get(),
		ConfigDir:       s.d.ConfigDir,
		KubeconfigFiles: s.d.Kube.Loader().Files(),
		Shells:          shells,
		Trusted:         trusted,
	}
}

// Save checks and stores new settings. The terminal can be switched off
// here, but only EnableTerminal switches it on.
func (s *SettingsService) Save(next settings.Settings) (settings.Settings, error) {
	cur := s.d.Settings.Get()
	next.TerminalEnabled = cur.TerminalEnabled && next.TerminalEnabled
	saved, err := s.d.Settings.Update(func(x *settings.Settings) { *x = next })
	if err != nil {
		return cur, err
	}
	s.d.ApplySettings(saved)

	// Safety settings are worth a line in the audit log.
	var changed []string
	if saved.ClipboardClearSeconds != cur.ClipboardClearSeconds {
		changed = append(changed, fmt.Sprintf("clipboard clears after %ds", saved.ClipboardClearSeconds))
	}
	if saved.VaultAutoLockMinutes != cur.VaultAutoLockMinutes {
		changed = append(changed, fmt.Sprintf("vault locks after %dm", saved.VaultAutoLockMinutes))
	}
	if saved.TerminalPasteGuard != cur.TerminalPasteGuard {
		changed = append(changed, fmt.Sprintf("terminal paste check %s", onOff(saved.TerminalPasteGuard)))
	}
	if len(changed) > 0 {
		_, _ = s.d.Audit.Append(audit.Entry{Action: "settings-changed", Resource: strings.Join(changed, "; "), Outcome: audit.OutcomeInfo})
	}
	if cur.TerminalEnabled && !saved.TerminalEnabled {
		s.d.Terminals.CloseAll()
		_, _ = s.d.Audit.Append(audit.Entry{Action: "terminal-disabled", Outcome: audit.OutcomeInfo})
	}
	return saved, nil
}

// EnableTerminal asks in a native dialog before switching the integrated
// terminal on. It reports whether the terminal is now on.
func (s *SettingsService) EnableTerminal() (bool, error) {
	if s.d.Settings.Get().TerminalEnabled {
		return true, nil
	}
	ok := s.d.Native.Confirm(
		"Turn on the Kupi terminal?",
		"The terminal runs your own shell (PowerShell, bash…) inside Kupi. Commands typed there run directly on your computer and your clusters: they don't go through Kupi's change preview, confirmation or audit log.\n\nTurn it on only if you want a command line inside Kupi. You can turn it off again in Settings.",
		"Turn on the terminal",
	)
	if !ok {
		return false, nil
	}
	if _, err := s.d.Settings.Update(func(x *settings.Settings) { x.TerminalEnabled = true }); err != nil {
		return false, err
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "terminal-enabled", Outcome: audit.OutcomeInfo})
	return true, nil
}

// Reset returns every setting to its default. The terminal goes off.
func (s *SettingsService) Reset() (settings.Settings, error) {
	cur := s.d.Settings.Get()
	saved, err := s.d.Settings.Update(func(x *settings.Settings) {
		snippets := x.Snippets
		*x = settings.Defaults()
		x.Snippets = snippets // the user's own commands aren't preferences
	})
	if err != nil {
		return cur, err
	}
	s.d.ApplySettings(saved)
	if cur.TerminalEnabled {
		s.d.Terminals.CloseAll()
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "settings-reset", Outcome: audit.OutcomeInfo})
	return saved, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
