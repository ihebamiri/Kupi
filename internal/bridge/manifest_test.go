package bridge

import (
	"strings"
	"testing"
)

func TestParseObjects(t *testing.T) {
	manifest := `# a copied manifest
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
---
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: Service
  metadata:
    name: web
- apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: web
`
	objs, err := parseObjects(manifest, 10)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, o := range objs {
		kinds = append(kinds, o.GetKind())
	}
	if got := strings.Join(kinds, ","); got != "ConfigMap,Deployment,Service,ServiceAccount" {
		t.Errorf("kinds = %s", got)
	}

	if _, err := parseObjects(manifest, 3); err == nil || !strings.Contains(err.Error(), "more than 3") {
		t.Errorf("limit: %v", err)
	}
	if _, err := parseSingleObject(manifest); err == nil || !strings.Contains(err.Error(), "one at a time") {
		t.Errorf("single: %v", err)
	}
	if _, err := parseObjects("---\n# nothing\n---\n", 10); err == nil {
		t.Error("an empty manifest must fail")
	}
	if _, err := parseObjects("metadata:\n  name: x\n", 10); err == nil || !strings.Contains(err.Error(), "no kind") {
		t.Errorf("missing kind: %v", err)
	}
	if _, err := parseObjects(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"j"}}`, 10); err != nil {
		t.Errorf("JSON: %v", err)
	}
}
