//go:build integration

package guard_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/iheb-amiri/kupi/internal/guard"
	"github.com/iheb-amiri/kupi/internal/secrets"
)

// TestSecretWritesAgainstK3d creates a Secret, changes one key, removes
// another and reads values back, checking no plan ever carries a value.
func TestSecretWritesAgainstK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	mgr, g, log, _ := setup(t)
	sess, err := mgr.Connect(ctx, testContext)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	name := fmt.Sprintf("kupi-secret-it-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if p, err := g.PrepareDelete(context.Background(), guard.TargetRequest{Context: testContext, GVR: secrets.SecretsGVR, Namespace: "default", Name: name}); err == nil {
			_, _ = g.Commit(context.Background(), p.ID, guard.Confirmation{})
		}
	})

	commit := func(p *guard.Plan) {
		t.Helper()
		if !p.Allowed {
			t.Fatalf("plan not allowed: %s", p.DeniedReason)
		}
		for _, leak := range []string{"first-value-91", "second-value-27", "drop-me-55"} {
			if strings.Contains(p.Diff+strings.Join(p.Summary, " "), leak) {
				t.Fatalf("plan shows a value:\n%s", p.Diff)
			}
		}
		if _, err := g.Commit(ctx, p.ID, guard.Confirmation{}); err != nil {
			t.Fatal(err)
		}
	}

	p, err := g.PrepareSecretWrite(ctx, guard.SecretWrite{
		Context: testContext, Namespace: "default", Name: name,
		Set: map[string][]byte{"password": []byte("first-value-91"), "legacy": []byte("drop-me-55"), "user": []byte("admin")},
	})
	if err != nil {
		t.Fatal(err)
	}
	commit(p)

	p, err = g.PrepareSecretWrite(ctx, guard.SecretWrite{
		Context: testContext, Namespace: "default", Name: name,
		Set: map[string][]byte{"password": []byte("second-value-27")}, Delete: []string{"legacy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Diff, "password") || !strings.Contains(p.Diff, "legacy") {
		t.Errorf("diff doesn't name the changed keys:\n%s", p.Diff)
	}
	commit(p)

	d, err := secrets.Describe(ctx, sess.Reader(), "default", name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, k := range d.Keys {
		keys = append(keys, k.Name)
	}
	if strings.Join(keys, ",") != "password,user" {
		t.Errorf("keys after update = %v", keys)
	}
	if d.ValuesInAnnotation {
		t.Error("Kupi left values in the last-applied annotation")
	}
	for key, want := range map[string]string{"password": "second-value-27", "user": "admin"} {
		v, err := secrets.Value(ctx, sess.Reader(), "default", name, key)
		if err != nil || string(v) != want {
			t.Errorf("%s = %q, %v", key, v, err)
		}
	}
	if err := g.RecordAccess(testContext, "secret-revealed", "Secret default/"+name+" (key password)", ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := log.Recent(1)
	if len(entries) != 1 || entries[0].Action != "secret-revealed" {
		t.Errorf("last audit entry = %+v", entries)
	}
}
