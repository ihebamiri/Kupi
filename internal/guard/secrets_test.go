package guard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/iheb-amiri/kupi/internal/profile"
)

func TestSecretCreateNeverShowsValues(t *testing.T) {
	f := newFake(profile.Local)
	g, _ := newGuard(t, f)
	plan, err := g.PrepareSecretWrite(context.Background(), SecretWrite{
		Context: "test", Namespace: "shop", Name: "db",
		Set: map[string][]byte{"password": []byte("hunter2-create")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed {
		t.Fatalf("denied: %s", plan.DeniedReason)
	}
	all := plan.Diff + strings.Join(plan.Summary, " ")
	if strings.Contains(all, "hunter2") || strings.Contains(all, "aHVudGVyMi1jcmVhdGU") {
		t.Fatalf("plan leaks the value:\n%s", all)
	}
	if !strings.Contains(plan.Summary[0], "with keys password.") {
		t.Errorf("summary = %v", plan.Summary)
	}
	if _, err := g.Commit(context.Background(), plan.ID, Confirmation{}); err != nil {
		t.Fatal(err)
	}
	created := f.applied[0]
	if v, _, _ := unstructured.NestedString(created.Object, "data", "password"); v != "aHVudGVyMi1jcmVhdGU=" {
		t.Errorf("stored value = %q, want base64 of the input", v)
	}
}

func TestSecretUpdateSendsOnlyChangedKeys(t *testing.T) {
	f := newFake(profile.Production)
	f.put(secrets, &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]interface{}{"name": "db", "namespace": "shop"},
		"data":     map[string]interface{}{"password": "b2xk", "user": "YWRtaW4=", "legacy": "eA=="},
	}})
	g, _ := newGuard(t, f)
	if err := g.SetEditing("test", true, "rotate db password"); err != nil {
		t.Fatal(err)
	}
	plan, err := g.PrepareSecretWrite(context.Background(), SecretWrite{
		Context: "test", Namespace: "shop", Name: "db",
		Set: map[string][]byte{"password": []byte("new-secret-value")}, Delete: []string{"legacy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.NeedsReason {
		t.Error("production Secret changes need a reason")
	}
	if _, err := g.Commit(context.Background(), plan.ID, Confirmation{}); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("commit without reason: %v", err)
	}
	if _, err := g.Commit(context.Background(), plan.ID, Confirmation{Reason: "quarterly rotation"}); err != nil {
		t.Fatal(err)
	}
	patch := string(f.patched[0])
	if strings.Contains(patch, `"user"`) {
		t.Errorf("unchanged key sent in patch: %s", patch)
	}
	if !strings.Contains(patch, `"legacy":null`) || !strings.Contains(patch, `"resourceVersion":"41"`) {
		t.Errorf("patch = %s, want legacy deleted and a resourceVersion precondition", patch)
	}
}

func TestSecretKeysAreValidated(t *testing.T) {
	g, _ := newGuard(t, newFake(profile.Local))
	for _, bad := range []string{"", "has space", "slash/key", "dollar$"} {
		_, err := g.PrepareSecretWrite(context.Background(), SecretWrite{Context: "test", Namespace: "shop", Name: "x", Set: map[string][]byte{bad: []byte("v")}})
		if err == nil {
			t.Errorf("key %q accepted", bad)
		}
	}
}

func TestRecordAccessNeedsReasonOnProduction(t *testing.T) {
	g, log := newGuard(t, newFake(profile.Production))
	if !g.AccessNeedsReason("test") {
		t.Error("production access should need a reason")
	}
	if err := g.RecordAccess("test", "secret-revealed", "Secret shop/db (key pw)", ""); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("no reason: %v", err)
	}
	if err := g.RecordAccess("test", "secret-revealed", "Secret shop/db (key pw)", "incident 42"); err != nil {
		t.Fatal(err)
	}
	entries, _ := log.Recent(1)
	if len(entries) != 1 || entries[0].Reason != "incident 42" || entries[0].Action != "secret-revealed" {
		t.Errorf("audit = %+v", entries)
	}
	if err := g.RecordAccess("other", "secret-revealed", "x", "r"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("unknown context: %v", err)
	}
}
