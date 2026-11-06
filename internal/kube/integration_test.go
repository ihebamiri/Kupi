//go:build integration

package kube_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/trust"
)

const testContext = "k3d-kupi-test"

var (
	podsGVR       = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	configMapsGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
)

func connect(t *testing.T) *kube.Session {
	t.Helper()
	dir := t.TempDir()
	ts, _ := trust.Open(filepath.Join(dir, "trust.json"))
	ps, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	s, err := kube.NewManager(kubeconfig.NewLoader(), ts, ps).Connect(context.Background(), testContext)
	if err != nil {
		t.Fatalf("connect to %s (run `wails3 task cluster:up`): %v", testContext, err)
	}
	return s
}

func TestReadSide(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r := connect(t).Reader()

	types, err := r.ResourceTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ty := range types {
		if ty.Group == "apps" && ty.Resource == "deployments" && ty.Namespaced {
			found = true
		}
		if strings.Contains(ty.Resource, "/") {
			t.Errorf("subresource %s listed as a type", ty.Resource)
		}
	}
	if !found {
		t.Error("deployments.apps not discovered")
	}

	table, err := r.Table(ctx, podsGVR, true, "kube-system", kube.TableOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(table.ColumnDefinitions) == 0 || table.ColumnDefinitions[0].Name != "Name" || len(table.Rows) == 0 {
		t.Fatalf("pods table: %d columns, %d rows", len(table.ColumnDefinitions), len(table.Rows))
	}
	if len(table.Rows[0].Object.Raw) == 0 {
		t.Error("table rows should carry object metadata")
	}

	// Paging: a small limit returns exactly that many rows.
	small, err := r.Table(ctx, podsGVR, true, "", kube.TableOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(small.Rows) != 2 {
		t.Errorf("limit 2 returned %d rows", len(small.Rows))
	}
}

func TestMetadataWatchSeesChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s := connect(t)
	list, err := s.Reader().Table(ctx, configMapsGVR, true, "default", kube.TableOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.Reader().WatchMetadata(ctx, configMapsGVR, "default", metav1.ListOptions{ResourceVersion: list.ResourceVersion})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	name := fmt.Sprintf("kupi-watch-%d", time.Now().UnixNano())
	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": name, "namespace": "default"},
		"data":     map[string]interface{}{"secretish": "should-not-travel"},
	}}
	if _, err := s.Mutator().Apply(ctx, configMapsGVR, "default", cm, kube.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Mutator().Delete(context.Background(), configMapsGVR, "default", name, "", false) })

	select {
	case ev := <-w.ResultChan():
		data := fmt.Sprintf("%v", ev.Object)
		if strings.Contains(data, "should-not-travel") {
			t.Error("metadata watch carried object data")
		}
	case <-ctx.Done():
		t.Fatal("no watch event")
	}
}

func TestLogsAndExec(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s := connect(t)
	name := fmt.Sprintf("kupi-exec-%d", time.Now().UnixNano())
	pod := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]interface{}{"name": name, "namespace": "default"},
		"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{
			"name": "main", "image": "busybox:1.36",
			"command": []interface{}{"sh", "-c", "echo kupi-log-line; sleep 3600"},
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

	rc, err := s.Reader().Logs(ctx, "default", name, kube.LogOptions{Container: "main", TailLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	logs, _ := io.ReadAll(rc)
	rc.Close()
	if !strings.Contains(string(logs), "kupi-log-line") {
		t.Errorf("logs = %q", logs)
	}

	var out bytes.Buffer
	err = s.Mutator().Exec(ctx, "default", name, "main", []string{"sh", "-c", "echo exec-ok"}, kube.ExecStreams{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "exec-ok") {
		t.Errorf("exec output = %q", out.String())
	}
}
