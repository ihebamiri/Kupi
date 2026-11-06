package guard

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/validate"
)

var secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// SecretWrite creates a Secret or changes some of its keys. Keys not named
// in Set or Delete are left exactly as they are, so their values never
// travel through Kupi.
type SecretWrite struct {
	Context   string
	Namespace string
	Name      string
	// Type is used only when creating, e.g. Opaque or kubernetes.io/tls.
	Type   string
	Set    map[string][]byte
	Delete []string
	Labels map[string]string
}

// PrepareSecretWrite previews creating or updating a Secret. The diff shows
// which keys change, never their values.
func (g *Guard) PrepareSecretWrite(ctx context.Context, req SecretWrite) (*Plan, error) {
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	if err := validate.Namespace(req.Namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(req.Name); err != nil {
		return nil, err
	}
	for k := range req.Set {
		if err := validSecretKey(k); err != nil {
			return nil, err
		}
	}
	for _, k := range req.Delete {
		if err := validSecretKey(k); err != nil {
			return nil, err
		}
	}
	if len(req.Set) == 0 && len(req.Delete) == 0 {
		return nil, fmt.Errorf("nothing to change")
	}

	reader := s.Reader()
	current, err := reader.Get(ctx, secretsGVR, req.Namespace, req.Name)
	exists := err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("read the Secret: %w", err)
	}

	p := basePlan(s, rules, editing, ActionApply)
	p.APIVersion, p.Kind, p.Namespace, p.Name = "v1", "Secret", req.Namespace, req.Name

	data := map[string]interface{}{}
	for k, v := range req.Set {
		data[k] = base64.StdEncoding.EncodeToString(v)
	}
	if !exists {
		if len(req.Delete) > 0 {
			return nil, fmt.Errorf("the Secret doesn't exist, so there are no keys to delete")
		}
		typ := req.Type
		if typ == "" {
			typ = "Opaque"
		}
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1", "kind": "Secret",
			"metadata": map[string]interface{}{"name": req.Name, "namespace": req.Namespace},
			"type":     typ,
			"data":     data,
		}}
		if len(req.Labels) > 0 {
			obj.SetLabels(req.Labels)
		}
		g.checkRBAC(ctx, p, reader, "create", secretsGVR, req.Namespace, "")
		after, err := s.Mutator().Apply(ctx, secretsGVR, req.Namespace, obj, kube.ApplyOptions{DryRun: true})
		if err != nil {
			p.deny("The cluster rejected this Secret: " + err.Error())
			after = obj
		}
		p.Diff, _ = render(nil, after)
		p.Summary = []string{fmt.Sprintf("Creates Secret %s/%s (%s) with keys %s.", req.Namespace, req.Name, typ, keyNames(req.Set))}
		p.exec = func(ctx context.Context, m kube.Mutator) error {
			_, err := m.Apply(ctx, secretsGVR, req.Namespace, obj, kube.ApplyOptions{})
			return err
		}
		return g.store(p), nil
	}

	g.checkRBAC(ctx, p, reader, "patch", secretsGVR, req.Namespace, req.Name)
	p.Warnings = append(p.Warnings, ownerWarnings(current)...)
	if owner := managingOwner(current); owner != "" {
		p.Warnings = append(p.Warnings, fmt.Sprintf("This Secret is created by %s, which will overwrite changes made here. Change it at the source instead.", owner))
	}
	for _, k := range req.Delete {
		data[k] = nil
	}
	patch, err := mergePatch(current.GetResourceVersion(), map[string]interface{}{"data": data})
	if err != nil {
		return nil, err
	}
	after, err := s.Mutator().MergePatch(ctx, secretsGVR, req.Namespace, req.Name, patch, true)
	if err != nil {
		p.deny("The cluster rejected this change: " + err.Error())
		after = nil
	}
	if after != nil {
		p.Diff, _ = render(current, after)
	}
	p.Summary = []string{fmt.Sprintf("Updates Secret %s/%s: %s.", req.Namespace, req.Name, keyList(req.Set, req.Delete))}
	p.exec = func(ctx context.Context, m kube.Mutator) error {
		_, err := m.MergePatch(ctx, secretsGVR, req.Namespace, req.Name, patch, false)
		return err
	}
	return g.store(p), nil
}

// managingOwner names a controller that generates the Secret, if any.
func managingOwner(obj *unstructured.Unstructured) string {
	for _, o := range obj.GetOwnerReferences() {
		switch o.Kind {
		case "ExternalSecret", "SealedSecret", "Certificate", "PushSecret":
			return fmt.Sprintf("the %s %s", o.Kind, o.Name)
		}
	}
	return ""
}

func keyList(set map[string][]byte, del []string) string {
	var parts []string
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, "sets "+k)
	}
	for _, k := range del {
		parts = append(parts, "removes "+k)
	}
	if len(parts) == 0 {
		return "no keys"
	}
	return strings.Join(parts, ", ")
}

// keyNames lists the keys being set, sorted.
func keyNames(set map[string][]byte) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// validSecretKey follows the API server's rule for data keys.
func validSecretKey(k string) error {
	if k == "" || len(k) > 253 {
		return fmt.Errorf("key names must be 1 to 253 characters")
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return fmt.Errorf("key %q may only use letters, digits, '-', '_' and '.'", k)
		}
	}
	return nil
}

// AccessNeedsReason reports whether reading sensitive data, such as a Secret
// value, needs a written reason on this context.
func (g *Guard) AccessNeedsReason(contextName string) bool {
	s, ok := g.sessions.Session(contextName)
	return !ok || profile.RulesFor(s.Profile()).RequireReason
}

// RecordAccess audits reading sensitive data. Where the profile asks for
// reasons, one is required. If the entry can't be written the caller must
// not hand out the data.
func (g *Guard) RecordAccess(contextName, action, resource, reason string) error {
	s, ok := g.sessions.Session(contextName)
	if !ok {
		return ErrNotConnected
	}
	if err := validate.Reason(reason); err != nil {
		return err
	}
	if profile.RulesFor(s.Profile()).RequireReason && strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	if _, err := g.audit.Append(audit.Entry{
		Context:  contextName,
		Profile:  string(s.Profile()),
		OSUser:   g.osUser,
		KubeUser: s.UserName(),
		Action:   action,
		Resource: resource,
		Reason:   reason,
		Outcome:  audit.OutcomeInfo,
	}); err != nil {
		return fmt.Errorf("the access could not be written to the audit log: %w", err)
	}
	return nil
}
