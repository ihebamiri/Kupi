package graph

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (f *fakeReader) Get(_ context.Context, gvr schema.GroupVersionResource, _ string, name string) (*unstructured.Unstructured, error) {
	for _, o := range f.lists[gvr.Resource] {
		u := &unstructured.Unstructured{Object: o}
		if u.GetName() == name {
			return u, nil
		}
	}
	return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
}

func ownedBy(kind, name, uid string) []interface{} {
	return []interface{}{map[string]interface{}{"kind": kind, "name": name, "uid": uid, "controller": true, "apiVersion": "v1"}}
}

func podObj(name, ownerKind, ownerName, ownerUID string, status map[string]interface{}) map[string]interface{} {
	meta := map[string]interface{}{"name": name, "namespace": "shop", "uid": name + "-uid", "labels": map[string]interface{}{"app": "api"}}
	if ownerKind != "" {
		meta["ownerReferences"] = ownedBy(ownerKind, ownerName, ownerUID)
	}
	return map[string]interface{}{"kind": "Pod", "metadata": meta, "spec": map[string]interface{}{"nodeName": "node-1", "containers": []interface{}{map[string]interface{}{"name": "c"}}}, "status": status}
}

func running(ready bool, waiting string, restarts int64) map[string]interface{} {
	state := map[string]interface{}{"running": map[string]interface{}{}}
	if waiting != "" {
		state = map[string]interface{}{"waiting": map[string]interface{}{"reason": waiting}}
	}
	return map[string]interface{}{"phase": "Running", "podIP": "10.0.0.1", "containerStatuses": []interface{}{map[string]interface{}{"ready": ready, "restartCount": restarts, "state": state}}}
}

func TestPodsOfFollowsOwnership(t *testing.T) {
	r := &fakeReader{lists: map[string][]map[string]interface{}{
		"deployments": {{"kind": "Deployment", "metadata": map[string]interface{}{"name": "api", "namespace": "shop", "uid": "dep-uid"},
			"spec": map[string]interface{}{"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "api"}}}}},
		"replicasets": {
			{"kind": "ReplicaSet", "metadata": map[string]interface{}{"name": "api-new", "namespace": "shop", "uid": "rs-new", "ownerReferences": ownedBy("Deployment", "api", "dep-uid")}},
			{"kind": "ReplicaSet", "metadata": map[string]interface{}{"name": "other", "namespace": "shop", "uid": "rs-other", "ownerReferences": ownedBy("Deployment", "other", "other-uid")}},
		},
		"pods": {
			podObj("api-a", "ReplicaSet", "api-new", "rs-new", running(true, "", 0)),
			podObj("api-b", "ReplicaSet", "api-new", "rs-new", running(false, "CrashLoopBackOff", 7)),
			// Same labels, different owner: not this Deployment's pod.
			podObj("other-a", "ReplicaSet", "other", "rs-other", running(true, "", 0)),
			podObj("done", "ReplicaSet", "api-new", "rs-new", map[string]interface{}{"phase": "Succeeded"}),
		},
		"services": {
			{"kind": "Service", "metadata": map[string]interface{}{"name": "api", "namespace": "shop"}, "spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}}},
			{"kind": "Service", "metadata": map[string]interface{}{"name": "external", "namespace": "shop"}, "spec": map[string]interface{}{"type": "ExternalName"}},
		},
	}}
	ctx := context.Background()

	res, err := PodsOf(ctx, r, "Deployment", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pods) != 3 {
		t.Fatalf("pods = %+v", res.Pods)
	}
	first := res.Pods[0]
	if first.Name != "api-b" || first.Status != "CrashLoopBackOff" || first.Health != Bad || first.Restarts != 7 || first.Ready != "0/1" {
		t.Errorf("failing pod should come first with its reason: %+v", first)
	}
	for _, p := range res.Pods {
		if p.Name == "other-a" {
			t.Error("a pod of another Deployment was included")
		}
		if p.Name == "done" && p.Status != "Completed" {
			t.Errorf("succeeded pod status = %q", p.Status)
		}
		if p.Name == "api-a" && (p.Node != "node-1" || p.IP != "10.0.0.1" || p.Ready != "1/1") {
			t.Errorf("api-a = %+v", p)
		}
	}

	svc, err := PodsOf(ctx, r, "Service", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.Pods) != 4 {
		t.Errorf("a Service lists every pod its selector matches, got %d", len(svc.Pods))
	}
	ext, err := PodsOf(ctx, r, "Service", "shop", "external")
	if err != nil || len(ext.Pods) != 0 || ext.Note == "" {
		t.Errorf("selector-less Service: %+v %v", ext, err)
	}
	if _, err := PodsOf(ctx, r, "ConfigMap", "shop", "x"); err == nil {
		t.Error("ConfigMaps don't run pods")
	}
}
