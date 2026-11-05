// Package vault is Kupi's local, encrypted store for secret values: named
// items, each a set of keys and values, that the user can turn into
// Kubernetes Secrets instead of keeping credentials in YAML files.
//
// The data file is encrypted with age to an X25519 key. That key is kept in
// the operating system's password store (Windows Credential Manager, the
// Secret Service on Linux, or KWallet) or, where there is none, in a second
// file encrypted with the user's passphrase. Writes only need the public
// half of the key, so the private key is never held longer than unlocking
// takes. Decrypted values stay in memory only while the vault is unlocked,
// and it locks itself after a period without use.
package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/iheb-amiri/kupi/internal/appdir"
)

// Mode is where the vault key is kept.
type Mode string

const (
	ModeKeyring    Mode = "keyring"
	ModePassphrase Mode = "passphrase"
)

// Limits.
const (
	MinPassphrase   = 12
	MaxItems        = 500
	MaxItemBytes    = 1 << 20
	MaxKeysPerItem  = 250
	DefaultAutoLock = 15 * time.Minute
)

var (
	ErrLocked      = errors.New("the vault is locked")
	ErrNoVault     = errors.New("there is no vault yet")
	ErrExists      = errors.New("a vault already exists")
	ErrNoItem      = errors.New("no such item in the vault")
	ErrBadPassword = errors.New("wrong passphrase")
)

// KeyStore keeps the vault key in the OS password store.
type KeyStore interface {
	Available() (name string, ok bool)
	Get() (string, error)
	Set(identity string) error
	Delete() error
}

// Item is one named set of values.
type Item struct {
	Description string            `json:"description"`
	Updated     time.Time         `json:"updated"`
	Values      map[string]string `json:"values"`
}

// ItemInfo describes an item without its values.
type ItemInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Updated     string   `json:"updated"`
	Keys        []string `json:"keys"`
}

// Status describes the vault for the UI.
type Status struct {
	Exists           bool   `json:"exists"`
	Unlocked         bool   `json:"unlocked"`
	Mode             Mode   `json:"mode"`
	KeyringAvailable bool   `json:"keyringAvailable"`
	KeyringName      string `json:"keyringName"`
	AutoLockMinutes  int    `json:"autoLockMinutes"`
	Items            int    `json:"items"`
}

type meta struct {
	Version int  `json:"version"`
	Mode    Mode `json:"mode"`
}

type contents struct {
	Version int              `json:"version"`
	Items   map[string]*Item `json:"items"`
}

// Vault is the local secret store.
type Vault struct {
	dir      string
	keys     KeyStore
	autoLock time.Duration
	now      func() time.Time

	mu        sync.Mutex
	unlocked  bool
	recipient age.Recipient
	data      *contents
	timer     *time.Timer
	gen       uint64
}

// New returns the vault stored in dir.
func New(dir string, keys KeyStore) *Vault {
	return &Vault{dir: dir, keys: keys, autoLock: DefaultAutoLock, now: time.Now}
}

func (v *Vault) dataPath() string { return filepath.Join(v.dir, "vault.age") }
func (v *Vault) metaPath() string { return filepath.Join(v.dir, "vault.json") }
func (v *Vault) keyPath() string  { return filepath.Join(v.dir, "vault-key.age") }

// Status reports whether the vault exists and is unlocked.
func (v *Vault) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	st := Status{AutoLockMinutes: int(v.autoLock / time.Minute), Unlocked: v.unlocked}
	st.KeyringName, st.KeyringAvailable = v.keys.Available()
	if m, err := v.readMeta(); err == nil {
		st.Exists, st.Mode = true, m.Mode
	}
	if v.unlocked {
		st.Items = len(v.data.Items)
	}
	return st
}

// Create makes a new, empty vault and leaves it unlocked.
func (v *Vault) Create(mode Mode, passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := os.Stat(v.metaPath()); err == nil {
		return ErrExists
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate the vault key: %w", err)
	}
	switch mode {
	case ModeKeyring:
		if _, ok := v.keys.Available(); !ok {
			return fmt.Errorf("no password store is available on this system; use a passphrase instead")
		}
		if err := v.keys.Set(id.String()); err != nil {
			return fmt.Errorf("save the vault key in the password store: %w", err)
		}
	case ModePassphrase:
		if len([]rune(passphrase)) < MinPassphrase {
			return fmt.Errorf("the passphrase must be at least %d characters", MinPassphrase)
		}
		r, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return err
		}
		sealed, err := encrypt([]byte(id.String()), r)
		if err != nil {
			return err
		}
		if err := appdir.WriteFileAtomic(v.keyPath(), sealed); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown vault mode %q", mode)
	}
	v.recipient = id.Recipient()
	v.data = &contents{Version: 1, Items: map[string]*Item{}}
	if err := v.saveLocked(); err != nil {
		return err
	}
	m, _ := json.Marshal(meta{Version: 1, Mode: mode})
	if err := appdir.WriteFileAtomic(v.metaPath(), m); err != nil {
		return err
	}
	v.unlocked = true
	v.touchLocked()
	return nil
}

// Unlock decrypts the vault. The passphrase is only used in passphrase mode.
func (v *Vault) Unlock(passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.unlocked {
		v.touchLocked()
		return nil
	}
	m, err := v.readMeta()
	if err != nil {
		return err
	}
	var idText string
	switch m.Mode {
	case ModeKeyring:
		idText, err = v.keys.Get()
		if err != nil {
			return fmt.Errorf("read the vault key from the password store: %w", err)
		}
	case ModePassphrase:
		sealed, err := os.ReadFile(v.keyPath())
		if err != nil {
			return fmt.Errorf("read the vault key file: %w", err)
		}
		pi, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return ErrBadPassword
		}
		plain, err := decrypt(sealed, pi)
		if err != nil {
			return ErrBadPassword
		}
		idText = string(plain)
	default:
		return fmt.Errorf("unknown vault mode %q", m.Mode)
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(idText))
	if err != nil {
		return fmt.Errorf("the stored vault key is damaged: %w", err)
	}
	sealed, err := os.ReadFile(v.dataPath())
	if err != nil {
		return fmt.Errorf("read the vault: %w", err)
	}
	plain, err := decrypt(sealed, id)
	if err != nil {
		return fmt.Errorf("the vault can't be decrypted with this key (it may have been changed or damaged): %w", err)
	}
	var c contents
	if err := json.Unmarshal(plain, &c); err != nil {
		return fmt.Errorf("read the vault: %w", err)
	}
	if c.Items == nil {
		c.Items = map[string]*Item{}
	}
	v.data, v.recipient, v.unlocked = &c, id.Recipient(), true
	v.touchLocked()
	return nil
}

// Lock forgets the decrypted values.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
}

// SetAutoLock changes how long the vault stays unlocked without use.
func (v *Vault) SetAutoLock(d time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoLock = d
	if v.unlocked {
		v.touchLocked()
	}
}

func (v *Vault) lockLocked() {
	if v.timer != nil {
		v.timer.Stop()
		v.timer = nil
	}
	v.unlocked, v.data, v.recipient = false, nil, nil
}

// touchLocked restarts the auto-lock countdown.
func (v *Vault) touchLocked() {
	if v.timer != nil {
		v.timer.Stop()
	}
	v.gen++
	gen := v.gen
	v.timer = time.AfterFunc(v.autoLock, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		// A timer that fired while the vault was being used again is stale.
		if v.gen == gen {
			v.lockLocked()
		}
	})
}

// Items lists items without their values.
func (v *Vault) Items() ([]ItemInfo, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	v.touchLocked()
	out := make([]ItemInfo, 0, len(v.data.Items))
	for name, it := range v.data.Items {
		keys := make([]string, 0, len(it.Values))
		for k := range it.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, ItemInfo{Name: name, Description: it.Description, Updated: it.Updated.UTC().Format(time.RFC3339), Keys: keys})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var (
	itemNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)
	keyRe      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`)
)

// ValidItemName checks an item name.
func ValidItemName(name string) error {
	if !itemNameRe.MatchString(name) {
		return fmt.Errorf("item names use lowercase letters, digits, '.', '-' and '_', up to 63 characters")
	}
	return nil
}

// ValidKey checks a key name. Keys follow the rules for Secret keys.
func ValidKey(k string) error {
	if !keyRe.MatchString(k) || k == "." || k == ".." {
		return fmt.Errorf("key %q may only use letters, digits, '-', '_' and '.'", k)
	}
	return nil
}

// Put creates an item or changes some of its values.
func (v *Vault) Put(name, description string, set map[string]string, del []string) error {
	if err := ValidItemName(name); err != nil {
		return err
	}
	if len(description) > 200 || strings.ContainsAny(description, "\r\n") {
		return fmt.Errorf("the description must be one line of up to 200 characters")
	}
	for k := range set {
		if err := ValidKey(k); err != nil {
			return err
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	v.touchLocked()
	it, ok := v.data.Items[name]
	if !ok {
		if len(v.data.Items) >= MaxItems {
			return fmt.Errorf("the vault holds at most %d items", MaxItems)
		}
		it = &Item{}
	}
	values := make(map[string]string, len(it.Values)+len(set))
	for k, val := range it.Values {
		values[k] = val
	}
	for _, k := range del {
		delete(values, k)
	}
	for k, val := range set {
		values[k] = val
	}
	size := 0
	for k, val := range values {
		size += len(k) + len(val)
	}
	switch {
	case len(values) == 0:
		return fmt.Errorf("an item needs at least one key")
	case len(values) > MaxKeysPerItem:
		return fmt.Errorf("an item holds at most %d keys", MaxKeysPerItem)
	case size > MaxItemBytes:
		return fmt.Errorf("an item holds at most 1 MiB")
	}
	prev, had := v.data.Items[name]
	v.data.Items[name] = &Item{Description: description, Updated: v.now(), Values: values}
	if err := v.saveLocked(); err != nil {
		if had {
			v.data.Items[name] = prev
		} else {
			delete(v.data.Items, name)
		}
		return err
	}
	return nil
}

// Delete removes an item.
func (v *Vault) Delete(name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return ErrLocked
	}
	v.touchLocked()
	prev, ok := v.data.Items[name]
	if !ok {
		return ErrNoItem
	}
	delete(v.data.Items, name)
	if err := v.saveLocked(); err != nil {
		v.data.Items[name] = prev
		return err
	}
	return nil
}

// Value returns one value.
func (v *Vault) Value(item, key string) (string, error) {
	values, err := v.Values(item)
	if err != nil {
		return "", err
	}
	val, ok := values[key]
	if !ok {
		return "", fmt.Errorf("item %s has no key %q", item, key)
	}
	return val, nil
}

// Values returns a copy of every value of an item.
func (v *Vault) Values(item string) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.unlocked {
		return nil, ErrLocked
	}
	v.touchLocked()
	it, ok := v.data.Items[item]
	if !ok {
		return nil, ErrNoItem
	}
	out := make(map[string]string, len(it.Values))
	for k, val := range it.Values {
		out[k] = val
	}
	return out, nil
}

// Reset deletes the vault and its key for good, for example after the
// passphrase was forgotten.
func (v *Vault) Reset() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	m, err := v.readMeta()
	if err != nil {
		return err
	}
	v.lockLocked()
	if m.Mode == ModeKeyring {
		if err := v.keys.Delete(); err != nil {
			return fmt.Errorf("remove the vault key from the password store: %w", err)
		}
	}
	for _, p := range []string{v.dataPath(), v.keyPath(), v.metaPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", filepath.Base(p), err)
		}
	}
	return nil
}

// Close locks the vault and stops its timer. Called on shutdown.
func (v *Vault) Close() { v.Lock() }

func (v *Vault) readMeta() (meta, error) {
	raw, err := os.ReadFile(v.metaPath())
	if errors.Is(err, os.ErrNotExist) {
		return meta{}, ErrNoVault
	}
	if err != nil {
		return meta{}, fmt.Errorf("read the vault settings: %w", err)
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return meta{}, fmt.Errorf("read the vault settings: %w", err)
	}
	return m, nil
}

func (v *Vault) saveLocked() error {
	plain, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	sealed, err := encrypt(plain, v.recipient)
	if err != nil {
		return err
	}
	return appdir.WriteFileAtomic(v.dataPath(), sealed)
}

func encrypt(plain []byte, r age.Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	return buf.Bytes(), nil
}

func decrypt(sealed []byte, id age.Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(sealed), id)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(r, 64<<20))
}
