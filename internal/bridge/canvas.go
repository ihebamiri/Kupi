package bridge

import (
	"context"

	"github.com/iheb-amiri/kupi/internal/graph"
	"github.com/iheb-amiri/kupi/internal/validate"
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
