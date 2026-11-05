package validate

import (
	"strings"
	"testing"
)

func TestValidators(t *testing.T) {
	ok := []error{
		Name("api"), Name("system:controller:deployment"), Namespace("shop"), OptionalNamespace(""),
		GroupVersionResource("", "v1", "pods"), GroupVersionResource("apps", "v1", "deployments"),
		GroupVersionResource("gateway.networking.k8s.io", "v1beta1", "httproutes"),
		Reason("rotate the db password\nticket 42"), Replicas(0), Replicas(3), ContextName("k3d-dev"),
	}
	for i, err := range ok {
		if err != nil {
			t.Errorf("case %d: unexpected error %v", i, err)
		}
	}
	bad := []error{
		Name(""), Name("a/b"), Name(".."), Name(strings.Repeat("a", 254)),
		Namespace("Shop"), Namespace(""), GroupVersionResource("", "1", "pods"),
		GroupVersionResource("", "v1", "Pods"), GroupVersionResource("", "v1", "pods/exec"),
		Reason(strings.Repeat("x", MaxReasonLen+1)), Reason("bell\a"), Replicas(-1), Replicas(MaxReplicas + 1),
		ContextName(""), ContextName("ctx\x00"), Manifest("   "),
	}
	for i, err := range bad {
		if err == nil {
			t.Errorf("bad case %d accepted", i)
		}
	}
}
