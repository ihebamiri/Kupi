// Package redact hides secret values before objects are shown, diffed or
// logged. Kubernetes Secrets are only base64-encoded, so any copy of their
// data that reaches the UI, an error message or the audit log is a leak.
package redact

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// Placeholder replaces a secret value.
	Placeholder = "••••••"
	// Unchanged marks a secret value that is the same on both sides of a diff.
	Unchanged = "•••••• (unchanged)"
	// Changed marks the new side of a secret value that differs in a diff.
	Changed = "•••••• (changed)"
	// Previous marks the old side of a secret value that differs in a diff.
	// It must differ from Changed, or the diff would show no change at all.
	Previous = "•••••• (before)"

	lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"
)

// IsSecret reports whether obj is a core/v1 Secret.
func IsSecret(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	gvk := obj.GroupVersionKind()
	return gvk.Group == "" && gvk.Kind == "Secret"
}

// Object returns a deep copy of obj that is safe to display. For Secrets every
// value under data and stringData is replaced with Placeholder. For every
// object the last-applied-configuration annotation is removed, because for
// Secrets it holds the full data in plain text.
func Object(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	out := obj.DeepCopy()
	stripLastApplied(out)
	if IsSecret(out) {
		for _, field := range []string{"data", "stringData"} {
			values, found, _ := unstructured.NestedMap(out.Object, field)
			if !found {
				continue
			}
			for k := range values {
				values[k] = Placeholder
			}
			_ = unstructured.SetNestedMap(out.Object, values, field)
		}
	}
	return out
}

// SecretPair returns display-safe copies of the old and new versions of an
// object for a diff. For Secrets, each value becomes Unchanged or Changed, so
// the diff shows which keys changed without revealing any value. Either side
// may be nil (object created or deleted).
func SecretPair(oldObj, newObj *unstructured.Unstructured) (*unstructured.Unstructured, *unstructured.Unstructured) {
	if !IsSecret(oldObj) && !IsSecret(newObj) {
		return Object(oldObj), Object(newObj)
	}
	var oldOut, newOut *unstructured.Unstructured
	if oldObj != nil {
		oldOut = oldObj.DeepCopy()
		stripLastApplied(oldOut)
	}
	if newObj != nil {
		newOut = newObj.DeepCopy()
		stripLastApplied(newOut)
	}
	for _, field := range []string{"data", "stringData"} {
		oldVals := nestedMap(oldOut, field)
		newVals := nestedMap(newOut, field)
		for k, nv := range newVals {
			if ov, ok := oldVals[k]; ok && reflect.DeepEqual(ov, nv) {
				newVals[k] = Unchanged
				oldVals[k] = Unchanged
			} else {
				newVals[k] = Changed
				if ok {
					oldVals[k] = Previous
				}
			}
		}
		for k := range oldVals {
			if _, ok := newVals[k]; !ok {
				oldVals[k] = Placeholder
			}
		}
		setNestedMap(oldOut, oldVals, field)
		setNestedMap(newOut, newVals, field)
	}
	return oldOut, newOut
}

func stripLastApplied(obj *unstructured.Unstructured) {
	annotations := obj.GetAnnotations()
	if _, ok := annotations[lastAppliedAnnotation]; !ok {
		return
	}
	delete(annotations, lastAppliedAnnotation)
	if len(annotations) == 0 {
		annotations = nil
	}
	obj.SetAnnotations(annotations)
}

func nestedMap(obj *unstructured.Unstructured, field string) map[string]interface{} {
	if obj == nil {
		return map[string]interface{}{}
	}
	m, found, _ := unstructured.NestedMap(obj.Object, field)
	if !found {
		return map[string]interface{}{}
	}
	return m
}

func setNestedMap(obj *unstructured.Unstructured, m map[string]interface{}, field string) {
	if obj == nil || len(m) == 0 {
		return
	}
	_ = unstructured.SetNestedMap(obj.Object, m, field)
}
