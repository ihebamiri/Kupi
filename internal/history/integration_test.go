//go:build integration

package history_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/history"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/trust"
)

// TestRecorderSeesKupiChanges changes a ConfigMap on kupi-test through the
// Guard and expects the recorder to log it with the kupi field manager.
func TestRecorderSeesKupiChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	trustStore, _ := trust.Open(filepath.Join(dir, "trust.json"))
	profiles, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	log, _ := audit.Open(filepath.Join(dir, "audit.log"))
	mgr := kube.NewManager(kubeconfig.NewLoader(), trustStore, profiles)
	g := guard.New(guard.SessionsFunc(func(name string) (guard.Session, bool) {
		s, ok := mgr.Session(name)
		return s, ok
	}), log)
	sess, err := mgr.Connect(ctx, "k3d-kupi-test")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	store := history.Open(filepath.Join(dir, "history"))
	defer store.Close()
	rec := history.NewRecorder(store)
	defer rec.StopAll()
	rec.Start("k3d-kupi-test", sess.Reader(), "default")
	time.Sleep(3 * time.Second) // let the initial lists finish

	name := fmt.Sprintf("kupi-history-it-%d", time.Now().UnixNano())
	apply := func(value string) {
		t.Helper()
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]interface{}{"name": name, "namespace": "default"},
			"data":     map[string]interface{}{"mode": value},
		}}
		p, err := g.PrepareApply(ctx, guard.ApplyRequest{Context: "k3d-kupi-test", Object: obj})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Commit(ctx, p.ID, guard.Confirmation{}); err != nil {
			t.Fatal(err)
		}
	}
	apply("one")
	apply("two")
	t.Cleanup(func() {
		p, err := g.PrepareDelete(context.Background(), guard.TargetRequest{Context: "k3d-kupi-test", GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespace: "default", Name: name})
		if err == nil {
			_, _ = g.Commit(context.Background(), p.ID, guard.Confirmation{})
		}
	})

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		changes, _ := store.Changes("k3d-kupi-test", history.Query{Name: name})
		if len(changes) >= 2 {
			latest := changes[0]
			if latest.Action != history.ActionChanged || latest.Manager != "kupi" || !strings.Contains(latest.Diff, "+  mode: two") {
				t.Fatalf("latest change = %+v\n%s", latest, latest.Diff)
			}
			if changes[1].Action != history.ActionCreated {
				t.Errorf("first change = %+v", changes[1])
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("changes not recorded; status %+v", rec.Status("k3d-kupi-test"))
}
