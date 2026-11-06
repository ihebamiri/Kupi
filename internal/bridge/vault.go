package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/secrets"
	"github.com/ihebamiri/kupi/internal/validate"
	"github.com/ihebamiri/kupi/internal/vault"
)

const (
	maxPassphrase = 1024
	// VaultResetConfirm is the text the user types to delete the vault.
	VaultResetConfirm = "delete my vault"
)

// VaultService is the local encrypted store for secret values.
type VaultService struct{ d Deps }

// NewVaultService returns the vault service.
func NewVaultService(d Deps) *VaultService { return &VaultService{d: d} }

// Status reports whether the vault exists and is unlocked.
func (s *VaultService) Status() vault.Status { return s.d.Vault.Status() }

// Create makes a new vault. mode is "keyring" or "passphrase".
func (s *VaultService) Create(mode, passphrase string) error {
	if len(passphrase) > maxPassphrase {
		return fmt.Errorf("the passphrase is too long")
	}
	return s.d.Vault.Create(vault.Mode(mode), passphrase)
}

// Unlock decrypts the vault.
func (s *VaultService) Unlock(passphrase string) error {
	if len(passphrase) > maxPassphrase {
		return vault.ErrBadPassword
	}
	return s.d.Vault.Unlock(passphrase)
}

// Lock forgets the decrypted values now.
func (s *VaultService) Lock() { s.d.Vault.Lock() }

// Items lists items and their key names.
func (s *VaultService) Items() ([]vault.ItemInfo, error) { return s.d.Vault.Items() }

// SaveItem creates an item or changes some of its values.
func (s *VaultService) SaveItem(name, description string, set map[string]string, del []string) error {
	if len(set) > vault.MaxKeysPerItem || len(del) > vault.MaxKeysPerItem {
		return fmt.Errorf("too many keys")
	}
	size := 0
	for k, v := range set {
		size += len(k) + len(v)
	}
	if size > vault.MaxItemBytes {
		return fmt.Errorf("an item holds at most 1 MiB")
	}
	for _, k := range del {
		if err := vault.ValidKey(k); err != nil {
			return err
		}
	}
	return s.d.Vault.Put(name, description, set, del)
}

// DeleteItem removes an item.
func (s *VaultService) DeleteItem(name string) error {
	if err := vault.ValidItemName(name); err != nil {
		return err
	}
	return s.d.Vault.Delete(name)
}

// Reveal returns one value.
func (s *VaultService) Reveal(item, key string) (string, error) {
	if err := checkVaultRef(item, key); err != nil {
		return "", err
	}
	return s.d.Vault.Value(item, key)
}

// CopyValue puts one value on the clipboard without sending it to the UI.
func (s *VaultService) CopyValue(item, key string) error {
	if err := checkVaultRef(item, key); err != nil {
		return err
	}
	v, err := s.d.Vault.Value(item, key)
	if err != nil {
		return err
	}
	return s.d.Clipboard.Copy(v, s.d.clipboardTTL())
}

// PrepareSecret previews creating a Kubernetes Secret, or adding to one,
// from every value of a vault item. The review and commit go through the
// Guard like any other change.
func (s *VaultService) PrepareSecret(ctx context.Context, contextName, namespace, secretName, item, secretType string) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := secretTarget(namespace, secretName); err != nil {
		return PlanView{}, err
	}
	if err := vault.ValidItemName(item); err != nil {
		return PlanView{}, err
	}
	switch secretType {
	case "", secrets.TypeOpaque, secrets.TypeBasicAuth, secrets.TypeSSHAuth:
	default:
		return PlanView{}, fmt.Errorf("vault items can become Opaque, basic-auth or ssh-auth Secrets")
	}
	values, err := s.d.Vault.Values(item)
	if err != nil {
		return PlanView{}, err
	}
	set := make(map[string][]byte, len(values))
	for k, v := range values {
		set[k] = []byte(v)
	}
	plan, err := s.d.Guard.PrepareSecretWrite(ctx, guard.SecretWrite{
		Context: contextName, Namespace: namespace, Name: secretName, Type: secretType, Set: set,
		Labels: map[string]string{"app.kubernetes.io/created-by": "kupi"},
	})
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// Reset deletes the vault and its key for good.
func (s *VaultService) Reset(confirm string) error {
	if strings.TrimSpace(confirm) != VaultResetConfirm {
		return fmt.Errorf("type %q to confirm", VaultResetConfirm)
	}
	return s.d.Vault.Reset()
}

func checkVaultRef(item, key string) error {
	if err := vault.ValidItemName(item); err != nil {
		return err
	}
	return vault.ValidKey(key)
}
