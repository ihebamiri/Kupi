package graph

import (
	"context"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ihebamiri/kupi/internal/kube"
)

// maxPodsOf caps how many pods PodsOf returns.
const maxPodsOf = 200

// PodSummary is one pod of a workload or Service, for the canvas.
type PodSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
	Created   string `json:"created"`
	// Status reads like kubectl's STATUS column: Running, CrashLoopBackOff…
	Status   string            `json:"status"`
	Ready    string            `json:"ready"`
	Restarts int64             `json:"restarts"`
	Node     string            `json:"node"`
	IP       string            `json:"ip"`
	Health   string            `json:"health"`
	Labels   map[string]string `json:"labels"`
}

// PodsResult lists the pods behind one object.
type PodsResult struct {
	Pods []PodSummary `json:"pods"`
	// Note explains an empty list, for example a Service with no selector.
	Note      string `json:"note"`
	Truncated bool   `json:"truncated"`
}

var podParents = map[string]gvrKind{
	"Deployment":  deploymentsT,
	"StatefulSet": statefulSetsT,
	"DaemonSet":   daemonSetsT,
	"ReplicaSet":  replicaSetsT,
	"Job":         jobsT,
	"CronJob":     cronJobsT,
	"Service":     servicesT,
}

// HasPods reports whether PodsOf supports a kind.
func HasPods(kind string) bool {
	_, ok := podParents[kind]
	return ok
}

// PodsOf returns the pods a workload owns (following Deployment →
// ReplicaSet and CronJob → Job), or the pods a Service selects. Unhealthy
// pods come first.
func PodsOf(ctx context.Context, r kube.Reader, kind, namespace, name string) (*PodsResult, error) {
	t, ok := podParents[kind]
	if !ok {
		return nil, fmt.Errorf("%s doesn't run pods", kind)
	}
	obj, err := r.Get(ctx, t.gvr, namespace, name)
	if err != nil {
		return nil, err
	}

	opts := metav1.ListOptions{}
	if kind == "Service" {
		sel, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector")
		if len(sel) == 0 {
			return &PodsResult{Pods: []PodSummary{}, Note: "This Service has no selector, so it doesn't send traffic to pods by itself."}, nil
		}
		opts.LabelSelector = labels.SelectorFromSet(sel).String()
	} else if raw, found, _ := unstructured.NestedMap(obj.Object, "spec", "selector"); found {
		var ls metav1.LabelSelector
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &ls); err == nil {
			if sel, err := metav1.LabelSelectorAsSelector(&ls); err == nil {
				opts.LabelSelector = sel.String()
			}
		}
	}
	list, err := r.List(ctx, podsT.gvr, namespace, opts)
	if err != nil {
		return nil, err
	}

	// Which controllers count as "this workload": itself, plus the
	// ReplicaSets of a Deployment or the Jobs of a CronJob.
	owners := map[types.UID]bool{obj.GetUID(): true}
	var middle *gvrKind
	switch kind {
	case "Deployment":
		middle = &replicaSetsT
	case "CronJob":
		middle = &jobsT
	}
	if middle != nil {
		mids, err := r.List(ctx, middle.gvr, namespace, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for i := range mids.Items {
			if c := controller(&mids.Items[i]); c != nil && c.UID == obj.GetUID() {
				owners[mids.Items[i].GetUID()] = true
			}
		}
	}

	res := &PodsResult{Pods: []PodSummary{}}
	for i := range list.Items {
		p := &list.Items[i]
		if kind != "Service" {
			c := controller(p)
			if c == nil || !owners[c.UID] {
				continue
			}
		}
		if len(res.Pods) >= maxPodsOf {
			res.Truncated = true
			break
		}
		res.Pods = append(res.Pods, summarizePod(p))
	}
	sort.SliceStable(res.Pods, func(i, j int) bool {
		a, b := rank(res.Pods[i].Health), rank(res.Pods[j].Health)
		if a != b {
			return a > b
		}
		return res.Pods[i].Name < res.Pods[j].Name
	})
	if len(res.Pods) == 0 {
		res.Note = "No pods right now."
	}
	return res, nil
}

func summarizePod(p *unstructured.Unstructured) PodSummary {
	info := inspectPod(p)
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	ready := 0
	terminated := ""
	for _, s := range statuses {
		sm, _ := s.(map[string]interface{})
		if r, _ := sm["ready"].(bool); r {
			ready++
		}
		if reason, ok, _ := unstructured.NestedString(sm, "state", "terminated", "reason"); ok && terminated == "" {
			terminated = reason
		}
	}
	total := len(statuses)
	if total == 0 {
		cs, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
		total = len(cs)
	}
	status := phase
	switch {
	case p.GetDeletionTimestamp() != nil:
		status = "Terminating"
	case info.reason != "":
		status = info.reason
	case phase == "Succeeded":
		status = "Completed"
	case terminated != "" && phase != "Running":
		status = terminated
	}
	node, _, _ := unstructured.NestedString(p.Object, "spec", "nodeName")
	ip, _, _ := unstructured.NestedString(p.Object, "status", "podIP")
	return PodSummary{
		Name:      p.GetName(),
		Namespace: p.GetNamespace(),
		UID:       string(p.GetUID()),
		Created:   p.GetCreationTimestamp().Format(time.RFC3339),
		Status:    status,
		Ready:     fmt.Sprintf("%d/%d", ready, total),
		Restarts:  info.restarts,
		Node:      node,
		IP:        ip,
		Health:    info.health,
		Labels:    p.GetLabels(),
	}
}
