package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	kschema "github.com/iheb-amiri/kupi/internal/schema"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// ParseResult is a YAML document converted to JSON, or why it couldn't be.
// Errors are expected while the user is typing, so they are data, not failures.
type ParseResult struct {
	JSON  string `json:"json"`
	Error string `json:"error"`
}

// FormService backs the form and YAML editors.
type FormService struct{ d Deps }

// NewFormService returns the form service.
func NewFormService(d Deps) *FormService { return &FormService{d: d} }

// Schema returns the form tree (JSON) for a kind, read from the cluster's
// own OpenAPI schema, so custom resources get forms too.
func (s *FormService) Schema(ctx context.Context, contextName, group, version, kind string) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	if err := validate.GroupVersionKind(group, version, kind); err != nil {
		return "", err
	}
	doc, err := sess.Reader().OpenAPI(ctx, schema.GroupVersion{Group: group, Version: version})
	if err != nil {
		return "", err
	}
	node, err := kschema.Build(doc, group, version, kind)
	if err != nil {
		return "", fmt.Errorf("%s/%s %s: %w", group, version, kind, err)
	}
	data, err := json.Marshal(node)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToYAML renders a JSON object as YAML.
func (s *FormService) ToYAML(objectJSON string) (string, error) {
	if len(objectJSON) > validate.MaxManifestBytes {
		return "", fmt.Errorf("object is too large")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(objectJSON), &obj); err != nil {
		return "", fmt.Errorf("invalid object: %w", err)
	}
	out, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FromYAML parses one YAML object into JSON.
func (s *FormService) FromYAML(manifest string) ParseResult {
	if err := validate.Manifest(manifest); err != nil {
		return ParseResult{Error: err.Error()}
	}
	obj, err := parseSingleObject(manifest)
	if err != nil {
		return ParseResult{Error: err.Error()}
	}
	data, err := json.Marshal(obj.Object)
	if err != nil {
		return ParseResult{Error: err.Error()}
	}
	return ParseResult{JSON: string(data)}
}

// Object returns a live object as clean YAML for editing: server-managed
// fields and status removed. Secrets are refused here; they get their own
// editor so values never pass through a plain text field.
func (s *FormService) Object(ctx context.Context, contextName string, ref ResourceRef) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return "", err
	}
	if req.GVR.Group == "" && req.GVR.Resource == "secrets" {
		return "", fmt.Errorf("Secrets are edited in the secrets screen, so their values never pass through plain YAML")
	}
	obj, err := sess.Reader().Get(ctx, req.GVR, ref.Namespace, ref.Name)
	if err != nil {
		return "", err
	}
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(obj.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	if annotations, found, _ := unstructured.NestedMap(obj.Object, "metadata", "annotations"); found && len(annotations) == 0 {
		unstructured.RemoveNestedField(obj.Object, "metadata", "annotations")
	}
	unstructured.RemoveNestedField(obj.Object, "status")
	out, err := yaml.Marshal(obj.Object)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// MatchingPods counts pods in a namespace whose labels match selector, so
// the editor can warn about a Service or Deployment that selects nothing.
func (s *FormService) MatchingPods(ctx context.Context, contextName, namespace string, selector map[string]string) (int, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return 0, err
	}
	if err := validate.Namespace(namespace); err != nil {
		return 0, err
	}
	if len(selector) == 0 {
		return 0, fmt.Errorf("the selector is empty")
	}
	for k, v := range selector {
		if msgs := validation.IsQualifiedName(k); len(msgs) > 0 {
			return 0, fmt.Errorf("invalid label key %q", k)
		}
		if msgs := validation.IsValidLabelValue(v); len(msgs) > 0 {
			return 0, fmt.Errorf("invalid label value %q", v)
		}
	}
	list, err := sess.Reader().List(ctx, podsGVR, namespace, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(selector).String()})
	if err != nil {
		return 0, err
	}
	return len(list.Items), nil
}
