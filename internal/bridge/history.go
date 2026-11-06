package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/iheb-amiri/kupi/internal/history"
	"github.com/iheb-amiri/kupi/internal/incident"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// HistoryQuery filters recorded history. Empty fields match everything.
type HistoryQuery struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	// Hours back from now, 1 to 336 (14 days).
	Hours int `json:"hours"`
	Limit int `json:"limit"`
}

// HistoryService serves the changes and warnings Kupi recorded.
type HistoryService struct{ d Deps }

// NewHistoryService returns the history service.
func NewHistoryService(d Deps) *HistoryService { return &HistoryService{d: d} }

func (q HistoryQuery) toQuery() (history.Query, error) {
	if err := validate.OptionalNamespace(q.Namespace); err != nil {
		return history.Query{}, err
	}
	if q.Name != "" {
		if err := validate.Name(q.Name); err != nil {
			return history.Query{}, err
		}
	}
	if len(q.Kind) > 63 {
		return history.Query{}, fmt.Errorf("invalid kind")
	}
	// The store also stops at its retention; 90 days is the longest it can be.
	hours := min(max(q.Hours, 1), 90*24)
	limit := min(max(q.Limit, 1), 2000)
	return history.Query{Since: time.Now().Add(-time.Duration(hours) * time.Hour), Namespace: q.Namespace, Kind: q.Kind, Name: q.Name, Limit: limit}, nil
}

// Changes returns recorded changes, newest first.
func (s *HistoryService) Changes(contextName string, q HistoryQuery) ([]history.Change, error) {
	if err := validate.ContextName(contextName); err != nil {
		return nil, err
	}
	hq, err := q.toQuery()
	if err != nil {
		return nil, err
	}
	return s.d.HistoryStore.Changes(contextName, hq)
}

// Events returns recorded warning events, newest first.
func (s *HistoryService) Events(contextName string, q HistoryQuery) ([]history.Event, error) {
	if err := validate.ContextName(contextName); err != nil {
		return nil, err
	}
	hq, err := q.toQuery()
	if err != nil {
		return nil, err
	}
	return s.d.HistoryStore.Events(contextName, hq)
}

// Status reports whether Kupi is recording a context.
func (s *HistoryService) Status(contextName string) (history.Status, error) {
	if err := validate.ContextName(contextName); err != nil {
		return history.Status{}, err
	}
	return s.d.History.Status(contextName), nil
}

// IncidentService builds the incident view.
type IncidentService struct{ d Deps }

// NewIncidentService returns the incident service.
func NewIncidentService(d Deps) *IncidentService { return &IncidentService{d: d} }

// Report shows what is failing in a namespace ("" for all) and what changed
// in the last minutes (5 to 1440).
func (s *IncidentService) Report(ctx context.Context, contextName, namespace string, minutes int) (*incident.Report, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return nil, err
	}
	minutes = min(max(minutes, 5), 1440)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return incident.Build(ctx, incident.Input{
		Reader: sess.Reader(), Store: s.d.HistoryStore, Context: contextName, Namespace: namespace,
		Window: time.Duration(minutes) * time.Minute, Now: time.Now(), HistorySince: s.d.History.Status(contextName).Since,
	})
}
