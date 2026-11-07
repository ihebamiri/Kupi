package bridge

import (
	"strings"
	"testing"
)

func TestGitDocumentDropsClusterFields(t *testing.T) {
	obj, err := parseSingleObject(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: shop
  uid: 1234
  resourceVersion: "99"
  generation: 3
  creationTimestamp: "2026-11-01T00:00:00Z"
  labels:
    app: web
    argocd.argoproj.io/instance: shop
  annotations:
    argocd.argoproj.io/tracking-id: shop:apps/Deployment:shop/web
    deployment.kubernetes.io/revision: "4"
spec:
  replicas: 3
status:
  readyReplicas: 3
`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gitDocument(obj)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "status", "readyReplicas", "argocd.argoproj.io", "deployment.kubernetes.io", "annotations"} {
		if strings.Contains(doc, gone) {
			t.Errorf("%s left in:\n%s", gone, doc)
		}
	}
	for _, kept := range []string{"app: web", "replicas: 3", "namespace: shop"} {
		if !strings.Contains(doc, kept) {
			t.Errorf("%s missing:\n%s", kept, doc)
		}
	}
}
