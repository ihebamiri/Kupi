package bridge

import (
	"context"
	"fmt"

	"github.com/ihebamiri/kupi/internal/graph"
	"github.com/ihebamiri/kupi/internal/validate"
)

// CanvasService builds the Observe canvas.
type CanvasService struct{ d Deps }

// NewCanvasService returns the canvas service.
func NewCanvasService(d Deps) *CanvasService { return &CanvasService{d: d} }

// Graph returns how the objects in a namespace ("" for all) connect.
func (s *CanvasService) Graph(ctx context.Context, contextName, namespace string) (*graph.Graph, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return nil, err
	}
	return graph.Build(ctx, sess.Reader(), namespace)
}

// Pods returns the pods behind a workload or Service on the canvas.
func (s *CanvasService) Pods(ctx context.Context, contextName, kind, namespace, name string) (*graph.PodsResult, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if !graph.HasPods(kind) {
		return nil, fmt.Errorf("%q doesn't run pods", kind)
	}
	if err := validate.Namespace(namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(name); err != nil {
		return nil, err
	}
	return graph.PodsOf(ctx, sess.Reader(), kind, namespace, name)
}
