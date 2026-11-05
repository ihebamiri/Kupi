package guard

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/iheb-amiri/kupi/internal/gitops"
)

// GitOps actions. Each one asks the tool to do its own work; Kupi never
// changes what the tool deploys.
const (
	GitOpsRefresh   = "refresh"   // Argo CD: compare with Git now
	GitOpsSync      = "sync"      // Argo CD: sync to the target revision now
	GitOpsReconcile = "reconcile" // Flux: reconcile now
	GitOpsSuspend   = "suspend"   // Flux: stop reconciling
	GitOpsResume    = "resume"    // Flux: start reconciling again
)

// GitOpsRequest names an app and what to ask of it.
type GitOpsRequest struct {
	Context   string
	Kind      string
	Namespace string
	Name      string
	Action    string
}

// PrepareGitOps previews asking Argo CD or Flux to act on one of its apps.
func (g *Guard) PrepareGitOps(ctx context.Context, req GitOpsRequest) (*Plan, error) {
	gvr, ok := gitops.GVRFor(req.Kind)
	if !ok {
		return nil, fmt.Errorf("%s isn't an Argo CD or Flux app", req.Kind)
	}
	argo := req.Kind == "Application"
	var body map[string]interface{}
	var summary string
	switch {
	case argo && req.Action == GitOpsRefresh:
		body = map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"argocd.argoproj.io/refresh": "normal"}}}
		summary = "Asks Argo CD to compare %s with Git now. Nothing is deployed."
	case argo && req.Action == GitOpsSync:
		body = map[string]interface{}{"operation": map[string]interface{}{
			"initiatedBy": map[string]interface{}{"username": "kupi"},
			"sync":        map[string]interface{}{},
		}}
		summary = "Asks Argo CD to sync %s to its target revision in Git now."
	case !argo && req.Action == GitOpsReconcile:
		body = map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"reconcile.fluxcd.io/requestedAt": g.now().UTC().Format("2006-01-02T15:04:05.000000000Z")}}}
		summary = "Asks Flux to reconcile %s with its source now."
	case !argo && (req.Action == GitOpsSuspend || req.Action == GitOpsResume):
		body = map[string]interface{}{"spec": map[string]interface{}{"suspend": req.Action == GitOpsSuspend}}
		if req.Action == GitOpsSuspend {
			summary = "Stops Flux from reconciling %s until it is resumed. Live edits then stay, but drift from Git."
		} else {
			summary = "Lets Flux reconcile %s again. It will revert live edits made while it was suspended."
		}
	default:
		return nil, fmt.Errorf("%q isn't an action for a %s", req.Action, req.Kind)
	}

	s, rules, editing, err := g.begin(req.Context)
	if err != nil {
		return nil, err
	}
	reader := s.Reader()
	current, err := reader.Get(ctx, gvr, req.Namespace, req.Name)
	if err != nil {
		return nil, fmt.Errorf("read the app: %w", err)
	}
	if argo && req.Action == GitOpsSync {
		if phase, _, _ := unstructured.NestedString(current.Object, "status", "operationState", "phase"); phase == "Running" {
			return nil, fmt.Errorf("an operation is already running on this Argo CD app")
		}
	}
	p := targetPlan(s, rules, editing, ActionApply, current)
	g.checkRBAC(ctx, p, reader, "patch", gvr, req.Namespace, req.Name)
	patch, err := mergePatch(current.GetResourceVersion(), body)
	if err != nil {
		return nil, err
	}
	target := TargetRequest{Context: req.Context, GVR: gvr, Namespace: req.Namespace, Name: req.Name}
	p.exec = g.patchAndPreview(ctx, s, p, target, current, patch)
	p.Summary = []string{fmt.Sprintf(summary, p.Resource())}
	return g.store(p), nil
}
