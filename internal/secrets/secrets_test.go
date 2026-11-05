package secrets

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	objects   map[string][]map[string]interface{} // by resource
	forbidden map[string]bool
}

func (f *fakeReader) Get(_ context.Context, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	for _, o := range f.objects[gvr.Resource] {
		u := &unstructured.Unstructured{Object: o}
		if u.GetNamespace() == ns && u.GetName() == name {
			return u.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if f.forbidden[gvr.Resource] {
		return nil, apierrors.NewForbidden(gvr.GroupResource(), "", nil)
	}
	out := &unstructured.UnstructuredList{}
	for _, o := range f.objects[gvr.Resource] {
		out.Items = append(out.Items, unstructured.Unstructured{Object: o})
	}
	return out, nil
}

func (f *fakeReader) Table(_ context.Context, gvr schema.GroupVersionResource, _ bool, _ string, _ kube.TableOptions) (*metav1.Table, error) {
	t := &metav1.Table{}
	for _, o := range f.objects[gvr.Resource] {
		u := &unstructured.Unstructured{Object: o}
		raw, _ := json.Marshal(metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: u.GetName(), Namespace: u.GetNamespace(), Annotations: u.GetAnnotations()}})
		t.Rows = append(t.Rows, metav1.TableRow{Object: runtime.RawExtension{Raw: raw}})
	}
	return t, nil
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func secret(name, typ string, data map[string]string, meta map[string]interface{}) map[string]interface{} {
	d := map[string]interface{}{}
	for k, v := range data {
		d[k] = b64(v)
	}
	m := map[string]interface{}{"name": name, "namespace": "shop"}
	for k, v := range meta {
		m[k] = v
	}
	return map[string]interface{}{"apiVersion": "v1", "kind": "Secret", "metadata": m, "type": typ, "data": d}
}

func deployment(name string, container map[string]interface{}, volumes []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]interface{}{"name": name, "namespace": "shop"},
		"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{
			"containers": []interface{}{container}, "volumes": volumes,
		}}},
	}
}

func selfSigned(t *testing.T, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "shop.example.com"},
		DNSNames: []string{"shop.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
}

func TestDescribeNeverCarriesValues(t *testing.T) {
	r := &fakeReader{objects: map[string][]map[string]interface{}{
		"secrets": {secret("db", "Opaque", map[string]string{"password": "s3cret-value-123", "user": "admin"}, map[string]interface{}{
			"annotations": map[string]interface{}{"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"czNjcmV0"}}`},
		})},
		"deployments": {deployment("api", map[string]interface{}{
			"name": "api",
			"env":  []interface{}{map[string]interface{}{"name": "DB_PASSWORD", "valueFrom": map[string]interface{}{"secretKeyRef": map[string]interface{}{"name": "db", "key": "password"}}}},
		}, nil)},
		"statefulsets": {map[string]interface{}{
			"kind": "StatefulSet", "metadata": map[string]interface{}{"name": "pg", "namespace": "shop"},
			"spec": map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{
				"containers": []interface{}{map[string]interface{}{"name": "pg"}},
				"volumes":    []interface{}{map[string]interface{}{"name": "creds", "secret": map[string]interface{}{"secretName": "db"}}},
			}}},
		}},
	}, forbidden: map[string]bool{"daemonsets": true}}

	d, err := Describe(context.Background(), r, "shop", "db", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(d)
	if strings.Contains(string(out), "s3cret") || strings.Contains(string(out), b64("s3cret-value-123")) {
		t.Fatalf("details leak a value: %s", out)
	}
	if len(d.Keys) != 2 || d.Keys[0].Name != "password" || d.Keys[0].Size != 16 {
		t.Errorf("keys = %+v", d.Keys)
	}
	if !d.ValuesInAnnotation {
		t.Error("last-applied copy not detected")
	}
	if len(d.UsedBy) != 2 {
		t.Fatalf("usedBy = %+v", d.UsedBy)
	}
	api := d.UsedBy[0]
	if api.Name != "api" || !api.NeedsRestart || !api.Restartable || !strings.Contains(api.How[0], "DB_PASSWORD") {
		t.Errorf("api usage = %+v", api)
	}
	if pg := d.UsedBy[1]; pg.NeedsRestart || !strings.Contains(pg.How[0], "volume creds") {
		t.Errorf("volume usage = %+v", pg)
	}
	if len(d.UsageSkipped) != 1 || !strings.HasPrefix(d.UsageSkipped[0], "DaemonSet") {
		t.Errorf("skipped = %v", d.UsageSkipped)
	}

	v, err := Value(context.Background(), r, "shop", "db", "password")
	if err != nil || string(v) != "s3cret-value-123" {
		t.Errorf("Value = %q, %v", v, err)
	}
	if _, err := Value(context.Background(), r, "shop", "db", "missing"); err == nil {
		t.Error("missing key returned a value")
	}
}

func TestManagedBy(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"External Secrets": {"ownerReferences": []interface{}{map[string]interface{}{"kind": "ExternalSecret", "name": "db", "apiVersion": "external-secrets.io/v1", "uid": "x"}}},
		"cert-manager":     {"annotations": map[string]interface{}{"cert-manager.io/certificate-name": "shop-tls"}},
		"Flux":             {"labels": map[string]interface{}{"kustomize.toolkit.fluxcd.io/name": "apps", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"}},
		"Argo CD":          {"labels": map[string]interface{}{"argocd.argoproj.io/instance": "shop"}},
		"Helm (release":    {"labels": map[string]interface{}{"app.kubernetes.io/managed-by": "Helm"}, "annotations": map[string]interface{}{"meta.helm.sh/release-name": "shop"}},
	}
	for want, meta := range cases {
		obj := &unstructured.Unstructured{Object: secret("x", "Opaque", nil, meta)}
		if got, note := managedBy(obj); !strings.Contains(got, want) || note == "" {
			t.Errorf("%s: got %q / %q", want, got, note)
		}
	}
	if got, _ := managedBy(&unstructured.Unstructured{Object: secret("x", "Opaque", nil, nil)}); got != "" {
		t.Errorf("plain Secret managed by %q", got)
	}
}

func TestCertInfo(t *testing.T) {
	now := time.Now()
	crt, key := selfSigned(t, now.Add(10*24*time.Hour))
	info := CertInfo(crt, key, now)
	if info.KeyStatus != "matches" || !info.SelfSigned || info.DNSNames[0] != "shop.example.com" {
		t.Errorf("info = %+v", info)
	}
	if len(info.Problems) != 1 || !strings.Contains(info.Problems[0], "expires in 9 days") {
		t.Errorf("problems = %v", info.Problems)
	}
	_, otherKey := selfSigned(t, now.Add(time.Hour))
	if got := CertInfo(crt, otherKey, now); got.KeyStatus != "mismatch" {
		t.Errorf("mismatched key status = %q", got.KeyStatus)
	}
	if CheckPair(crt, otherKey) == nil || CheckPair(crt, key) != nil {
		t.Error("CheckPair wrong")
	}
	if got := CertInfo([]byte("junk"), nil, now); len(got.Problems) == 0 {
		t.Error("junk certificate accepted")
	}
}

func TestDockerConfigJSON(t *testing.T) {
	raw, err := DockerConfigJSON("ghcr.io", "bot", "tok:en", "")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if a := doc.Auths["ghcr.io"]; a["auth"] != b64("bot:tok:en") || a["password"] != "tok:en" {
		t.Errorf("auth = %+v", a)
	}
	if got := Registries(raw, true); len(got) != 1 || got[0] != "ghcr.io" {
		t.Errorf("registries = %v", got)
	}
	for _, bad := range []string{"ghcr.io; rm", "http://", "a b"} {
		if _, err := DockerConfigJSON(bad, "u", "p", ""); err == nil {
			t.Errorf("registry %q accepted", bad)
		}
	}
	if _, err := DockerConfigJSON("", "u:x", "p", ""); err == nil {
		t.Error("user with colon accepted")
	}
}

func TestFormatEnv(t *testing.T) {
	text, n, skipped, _ := FormatEnv(map[string][]byte{
		"PLAIN": []byte("abc-123"), "SPACED": []byte("a b"), "QUOTE": []byte("it's\nmulti $HOME"),
		"bad-name": []byte("x"), "BIN": {0xff, 0xfe},
	})
	want := "PLAIN=abc-123\nQUOTE=\"it's\\nmulti \\$HOME\"\nSPACED='a b'\n"
	if text != want || n != 3 {
		t.Errorf("env =\n%s", text)
	}
	if strings.Join(skipped, ",") != "BIN,bad-name" {
		t.Errorf("skipped = %v", skipped)
	}
}

func TestScanFindsSecretsInTheOpen(t *testing.T) {
	r := &fakeReader{objects: map[string][]map[string]interface{}{
		"configmaps": {
			{"metadata": map[string]interface{}{"name": "app", "namespace": "shop"}, "data": map[string]interface{}{
				"DB_URL":           "postgres://app:hunter2@db:5432/shop",
				"SAFE_URL":         "postgres://app:${DB_PASSWORD}@db:5432/shop",
				"API_TOKEN":        "abcdef123456",
				"TOKEN_FILE":       "/var/run/token",
				"log_level":        "debug",
				"aws":              "AKIAABCDEFGHIJKLMNOP",
				"PASSWORD_ENABLED": "true",
			}},
		},
		"deployments": {deployment("api", map[string]interface{}{
			"name": "api",
			"env": []interface{}{
				map[string]interface{}{"name": "ADMIN_PASSWORD", "value": "letmein-now"},
				map[string]interface{}{"name": "REF", "valueFrom": map[string]interface{}{"secretKeyRef": map[string]interface{}{"name": "db", "key": "pw"}}},
			},
			"args": []interface{}{"--db-password=hunter22", "--token=$(TOKEN)", "--port=8080"},
		}, nil)},
		"secrets": {secret("db", "Opaque", nil, map[string]interface{}{
			"annotations": map[string]interface{}{"kubectl.kubernetes.io/last-applied-configuration": `{"stringData":{"pw":"x"}}`},
		})},
	}}
	res, err := Scan(context.Background(), r, "shop")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range res.Findings {
		got = append(got, f.Severity+" "+f.Kind+" "+f.Where)
		if strings.Contains(f.Problem+f.Where+f.Fix, "hunter2") || strings.Contains(f.Problem+f.Where, "letmein") {
			t.Errorf("finding leaks a value: %+v", f)
		}
	}
	want := []string{
		"high ConfigMap key DB_URL",
		"high ConfigMap key aws",
		"high Deployment container api, args",
		"medium ConfigMap key API_TOKEN",
		"medium Deployment container api, environment variable ADMIN_PASSWORD",
		"medium Secret annotation kubectl.kubernetes.io/last-applied-configuration",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
