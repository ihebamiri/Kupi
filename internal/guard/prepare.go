package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/validate"
)

// ApplyRequest creates or updates an object with server-side apply.
type ApplyRequest struct {
	Context string
	Object  *unstructured.Unstructured
	// ForceConflicts takes over fields managed by other tools. Only set after
	// the user reviewed the conflicts and chose to take them over.
	ForceConflicts bool
}

// TargetRequest names an existing object.
type TargetRequest struct {
	Context   string
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

var (
	scalable    = map[string]bool{"deployments": true, "statefulsets": true, "replicasets": true}
	restartable = map[string]bool{"deployments": true, "statefulsets": true, "daemonsets": true}
	hpaGVR      = schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}
)

// PrepareApply previews a server-side apply of req.Object.
func (g *Guard) PrepareApply(ctx context.Context, req ApplyRequest) (*Plan, error) {
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	if req.Object == nil {
		return nil, fmt.Errorf("nothing to apply")
	}
	obj := req.Object.DeepCopy()
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return nil, fmt.Errorf("the manifest needs apiVersion and kind")
	}
	if err := validate.Name(obj.GetName()); err != nil {
		return nil, err
	}
	reader := s.Reader()
	gvr, namespaced, err := reader.Resolve(gvk)
	if err != nil {
		return nil, err
	}
	ns := ""
	if namespaced {
		ns = obj.GetNamespace()
		if ns == "" {
			ns = s.DefaultNamespace()
		}
		if err := validate.Namespace(ns); err != nil {
			return nil, err
		}
	}
	obj.SetNamespace(ns)
	stripServerFields(obj)

	p := basePlan(s, rules, editing, ActionApply)
	p.APIVersion, p.Kind, p.Namespace, p.Name = obj.GetAPIVersion(), gvk.Kind, ns, obj.GetName()

	current, err := reader.Get(ctx, gvr, ns, obj.GetName())
	switch {
	case apierrors.IsNotFound(err):
		current = nil
	case err != nil:
		return nil, fmt.Errorf("read the current object: %w", err)
	}

	if current == nil {
		g.checkRBAC(ctx, p, reader, "create", gvr, ns, "")
	} else {
		g.checkRBAC(ctx, p, reader, "patch", gvr, ns, obj.GetName())
		p.Warnings = append(p.Warnings, ownerWarnings(current)...)
	}

	after, err := s.Mutator().Apply(ctx, gvr, ns, obj, kube.ApplyOptions{DryRun: true, Force: req.ForceConflicts})
	if err != nil {
		after = nil
		if apierrors.IsConflict(err) {
			p.Conflicts = causes(err)
			p.deny("Some fields are managed by other tools. Review the conflicts, then take them over only if you're sure.")
		} else {
			p.deny("The cluster rejected this change: " + err.Error())
		}
	}
	preview := after
	if preview == nil {
		preview = obj
	}
	p.Diff, p.Summary = render(current, preview)
	if current == nil {
		p.Summary = append([]string{fmt.Sprintf("Creates %s.", p.Resource())}, p.Summary...)
	}
	if after != nil && p.Diff == "" {
		p.deny("Nothing to change: the cluster already matches this manifest.")
	}
	if req.ForceConflicts {
		p.Warnings = append(p.Warnings, "Kupi will take ownership of fields currently managed by other tools.")
	}

	applyObj := obj.DeepCopy()
	if current != nil {
		// Fail the commit if the object changes between preview and commit.
		applyObj.SetResourceVersion(current.GetResourceVersion())
	}
	force := req.ForceConflicts
	p.exec = func(ctx context.Context, m kube.Mutator) error {
		_, err := m.Apply(ctx, gvr, ns, applyObj, kube.ApplyOptions{Force: force})
		return err
	}
	return g.store(p), nil
}

// PrepareDelete previews deleting an object.
func (g *Guard) PrepareDelete(ctx context.Context, req TargetRequest) (*Plan, error) {
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	reader := s.Reader()
	current, err := reader.Get(ctx, req.GVR, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("read the object to delete: %w", err)
	}
	p := targetPlan(s, rules, editing, ActionDelete, current)
	if rules.ConfirmByName {
		p.ConfirmName = req.Name
	}
	g.checkRBAC(ctx, p, reader, "delete", req.GVR, req.Namespace, req.Name)
	p.Warnings = append(p.Warnings, ownerWarnings(current)...)
	if current.GetKind() == "Namespace" {
		p.Warnings = append(p.Warnings, "Deleting a namespace deletes everything inside it.")
	}
	rv := current.GetResourceVersion()
	if err := s.Mutator().Delete(ctx, req.GVR, req.Namespace, req.Name, rv, true); err != nil {
		p.deny("The cluster rejected this delete: " + err.Error())
	}
	p.Diff, _ = render(current, nil)
	p.Summary = []string{fmt.Sprintf("Deletes %s.", p.Resource())}

	gvr, ns, name := req.GVR, req.Namespace, req.Name
	p.exec = func(ctx context.Context, m kube.Mutator) error {
		return m.Delete(ctx, gvr, ns, name, rv, false)
	}
	return g.store(p), nil
}

// PrepareScale previews changing the replica count of a workload.
func (g *Guard) PrepareScale(ctx context.Context, req TargetRequest, replicas int32) (*Plan, error) {
	if req.GVR.Group != "apps" || !scalable[req.GVR.Resource] {
		return nil, fmt.Errorf("%s can't be scaled", req.GVR.Resource)
	}
	if err := validate.Replicas(replicas); err != nil {
		return nil, err
	}
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	reader := s.Reader()
	current, err := reader.Get(ctx, req.GVR, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("read the workload: %w", err)
	}
	from, found, _ := unstructured.NestedInt64(current.Object, "spec", "replicas")
	if !found {
		from = 1
	}
	p := targetPlan(s, rules, editing, ActionScale, current)
	if replicas == 0 {
		p.Warnings = append(p.Warnings, "Scaling to 0 stops every pod of this workload.")
		if rules.ConfirmByName {
			p.ConfirmName = req.Name
		}
	}
	g.checkRBAC(ctx, p, reader, "patch", req.GVR, req.Namespace, req.Name)
	p.Warnings = append(p.Warnings, ownerWarnings(current)...)
	p.Warnings = append(p.Warnings, g.hpaWarnings(ctx, reader, current)...)

	patch, err := mergePatch(current.GetResourceVersion(), map[string]interface{}{
		"spec": map[string]interface{}{"replicas": replicas},
	})
	if err != nil {
		return nil, err
	}
	p.exec = g.patchAndPreview(ctx, s, p, req, current, patch)
	p.Summary = append([]string{fmt.Sprintf("Scales %s from %d to %d copies.", p.Resource(), from, replicas)}, p.Summary...)
	return g.store(p), nil
}

// PrepareRestart previews a rolling restart of a workload's pods.
func (g *Guard) PrepareRestart(ctx context.Context, req TargetRequest) (*Plan, error) {
	if req.GVR.Group != "apps" || !restartable[req.GVR.Resource] {
		return nil, fmt.Errorf("%s can't be restarted", req.GVR.Resource)
	}
	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	reader := s.Reader()
	current, err := reader.Get(ctx, req.GVR, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("read the workload: %w", err)
	}
	p := targetPlan(s, rules, editing, ActionRestart, current)
	g.checkRBAC(ctx, p, reader, "patch", req.GVR, req.Namespace, req.Name)
	p.Warnings = append(p.Warnings, ownerWarnings(current)...)

	patch, err := mergePatch(current.GetResourceVersion(), map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"kubectl.kubernetes.io/restartedAt": g.now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	p.exec = g.patchAndPreview(ctx, s, p, req, current, patch)
	p.Summary = []string{fmt.Sprintf("Restarts every pod of %s, following its rollout strategy.", p.Resource())}
	return g.store(p), nil
}

// patchAndPreview dry-runs a merge patch, fills the plan's diff and summary,
// and returns the function that applies the patch for real.
func (g *Guard) patchAndPreview(ctx context.Context, s Session, p *Plan, req TargetRequest, current *unstructured.Unstructured, patch []byte) func(context.Context, kube.Mutator) error {
	after, err := s.Mutator().MergePatch(ctx, req.GVR, req.Namespace, req.Name, patch, true)
	if err != nil {
		p.deny("The cluster rejected this change: " + err.Error())
		after = nil
	}
	if after != nil {
		p.Diff, p.Summary = render(current, after)
	}
	gvr, ns, name := req.GVR, req.Namespace, req.Name
	return func(ctx context.Context, m kube.Mutator) error {
		_, err := m.MergePatch(ctx, gvr, ns, name, patch, false)
		return err
	}
}

func (g *Guard) begin(contextName string) (Session, profile.Rules, bool, error) {
	s, ok := g.sessions.Session(contextName)
	if !ok {
		return nil, profile.Rules{}, false, ErrNotConnected
	}
	return s, profile.RulesFor(s.Profile()), g.EditingEnabled(contextName, s.Profile()), nil
}

func basePlan(s Session, rules profile.Rules, editing bool, action Action) *Plan {
	p := &Plan{
		Context:     s.ContextName(),
		Profile:     s.Profile(),
		Action:      action,
		Allowed:     true,
		NeedsReason: rules.RequireReason,
	}
	if !editing {
		p.deny("Editing is off for this cluster. Switch it on for this session to make changes.")
	}
	return p
}

func targetPlan(s Session, rules profile.Rules, editing bool, action Action, current *unstructured.Unstructured) *Plan {
	p := basePlan(s, rules, editing, action)
	p.APIVersion, p.Kind, p.Namespace, p.Name = current.GetAPIVersion(), current.GetKind(), current.GetNamespace(), current.GetName()
	return p
}

// deny marks the plan as not allowed, keeping the first reason given.
func (p *Plan) deny(reason string) {
	if p.Allowed {
		p.Allowed = false
		p.DeniedReason = reason
	}
}

func (g *Guard) checkRBAC(ctx context.Context, p *Plan, r kube.Reader, verb string, gvr schema.GroupVersionResource, ns, name string) {
	allowed, reason, err := r.CanI(ctx, verb, gvr, "", ns, name)
	if err != nil {
		p.Warnings = append(p.Warnings, "Couldn't check your permissions: "+err.Error())
		return
	}
	if !allowed {
		where := "the cluster"
		if ns != "" {
			where = "namespace " + ns
		}
		msg := fmt.Sprintf("Your account can't %s %s in %s.", verb, gvr.Resource, where)
		if reason != "" {
			msg += " " + reason
		}
		p.deny(msg)
	}
}

func (g *Guard) hpaWarnings(ctx context.Context, r kube.Reader, workload *unstructured.Unstructured) []string {
	list, err := r.List(ctx, hpaGVR, workload.GetNamespace(), metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var out []string
	for _, hpa := range list.Items {
		kind, _, _ := unstructured.NestedString(hpa.Object, "spec", "scaleTargetRef", "kind")
		name, _, _ := unstructured.NestedString(hpa.Object, "spec", "scaleTargetRef", "name")
		if kind == workload.GetKind() && name == workload.GetName() {
			out = append(out, fmt.Sprintf("The autoscaler %s manages this workload's copies and may undo this change.", hpa.GetName()))
		}
	}
	return out
}

// ownerWarnings flags objects that another tool will reconcile back.
func ownerWarnings(obj *unstructured.Unstructured) []string {
	labels, annotations := obj.GetLabels(), obj.GetAnnotations()
	var out []string
	if release := annotations["meta.helm.sh/release-name"]; release != "" || labels["app.kubernetes.io/managed-by"] == "Helm" {
		if release == "" {
			release = "that manages it"
		}
		out = append(out, fmt.Sprintf("Managed by the Helm release %s: the next helm upgrade will undo changes made here.", release))
	}
	if labels["argocd.argoproj.io/instance"] != "" || annotations["argocd.argoproj.io/tracking-id"] != "" {
		out = append(out, "Managed by Argo CD: the next sync will revert changes made here. Change it in Git instead.")
	}
	if labels["kustomize.toolkit.fluxcd.io/name"] != "" || labels["helm.toolkit.fluxcd.io/name"] != "" {
		out = append(out, "Managed by Flux: the next reconcile will revert changes made here. Change it in Git instead.")
	}
	return out
}

func stripServerFields(obj *unstructured.Unstructured) {
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(obj.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(obj.Object, "status")
}

// mergePatch adds a resourceVersion precondition to a merge patch body,
// keeping any metadata (such as annotations) the body already sets.
func mergePatch(resourceVersion string, body map[string]interface{}) ([]byte, error) {
	meta, _ := body["metadata"].(map[string]interface{})
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["resourceVersion"] = resourceVersion
	body["metadata"] = meta
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode patch: %w", err)
	}
	return data, nil
}

func causes(err error) []string {
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		if details := status.Status().Details; details != nil {
			var out []string
			for _, c := range details.Causes {
				switch {
				case c.Message != "":
					out = append(out, c.Message)
				case c.Field != "":
					out = append(out, c.Field)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []string{err.Error()}
}
