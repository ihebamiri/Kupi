package charts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chartArchive(t *testing.T, name, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		name + "/Chart.yaml":        fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\nappVersion: \"1.27\"\ndescription: A demo chart\n", name, version),
		name + "/values.yaml":       "# How many copies\nreplicas: 1\n",
		name + "/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n",
	}
	for path, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(0, 0)}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// testRepo serves a Helm repository and an Artifact Hub search over https.
func testRepo(t *testing.T, tamper bool) (*Client, string) {
	t.Helper()
	v1 := chartArchive(t, "demo", "1.0.0")
	v2 := chartArchive(t, "demo", "1.2.0")
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	d2 := sum(v2)
	if tamper {
		d2 = strings.Repeat("0", 64)
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			fmt.Fprintf(w, `apiVersion: v1
entries:
  demo:
  - name: demo
    version: 1.0.0
    apiVersion: v2
    created: "2026-01-01T00:00:00Z"
    digest: %s
    urls: [charts/demo-1.0.0.tgz]
  - name: demo
    version: 1.2.0
    apiVersion: v2
    appVersion: "1.27"
    digest: %s
    urls: [%s/charts/demo-1.2.0.tgz]
  insecure:
  - name: insecure
    version: 0.1.0
    apiVersion: v2
    urls: [http://example.com/insecure-0.1.0.tgz]
`, sum(v1), d2, srv.URL)
		case "/charts/demo-1.0.0.tgz":
			w.Write(v1)
		case "/charts/demo-1.2.0.tgz":
			w.Write(v2)
		case "/api/v1/packages/search":
			if r.URL.Query().Get("ts_query_web") != "demo" {
				http.Error(w, "bad query", 400)
				return
			}
			fmt.Fprintf(w, `{"packages":[
			  {"name":"demo","version":"1.2.0","app_version":"1.27","description":"Demo","stars":7,"deprecated":true,"repository":{"name":"old","display_name":"Old","url":"%s"}},
			  {"name":"demo","version":"1.2.0","description":"Demo","stars":40,"repository":{"name":"example","display_name":"Example","url":"%s","verified_publisher":true}},
			  {"name":"evil","version":"1.0.0","repository":{"name":"x","url":"http://plain.example.com"}}
			]}`, srv.URL, srv.URL)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New()
	c.http = srv.Client()
	c.hub = srv.URL
	return c, srv.URL
}

func TestVersionsAndLoad(t *testing.T) {
	c, repoURL := testRepo(t, false)
	ctx := context.Background()
	s := Source{Repo: repoURL, Chart: "demo"}

	vs, err := c.Versions(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].Version != "1.2.0" || vs[1].Created == "" {
		t.Errorf("versions = %+v", vs)
	}
	ch, info, err := c.Load(ctx, s, "")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Metadata.Version != "1.2.0" || info.AppVersion != "1.27" || !strings.Contains(info.Values, "# How many copies") || len(info.Digest) != 64 {
		t.Errorf("latest = %+v", info)
	}
	// Relative chart URLs resolve against the repository.
	if _, info, err = c.Load(ctx, s, "1.0.0"); err != nil || info.Version != "1.0.0" {
		t.Errorf("1.0.0: %+v %v", info, err)
	}
	if _, _, err := c.Load(ctx, s, "9.9.9"); err == nil {
		t.Error("a missing version loaded")
	}
	if _, _, err := c.Load(ctx, Source{Repo: repoURL, Chart: "insecure"}, ""); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Errorf("plain http download: %v", err)
	}
}

func TestTamperedChartIsRefused(t *testing.T) {
	c, repoURL := testRepo(t, true)
	if _, _, err := c.Load(context.Background(), Source{Repo: repoURL, Chart: "demo"}, "1.2.0"); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("tampered chart: %v", err)
	}
}

func TestSearch(t *testing.T) {
	c, repoURL := testRepo(t, false)
	pkgs, err := c.Search(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("plain-http repositories must be left out: %+v", pkgs)
	}
	if pkgs[0].Deprecated || !pkgs[0].VerifiedPublisher || pkgs[0].RepoURL != repoURL || !strings.HasSuffix(pkgs[0].Page, "/packages/helm/example/demo") {
		t.Errorf("deprecated charts go last: %+v", pkgs)
	}
	if _, err := c.Search(context.Background(), "  "); err == nil {
		t.Error("empty search accepted")
	}
}

func TestSourceCheck(t *testing.T) {
	for _, s := range []Source{
		{Repo: "http://charts.example.com", Chart: "x"},
		{Repo: "https://user:pass@charts.example.com", Chart: "x"},
		{Repo: "https://charts.example.com?x=1", Chart: "x"},
		{Repo: "file:///etc", Chart: "x"},
		{Repo: "https://charts.example.com", Chart: "../x"},
		{Repo: "oci://", Chart: "x"},
	} {
		if s.Check() == nil {
			t.Errorf("%+v accepted", s)
		}
	}
	for _, s := range []Source{
		{Repo: "https://charts.bitnami.com/bitnami", Chart: "nginx"},
		{Repo: "oci://registry-1.docker.io/bitnamicharts", Chart: "nginx"},
	} {
		if err := s.Check(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	if got := ociRef(Source{Repo: "oci://ghcr.io/org/charts/", Chart: "app"}, "1.0.0"); got != "ghcr.io/org/charts/app:1.0.0" {
		t.Errorf("ociRef = %s", got)
	}
}
