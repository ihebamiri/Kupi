package netpol

import (
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// The NetworkPolicies in docs/testdata give the answers the testing guide
// promises in its "Check traffic" steps.
func TestDocsFixture(t *testing.T) {
	raw, err := os.ReadFile("../../docs/testdata/network-policies.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var objs []unstructured.Unstructured
	for _, doc := range strings.Split(string(raw), "\n---\n") {
		m := map[string]any{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		objs = append(objs, unstructured.Unstructured{Object: m})
	}
	policies, problems := Parse(objs)
	if len(problems) > 0 || len(policies) != 2 {
		t.Fatalf("policies %d, problems %v", len(policies), problems)
	}

	namespaces := map[string]Namespace{
		"kupi-demo":  {Name: "kupi-demo", Labels: map[string]string{"kubernetes.io/metadata.name": "kupi-demo"}},
		"kupi-multi": {Name: "kupi-multi", Labels: map[string]string{"kubernetes.io/metadata.name": "kupi-multi"}},
	}
	ticker := Pod{Name: "ticker-1", Namespace: "kupi-demo", Labels: map[string]string{"app": "ticker"}}
	crasher := Pod{Name: "crasher", Namespace: "kupi-demo"}
	hello := Pod{Name: "hello-1", Namespace: "kupi-multi", Labels: map[string]string{"app": "hello"}, Ports: map[string]int32{"http": 8080}}

	cases := []struct {
		name    string
		q       Query
		allowed bool
	}{
		{"ticker to hello on the http port", Query{From: ticker, To: hello, Port: 8080}, true},
		{"crasher to hello", Query{From: crasher, To: hello, Port: 8080}, false},
		{"ticker to hello on another port", Query{From: ticker, To: hello, Port: 9090}, false},
		{"hello to ticker (no policy in kupi-demo)", Query{From: hello, To: ticker, Port: 8080}, true},
	}
	for _, c := range cases {
		v := Evaluate(policies, namespaces, c.q)
		if v.Allowed != c.allowed {
			t.Errorf("%s: allowed = %v, want %v (egress: %s; ingress: %s)", c.name, v.Allowed, c.allowed, v.Egress.Reason, v.Ingress.Reason)
		}
	}
}
