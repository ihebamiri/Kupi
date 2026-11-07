package gitpr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseGitHub(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/org/shop-deploy":       "org/shop-deploy",
		"https://github.com/org/shop-deploy.git":   "org/shop-deploy",
		"https://github.com/org/shop-deploy/":      "org/shop-deploy",
		"git@github.com:org/shop-deploy.git":       "org/shop-deploy",
		"ssh://git@github.com/org/shop-deploy.git": "org/shop-deploy",
	} {
		r, err := ParseGitHub(in)
		if err != nil || r.String() != want {
			t.Errorf("%s: %v %v", in, r, err)
		}
	}
	for _, bad := range []string{"https://gitlab.com/org/repo", "https://github.com/org", "https://github.com/org/repo/tree/main", "nonsense"} {
		if _, err := ParseGitHub(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestBranchOf(t *testing.T) {
	for in, want := range map[string]string{
		"main": "main", "main@sha1:abc123": "main", "refs/heads/prod": "prod",
		"HEAD": "", "": "", "v1.2.0": "", "3f2a1bc": "", "refs/tags/v1": "",
	} {
		if got := branchOf(in); got != want {
			t.Errorf("branchOf(%q) = %q, want %q", in, got, want)
		}
	}
}

const file = `# Shop workloads
apiVersion: v1
kind: ConfigMap
metadata:
  name: shop-settings
data:
  COLOR: blue
---
# The web tier
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
--- # trailing service
apiVersion: v1
kind: Service
metadata:
  name: web
`

func TestReplaceDocument(t *testing.T) {
	id := ObjectID{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "shop", Name: "web"}
	out, ok := replaceDocument(file, id, "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 5\n")
	if !ok {
		t.Fatal("not found")
	}
	if !strings.Contains(out, "replicas: 5") || strings.Contains(out, "replicas: 2") {
		t.Errorf("not replaced:\n%s", out)
	}
	for _, keep := range []string{"# Shop workloads", "# The web tier", "COLOR: blue", "--- # trailing service", "kind: Service"} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
	if _, ok := replaceDocument(file, ObjectID{APIVersion: "v1", Kind: "Deployment", Name: "web"}, "x"); ok {
		t.Error("a different API group matched")
	}
	if _, ok := replaceDocument(file, ObjectID{APIVersion: "v1", Kind: "Service", Namespace: "other", Name: "web"}, "x"); !ok {
		t.Error("a document without a namespace should match any namespace")
	}
}

// fakeGitHub serves the API calls Prepare and Open make.
func fakeGitHub(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var calls []string
	enc := base64.StdEncoding.EncodeToString([]byte(file))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/org/deploy":
			io.WriteString(w, `{"default_branch":"main","html_url":"https://github.com/org/deploy"}`)
		case r.Method == "GET" && r.URL.Path == "/repos/org/deploy/git/ref/heads/main":
			io.WriteString(w, `{"object":{"sha":"base123"}}`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/org/deploy/git/ref/heads/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "GET" && r.URL.Path == "/repos/org/deploy/git/trees/base123":
			io.WriteString(w, `{"tree":[
			  {"path":"apps/shop/README.md","type":"blob","sha":"r","size":10},
			  {"path":"apps/other/web.yaml","type":"blob","sha":"o","size":10},
			  {"path":"apps/shop/workloads.yaml","type":"blob","sha":"w","size":300}
			]}`)
		case r.Method == "GET" && r.URL.Path == "/repos/org/deploy/git/blobs/w":
			json.NewEncoder(w).Encode(map[string]string{"content": enc, "encoding": "base64"})
		case r.Method == "POST" && r.URL.Path == "/repos/org/deploy/git/refs":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{}`)
		case r.Method == "PUT" && r.URL.Path == "/repos/org/deploy/contents/apps/shop/workloads.yaml":
			io.WriteString(w, `{}`)
		case r.Method == "POST" && r.URL.Path == "/repos/org/deploy/pulls":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"html_url":"https://github.com/org/deploy/pull/7"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := New()
	c.api = srv.URL
	c.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	return c, &calls
}

func TestPrepareAndOpen(t *testing.T) {
	c, calls := fakeGitHub(t)
	ctx := context.Background()
	id := ObjectID{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "shop", Name: "web"}
	doc := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 4\n"

	p, err := c.Prepare(ctx, "tok", "https://github.com/org/deploy.git", "v1.0.0", "./apps/shop", id, doc)
	if err != nil {
		t.Fatal(err)
	}
	if p.File != "apps/shop/workloads.yaml" || p.Base != "main" || !strings.Contains(p.Diff, "+  replicas: 4") || !strings.Contains(p.Diff, "-  replicas: 2") {
		t.Errorf("proposal = %+v\n%s", p, p.Diff)
	}
	for _, c := range *calls {
		if strings.HasSuffix(c, "/blobs/o") {
			t.Error("files outside the app's path were read")
		}
	}
	url, err := c.Open(ctx, "tok", p, id, "Scale web to 4", "From Kupi")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/org/deploy/pull/7" {
		t.Errorf("url = %s", url)
	}

	if _, err := c.Prepare(ctx, "tok", "https://github.com/org/deploy", "main", "apps/shop", ObjectID{APIVersion: "v1", Kind: "Secret", Name: "nope"}, doc); err == nil || !strings.Contains(err.Error(), "Kustomize") {
		t.Errorf("missing object: %v", err)
	}
	if _, err := c.Prepare(ctx, "bad", "https://github.com/org/deploy", "main", "", id, doc); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("bad token: %v", err)
	}
	if _, err := c.Prepare(ctx, "tok", "https://gitlab.com/org/deploy", "main", "", id, doc); err != ErrUnsupported {
		t.Errorf("gitlab: %v", err)
	}
}
