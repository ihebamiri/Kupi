package helmrel

import (
	"strings"
	"testing"
)

const v1 = `---
# Source: shop/templates/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: shop-db
stringData:
  password: hunter2-old
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shop
spec:
  replicas: 1
`

const v2 = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: shop
spec:
  replicas: 3
---
apiVersion: v1
kind: Secret
metadata:
  name: shop-db
stringData:
  password: hunter2-new
---
apiVersion: v1
kind: Service
metadata:
  name: shop
  namespace: other
`

func TestDiffHidesSecretsAndPairsObjects(t *testing.T) {
	d := Diff(v1, v2, "shop")
	if strings.Contains(d, "hunter2") {
		t.Fatalf("diff leaks a value:\n%s", d)
	}
	for _, want := range []string{"-  replicas: 1", "+  replicas: 3", "+  password: •••••• (changed)", "+kind: Service"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff lacks %q:\n%s", want, d)
		}
	}
	if s := SafeManifest(v1, "shop"); strings.Contains(s, "hunter2") || !strings.Contains(s, "namespace: shop") {
		t.Errorf("safe manifest:\n%s", s)
	}
	res := Resources(v2, "shop")
	if len(res) != 3 || res[0].Kind != "Deployment" || res[2].Namespace != "other" {
		t.Errorf("resources = %+v", res)
	}
}

// Seen with the prometheus chart: its ClusterRole has no namespace and
// must not get the release's.
func TestClusterScopedObjectsGetNoNamespace(t *testing.T) {
	objs, err := Objects("kind: ClusterRole\nmetadata:\n  name: prometheus-server\n---\nkind: ConfigMap\nmetadata:\n  name: prometheus-server\n", "monitoring")
	if err != nil || len(objs) != 2 {
		t.Fatalf("objects = %v, %v", objs, err)
	}
	if objs[0].GetNamespace() != "" || objs[1].GetNamespace() != "monitoring" {
		t.Errorf("namespaces = %q, %q", objs[0].GetNamespace(), objs[1].GetNamespace())
	}
}

func TestValuesRoundTrip(t *testing.T) {
	vals, err := ParseValues("replicaCount: 2\nimage:\n  tag: v2\n")
	if err != nil || vals["replicaCount"] != float64(2) {
		t.Fatalf("parse = %v, %v", vals, err)
	}
	if _, err := ParseValues("- a list"); err == nil {
		t.Error("a list was accepted as values")
	}
	if out, _ := ValuesYAML(nil); out != "{}\n" {
		t.Errorf("empty values = %q", out)
	}
}
