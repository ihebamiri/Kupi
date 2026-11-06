// Package helmrel turns Helm releases into views that are safe to show:
// summaries, the rendered manifest with Secret values hidden, the list of
// resources a release owns, and diffs between two manifests.
package helmrel

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/diff"
	"github.com/ihebamiri/kupi/internal/redact"
)

// Release summarises one revision of a release.
type Release struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion"`
	Status       string `json:"status"`
	Description  string `json:"description"`
	Updated      string `json:"updated"`
}

// View summarises a release.
func View(r *rspb.Release) Release {
	v := Release{Name: r.Name, Namespace: r.Namespace, Revision: r.Version}
	if r.Chart != nil && r.Chart.Metadata != nil {
		v.Chart, v.ChartVersion, v.AppVersion = r.Chart.Metadata.Name, r.Chart.Metadata.Version, r.Chart.Metadata.AppVersion
	}
	if r.Info != nil {
		v.Status, v.Description = string(r.Info.Status), r.Info.Description
		if !r.Info.LastDeployed.IsZero() {
			v.Updated = r.Info.LastDeployed.UTC().Format(time.RFC3339)
		}
	}
	return v
}

// Resource is one object a release renders.
type Resource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
}

// Objects splits a rendered manifest into objects. Objects without a
// namespace get the release's.
func Objects(manifest, namespace string) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured
	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, err
		}
		if len(doc) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: doc}
		if obj.GetNamespace() == "" && namespace != "" {
			obj.SetNamespace(namespace)
		}
		out = append(out, obj)
	}
	return out, nil
}

func objKey(o *unstructured.Unstructured) string {
	return o.GetKind() + "/" + o.GetNamespace() + "/" + o.GetName()
}

// Resources lists the objects in a manifest, sorted by kind and name.
func Resources(manifest, namespace string) []Resource {
	objs, _ := Objects(manifest, namespace)
	out := make([]Resource, 0, len(objs))
	for _, o := range objs {
		out = append(out, Resource{APIVersion: o.GetAPIVersion(), Kind: o.GetKind(), Namespace: o.GetNamespace(), Name: o.GetName()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})
	return out
}

// render writes objects as YAML documents in a stable order.
func render(objs map[string]*unstructured.Unstructured, keys []string) string {
	var b bytes.Buffer
	for _, k := range keys {
		o := objs[k]
		if o == nil {
			continue
		}
		data, err := yaml.Marshal(o.Object)
		if err != nil {
			continue
		}
		b.WriteString("---\n")
		b.Write(data)
	}
	return b.String()
}

func index(manifest, namespace string) (map[string]*unstructured.Unstructured, []string) {
	objs, _ := Objects(manifest, namespace)
	m := make(map[string]*unstructured.Unstructured, len(objs))
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		k := objKey(o)
		if _, dup := m[k]; !dup {
			keys = append(keys, k)
		}
		m[k] = o
	}
	return m, keys
}

// SafeManifest returns the manifest with Secret values hidden.
func SafeManifest(manifest, namespace string) string {
	objs, keys := index(manifest, namespace)
	for k, o := range objs {
		objs[k] = redact.Object(o)
	}
	sort.Strings(keys)
	return render(objs, keys)
}

// Diff compares two manifests object by object. Secret values show only as
// changed or unchanged.
func Diff(before, after, namespace string) string {
	a, aKeys := index(before, namespace)
	b, bKeys := index(after, namespace)
	seen := map[string]bool{}
	var keys []string
	for _, k := range append(aKeys, bKeys...) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		a[k], b[k] = redact.SecretPair(a[k], b[k])
	}
	return diff.Unified("current", "after change", render(a, keys), render(b, keys), 3)
}

// ValuesYAML renders values for editing. Empty values render as "{}".
func ValuesYAML(values map[string]any) (string, error) {
	if len(values) == 0 {
		return "{}\n", nil
	}
	b, err := yaml.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseValues reads edited values YAML. Only a mapping is accepted.
func ParseValues(text string) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	if err := yaml.Unmarshal([]byte(text), &out); err != nil {
		return nil, errors.New("the values aren't valid YAML: " + err.Error())
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
