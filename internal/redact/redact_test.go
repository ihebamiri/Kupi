package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func secret(data map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name": "db",
			"annotations": map[string]interface{}{
				"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"aHVudGVyMg=="}}`,
			},
		},
		"data":       data,
		"stringData": map[string]interface{}{"token": "plain-text-token"},
	}}
}

func TestObjectHidesValues(t *testing.T) {
	in := secret(map[string]interface{}{"password": "aHVudGVyMg=="})
	out := Object(in)
	raw, _ := json.Marshal(out.Object)
	for _, leak := range []string{"aHVudGVyMg==", "plain-text-token", "last-applied"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("redacted object still contains %q: %s", leak, raw)
		}
	}
	if v, _, _ := unstructured.NestedString(in.Object, "data", "password"); v != "aHVudGVyMg==" {
		t.Error("Object must not modify its input")
	}
}

func TestSecretPairMarksChanges(t *testing.T) {
	oldObj := secret(map[string]interface{}{"password": "b2xk", "user": "YWRtaW4=", "gone": "eA=="})
	newObj := secret(map[string]interface{}{"password": "bmV3", "user": "YWRtaW4=", "added": "eQ=="})
	o, n := SecretPair(oldObj, newObj)
	check := func(obj *unstructured.Unstructured, key, want string) {
		t.Helper()
		if got, _, _ := unstructured.NestedString(obj.Object, "data", key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	check(n, "password", Changed)
	check(o, "password", Previous)
	check(n, "user", Unchanged)
	check(o, "user", Unchanged)
	check(n, "added", Changed)
	check(o, "gone", Placeholder)
}

func TestNonSecretsKeepData(t *testing.T) {
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "c"},
		"data":     map[string]interface{}{"color": "blue"},
	}}
	if got, _, _ := unstructured.NestedString(Object(cm).Object, "data", "color"); got != "blue" {
		t.Errorf("ConfigMap data changed: %q", got)
	}
}
