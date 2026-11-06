package graph

import (
	"context"
	"encoding/json"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	lists map[string][]map[string]interface{}
	names map[string][]string
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items, ok := f.lists[gvr.Resource]
	if !ok {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), "")
	}
	out := &unstructured.UnstructuredList{}
	for _, o := range items {
		out.Items = append(out.Items, unstructured.Unstructured{Object: o})
	}
	return out, nil
}

func (f *fakeReader) Table(_ context.Context, gvr schema.GroupVersionResource, _ bool, _ string, _ kube.TableOptions) (*metav1.Table, error) {
	t := &metav1.Table{}
	for _, n := range f.names[gvr.Resource] {
		raw, _ := json.Marshal(metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "shop"}})
		t.Rows = append(t.Rows, metav1.TableRow{Object: runtime.RawExtension{Raw: raw}})
	}
	return t, nil
}

func objMeta(name string, extra map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{"name": name, "namespace": "shop", "uid": name + "-uid"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

var yes = true

func owned(kind, name string) map[string]interface{} {
	return map[string]interface{}{"ownerReferences": []interface{}{map[string]interface{}{"kind": kind, "name": name, "controller": yes, "apiVersion": "v1", "uid": "x"}}}
}

func app() *fakeReader {
	template := map[string]interface{}{
		"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "api"}},
		"spec": map[string]interface{}{
			"containers": []interface{}{map[string]interface{}{
				"name": "api", "image": "api:1",
				"envFrom": []interface{}{map[string]interface{}{"configMapRef": map[string]interface{}{"name": "api-config"}}},
				"env":     []interface{}{map[string]interface{}{"name": "PW", "valueFrom": map[string]interface{}{"secretKeyRef": map[string]interface{}{"name": "db-creds", "key": "pw"}}}},
			}},
			"volumes": []interface{}{map[string]interface{}{"name": "data", "persistentVolumeClaim": map[string]interface{}{"claimName": "api-data"}}},
		},
	}
	rsMeta := objMeta("api-5d4", owned("Deployment", "api"))
	podMeta := func(n string) map[string]interface{} {
		m := objMeta(n, owned("ReplicaSet", "api-5d4"))
		m["labels"] = map[string]interface{}{"app": "api"}
		return m
	}
	pod := func(n string, waiting string) map[string]interface{} {
		state := map[string]interface{}{"running": map[string]interface{}{}}
		ready := true
		if waiting != "" {
			state = map[string]interface{}{"waiting": map[string]interface{}{"reason": waiting}}
			ready = false
		}
		return map[string]interface{}{"kind": "Pod", "metadata": podMeta(n), "spec": template["spec"],
			"status": map[string]interface{}{"phase": "Running", "containerStatuses": []interface{}{map[string]interface{}{"ready": ready, "restartCount": int64(4), "state": state}}}}
	}
	return &fakeReader{
		lists: map[string][]map[string]interface{}{
			"deployments": {{"kind": "Deployment", "metadata": objMeta("api", nil),
				"spec": map[string]interface{}{"replicas": int64(2), "template": template}, "status": map[string]interface{}{"readyReplicas": int64(1)}}},
			"replicasets": {{"kind": "ReplicaSet", "metadata": rsMeta, "spec": map[string]interface{}{"template": template}}},
			"pods":        {pod("api-1", ""), pod("api-2", "CrashLoopBackOff")},
			"services": {
				{"kind": "Service", "metadata": objMeta("api", nil), "spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}}},
				{"kind": "Service", "metadata": objMeta("orphan", nil), "spec": map[string]interface{}{"type": "NodePort", "selector": map[string]interface{}{"app": "nothing"}}},
			},
			"ingresses": {{"kind": "Ingress", "metadata": objMeta("web", nil), "spec": map[string]interface{}{"rules": []interface{}{
				map[string]interface{}{"host": "shop.example.com", "http": map[string]interface{}{"paths": []interface{}{
					map[string]interface{}{"backend": map[string]interface{}{"service": map[string]interface{}{"name": "api"}}},
					map[string]interface{}{"backend": map[string]interface{}{"service": map[string]interface{}{"name": "gone"}}},
				}}},
			}}}},
			"horizontalpodautoscalers": {{"kind": "HorizontalPodAutoscaler", "metadata": objMeta("api", nil),
				"spec": map[string]interface{}{"scaleTargetRef": map[string]interface{}{"kind": "Deployment", "name": "api"}, "maxReplicas": int64(5)}}},
			"persistentvolumeclaims": {{"kind": "PersistentVolumeClaim", "metadata": objMeta("api-data", nil),
				"spec": map[string]interface{}{"resources": map[string]interface{}{"requests": map[string]interface{}{"storage": "1Gi"}}}, "status": map[string]interface{}{"phase": "Bound"}}},
		},
		names: map[string][]string{"configmaps": {"api-config"}},
	}
}

func find(t *testing.T, g *Graph, id string) Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %s missing; have %d nodes", id, len(g.Nodes))
	return Node{}
}

func hasEdge(g *Graph, from, to, kind string) bool {
	for _, e := range g.Edges {
		if e.Source == from && e.Target == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestBuild(t *testing.T) {
	g, err := Build(context.Background(), app(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	dep := find(t, g, "Deployment/shop/api")
	if dep.Pods != 2 || dep.Ready != 1 || dep.Restarts != 8 || dep.Health != Bad {
		t.Errorf("deployment = pods %d ready %d restarts %d health %s", dep.Pods, dep.Ready, dep.Restarts, dep.Health)
	}
	for _, id := range []string{"ReplicaSet/shop/api-5d4", "Pod/shop/api-1"} {
		for _, n := range g.Nodes {
			if n.ID == id {
				t.Errorf("%s should be folded into its Deployment", id)
			}
		}
	}
	for _, e := range []struct{ from, to, kind string }{
		{"Ingress/shop/web", "Service/shop/api", "routes"},
		{"Service/shop/api", "Deployment/shop/api", "selects"},
		{"Deployment/shop/api", "ConfigMap/shop/api-config", "uses"},
		{"Deployment/shop/api", "Secret/shop/db-creds", "uses"},
		{"Deployment/shop/api", "PersistentVolumeClaim/shop/api-data", "mounts"},
		{"HorizontalPodAutoscaler/shop/api", "Deployment/shop/api", "scales"},
	} {
		if !hasEdge(g, e.from, e.to, e.kind) {
			t.Errorf("missing edge %s -%s-> %s", e.from, e.kind, e.to)
		}
	}
	if s := find(t, g, "Secret/shop/db-creds"); s.Health != Bad {
		t.Error("a referenced Secret that doesn't exist should be marked missing")
	}
	if s := find(t, g, "Service/shop/gone"); s.Health != Bad {
		t.Error("an Ingress backend that doesn't exist should be marked missing")
	}
	if ing := find(t, g, "Ingress/shop/web"); ing.Health != Bad {
		t.Error("an Ingress with a missing backend should be unhealthy")
	}
	orphan := find(t, g, "Service/shop/orphan")
	if orphan.Health != Warn || !contains(orphan.Flags, FlagPublic) {
		t.Errorf("orphan service = %s %v", orphan.Health, orphan.Flags)
	}
	if !contains(dep.Flags, FlagRoot) || !contains(dep.Flags, FlagNoLimits) {
		t.Errorf("deployment flags = %v, want root and no-limit warnings", dep.Flags)
	}
	if pvc := find(t, g, "PersistentVolumeClaim/shop/api-data"); pvc.Health != Good {
		t.Errorf("bound claim health = %s", pvc.Health)
	}
}

func TestAutoInjectedCAIsHidden(t *testing.T) {
	spec := map[string]interface{}{"volumes": []interface{}{map[string]interface{}{
		"name": "kube-api-access-x",
		"projected": map[string]interface{}{"sources": []interface{}{
			map[string]interface{}{"serviceAccountToken": map[string]interface{}{"path": "token"}},
			map[string]interface{}{"configMap": map[string]interface{}{"name": "kube-root-ca.crt"}},
			map[string]interface{}{"configMap": map[string]interface{}{"name": "app-config"}},
		}},
	}}}
	refs := podRefs(spec)
	if len(refs) != 1 || refs[0].name != "app-config" {
		t.Errorf("refs = %+v, want only app-config", refs)
	}
}

func TestSecurityFlags(t *testing.T) {
	spec := map[string]interface{}{
		"hostNetwork":     true,
		"securityContext": map[string]interface{}{"runAsNonRoot": true},
		"volumes":         []interface{}{map[string]interface{}{"name": "h", "hostPath": map[string]interface{}{"path": "/"}}},
		"containers": []interface{}{map[string]interface{}{
			"name": "a", "securityContext": map[string]interface{}{"privileged": true},
			"resources": map[string]interface{}{"limits": map[string]interface{}{"memory": "1Gi"}},
		}},
	}
	flags := securityFlags(spec)
	for _, f := range []string{FlagHostNetwork, FlagHostPath, FlagPrivileged} {
		if !contains(flags, f) {
			t.Errorf("missing %s in %v", f, flags)
		}
	}
	if contains(flags, FlagRoot) || contains(flags, FlagNoLimits) {
		t.Errorf("unexpected flags %v", flags)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
