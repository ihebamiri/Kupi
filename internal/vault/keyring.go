package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/99designs/keyring"
)

const (
	keyringService = "kupi"
	keyringKey     = "vault-key"
)

// allowedBackends are real OS password stores. The library's file and
// pass backends are excluded: the passphrase mode covers that case.
var allowedBackends = []keyring.BackendType{keyring.WinCredBackend, keyring.SecretServiceBackend, keyring.KWalletBackend}

var backendNames = map[keyring.BackendType]string{
	keyring.WinCredBackend:       "Windows Credential Manager",
	keyring.SecretServiceBackend: "the Secret Service (GNOME Keyring or KeePassXC)",
	keyring.KWalletBackend:       "KWallet",
}

// OSKeyStore keeps the vault key in the operating system's password store.
type OSKeyStore struct {
	// Key is the entry name; empty means the default.
	Key string
}

func (s OSKeyStore) key() string {
	if s.Key == "" {
		return keyringKey
	}
	return s.Key
}

// backend picks the first allowed password store that actually opens. Being
// compiled in isn't enough: on Linux the Secret Service backend is always
// listed, even when no keyring daemon runs (servers, CI, minimal desktops).
func (OSKeyStore) backend() (keyring.BackendType, bool) {
	available := map[keyring.BackendType]bool{}
	for _, b := range keyring.AvailableBackends() {
		available[b] = true
	}
	for _, b := range allowedBackends {
		if !available[b] {
			continue
		}
		if _, err := keyring.Open(config(b)); err == nil {
			return b, true
		}
	}
	return "", false
}

func config(b keyring.BackendType) keyring.Config {
	return keyring.Config{
		ServiceName:     keyringService,
		AllowedBackends: []keyring.BackendType{b},
		// The user's default collection, so no extra keyring password prompt.
		LibSecretCollectionName: "login",
		KWalletAppID:            keyringService,
		KWalletFolder:           keyringService,
		WinCredPrefix:           keyringService,
	}
}

func (s OSKeyStore) open() (keyring.Keyring, error) {
	b, ok := s.backend()
	if !ok {
		return nil, errors.New("no password store is available")
	}
	return keyring.Open(config(b))
}

// Available names the password store Kupi would use.
func (s OSKeyStore) Available() (string, bool) {
	b, ok := s.backend()
	return backendNames[b], ok
}

// Get reads the vault key.
func (s OSKeyStore) Get() (string, error) {
	ring, err := s.open()
	if err != nil {
		return "", err
	}
	item, err := ring.Get(s.key())
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return "", errors.New("the vault key is missing from the password store")
	}
	if err != nil {
		return "", err
	}
	return string(item.Data), nil
}

// Set stores the vault key.
func (s OSKeyStore) Set(identity string) error {
	ring, err := s.open()
	if err != nil {
		return err
	}
	return ring.Set(keyring.Item{
		Key:         s.key(),
		Data:        []byte(identity),
		Label:       "Kupi vault key",
		Description: "Decrypts the Kupi local secret vault",
	})
}

// Delete removes the vault key.
func (s OSKeyStore) Delete() error {
	ring, err := s.open()
	if err != nil {
		return err
	}
	if err := ring.Remove(s.key()); err != nil && !errors.Is(err, keyring.ErrKeyNotFound) {
		return err
	}
	return nil
}

// KeyNameFor names the password-store entry for a vault kept somewhere other
// than the default config dir, so test and portable setups never overwrite
// the default vault key.
func KeyNameFor(dir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return keyringKey + "-" + hex.EncodeToString(sum[:8])
}
