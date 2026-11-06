package bridge

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/secrets"
	"github.com/ihebamiri/kupi/internal/validate"
)

const (
	// maxSecretBytes is the API server's limit for a whole Secret.
	maxSecretBytes = 1 << 20
	maxSecretKeys  = 250
)

// SecretView is a Secret's details plus what revealing a value needs.
type SecretView struct {
	Details           secrets.Details `json:"details"`
	RevealNeedsReason bool            `json:"revealNeedsReason"`
	ClearAfterSeconds int             `json:"clearAfterSeconds"`
}

// RevealedValue is one Secret value the user asked to see.
type RevealedValue struct {
	Value string `json:"value"`
	// Encoding is "text", or "base64" for binary values.
	Encoding string `json:"encoding"`
}

// EnvCopy reports what "Copy as .env" put on the clipboard.
type EnvCopy struct {
	Keys    int      `json:"keys"`
	Skipped []string `json:"skipped"`
}

// SecretWriteRequest creates a Secret or changes some of its keys. Keys not
// named are left untouched on the server.
type SecretWriteRequest struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Set       map[string]string `json:"set"`
	Delete    []string          `json:"delete"`
}

// SecretService shows Secrets without sending their values to the UI unless
// the user reveals one, and prepares Secret changes through the Guard.
type SecretService struct{ d Deps }

// NewSecretService returns the secret service.
func NewSecretService(d Deps) *SecretService { return &SecretService{d: d} }

func secretTarget(namespace, name string) error {
	if err := validate.Namespace(namespace); err != nil {
		return err
	}
	return validate.Name(name)
}

func validKey(key string) error {
	if key == "" || len(key) > 253 {
		return fmt.Errorf("invalid key name")
	}
	return nil
}

// Describe returns a Secret's keys, certificate details, owner and users.
func (s *SecretService) Describe(ctx context.Context, contextName, namespace, name string) (SecretView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return SecretView{}, err
	}
	if err := secretTarget(namespace, name); err != nil {
		return SecretView{}, err
	}
	d, err := secrets.Describe(ctx, sess.Reader(), namespace, name, time.Now())
	if err != nil {
		return SecretView{}, err
	}
	return SecretView{
		Details:           *d,
		RevealNeedsReason: s.d.Guard.AccessNeedsReason(contextName),
		ClearAfterSeconds: int(s.d.clipboardTTL() / time.Second),
	}, nil
}

// Reveal returns one value, after writing the access to the audit log.
func (s *SecretService) Reveal(ctx context.Context, contextName, namespace, name, key, reason string) (RevealedValue, error) {
	value, err := s.value(ctx, contextName, namespace, name, key)
	if err != nil {
		return RevealedValue{}, err
	}
	if err := s.d.Guard.RecordAccess(contextName, "secret-revealed", secretResource(namespace, name, key), reason); err != nil {
		return RevealedValue{}, err
	}
	if utf8.Valid(value) {
		return RevealedValue{Value: string(value), Encoding: "text"}, nil
	}
	return RevealedValue{Value: base64.StdEncoding.EncodeToString(value), Encoding: "base64"}, nil
}

// CopyValue puts one value on the system clipboard without sending it to the
// UI, and clears it again after the time set in Settings.
func (s *SecretService) CopyValue(ctx context.Context, contextName, namespace, name, key, reason string) error {
	value, err := s.value(ctx, contextName, namespace, name, key)
	if err != nil {
		return err
	}
	if !utf8.Valid(value) {
		return fmt.Errorf("this value is binary; reveal it to see it as base64")
	}
	if err := s.d.Guard.RecordAccess(contextName, "secret-copied", secretResource(namespace, name, key), reason); err != nil {
		return err
	}
	return s.d.Clipboard.Copy(string(value), s.d.clipboardTTL())
}

// CopyEnv puts every value on the clipboard as a .env file.
func (s *SecretService) CopyEnv(ctx context.Context, contextName, namespace, name, reason string) (EnvCopy, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return EnvCopy{}, err
	}
	if err := secretTarget(namespace, name); err != nil {
		return EnvCopy{}, err
	}
	text, n, skipped, err := secrets.EnvFile(ctx, sess.Reader(), namespace, name)
	if err != nil {
		return EnvCopy{}, err
	}
	if n == 0 {
		return EnvCopy{}, fmt.Errorf("no key in this Secret can be written as an environment variable")
	}
	if err := s.d.Guard.RecordAccess(contextName, "secret-copied", fmt.Sprintf("Secret %s/%s (all keys as .env)", namespace, name), reason); err != nil {
		return EnvCopy{}, err
	}
	if err := s.d.Clipboard.Copy(text, s.d.clipboardTTL()); err != nil {
		return EnvCopy{}, err
	}
	if skipped == nil {
		skipped = []string{}
	}
	return EnvCopy{Keys: n, Skipped: skipped}, nil
}

func (s *SecretService) value(ctx context.Context, contextName, namespace, name, key string) ([]byte, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := secretTarget(namespace, name); err != nil {
		return nil, err
	}
	if err := validKey(key); err != nil {
		return nil, err
	}
	return secrets.Value(ctx, sess.Reader(), namespace, name, key)
}

func secretResource(namespace, name, key string) string {
	return fmt.Sprintf("Secret %s/%s (key %s)", namespace, name, key)
}

// PrepareWrite previews creating a Secret or changing some of its keys.
func (s *SecretService) PrepareWrite(ctx context.Context, contextName string, req SecretWriteRequest) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := secretTarget(req.Namespace, req.Name); err != nil {
		return PlanView{}, err
	}
	switch req.Type {
	case "", secrets.TypeOpaque, secrets.TypeBasicAuth, secrets.TypeSSHAuth:
	default:
		return PlanView{}, fmt.Errorf("use the TLS or registry form for Secrets of type %s", req.Type)
	}
	if len(req.Set)+len(req.Delete) > maxSecretKeys {
		return PlanView{}, fmt.Errorf("too many keys in one change (limit %d)", maxSecretKeys)
	}
	set := make(map[string][]byte, len(req.Set))
	total := 0
	for k, v := range req.Set {
		total += len(k) + len(v)
		set[k] = []byte(v)
	}
	if total > maxSecretBytes {
		return PlanView{}, fmt.Errorf("a Secret can hold at most 1 MiB")
	}
	return s.prepare(ctx, guard.SecretWrite{
		Context: contextName, Namespace: req.Namespace, Name: req.Name, Type: req.Type, Set: set, Delete: req.Delete,
	})
}

// PrepareTLS previews writing a TLS Secret from a PEM certificate and key,
// after checking they belong together.
func (s *SecretService) PrepareTLS(ctx context.Context, contextName, namespace, name, certPEM, keyPEM string) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := secretTarget(namespace, name); err != nil {
		return PlanView{}, err
	}
	if len(certPEM)+len(keyPEM) > maxSecretBytes {
		return PlanView{}, fmt.Errorf("a Secret can hold at most 1 MiB")
	}
	if err := secrets.CheckPair([]byte(certPEM), []byte(keyPEM)); err != nil {
		return PlanView{}, err
	}
	return s.prepare(ctx, guard.SecretWrite{
		Context: contextName, Namespace: namespace, Name: name, Type: secrets.TypeTLS,
		Set: map[string][]byte{"tls.crt": []byte(certPEM), "tls.key": []byte(keyPEM)},
	})
}

// PrepareRegistry previews writing a registry login Secret for image pulls.
func (s *SecretService) PrepareRegistry(ctx context.Context, contextName, namespace, name, server, username, password, email string) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := secretTarget(namespace, name); err != nil {
		return PlanView{}, err
	}
	value, err := secrets.DockerConfigJSON(server, username, password, email)
	if err != nil {
		return PlanView{}, err
	}
	return s.prepare(ctx, guard.SecretWrite{
		Context: contextName, Namespace: namespace, Name: name, Type: secrets.TypeDockerConfigJSON,
		Set: map[string][]byte{".dockerconfigjson": value},
	})
}

func (s *SecretService) prepare(ctx context.Context, req guard.SecretWrite) (PlanView, error) {
	plan, err := s.d.Guard.PrepareSecretWrite(ctx, req)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// CheckCertificate describes a PEM certificate (and key, if given) before
// it is saved.
func (s *SecretService) CheckCertificate(certPEM, keyPEM string) (secrets.TLSInfo, error) {
	if len(certPEM)+len(keyPEM) > maxSecretBytes {
		return secrets.TLSInfo{}, fmt.Errorf("a Secret can hold at most 1 MiB")
	}
	return secrets.CertInfo([]byte(certPEM), []byte(keyPEM), time.Now()), nil
}

// Scan looks for credentials stored in the open in a namespace ("" for all).
func (s *SecretService) Scan(ctx context.Context, contextName, namespace string) (secrets.ScanResult, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return secrets.ScanResult{}, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return secrets.ScanResult{}, err
	}
	return secrets.Scan(ctx, sess.Reader(), namespace)
}
