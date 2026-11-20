package graph

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
)

var nodeGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}

// Capacity is how much CPU and memory the cluster's schedulable nodes offer
// and how much running pods already request. The scheduler places pods by
// requests, not live use, so this is what decides whether new pods fit.
type Capacity struct {
	Nodes int `json:"nodes"`
	// CPU in millicores, memory in bytes.
	CPUAllocatable int64 `json:"cpuAllocatable"`
	MemAllocatable int64 `json:"memAllocatable"`
	CPURequested   int64 `json:"cpuRequested"`
	MemRequested   int64 `json:"memRequested"`
	// The most free CPU and memory on any one node: a pod must fit on a single node.
	LargestFreeCPU int64 `json:"largestFreeCpu"`
	LargestFreeMem int64 `json:"largestFreeMem"`
}

type nodeRoom struct{ cpu, mem, reqCPU, reqMem int64 }

// ClusterCapacity sums allocatable resources of Ready, schedulable nodes and
// the requests of pods placed on them that haven't finished.
func ClusterCapacity(ctx context.Context, r kube.Reader) (*Capacity, error) {
	nodes, err := r.List(ctx, nodeGVR, "", metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading nodes: %w", err)
	}
	room := map[string]*nodeRoom{}
	for _, n := range nodes.Items {
		if unschedulable, _, _ := unstructured.NestedBool(n.Object, "spec", "unschedulable"); unschedulable || !nodeReady(n.Object) {
			continue
		}
		alloc, _, _ := unstructured.NestedStringMap(n.Object, "status", "allocatable")
		room[n.GetName()] = &nodeRoom{cpu: milli(alloc["cpu"]), mem: bytesOf(alloc["memory"])}
	}
	pods, err := r.List(ctx, podsT.gvr, "", metav1.ListOptions{FieldSelector: "status.phase!=Succeeded,status.phase!=Failed"})
	if err != nil {
		return nil, fmt.Errorf("reading pods: %w", err)
	}
	for _, p := range pods.Items {
		node, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName")
		nr := room[node]
		if nr == nil {
			continue
		}
		cpu, mem := podRequests(p.Object)
		nr.reqCPU += cpu
		nr.reqMem += mem
	}
	c := &Capacity{Nodes: len(room)}
	for _, nr := range room {
		c.CPUAllocatable += nr.cpu
		c.MemAllocatable += nr.mem
		c.CPURequested += nr.reqCPU
		c.MemRequested += nr.reqMem
		c.LargestFreeCPU = max(c.LargestFreeCPU, nr.cpu-nr.reqCPU)
		c.LargestFreeMem = max(c.LargestFreeMem, nr.mem-nr.reqMem)
	}
	return c, nil
}

func nodeReady(obj map[string]interface{}) bool {
	conds, _, _ := unstructured.NestedSlice(obj, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]interface{})
		if m["type"] == "Ready" {
			return m["status"] == "True"
		}
	}
	return false
}

// podRequests is what the scheduler reserves for a pod: the sum of its
// containers' requests, or its largest init container's if that is more.
func podRequests(obj map[string]interface{}) (cpu, mem int64) {
	sum := func(field string) (c, m int64, most [2]int64) {
		list, _, _ := unstructured.NestedSlice(obj, "spec", field)
		for _, item := range list {
			ct, _ := item.(map[string]interface{})
			req, _, _ := unstructured.NestedStringMap(ct, "resources", "requests")
			cc, mm := milli(req["cpu"]), bytesOf(req["memory"])
			c += cc
			m += mm
			most[0] = max(most[0], cc)
			most[1] = max(most[1], mm)
		}
		return
	}
	cpu, mem, _ = sum("containers")
	_, _, init := sum("initContainers")
	return max(cpu, init[0]), max(mem, init[1])
}

func milli(q string) int64 {
	if q == "" {
		return 0
	}
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return 0
	}
	return v.MilliValue()
}

func bytesOf(q string) int64 {
	if q == "" {
		return 0
	}
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return 0
	}
	return v.Value()
}
