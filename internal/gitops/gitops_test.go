package gitops

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	objects map[string][]map[string]interface{}
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items, ok := f.objects[gvr.Resource]
	if !ok {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), "")
	}
	l := &unstructured.UnstructuredList{}
	for _, o := range items {
		l.Items = append(l.Items, unstructured.Unstructured{Object: o})
	}
	return l, nil
}

func TestListReadsArgoAndFlux(t *testing.T) {
	r := &fakeReader{objects: map[string][]map[string]interface{}{
		"applications": {{
			"metadata": map[string]interface{}{"name": "shop", "namespace": "argocd"},
			"spec": map[string]interface{}{
				"source":      map[string]interface{}{"repoURL": "git@github.com:acme/deploy.git", "path": "apps/shop", "targetRevision": "main"},
				"destination": map[string]interface{}{"server": "https://kubernetes.default.svc", "namespace": "shop"},
				"syncPolicy":  map[string]interface{}{"automated": map[string]interface{}{"selfHeal": true}},
			},
			"status": map[string]interface{}{
				"sync":      map[string]interface{}{"status": "OutOfSync", "revision": "abc123"},
				"health":    map[string]interface{}{"status": "Healthy"},
				"resources": []interface{}{map[string]interface{}{"status": "Synced"}, map[string]interface{}{"status": "OutOfSync"}},
			},
		}},
		"kustomizations": {{
			"metadata": map[string]interface{}{"name": "apps", "namespace": "flux-system"},
			"spec":     map[string]interface{}{"path": "./clusters/prod", "sourceRef": map[string]interface{}{"kind": "GitRepository", "name": "flux-system"}},
			"status": map[string]interface{}{
				"lastAppliedRevision": "main@sha1:def",
				"conditions":          []interface{}{map[string]interface{}{"type": "Ready", "status": "True", "message": "Applied revision: main@sha1:def"}},
				"inventory":           map[string]interface{}{"entries": []interface{}{map[string]interface{}{}, map[string]interface{}{}}},
			},
		}},
		"gitrepositories": {{
			"metadata": map[string]interface{}{"name": "flux-system", "namespace": "flux-system"},
			"spec":     map[string]interface{}{"url": "ssh://git@github.com/acme/fleet.git"},
		}},
	}}
	ov, err := List(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Installed) != 2 || len(ov.Apps) != 2 {
		t.Fatalf("overview = %+v", ov)
	}
	argo := ov.Apps[0] // out of sync sorts first
	if argo.Tool != ArgoCD || argo.Sync != "OutOfSync" || argo.OutOfSync != 1 || !argo.AutoSync || argo.RepoURL != "https://github.com/acme/deploy" || argo.Destination != "https://kubernetes.default.svc / shop" {
		t.Errorf("argo = %+v", argo)
	}
	flux := ov.Apps[1]
	if flux.Sync != "Ready" || flux.Resources != 2 || flux.RepoURL != "https://github.com/acme/fleet" || flux.Revision != "main@sha1:def" {
		t.Errorf("flux = %+v", flux)
	}
}

func TestOwnerOfAndBrowseURL(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "team-a_shop:apps/Deployment:shop/api"})
	if o := OwnerOf(obj); o == nil || o.Tool != ArgoCD || o.Namespace != "team-a" || o.Name != "shop" {
		t.Errorf("tracking id owner = %+v", o)
	}
	obj.SetAnnotations(nil)
	obj.SetLabels(map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"})
	if o := OwnerOf(obj); o == nil || o.Kind != "Kustomization" || o.Namespace != "flux-system" {
		t.Errorf("flux owner = %+v", o)
	}
	if OwnerOf(&unstructured.Unstructured{}) != nil {
		t.Error("plain object has an owner")
	}
	for in, want := range map[string]string{
		"https://gitlab.com/acme/deploy.git": "https://gitlab.com/acme/deploy",
		"git@github.com:acme/deploy.git":     "https://github.com/acme/deploy",
		"http://insecure.example/repo":       "",
		"javascript:alert(1)":                "",
		"oci://ghcr.io/acme/charts":          "",
	} {
		if got := BrowseURL(in); got != want {
			t.Errorf("BrowseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
