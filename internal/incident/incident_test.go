package incident

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/history"
	"github.com/ihebamiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	objects map[string][]map[string]interface{}
	logs    string
	lastLog kube.LogOptions
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if gvr.Resource == "nodes" {
		return nil, apierrors.NewForbidden(gvr.GroupResource(), "", nil)
	}
	l := &unstructured.UnstructuredList{}
	for _, o := range f.objects[gvr.Resource] {
		l.Items = append(l.Items, unstructured.Unstructured{Object: o})
	}
	return l, nil
}

func (f *fakeReader) Logs(_ context.Context, _, _ string, opts kube.LogOptions) (io.ReadCloser, error) {
	f.lastLog = opts
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func meta(name string, created time.Time) map[string]interface{} {
	return map[string]interface{}{"name": name, "namespace": "shop", "creationTimestamp": created.UTC().Format(time.RFC3339)}
}

func TestBuildFindsWhatIsBroken(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Hour)
	r := &fakeReader{
		logs: "starting\nlistening on :8080\nERROR: connection refused to db:5432\nshutting down\n",
		objects: map[string][]map[string]interface{}{
			"pods": {
				{"metadata": meta("api-1", old), "status": map[string]interface{}{
					"phase": "Running",
					"containerStatuses": []interface{}{map[string]interface{}{
						"name": "api", "restartCount": int64(7),
						"state":     map[string]interface{}{"waiting": map[string]interface{}{"reason": "CrashLoopBackOff", "message": "back-off 5m0s"}},
						"lastState": map[string]interface{}{"terminated": map[string]interface{}{"reason": "Error", "exitCode": int64(1)}},
					}},
				}},
				{"metadata": meta("web-1", old), "status": map[string]interface{}{"phase": "Running", "containerStatuses": []interface{}{map[string]interface{}{"name": "web", "state": map[string]interface{}{"running": map[string]interface{}{}}}}}},
				{"metadata": meta("batch-1", now.Add(-10*time.Minute)), "status": map[string]interface{}{
					"phase":      "Pending",
					"conditions": []interface{}{map[string]interface{}{"type": "PodScheduled", "status": "False", "message": "0/1 nodes: insufficient memory"}},
				}},
				{"metadata": meta("done-1", old), "status": map[string]interface{}{"phase": "Succeeded"}},
			},
			"deployments": {
				{"metadata": meta("api", old), "spec": map[string]interface{}{"replicas": int64(2)}, "status": map[string]interface{}{"availableReplicas": int64(0)}},
				{"metadata": meta("web", old), "spec": map[string]interface{}{"replicas": int64(1)}, "status": map[string]interface{}{"availableReplicas": int64(1)}},
			},
			"services": {
				{"metadata": meta("api", old), "spec": map[string]interface{}{"selector": map[string]interface{}{"app": "api"}}},
				{"metadata": meta("web", old), "spec": map[string]interface{}{"selector": map[string]interface{}{"app": "web"}}},
			},
			"endpointslices": {
				{"metadata": map[string]interface{}{"name": "web-x", "namespace": "shop", "labels": map[string]interface{}{"kubernetes.io/service-name": "web"}},
					"endpoints": []interface{}{map[string]interface{}{"conditions": map[string]interface{}{"ready": true}}}},
			},
			"events": {
				{"metadata": map[string]interface{}{"name": "e", "namespace": "shop", "uid": "e1"}, "type": "Warning", "reason": "BackOff",
					"involvedObject": map[string]interface{}{"kind": "Pod", "name": "api-1"}, "count": int64(9), "lastTimestamp": now.Add(-time.Minute).UTC().Format(time.RFC3339)},
				{"metadata": map[string]interface{}{"name": "old", "namespace": "shop", "uid": "e2"}, "type": "Warning", "reason": "Old",
					"lastTimestamp": now.Add(-3 * time.Hour).UTC().Format(time.RFC3339)},
			},
		},
	}
	store := history.Open(t.TempDir())
	defer store.Close()
	_ = store.AddChange("c", history.Change{Time: now.Add(-4 * time.Minute), Kind: "ConfigMap", Namespace: "shop", Name: "api-config", Action: history.ActionChanged, Manager: "kubectl-edit"})

	rep, err := Build(context.Background(), Input{Reader: r, Store: store, Context: "c", Namespace: "shop", Window: 30 * time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range rep.Problems {
		got = append(got, p.Severity+" "+p.Kind+"/"+p.Name+": "+p.Problem)
	}
	want := []string{
		"critical Deployment/api: 0 of 2 ready",
		"critical Pod/api-1: CrashLoopBackOff in container api (restarted 7 times)",
		"critical Pod/batch-1: Can't be scheduled",
		"warning Service/api: No ready pods behind it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(rep.Changes) != 1 || len(rep.Events) != 1 || rep.Events[0].Count != 9 {
		t.Errorf("changes %+v events %+v", rep.Changes, rep.Events)
	}
	if len(rep.Logs) != 1 || !rep.Logs[0].Matched || len(rep.Logs[0].Lines) != 1 || !r.lastLog.Previous {
		t.Errorf("logs = %+v (opts %+v)", rep.Logs, r.lastLog)
	}
	if len(rep.Skipped) != 1 || !strings.HasPrefix(rep.Skipped[0], "Node") {
		t.Errorf("skipped = %v", rep.Skipped)
	}
}
