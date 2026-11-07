package prom

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	services []map[string]any
	queries  []string
}

func (f *fakeReader) List(context.Context, schema.GroupVersionResource, string, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	out := &unstructured.UnstructuredList{}
	for _, s := range f.services {
		out.Items = append(out.Items, unstructured.Unstructured{Object: s})
	}
	return out, nil
}

func (f *fakeReader) ServiceProxyGet(_ context.Context, ns, svc, port, path string, params map[string]string) ([]byte, error) {
	f.queries = append(f.queries, ns+"/"+svc+":"+port+" "+path+" "+params["query"])
	if strings.Contains(params["query"], "cpu") {
		return []byte(`{"status":"success","data":{"result":[{"values":[[1700000000,"12.5"],[1700000060,"20"]]}]}}`), nil
	}
	return []byte(`{"status":"success","data":{"result":[{"values":[[1700000000,"1048576"],[1700000060,"2097152"]]}]}}`), nil
}

func svc(ns, name string, labels map[string]any, ports ...map[string]any) map[string]any {
	ps := []any{}
	for _, p := range ports {
		ps = append(ps, p)
	}
	return map[string]any{"metadata": map[string]any{"namespace": ns, "name": name, "labels": labels}, "spec": map[string]any{"ports": ps}}
}

func TestFindAndHistory(t *testing.T) {
	r := &fakeReader{services: []map[string]any{
		svc("monitoring", "kube-prometheus-stack-operator", nil, map[string]any{"name": "https", "port": int64(443)}),
		svc("monitoring", "kube-prometheus-stack-alertmanager", map[string]any{"app.kubernetes.io/name": "alertmanager"}, map[string]any{"name": "http-web", "port": int64(9093)}),
		svc("monitoring", "prometheus-operated", nil, map[string]any{"name": "web", "port": int64(9090)}),
		svc("shop", "web", nil, map[string]any{"name": "http", "port": int64(80)}),
	}}
	ep, err := Find(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if ep.String() != "monitoring/prometheus-operated:9090" {
		t.Errorf("endpoint = %s", ep)
	}
	samples, err := PodHistory(context.Background(), r, ep, "shop", "web-1", 6*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].CPU != 12 || samples[1].Mem != 2097152 || samples[0].T != 1700000000 {
		t.Errorf("samples = %+v", samples)
	}
	if !strings.Contains(r.queries[0], `pod="web-1"`) || !strings.Contains(r.queries[0], "query_range") {
		t.Errorf("query = %s", r.queries[0])
	}
	if _, err := PodHistory(context.Background(), r, ep, "shop", `web"} or vector(1) #`, time.Hour); err == nil {
		t.Error("a name that could inject PromQL was accepted")
	}
	if _, err := Find(context.Background(), &fakeReader{}); err != ErrNotFound {
		t.Errorf("no Prometheus: %v", err)
	}
}

func TestParseMatrixErrors(t *testing.T) {
	if _, err := parseMatrix([]byte(`{"status":"error","error":"bad query"}`)); err == nil || !strings.Contains(err.Error(), "bad query") {
		t.Errorf("error status: %v", err)
	}
}
