package metrics

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	noMetrics bool
}

func item(ns, name string, fields map[string]interface{}) unstructured.Unstructured {
	obj := map[string]interface{}{"metadata": map[string]interface{}{"name": name, "namespace": ns}}
	for k, v := range fields {
		obj[k] = v
	}
	return unstructured.Unstructured{Object: obj}
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	switch {
	case gvr == nodeMetricsGVR && f.noMetrics:
		return nil, apierrors.NewNotFound(gvr.GroupResource(), "")
	case gvr == nodeMetricsGVR:
		return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			item("", "node-1", map[string]interface{}{"usage": map[string]interface{}{"cpu": "250m", "memory": "1Gi"}}),
		}}, nil
	case gvr == podMetricsGVR:
		return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			item("shop", "api", map[string]interface{}{"containers": []interface{}{
				map[string]interface{}{"usage": map[string]interface{}{"cpu": "120m", "memory": "64Mi"}},
				map[string]interface{}{"usage": map[string]interface{}{"cpu": "30m", "memory": "16Mi"}},
			}}),
			item("shop", "worker", map[string]interface{}{"containers": []interface{}{
				map[string]interface{}{"usage": map[string]interface{}{"cpu": "5m", "memory": "512Mi"}},
			}}),
		}}, nil
	case gvr == nodesGVR:
		return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			item("", "node-1", map[string]interface{}{"status": map[string]interface{}{"allocatable": map[string]interface{}{"cpu": "4", "memory": "8Gi"}}}),
		}}, nil
	}
	return &unstructured.UnstructuredList{}, nil
}

func TestSampleAndOverview(t *testing.T) {
	m := New()
	r := &fakeReader{}
	s := &sampler{reader: r, now: time.Now, series: map[string]*ring{}}
	s.sample(context.Background())
	m.samplers["ctx"] = s
	m.cancels["ctx"] = func() {}

	o := m.Overview("ctx", r, "", 10)
	if !o.Status.Available || len(o.Nodes) != 1 {
		t.Fatalf("overview = %+v", o)
	}
	n := o.Nodes[0]
	if n.CPU != 250 || n.Mem != 1<<30 || n.CPUCapacity != 4000 || n.MemCapacity != 8<<30 {
		t.Errorf("node usage = %+v", n)
	}
	if o.TopCPU[0].Name != "api" || o.TopCPU[0].CPU != 150 || o.TopCPU[0].Mem != 80<<20 {
		t.Errorf("top CPU = %+v", o.TopCPU[0])
	}
	if o.TopMem[0].Name != "worker" {
		t.Errorf("top memory = %+v", o.TopMem[0])
	}
	_, series := m.PodSeries("ctx", r, "shop", "api")
	if len(series) != 1 || series[0].CPU != 150 {
		t.Errorf("series = %+v", series)
	}
}

func TestMissingMetricsServer(t *testing.T) {
	s := &sampler{reader: &fakeReader{noMetrics: true}, now: time.Now, series: map[string]*ring{}}
	s.sample(context.Background())
	if s.status.Available || s.status.Reason == "" {
		t.Errorf("status = %+v, want a reason metrics are missing", s.status)
	}
}

func TestRingKeepsLastHourInOrder(t *testing.T) {
	var r ring
	for i := 0; i < Keep+5; i++ {
		r.add(Sample{T: int64(i)})
	}
	got := r.samples()
	if len(got) != Keep || got[0].T != 5 || got[len(got)-1].T != int64(Keep+4) {
		t.Errorf("ring holds %d samples from %d to %d", len(got), got[0].T, got[len(got)-1].T)
	}
}
