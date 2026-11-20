package graph

import (
	"context"
	"testing"
)

func node(name string, ready, unschedulable bool, cpu, mem string) map[string]interface{} {
	status := "False"
	if ready {
		status = "True"
	}
	return map[string]interface{}{
		"metadata": map[string]interface{}{"name": name},
		"spec":     map[string]interface{}{"unschedulable": unschedulable},
		"status": map[string]interface{}{
			"allocatable": map[string]interface{}{"cpu": cpu, "memory": mem},
			"conditions":  []interface{}{map[string]interface{}{"type": "Ready", "status": status}},
		},
	}
}

func pod(name, nodeName string, containers, initContainers []interface{}) map[string]interface{} {
	spec := map[string]interface{}{"nodeName": nodeName, "containers": containers}
	if initContainers != nil {
		spec["initContainers"] = initContainers
	}
	return map[string]interface{}{"metadata": objMeta(name, nil), "spec": spec}
}

func ctr(cpu, mem string) interface{} {
	return map[string]interface{}{"resources": map[string]interface{}{"requests": map[string]interface{}{"cpu": cpu, "memory": mem}}}
}

func TestClusterCapacity(t *testing.T) {
	r := &fakeReader{lists: map[string][]map[string]interface{}{
		"nodes": {
			node("a", true, false, "2", "4Gi"),
			node("b", true, false, "1500m", "2Gi"),
			node("cordoned", true, true, "8", "16Gi"),
			node("down", false, false, "8", "16Gi"),
		},
		"pods": {
			pod("web-1", "a", []interface{}{ctr("500m", "512Mi"), ctr("100m", "64Mi")}, nil),
			// An init container that asks for more than the containers counts instead.
			pod("job-1", "b", []interface{}{ctr("100m", "128Mi")}, []interface{}{ctr("1", "64Mi")}),
			// Pods on nodes that don't count, or not placed yet, are ignored.
			pod("x", "cordoned", []interface{}{ctr("4", "8Gi")}, nil),
			pod("pending", "", []interface{}{ctr("4", "8Gi")}, nil),
		},
	}}
	c, err := ClusterCapacity(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	gi := int64(1 << 30)
	mi := int64(1 << 20)
	want := Capacity{
		Nodes:          2,
		CPUAllocatable: 3500,
		MemAllocatable: 6 * gi,
		CPURequested:   600 + 1000,
		MemRequested:   576*mi + 128*mi,
		LargestFreeCPU: 2000 - 600,
		LargestFreeMem: 4*gi - 576*mi,
	}
	if *c != want {
		t.Fatalf("got %+v\nwant %+v", *c, want)
	}
}
