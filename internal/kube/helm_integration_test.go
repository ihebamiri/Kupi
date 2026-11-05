//go:build integration

package kube

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmkube "helm.sh/helm/v4/pkg/kube"

	"github.com/iheb-amiri/kupi/internal/kubeconfig"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/trust"
)

// TestHelmLifecycle installs a tiny chart on kupi-test, then upgrades (dry
// run and real), rolls back and uninstalls it through Kupi's Helm client.
func TestHelmLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	trustStore, _ := trust.Open(filepath.Join(dir, "trust.json"))
	profiles, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	mgr := NewManager(kubeconfig.NewLoader(), trustStore, profiles)
	sess, err := mgr.Connect(ctx, "k3d-kupi-test")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	h := &helmClient{s: sess}
	name := fmt.Sprintf("kupi-it-%d", time.Now().Unix()%100000)
	ch := &chartv2.Chart{
		Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "kupi-it", Version: "0.1.0", Type: "application"},
		Templates: []*common.File{{Name: "templates/cm.yaml", Data: []byte(
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-cfg\ndata:\n  color: {{ .Values.color | quote }}\n")}},
		Values: map[string]any{"color": "blue"},
	}
	cfg, err := h.config("default")
	if err != nil {
		t.Fatal(err)
	}
	in := action.NewInstall(cfg)
	in.ReleaseName, in.Namespace = name, "default"
	in.WaitStrategy = helmkube.HookOnlyStrategy
	if _, err := in.RunWithContext(ctx, ch, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Cleanup(func() {
		if cfg, err := h.config("default"); err == nil {
			_, _ = action.NewUninstall(cfg).Run(name)
		}
	})

	rels, err := h.HelmReleases(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, r := range rels {
		found[r.Namespace+"/"+r.Name] = true
	}
	if !found["default/"+name] {
		t.Fatalf("installed release not listed: %v", found)
	}

	dry, err := h.HelmUpgrade(ctx, HelmUpgrade{Namespace: "default", Name: name, Values: map[string]any{"color": "red"}, FromRevision: 1}, true)
	if err != nil {
		t.Fatalf("dry-run upgrade: %v", err)
	}
	if !strings.Contains(dry.Manifest, `color: "red"`) {
		t.Errorf("dry-run manifest:\n%s", dry.Manifest)
	}
	if cur, _ := h.HelmRelease(ctx, "default", name, 0); cur.Version != 1 {
		t.Fatalf("dry run created revision %d", cur.Version)
	}
	if _, err := h.HelmUpgrade(ctx, HelmUpgrade{Namespace: "default", Name: name, Values: map[string]any{"color": "red"}, FromRevision: 1}, false); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := h.HelmUpgrade(ctx, HelmUpgrade{Namespace: "default", Name: name, Values: map[string]any{"color": "green"}, FromRevision: 1}, false); !errors.Is(err, ErrReleaseChanged) {
		t.Errorf("stale upgrade: %v", err)
	}
	if err := h.HelmRollback(ctx, "default", name, 1, 2); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	hist, err := h.HelmHistory(ctx, "default", name)
	if err != nil || len(hist) != 3 || hist[0].Version != 3 || !strings.Contains(hist[0].Manifest, `color: "blue"`) {
		t.Fatalf("history after rollback: %d revisions, err %v", len(hist), err)
	}
	if err := h.HelmUninstall(ctx, "default", name, 3); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := h.HelmRelease(ctx, "default", name, 0); err == nil {
		t.Error("release still present after uninstall")
	}
}

// TestInstallDemoRelease installs the Helm release "demo-web" in kupi-demo,
// with two revisions, for trying the Helm page by hand. It runs only with
// KUPI_DEMO_RELEASE=1 and leaves the release in place.
func TestInstallDemoRelease(t *testing.T) {
	if os.Getenv("KUPI_DEMO_RELEASE") != "1" {
		t.Skip("set KUPI_DEMO_RELEASE=1 to install the demo release")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	trustStore, _ := trust.Open(filepath.Join(dir, "trust.json"))
	profiles, _ := profile.OpenStore(filepath.Join(dir, "profiles.json"))
	sess, err := NewManager(kubeconfig.NewLoader(), trustStore, profiles).Connect(ctx, "k3d-kupi-test")
	if err != nil {
		t.Fatal(err)
	}
	h := &helmClient{s: sess}
	if _, err := h.HelmRelease(ctx, "kupi-demo", "demo-web", 0); err == nil {
		t.Skip("demo-web is already installed")
	}
	tpl := func(name, body string) *common.File {
		return &common.File{Name: "templates/" + name, Data: []byte(body)}
	}
	ch := &chartv2.Chart{
		Metadata: &chartv2.Metadata{APIVersion: "v2", Name: "demo-web", Version: "1.2.0", AppVersion: "1.27", Description: "A small web server for trying Kupi's Helm page.", Type: "application"},
		Templates: []*common.File{
			tpl("config.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-config\ndata:\n  greeting: {{ .Values.greeting | quote }}\n"),
			tpl("secret.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: {{ .Release.Name }}-auth\nstringData:\n  adminPassword: {{ .Values.adminPassword | quote }}\n"),
			tpl("deployment.yaml", `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels: {app: {{ .Release.Name }}}
  template:
    metadata:
      labels: {app: {{ .Release.Name }}}
    spec:
      securityContext: {runAsNonRoot: true, seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: web
          image: nginxinc/nginx-unprivileged:1.27-alpine
          ports: [{containerPort: 8080}]
          resources: {requests: {cpu: 10m, memory: 16Mi}, limits: {memory: 64Mi}}
          securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
`),
			tpl("NOTES.txt", "demo-web is running. Forward port 8080 of the Deployment {{ .Release.Name }} to open it.\n"),
		},
		Values: map[string]any{"replicas": 1, "greeting": "hello", "adminPassword": "change-me"},
		Schema: []byte(`{"type":"object","properties":{"replicas":{"type":"integer","minimum":0,"maximum":10}},"required":["replicas"]}`),
	}
	cfg, err := h.config("kupi-demo")
	if err != nil {
		t.Fatal(err)
	}
	in := action.NewInstall(cfg)
	in.ReleaseName, in.Namespace = "demo-web", "kupi-demo"
	in.WaitStrategy = helmkube.HookOnlyStrategy
	if _, err := in.RunWithContext(ctx, ch, map[string]any{"adminPassword": "s3cret-demo"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := h.HelmUpgrade(ctx, HelmUpgrade{Namespace: "kupi-demo", Name: "demo-web", FromRevision: 1,
		Values: map[string]any{"adminPassword": "s3cret-demo", "greeting": "hello from revision 2", "replicas": 2}}, false); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
}
