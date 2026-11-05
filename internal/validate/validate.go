// Package validate checks every value that crosses the bridge from the UI.
// The UI is treated as untrusted: each bridge method validates its inputs
// with these helpers before doing anything else.
package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/api/validation/path"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// MaxManifestBytes caps YAML sent from the UI for a single apply.
	MaxManifestBytes = 1 << 20
	// MaxReasonLen caps the free-text reason attached to production changes.
	MaxReasonLen = 500
	// MaxContextNameLen caps kubeconfig context names.
	MaxContextNameLen = 512
	// MaxReplicas is a sanity limit for scale requests.
	MaxReplicas = 10000
)

var (
	versionRe  = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)
	resourceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	kindRe     = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,62}$`)
)

// GroupVersionKind checks the parts of a type reference by kind.
func GroupVersionKind(group, version, kind string) error {
	if err := GroupVersionResource(group, version, "x"); err != nil {
		return err
	}
	if !kindRe.MatchString(kind) {
		return fmt.Errorf("invalid kind %q", kind)
	}
	return nil
}

// ContextName checks a kubeconfig context name. Existence is checked by the
// caller against the loaded kubeconfig.
func ContextName(name string) error {
	if name == "" {
		return fmt.Errorf("context name is required")
	}
	if len(name) > MaxContextNameLen {
		return fmt.Errorf("context name is too long")
	}
	if hasControl(name) {
		return fmt.Errorf("context name contains control characters")
	}
	return nil
}

// Name checks an object name. The API server requires names to be valid path
// segments; most types further restrict them, which the server enforces.
func Name(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > validation.DNS1123SubdomainMaxLength {
		return fmt.Errorf("name is longer than %d characters", validation.DNS1123SubdomainMaxLength)
	}
	if msgs := path.IsValidPathSegmentName(name); len(msgs) > 0 {
		return fmt.Errorf("invalid name %q: %s", name, strings.Join(msgs, "; "))
	}
	if hasControl(name) {
		return fmt.Errorf("name contains control characters")
	}
	return nil
}

// Namespace checks a namespace name. Use OptionalNamespace where "" means
// all namespaces or a cluster-scoped resource.
func Namespace(ns string) error {
	if ns == "" {
		return fmt.Errorf("namespace is required")
	}
	if msgs := validation.IsDNS1123Label(ns); len(msgs) > 0 {
		return fmt.Errorf("invalid namespace %q: %s", ns, strings.Join(msgs, "; "))
	}
	return nil
}

// OptionalNamespace accepts "" or a valid namespace.
func OptionalNamespace(ns string) error {
	if ns == "" {
		return nil
	}
	return Namespace(ns)
}

// GroupVersionResource checks the parts of a resource type reference.
func GroupVersionResource(group, version, resource string) error {
	if group != "" {
		if msgs := validation.IsDNS1123Subdomain(group); len(msgs) > 0 {
			return fmt.Errorf("invalid API group %q", group)
		}
	}
	if !versionRe.MatchString(version) {
		return fmt.Errorf("invalid API version %q", version)
	}
	if len(resource) > validation.DNS1123SubdomainMaxLength || !resourceRe.MatchString(resource) {
		return fmt.Errorf("invalid resource %q", resource)
	}
	return nil
}

// Reason checks the free-text reason a user gives for a production change.
func Reason(reason string) error {
	if len(reason) > MaxReasonLen {
		return fmt.Errorf("reason is longer than %d characters", MaxReasonLen)
	}
	for _, r := range reason {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("reason contains control characters")
		}
	}
	return nil
}

// Replicas checks a requested replica count.
func Replicas(n int32) error {
	if n < 0 || n > MaxReplicas {
		return fmt.Errorf("replicas must be between 0 and %d", MaxReplicas)
	}
	return nil
}

// Manifest checks the size of YAML sent from the UI.
func Manifest(manifest string) error {
	if strings.TrimSpace(manifest) == "" {
		return fmt.Errorf("manifest is empty")
	}
	if len(manifest) > MaxManifestBytes {
		return fmt.Errorf("manifest is larger than %d bytes", MaxManifestBytes)
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
