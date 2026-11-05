package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type memKeys struct {
	id        string
	available bool
}

func (m *memKeys) Available() (string, bool) { return "memory", m.available }
func (m *memKeys) Get() (string, error) {
	if m.id == "" {
		return "", errors.New("missing")
	}
	return m.id, nil
}
func (m *memKeys) Set(id string) error { m.id = id; return nil }
func (m *memKeys) Delete() error       { m.id = ""; return nil }

func TestKeyringVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keys := &memKeys{available: true}
	v := New(dir, keys)
	if st := v.Status(); st.Exists || !st.KeyringAvailable {
		t.Fatalf("status = %+v", st)
	}
	if err := v.Create(ModeKeyring, ""); err != nil {
		t.Fatal(err)
	}
	if err := v.Put("stripe-test", "Stripe test keys", map[string]string{"STRIPE_KEY": "sk_test_abc123", "WEBHOOK": "whsec_1"}, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.age"))
	if bytes.Contains(raw, []byte("sk_test_abc123")) || bytes.Contains(raw, []byte("stripe-test")) {
		t.Fatal("vault file holds plain text")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "vault.age")); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("vault file mode = %v", fi.Mode())
	}

	v.Lock()
	if _, err := v.Items(); !errors.Is(err, ErrLocked) {
		t.Errorf("locked vault listed items: %v", err)
	}
	if err := New(dir, keys).Create(ModeKeyring, ""); !errors.Is(err, ErrExists) {
		t.Errorf("second create: %v", err)
	}

	v2 := New(dir, keys)
	if err := v2.Unlock(""); err != nil {
		t.Fatal(err)
	}
	items, _ := v2.Items()
	if len(items) != 1 || items[0].Keys[0] != "STRIPE_KEY" || items[0].Description != "Stripe test keys" {
		t.Fatalf("items = %+v", items)
	}
	if err := v2.Put("stripe-test", "Stripe test keys", nil, []string{"WEBHOOK"}); err != nil {
		t.Fatal(err)
	}
	if val, _ := v2.Value("stripe-test", "STRIPE_KEY"); val != "sk_test_abc123" {
		t.Errorf("value = %q", val)
	}
	if _, err := v2.Value("stripe-test", "WEBHOOK"); err == nil {
		t.Error("deleted key still there")
	}
	if err := v2.Put("stripe-test", "", nil, []string{"STRIPE_KEY"}); err == nil {
		t.Error("emptied item accepted")
	}
	if err := v2.Put("Bad Name", "", map[string]string{"A": "b"}, nil); err == nil {
		t.Error("bad item name accepted")
	}

	if err := v2.Reset(); err != nil {
		t.Fatal(err)
	}
	if keys.id != "" || v2.Status().Exists {
		t.Error("reset left the key or vault behind")
	}
}

func TestPassphraseVault(t *testing.T) {
	dir := t.TempDir()
	keys := &memKeys{}
	v := New(dir, keys)
	if err := v.Create(ModeKeyring, ""); err == nil {
		t.Fatal("keyring mode without a password store")
	}
	if err := v.Create(ModePassphrase, "short"); err == nil {
		t.Fatal("short passphrase accepted")
	}
	if err := v.Create(ModePassphrase, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put("db", "", map[string]string{"password": "pw1"}, nil); err != nil {
		t.Fatal(err)
	}
	v.Close()
	if err := v.Unlock("wrong passphrase!!"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if err := v.Unlock("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Values("db"); got["password"] != "pw1" {
		t.Errorf("values = %v", got)
	}
}

func TestAutoLock(t *testing.T) {
	v := New(t.TempDir(), &memKeys{available: true})
	if err := v.Create(ModeKeyring, ""); err != nil {
		t.Fatal(err)
	}
	v.SetAutoLock(30 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if v.Status().Unlocked {
		t.Error("vault still unlocked after the auto-lock delay")
	}
}

func TestTamperedVaultRefused(t *testing.T) {
	dir := t.TempDir()
	keys := &memKeys{available: true}
	v := New(dir, keys)
	_ = v.Create(ModeKeyring, "")
	_ = v.Put("a", "", map[string]string{"k": "v"}, nil)
	v.Lock()
	path := filepath.Join(dir, "vault.age")
	raw, _ := os.ReadFile(path)
	raw[len(raw)-5] ^= 0xff
	_ = os.WriteFile(path, raw, 0o600)
	if err := v.Unlock(""); err == nil {
		t.Error("tampered vault decrypted")
	}
}
