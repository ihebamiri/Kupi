//go:build integration

package forward_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/forward"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/kubeconfig"
	"github.com/iheb-amiri/kupi/internal/metrics"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/trust"
)

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

type nop struct{}

func (nop) Emit(string, any) {}

func TestForwardServesTrafficFromK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	ts, _ := trust.Open(filepath.Join(dir, "trust.json"))
	ps, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	s, err := kube.NewManager(kubeconfig.NewLoader(), ts, ps).Connect(ctx, "k3d-kupi-test")
	if err != nil {
		t.Fatalf("connect (run `wails3 task cluster:up`): %v", err)
	}

	name := fmt.Sprintf("kupi-fwd-%d", time.Now().UnixNano())
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]interface{}{"name": name, "namespace": "default"},
		"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{
			"name": "web", "image": "busybox:1.36",
			"command": []interface{}{"sh", "-c", "echo kupi-forward-ok > /tmp/index.html && httpd -f -p 8080 -h /tmp"},
			"ports":   []interface{}{map[string]interface{}{"containerPort": int64(8080)}},
		}}},
	}}
	if _, err := s.Mutator().Apply(ctx, podsGVR, "default", pod, kube.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Mutator().Delete(context.Background(), podsGVR, "default", name, "", false) })
	for {
		p, err := s.Reader().Get(ctx, podsGVR, "default", name)
		if err == nil {
			if phase, _, _ := unstructured.NestedString(p.Object, "status", "phase"); phase == "Running" {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("pod never became Running")
		case <-time.After(time.Second):
		}
	}

	m := forward.New(nop{})
	f, err := m.Start("k3d-kupi-test", s, forward.Target{Kind: "Pod", Namespace: "default", Name: name, Port: 8080}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(f.ID)
	var local int
	for local == 0 {
		if l := m.List(); len(l) == 1 && l[0].Status == forward.StatusActive {
			local = l[0].LocalPort
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("forward never became active: %+v", m.List())
		case <-time.After(200 * time.Millisecond):
		}
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", local))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "kupi-forward-ok") {
		t.Errorf("body through the forward = %q", body)
	}

	mm := metrics.New()
	defer mm.StopAll()
	deadline := time.Now().Add(30 * time.Second)
	for {
		o := mm.Overview("k3d-kupi-test", s.Reader(), "", 5)
		if o.Status.Available && len(o.Nodes) > 0 && o.Nodes[0].CPUCapacity > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics never became available: %+v", o.Status)
		}
		time.Sleep(time.Second)
	}
}
