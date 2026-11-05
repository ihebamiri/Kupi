package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResourcePath(t *testing.T) {
	cases := []struct {
		gvr        schema.GroupVersionResource
		namespaced bool
		ns, want   string
	}{
		{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true, "shop", "/api/v1/namespaces/shop/pods"},
		{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true, "", "/api/v1/pods"},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true, "a", "/apis/apps/v1/namespaces/a/deployments"},
		{schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, false, "ignored", "/api/v1/nodes"},
	}
	for _, c := range cases {
		if got := resourcePath(c.gvr, c.namespaced, c.ns); got != c.want {
			t.Errorf("resourcePath(%v, %v, %q) = %q, want %q", c.gvr, c.namespaced, c.ns, got, c.want)
		}
	}
}
