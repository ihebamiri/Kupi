package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// MaxBatchObjects caps how many objects one manifest may hold.
const MaxBatchObjects = 200

// States of one object in a multi-object apply.
const (
	ItemCreate    = "create"
	ItemUpdate    = "update"
	ItemUnchanged = "unchanged"
	// ItemDeferred objects depend on a namespace or type the same manifest
	// creates, so the cluster can only check them when they are applied.
	ItemDeferred = "deferred"
	ItemDenied   = "denied"
)

// ApplyBatchRequest applies every object of a multi-object manifest.
type ApplyBatchRequest struct {
	Context        string
	Objects        []*unstructured.Unstructured
	ForceConflicts bool
}

// PlanItem is one object of a multi-object apply.
type PlanItem struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	State      string
	// Note explains a deferred or denied object.
	Note      string
	Diff      string
	Summary   []string
	Warnings  []string
	Conflicts []string
}

// Resource returns "Kind namespace/name".
func (it PlanItem) Resource() string {
	if it.Namespace == "" {
		return fmt.Sprintf("%s %s", it.Kind, it.Name)
	}
	return fmt.Sprintf("%s %s/%s", it.Kind, it.Namespace, it.Name)
}

// batchStep applies one object at commit time.
type batchStep struct {
	resource string
	diffHash string
	run      func(ctx context.Context, s Session) error
}

var (
	namespacesGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	crdGVR        = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
)

// applyOrder ranks kinds so that what others depend on comes first, the
// same order Helm installs in. Unknown kinds (custom resources) go last.
var applyOrder = func() map[string]int {
	kinds := []string{
		"Namespace", "NetworkPolicy", "ResourceQuota", "LimitRange", "PodDisruptionBudget",
		"ServiceAccount", "Secret", "ConfigMap", "StorageClass", "PersistentVolume",
		"PersistentVolumeClaim", "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding",
		"Role", "RoleBinding", "Service", "DaemonSet", "Pod", "ReplicationController",
		"ReplicaSet", "Deployment", "HorizontalPodAutoscaler", "StatefulSet", "Job", "CronJob",
		"IngressClass", "Ingress", "APIService", "MutatingWebhookConfiguration",
		"ValidatingWebhookConfiguration",
	}
	m := make(map[string]int, len(kinds))
	for i, k := range kinds {
		m[k] = i
	}
	return m
}()

func orderOf(kind string) int {
	if i, ok := applyOrder[kind]; ok {
		return i
	}
	return len(applyOrder)
}

// manifestCreates records what a manifest brings that the cluster lacks.
type manifestCreates struct {
	namespaces map[string]bool
	// kinds maps a custom type to its CRD's name and scope.
	kinds map[schema.GroupKind]crdInfo
}

type crdInfo struct {
	name       string
	namespaced bool
}

// PrepareApplyBatch previews applying several objects at once. Each object
// gets its own dry run, permission check and diff; the user confirms them
// together, and Commit applies them in dependency order.
func (g *Guard) PrepareApplyBatch(ctx context.Context, req ApplyBatchRequest) (*Plan, error) {
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	switch n := len(req.Objects); {
	case n == 0:
		return nil, fmt.Errorf("nothing to apply")
	case n > MaxBatchObjects:
		return nil, fmt.Errorf("the manifest holds %d objects; the limit is %d", n, MaxBatchObjects)
	}
	objs := make([]*unstructured.Unstructured, 0, len(req.Objects))
	for _, o := range req.Objects {
		if o == nil {
			return nil, fmt.Errorf("the manifest holds an empty object")
		}
		objs = append(objs, o.DeepCopy())
	}
	sort.SliceStable(objs, func(i, j int) bool { return orderOf(objs[i].GetKind()) < orderOf(objs[j].GetKind()) })

	reader := s.Reader()
	created := g.manifestCreates(ctx, reader, objs)

	p := basePlan(s, rules, editing, ActionApply)
	p.Kind = "Manifest"
	p.Name = fmt.Sprintf("%d objects", len(objs))
	seen := map[string]bool{}
	counts := map[string]int{}
	var diffs []string
	for _, obj := range objs {
		it, step := g.prepareBatchItem(ctx, s, obj, created, req.ForceConflicts)
		k := strings.Join([]string{obj.GetAPIVersion(), it.Kind, it.Namespace, it.Name}, "|")
		if seen[k] {
			it.State, it.Note, step = ItemDenied, "This object appears more than once in the manifest.", nil
		}
		seen[k] = true
		counts[it.State]++
		if it.State == ItemDenied {
			p.deny(fmt.Sprintf("%s can't be applied: %s", it.Resource(), it.Note))
		}
		if len(it.Conflicts) > 0 {
			p.Conflicts = append(p.Conflicts, prefixAll(it.Resource()+": ", it.Conflicts)...)
		}
		if it.Diff != "" {
			diffs = append(diffs, "# "+it.Resource()+"\n"+it.Diff)
		}
		if step != nil {
			p.steps = append(p.steps, *step)
		}
		p.Items = append(p.Items, it)
	}
	p.Diff = strings.Join(diffs, "\n")
	p.Summary = []string{batchSummary(counts)}
	if p.Allowed && len(p.steps) == 0 {
		p.deny("Nothing to change: the cluster already matches this manifest.")
	}
	if counts[ItemDeferred] > 0 {
		p.Warnings = append(p.Warnings, "Some objects can only be checked by the cluster when they're applied, because they need a namespace or type this manifest creates. If one fails, the objects before it stay applied.")
	}
	if req.ForceConflicts {
		p.Warnings = append(p.Warnings, "Kupi will take ownership of fields currently managed by other tools.")
	}
	return g.store(p), nil
}

// manifestCreates finds the namespaces and custom types a manifest creates
// that the cluster doesn't have yet.
func (g *Guard) manifestCreates(ctx context.Context, r kube.Reader, objs []*unstructured.Unstructured) manifestCreates {
	c := manifestCreates{namespaces: map[string]bool{}, kinds: map[schema.GroupKind]crdInfo{}}
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		switch {
		case gvk.Group == "" && gvk.Kind == "Namespace":
			if _, err := r.Get(ctx, namespacesGVR, "", o.GetName()); apierrors.IsNotFound(err) {
				c.namespaces[o.GetName()] = true
			}
		case gvk.Group == crdGVR.Group && gvk.Kind == "CustomResourceDefinition":
			group, _, _ := unstructured.NestedString(o.Object, "spec", "group")
			kind, _, _ := unstructured.NestedString(o.Object, "spec", "names", "kind")
			scope, _, _ := unstructured.NestedString(o.Object, "spec", "scope")
			if group != "" && kind != "" {
				c.kinds[schema.GroupKind{Group: group, Kind: kind}] = crdInfo{name: o.GetName(), namespaced: scope != "Cluster"}
			}
		}
	}
	return c
}

func (g *Guard) prepareBatchItem(ctx context.Context, s Session, obj *unstructured.Unstructured, created manifestCreates, force bool) (PlanItem, *batchStep) {
	gvk := obj.GroupVersionKind()
	it := PlanItem{APIVersion: obj.GetAPIVersion(), Kind: gvk.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
	deny := func(note string) (PlanItem, *batchStep) {
		it.State, it.Note = ItemDenied, note
		return it, nil
	}
	if gvk.Kind == "" || gvk.Version == "" {
		return deny("it needs apiVersion and kind.")
	}
	if err := validate.Name(obj.GetName()); err != nil {
		return deny(err.Error())
	}
	reader := s.Reader()
	gvr, namespaced, err := reader.Resolve(gvk)
	crd, newType := created.kinds[gvk.GroupKind()]
	switch {
	case err == nil:
		newType = false
	case newType:
		namespaced = crd.namespaced
	default:
		return deny(fmt.Sprintf("the cluster has no type %s, and this manifest doesn't define it.", gvk.Kind))
	}
	ns := ""
	if namespaced {
		ns = obj.GetNamespace()
		if ns == "" {
			ns = s.DefaultNamespace()
		}
		if err := validate.Namespace(ns); err != nil {
			return deny(err.Error())
		}
	}
	obj.SetNamespace(ns)
	it.Namespace = ns
	stripServerFields(obj)
	applyObj := obj.DeepCopy()
	resource := it.Resource()

	if newType || created.namespaces[ns] {
		it.State = ItemDeferred
		if newType {
			it.Note = fmt.Sprintf("Its type is defined by the CustomResourceDefinition %s in this manifest.", crd.name)
		} else {
			it.Note = fmt.Sprintf("Its namespace %s is created by this manifest.", ns)
			if msg := rbacProblem(ctx, reader, "create", gvr, ns); msg != "" {
				return deny(msg)
			}
		}
		it.Diff, it.Summary = render(nil, obj)
		it.Summary = append([]string{"Creates " + resource + "."}, it.Summary...)
		return it, &batchStep{resource: resource, diffHash: hashOf(it.Diff), run: func(ctx context.Context, s Session) error {
			target := gvr
			if newType {
				var err error
				if target, err = waitForType(ctx, s.Reader(), crd.name, gvk); err != nil {
					return err
				}
			}
			_, err := s.Mutator().Apply(ctx, target, ns, applyObj, kube.ApplyOptions{Force: force})
			return err
		}}
	}

	current, err := reader.Get(ctx, gvr, ns, obj.GetName())
	switch {
	case apierrors.IsNotFound(err):
		current = nil
	case err != nil:
		return deny("couldn't read the current object: " + err.Error())
	}
	verb, rbacName := "create", ""
	if current != nil {
		verb, rbacName = "patch", obj.GetName()
		it.Warnings = append(it.Warnings, ownerWarnings(current)...)
	}
	if msg := rbacProblemNamed(ctx, reader, verb, gvr, ns, rbacName); msg != "" {
		return deny(msg)
	}
	after, err := s.Mutator().Apply(ctx, gvr, ns, obj, kube.ApplyOptions{DryRun: true, Force: force})
	if err != nil {
		if apierrors.IsConflict(err) {
			it.Conflicts = causes(err)
			return deny("some fields are managed by other tools.")
		}
		return deny("the cluster rejected it: " + err.Error())
	}
	it.Diff, it.Summary = render(current, after)
	switch {
	case current == nil:
		it.State = ItemCreate
		it.Summary = append([]string{"Creates " + resource + "."}, it.Summary...)
	case it.Diff == "":
		it.State = ItemUnchanged
		return it, nil
	default:
		it.State = ItemUpdate
	}
	if current != nil {
		// Fail the commit if the object changes between preview and commit.
		applyObj.SetResourceVersion(current.GetResourceVersion())
	}
	return it, &batchStep{resource: resource, diffHash: hashOf(it.Diff), run: func(ctx context.Context, s Session) error {
		_, err := s.Mutator().Apply(ctx, gvr, ns, applyObj, kube.ApplyOptions{Force: force})
		return err
	}}
}

// commitBatch applies the steps in order, auditing each, and stops at the
// first failure. Kubernetes has no transactions, so earlier objects stay.
func (g *Guard) commitBatch(ctx context.Context, p *Plan, s Session, c Confirmation) (*Result, error) {
	var lastSeq int64
	for i, step := range p.steps {
		err := step.run(ctx, s)
		entry := audit.Entry{
			Context:    p.Context,
			Profile:    string(p.Profile),
			OSUser:     g.osUser,
			KubeUser:   s.UserName(),
			Action:     string(p.Action),
			Resource:   step.resource,
			Reason:     c.Reason,
			DiffSHA256: step.diffHash,
			Outcome:    audit.OutcomeApplied,
		}
		if err != nil {
			entry.Outcome = audit.OutcomeFailed
			entry.Error = err.Error()
		}
		logged, auditErr := g.audit.Append(entry)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("%s failed, nothing was applied: %w", step.resource, err)
			}
			return nil, fmt.Errorf("applied %d of %d objects, then %s failed (the ones before it stay applied): %w", i, len(p.steps), step.resource, err)
		}
		if auditErr != nil {
			return nil, fmt.Errorf("%s applied but the audit log could not be written: %w", step.resource, auditErr)
		}
		lastSeq = logged.Seq
	}
	return &Result{Outcome: audit.OutcomeApplied, Message: fmt.Sprintf("Applied %d objects", len(p.steps)), AuditSeq: lastSeq}, nil
}

// waitForType waits until a CRD applied a moment ago is established and its
// type is served, then returns the type's resource.
func waitForType(ctx context.Context, r kube.Reader, crdName string, gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if crd, err := r.Get(ctx, crdGVR, "", crdName); err == nil && established(crd) {
			if gvr, _, err := r.Resolve(gvk); err == nil {
				return gvr, nil
			}
		}
		select {
		case <-ctx.Done():
			return schema.GroupVersionResource{}, fmt.Errorf("the type %s from CustomResourceDefinition %s wasn't ready within a minute", gvk.Kind, crdName)
		case <-tick.C:
		}
	}
}

func established(crd *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(crd.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]interface{})
		if m["type"] == "Established" && m["status"] == "True" {
			return true
		}
	}
	return false
}

func rbacProblem(ctx context.Context, r kube.Reader, verb string, gvr schema.GroupVersionResource, ns string) string {
	return rbacProblemNamed(ctx, r, verb, gvr, ns, "")
}

// rbacProblemNamed returns why the account can't do verb, or "" when it can
// (or when the check itself failed: the dry run or the apply will tell).
func rbacProblemNamed(ctx context.Context, r kube.Reader, verb string, gvr schema.GroupVersionResource, ns, name string) string {
	if gvr.Resource == "" {
		return ""
	}
	allowed, reason, err := r.CanI(ctx, verb, gvr, "", ns, name)
	if err != nil || allowed {
		return ""
	}
	where := "the cluster"
	if ns != "" {
		where = "namespace " + ns
	}
	msg := fmt.Sprintf("your account can't %s %s in %s.", verb, gvr.Resource, where)
	if reason != "" {
		msg += " " + reason
	}
	return msg
}

func batchSummary(counts map[string]int) string {
	var parts []string
	for _, st := range []struct{ state, label string }{
		{ItemCreate, "to create"}, {ItemUpdate, "to update"}, {ItemDeferred, "to create once their namespace or type exists"},
		{ItemUnchanged, "unchanged"}, {ItemDenied, "blocked"},
	} {
		if n := counts[st.state]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st.label))
		}
	}
	return "Objects: " + strings.Join(parts, ", ") + "."
}

func prefixAll(prefix string, in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = prefix + s
	}
	return out
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
