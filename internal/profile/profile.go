// Package profile labels each cluster as local, dev, staging or production
// and sets the safety and performance defaults that go with the label.
// Profiles never remove features; they change how careful Kupi is.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/ihebamiri/kupi/internal/appdir"
)

// Kind is a cluster profile.
type Kind string

const (
	Local      Kind = "local"
	Dev        Kind = "dev"
	Staging    Kind = "staging"
	Production Kind = "production"
)

// Valid reports whether k is a known profile.
func (k Kind) Valid() bool {
	switch k {
	case Local, Dev, Staging, Production:
		return true
	}
	return false
}

// Rules are the defaults a profile applies.
type Rules struct {
	// EditingOnByDefault: changes are allowed without first switching
	// editing on for the session.
	EditingOnByDefault bool
	// RequireReason: every change needs a written reason, kept in the audit log.
	RequireReason bool
	// ConfirmByName: destructive actions need the resource's name typed out.
	ConfirmByName bool
	// QPS and Burst limit requests to the API server.
	QPS   float32
	Burst int
}

// RulesFor returns the defaults for a profile. Unknown kinds get the
// production rules.
func RulesFor(k Kind) Rules {
	switch k {
	case Local:
		return Rules{EditingOnByDefault: true, QPS: 50, Burst: 100}
	case Dev, Staging:
		return Rules{EditingOnByDefault: true, QPS: 20, Burst: 40}
	default:
		return Rules{RequireReason: true, ConfirmByName: true, QPS: 10, Burst: 20}
	}
}

var (
	localPrefixes = []string{"kind-", "k3d-", "minikube", "docker-desktop", "docker-for-desktop", "rancher-desktop"}
	prodWords     = []string{"prod", "prd", "production", "live"}
	stagingWords  = []string{"staging", "stage", "stg", "preprod", "uat", "qa"}
	devWords      = []string{"dev", "develop", "development", "test", "testing", "sandbox", "sbx"}
)

// Detect guesses a profile from a context's name, its cluster's name and the
// API server address. Anything Kupi cannot recognise is treated as production.
func Detect(contextName, clusterName, server string) Kind {
	for _, name := range []string{contextName, clusterName} {
		lower := strings.ToLower(name)
		for _, p := range localPrefixes {
			if strings.HasPrefix(lower, p) {
				return Local
			}
		}
	}
	if isLoopbackServer(server) {
		return Local
	}
	words := tokens(contextName + " " + clusterName)
	switch {
	case containsAny(words, prodWords):
		return Production
	case containsAny(words, stagingWords):
		return Staging
	case containsAny(words, devWords):
		return Dev
	}
	return Production
}

func isLoopbackServer(server string) bool {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "host.docker.internal", "0.0.0.0":
		return true
	}
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		out[t] = true
	}
	return out
}

func containsAny(words map[string]bool, list []string) bool {
	for _, w := range list {
		if words[w] {
			return true
		}
	}
	return false
}

// Store keeps the user's manual profile choices, keyed by context name.
type Store struct {
	path      string
	mu        sync.Mutex
	overrides map[string]Kind
}

// OpenStore loads overrides from path, starting empty if it does not exist.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, overrides: map[string]Kind{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profiles: %w", err)
	}
	if err := json.Unmarshal(data, &s.overrides); err != nil {
		return nil, fmt.Errorf("parse profiles: %w", err)
	}
	for ctx, k := range s.overrides {
		if !k.Valid() {
			delete(s.overrides, ctx)
		}
	}
	return s, nil
}

// Resolve returns the profile for a context: the user's choice if there is
// one, otherwise the detected profile.
func (s *Store) Resolve(contextName, clusterName, server string) (kind Kind, overridden bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.overrides[contextName]; ok {
		return k, true
	}
	return Detect(contextName, clusterName, server), false
}

// Set records the user's choice for a context. An empty kind clears it.
func (s *Store) Set(contextName string, k Kind) error {
	if k != "" && !k.Valid() {
		return fmt.Errorf("unknown profile %q", k)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if k == "" {
		delete(s.overrides, contextName)
	} else {
		s.overrides[contextName] = k
	}
	data, err := json.MarshalIndent(s.overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profiles: %w", err)
	}
	return appdir.WriteFileAtomic(s.path, data)
}
