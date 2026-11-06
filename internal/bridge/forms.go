package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/iheb-amiri/kupi/internal/guard"
	kschema "github.com/iheb-amiri/kupi/internal/schema"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// ParseResult is a YAML document converted to JSON, or why it couldn't be.
// Errors are expected while the user is typing, so they are data, not failures.
type ParseResult struct {
	JSON  string `json:"json"`
	Error string `json:"error"`
	// Objects lists "Kind name" for each object when the manifest holds
	// several; JSON is then empty because the form edits one at a time.
	Objects []string `json:"objects"`
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

// FromYAML parses a YAML manifest. One object comes back as JSON for the
// form; several come back as a list of names.
func (s *FormService) FromYAML(manifest string) ParseResult {
	if err := validate.Manifest(manifest); err != nil {
		return ParseResult{Error: err.Error()}
	}
	objs, err := parseObjects(manifest, guard.MaxBatchObjects)
	if err != nil {
		return ParseResult{Error: err.Error()}
	}
	if len(objs) > 1 {
		names := make([]string, 0, len(objs))
		for _, o := range objs {
			names = append(names, strings.TrimSpace(o.GetKind()+" "+o.GetName()))
		}
		return ParseResult{Objects: names}
	}
	data, err := json.Marshal(objs[0].Object)
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

// suggestTypes are the object types the form can suggest names for. A fixed
// list: the form never asks for arbitrary types.
var suggestTypes = map[string]struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}{
	"namespaces":             {schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, false},
	"configmaps":             {schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true},
	"secrets":                {schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true},
	"serviceaccounts":        {schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, true},
	"services":               {schema.GroupVersionResource{Version: "v1", Resource: "services"}, true},
	"persistentvolumeclaims": {schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, true},
	"persistentvolumes":      {schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, false},
	"nodes":                  {schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, false},
	"deployments":            {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
	"statefulsets":           {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true},
	"replicasets":            {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, true},
	"storageclasses":         {schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, false},
	"ingressclasses":         {schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}, false},
	"priorityclasses":        {schema.GroupVersionResource{Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses"}, false},
	"roles":                  {schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true},
	"clusterroles":           {schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false},
}

// maxSuggestions caps how many names one suggestion list holds.
const maxSuggestions = 500

// Suggest returns the names of existing objects of one type, for the form's
// drop-down lists. Namespace is ignored for cluster-wide types.
func (s *FormService) Suggest(ctx context.Context, contextName, resource, namespace string) ([]string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	t, ok := suggestTypes[resource]
	if !ok {
		return nil, fmt.Errorf("no suggestions for %q", resource)
	}
	ns := ""
	if t.namespaced {
		if err := validate.Namespace(namespace); err != nil {
			return nil, err
		}
		ns = namespace
	}
	list, err := sess.Reader().List(ctx, t.gvr, ns, metav1.ListOptions{Limit: maxSuggestions})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		if resource == "secrets" && isServiceAccountToken(&it) {
			continue
		}
		names = append(names, it.GetName())
	}
	sort.Strings(names)
	return names, nil
}

// Keys returns the key names (never the values) of a ConfigMap or Secret,
// for fields such as configMapKeyRef.key and secretKeyRef.key.
func (s *FormService) Keys(ctx context.Context, contextName, resource, namespace, name string) ([]string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if resource != "configmaps" && resource != "secrets" {
		return nil, fmt.Errorf("keys are only listed for ConfigMaps and Secrets")
	}
	if err := validate.Namespace(namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(name); err != nil {
		return nil, err
	}
	obj, err := sess.Reader().Get(ctx, suggestTypes[resource].gvr, namespace, name)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, field := range []string{"data", "binaryData", "stringData"} {
		m, _, _ := unstructured.NestedMap(obj.Object, field)
		for k := range m {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return slices.Compact(keys), nil
}

func isServiceAccountToken(obj *unstructured.Unstructured) bool {
	t, _, _ := unstructured.NestedString(obj.Object, "type")
	return t == "kubernetes.io/service-account-token"
}
