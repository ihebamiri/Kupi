package catalog

import (
	"testing"

	"github.com/ihebamiri/kupi/internal/kube"
)

func TestBuild(t *testing.T) {
	types := []kube.ResourceType{
		{Group: "example.com", Version: "v1", Resource: "widgets", Kind: "Widget"},
		{Group: "", Version: "v1", Resource: "secrets", Kind: "Secret"},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment"},
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod"},
		{Group: "events.k8s.io", Version: "v1", Resource: "events", Kind: "Event"},
		{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes", Kind: "HTTPRoute"},
	}
	got := Build(types)
	var order []string
	for _, e := range got {
		order = append(order, string(e.Category)+":"+e.Label)
	}
	want := []string{"workloads:Pods", "workloads:Deployments", "network:HTTPRoutes", "config:Secrets", "custom:Widgets"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("position %d = %s, want %s (all: %v)", i, order[i], want[i], order)
		}
	}
}

func TestPlural(t *testing.T) {
	for kind, want := range map[string]string{
		"Deployment": "Deployments", "NetworkPolicy": "NetworkPolicies", "Ingress": "Ingresses",
		"Endpoints": "Endpoints", "Gateway": "Gateways", "StorageClass": "StorageClasses", "Status": "Status",
	} {
		if got := Plural(kind); got != want {
			t.Errorf("Plural(%q) = %q, want %q", kind, got, want)
		}
	}
}
