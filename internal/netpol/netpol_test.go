package netpol

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func policies(t *testing.T, docs ...string) []Policy {
	t.Helper()
	var objs []unstructured.Unstructured
	for _, d := range docs {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(d), &m); err != nil {
			t.Fatal(err)
		}
		objs = append(objs, unstructured.Unstructured{Object: m})
	}
	ps, problems := Parse(objs)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	return ps
}

var (
	web = Pod{Name: "web-1", Namespace: "shop", Labels: map[string]string{"app": "web"}, IP: "10.42.0.5"}
	api = Pod{Name: "api-1", Namespace: "shop", Labels: map[string]string{"app": "api"}, IP: "10.42.0.6", Ports: map[string]int32{"http": 8080}}
	db  = Pod{Name: "db-0", Namespace: "data", Labels: map[string]string{"app": "db"}, IP: "10.42.1.9"}
	mon = Pod{Name: "prom-0", Namespace: "monitoring", Labels: map[string]string{"app": "prometheus"}, IP: "10.42.2.2"}

	namespaces = map[string]Namespace{
		"shop":       {Name: "shop", Labels: map[string]string{"team": "shop"}},
		"data":       {Name: "data"},
		"monitoring": {Name: "monitoring", Labels: map[string]string{"purpose": "monitoring"}},
	}
)

const denyAllShop = `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: default-deny, namespace: shop}
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
`

const apiFromWeb = `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: api-from-web, namespace: shop}
spec:
  podSelector: {matchLabels: {app: api}}
  ingress:
  - from:
    - podSelector: {matchLabels: {app: web}}
    ports:
    - port: http
`

const webEgressToApi = `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: web-egress, namespace: shop}
spec:
  podSelector: {matchLabels: {app: web}}
  policyTypes: [Egress]
  egress:
  - to:
    - podSelector: {matchLabels: {app: api}}
    ports:
    - {port: 8000, endPort: 8100}
`

func TestNoPoliciesMeansOpen(t *testing.T) {
	v := Evaluate(nil, namespaces, Query{From: web, To: api, Port: 8080})
	if !v.Allowed || v.Egress.Isolated || v.Ingress.Isolated {
		t.Errorf("verdict = %+v", v)
	}
}

func TestDenyAllAndAllowRules(t *testing.T) {
	ps := policies(t, denyAllShop, apiFromWeb, webEgressToApi)

	v := Evaluate(ps, namespaces, Query{From: web, To: api, Port: 8080})
	if !v.Allowed {
		t.Fatalf("web → api:8080 should pass: %+v", v)
	}
	if !strings.Contains(v.Ingress.Reason, "api-from-web") || !strings.Contains(v.Egress.Reason, "web-egress") {
		t.Errorf("reasons = %q / %q", v.Egress.Reason, v.Ingress.Reason)
	}

	// The named port "http" is 8080; 9090 isn't allowed in.
	if v := Evaluate(ps, namespaces, Query{From: web, To: api, Port: 9090}); v.Allowed || v.Ingress.Allowed {
		t.Errorf("web → api:9090 should be blocked on ingress: %+v", v)
	}
	// Egress allows 8000-8100 only.
	if v := Evaluate(ps, namespaces, Query{From: web, To: api, Port: 8200}); v.Egress.Allowed {
		t.Errorf("egress to 8200 should be blocked: %+v", v)
	}
	// UDP isn't TCP.
	if v := Evaluate(ps, namespaces, Query{From: web, To: api, Port: 8080, Protocol: "UDP"}); v.Allowed {
		t.Error("UDP passed a TCP rule")
	}
	// api → web: web's ingress is closed by default-deny.
	v = Evaluate(ps, namespaces, Query{From: api, To: web, Port: 80})
	if v.Allowed || v.Ingress.Allowed || !strings.Contains(v.Ingress.Reason, "default-deny") {
		t.Errorf("api → web: %+v", v)
	}
}

func TestNamespaceSelectorsAndTheAndOrTrap(t *testing.T) {
	// One peer with both selectors: monitoring namespace AND prometheus pods.
	and := policies(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: db-ingress, namespace: data}
spec:
  podSelector: {matchLabels: {app: db}}
  ingress:
  - from:
    - namespaceSelector: {matchLabels: {purpose: monitoring}}
      podSelector: {matchLabels: {app: prometheus}}
`)
	if v := Evaluate(and, namespaces, Query{From: mon, To: db, Port: 5432}); !v.Allowed {
		t.Errorf("prometheus → db: %+v", v)
	}
	if v := Evaluate(and, namespaces, Query{From: web, To: db, Port: 5432}); v.Allowed {
		t.Error("a pod outside the monitoring namespace got in")
	}

	// Two peers: monitoring namespace OR any pod labelled app=web in data.
	or := policies(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: db-ingress, namespace: data}
spec:
  podSelector: {matchLabels: {app: db}}
  ingress:
  - from:
    - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: shop}}
    - podSelector: {matchLabels: {app: web}}
`)
	if v := Evaluate(or, namespaces, Query{From: api, To: db, Port: 5432}); !v.Allowed {
		t.Errorf("anything in shop should pass (namespace name label): %+v", v)
	}
	if v := Evaluate(or, namespaces, Query{From: mon, To: db, Port: 5432}); v.Allowed {
		t.Error("monitoring matched neither peer")
	}
}

func TestIPBlocksAndAnyPort(t *testing.T) {
	ps := policies(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: db-ingress, namespace: data}
spec:
  podSelector: {}
  ingress:
  - from:
    - ipBlock: {cidr: 10.42.0.0/16, except: [10.42.0.6/32]}
    ports:
    - port: 5432
`)
	if v := Evaluate(ps, namespaces, Query{From: web, To: db, Port: 5432}); !v.Allowed {
		t.Errorf("web's IP is in the block: %+v", v)
	}
	if v := Evaluate(ps, namespaces, Query{From: api, To: db, Port: 5432}); v.Allowed {
		t.Error("api's IP is excluded")
	}
	// "Any port" isn't proven by a rule that lists ports.
	if v := Evaluate(ps, namespaces, Query{From: web, To: db}); v.Allowed {
		t.Error("any-port query passed a port-limited rule")
	}
}

func TestEgressOnlyPolicyLeavesIngressOpen(t *testing.T) {
	ps := policies(t, webEgressToApi)
	v := Evaluate(ps, namespaces, Query{From: api, To: web, Port: 80})
	if !v.Allowed || v.Ingress.Isolated {
		t.Errorf("an egress policy must not isolate ingress: %+v", v)
	}
}
