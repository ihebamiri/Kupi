package bridge

import (
	"context"
	"fmt"

	"github.com/ihebamiri/kupi/internal/gitops"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/validate"
)

// OwnerView says which GitOps app manages an object, and that app's state.
type OwnerView struct {
	Owner *gitops.Owner `json:"owner"`
	App   *gitops.App   `json:"app"`
	// Error is set when the owner is known but its app couldn't be read.
	Error string `json:"error"`
}

// GitOpsService shows Argo CD and Flux apps and asks them to act.
type GitOpsService struct{ d Deps }

// NewGitOpsService returns the GitOps service.
func NewGitOpsService(d Deps) *GitOpsService { return &GitOpsService{d: d} }

// Overview lists every Argo CD Application and Flux Kustomization and
// HelmRelease the user can see.
func (s *GitOpsService) Overview(ctx context.Context, contextName string) (*gitops.Overview, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	return gitops.List(ctx, sess.Reader())
}

// OwnerOf reports the GitOps app that manages an object, if any.
func (s *GitOpsService) OwnerOf(ctx context.Context, contextName string, ref ResourceRef) (OwnerView, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return OwnerView{}, err
	}
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return OwnerView{}, err
	}
	obj, err := sess.Reader().Get(ctx, req.GVR, ref.Namespace, ref.Name)
	if err != nil {
		return OwnerView{}, err
	}
	owner := gitops.OwnerOf(obj)
	if owner == nil {
		return OwnerView{}, nil
	}
	app, err := gitops.Find(ctx, sess.Reader(), owner)
	if err != nil {
		return OwnerView{Owner: owner, Error: err.Error()}, nil
	}
	return OwnerView{Owner: owner, App: app}, nil
}

// PrepareAction previews asking Argo CD or Flux to act on an app: refresh
// or sync (Argo CD), reconcile, suspend or resume (Flux).
func (s *GitOpsService) PrepareAction(ctx context.Context, contextName, kind, namespace, name, action string) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if _, ok := gitops.GVRFor(kind); !ok {
		return PlanView{}, fmt.Errorf("unknown app kind %q", kind)
	}
	if err := validate.Namespace(namespace); err != nil {
		return PlanView{}, err
	}
	if err := validate.Name(name); err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareGitOps(ctx, guard.GitOpsRequest{Context: contextName, Kind: kind, Namespace: namespace, Name: name, Action: action})
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}
