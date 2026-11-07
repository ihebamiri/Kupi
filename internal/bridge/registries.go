package bridge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/engine"
	"github.com/ihebamiri/kupi/internal/imagescan"
	"github.com/ihebamiri/kupi/internal/localk8s"
	"github.com/ihebamiri/kupi/internal/vault"
)

// Registry logins live in the vault as items named "registry-<host>", with
// the keys "username" and "password". The vault keeps them encrypted; they're
// only read to pull an image, and never sent to the UI.
const registryItemPrefix = "registry-"

var registryHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,50}[a-z0-9])?(:[0-9]{1,5})?$`)

// RegistryLogin is a saved login, without its password.
type RegistryLogin struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Updated  string `json:"updated"`
}

// RegistryLogins lists saved logins. The vault must be unlocked.
func (s *ImageService) RegistryLogins() ([]RegistryLogin, error) {
	items, err := s.d.Vault.Items()
	if err != nil {
		return nil, vaultErr(err)
	}
	out := []RegistryLogin{}
	for _, it := range items {
		if !strings.HasPrefix(it.Name, registryItemPrefix) {
			continue
		}
		user, err := s.d.Vault.Value(it.Name, "username")
		if err != nil {
			continue
		}
		out = append(out, RegistryLogin{Host: hostFromItem(it.Name, it.Description), Username: user, Updated: it.Updated})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out, nil
}

// SaveRegistryLogin stores (or replaces) a login in the vault.
func (s *ImageService) SaveRegistryLogin(host, username, password string) error {
	host = normalizeHost(host)
	if !registryHostRe.MatchString(host) {
		return fmt.Errorf("enter a registry host such as ghcr.io or registry.example.com:5000")
	}
	if username == "" || len(username) > 256 || strings.ContainsAny(username, "\r\n") {
		return fmt.Errorf("enter a user name (up to 256 characters)")
	}
	if password == "" || len(password) > 8192 {
		return fmt.Errorf("enter a password or access token (up to 8 KB)")
	}
	if err := s.d.Vault.Put(registryItem(host), "Registry login for "+host, map[string]string{"username": username, "password": password}, nil); err != nil {
		return vaultErr(err)
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "registry-login-saved", Resource: host, Outcome: audit.OutcomeInfo})
	return nil
}

// DeleteRegistryLogin removes a saved login.
func (s *ImageService) DeleteRegistryLogin(host string) error {
	host = normalizeHost(host)
	if !registryHostRe.MatchString(host) {
		return fmt.Errorf("invalid registry host")
	}
	if err := s.d.Vault.Delete(registryItem(host)); err != nil {
		return vaultErr(err)
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "registry-login-deleted", Resource: host, Outcome: audit.OutcomeInfo})
	return nil
}

// loginFor returns the saved login for an image's registry, if the vault is
// unlocked and holds one. Anything else means an anonymous pull.
// It also says whether the vault was locked, since a saved login can't be
// used then.
func (s *ImageService) loginFor(ref string) (auth *engine.RegistryAuth, locked bool) {
	host := registryOf(ref)
	vals, err := s.d.Vault.Values(registryItem(host))
	if errors.Is(err, vault.ErrLocked) {
		return nil, true
	}
	if err != nil || vals["username"] == "" || vals["password"] == "" {
		return nil, false
	}
	server := host
	if host == "docker.io" {
		server = "https://index.docker.io/v1/"
	}
	return &engine.RegistryAuth{Username: vals["username"], Password: vals["password"], ServerAddress: server}, false
}

// authFailure tells whether a pull failed for want of credentials.
func authFailure(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"unauthorized", "authorization", "authentication required", "access denied", "no basic auth"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// registryOf returns the registry host of an image reference, the way
// Docker reads it: no dot, colon or "localhost" in the first part means
// Docker Hub.
func registryOf(ref string) string {
	first, _, found := strings.Cut(ref, "/")
	if !found || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return "docker.io"
	}
	return strings.ToLower(first)
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	h = strings.TrimSuffix(h, "/")
	switch h {
	case "index.docker.io", "registry-1.docker.io", "hub.docker.com":
		return "docker.io"
	}
	return h
}

func registryItem(host string) string {
	return registryItemPrefix + strings.ReplaceAll(host, ":", "-")
}

// hostFromItem recovers the host from the item's description (item names
// can't hold ':').
func hostFromItem(name, description string) string {
	if h, ok := strings.CutPrefix(description, "Registry login for "); ok {
		return h
	}
	return strings.TrimPrefix(name, registryItemPrefix)
}

func vaultErr(err error) error {
	if errors.Is(err, vault.ErrLocked) {
		return errors.New("unlock the vault first: registry logins are kept in it")
	}
	return err
}

// ScanTools reports whether Trivy and cosign are installed.
func (s *ImageService) ScanTools() map[string]bool { return imagescan.Tools() }

// Scan checks an image for known vulnerabilities with Trivy.
func (s *ImageService) Scan(ctx context.Context, ref string) (*imagescan.Report, error) {
	if err := localk8s.ValidImage(ref); err != nil {
		return nil, err
	}
	return imagescan.Scan(ctx, ref)
}

// VerifySignature checks an image's Sigstore signatures with cosign.
func (s *ImageService) VerifySignature(ctx context.Context, ref string) (*imagescan.SignatureReport, error) {
	if err := localk8s.ValidImage(ref); err != nil {
		return nil, err
	}
	return imagescan.Verify(ctx, ref)
}
