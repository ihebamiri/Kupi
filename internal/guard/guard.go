// Package guard is the single checkpoint for every change Kupi makes to a
// cluster. A change happens in two steps:
//
//  1. Prepare builds a Plan: it checks the cluster profile's rules and the
//     user's RBAC permissions, runs a server-side dry run (which also runs the
//     cluster's admission policies) and renders a diff with secrets redacted.
//  2. Commit executes a Plan once, after checking the reason and confirmation
//     the profile requires, then records the outcome in the audit log.
//
// Plans are single-use and expire. Changes carry the resourceVersion seen at
// Prepare time, so a Commit fails if someone else changed the object since.
package guard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// PlanTTL is how long a prepared plan stays valid.
const PlanTTL = 5 * time.Minute

// Action is the kind of change a plan makes.
type Action string

const (
	ActionApply   Action = "apply"
	ActionDelete  Action = "delete"
	ActionScale   Action = "scale"
	ActionRestart Action = "restart"
)

var (
	ErrPlanNotFound   = errors.New("this change preview has expired or was already used; prepare it again")
	ErrNotConnected   = errors.New("not connected to this cluster")
	ErrReasonRequired = errors.New("a reason is required for changes on this cluster")
	ErrConfirmName    = errors.New("type the resource's name exactly to confirm")
	ErrNotAllowed     = errors.New("this change is not allowed")
)

// Session is what the Guard needs from a live cluster connection.
type Session interface {
	ContextName() string
	Profile() profile.Kind
	UserName() string
	DefaultNamespace() string
	Reader() kube.Reader
	Mutator() kube.Mutator
}

// Sessions looks up live sessions by context name.
type Sessions interface {
	Session(contextName string) (Session, bool)
}

// SessionsFunc adapts a function to Sessions.
type SessionsFunc func(contextName string) (Session, bool)

func (f SessionsFunc) Session(contextName string) (Session, bool) { return f(contextName) }

// Plan is a reviewed, ready-to-run change.
type Plan struct {
	ID         string
	Context    string
	Profile    profile.Kind
	Action     Action
	APIVersion string
	Kind       string
	Namespace  string
	Name       string

	// Diff is a unified YAML diff of the object before and after, with
	// secret values redacted.
	Diff string
	// Summary lists the changes in plain language.
	Summary   []string
	Warnings  []string
	Conflicts []string

	Allowed      bool
	DeniedReason string
	// NeedsReason: Commit requires a written reason.
	NeedsReason bool
	// ConfirmName: when set, Commit requires this exact text typed by the user.
	ConfirmName string
	ExpiresAt   time.Time

	diffHash string
	exec     func(ctx context.Context, m kube.Mutator) error
	shell    *ShellRequest
	used     bool
}

// Resource returns "Kind namespace/name" for display and audit.
func (p *Plan) Resource() string {
	if p.Namespace == "" {
		return fmt.Sprintf("%s %s", p.Kind, p.Name)
	}
	return fmt.Sprintf("%s %s/%s", p.Kind, p.Namespace, p.Name)
}

// Confirmation is what the user supplies at Commit.
type Confirmation struct {
	Reason    string
	TypedName string
}

// Result reports the outcome of a Commit.
type Result struct {
	Outcome  string
	Message  string
	AuditSeq int64
}

type editState struct {
	enabled bool
	reason  string
}

// Guard prepares and commits changes.
type Guard struct {
	sessions Sessions
	audit    *audit.Log
	now      func() time.Time
	osUser   string

	mu      sync.Mutex
	plans   map[string]*Plan
	editing map[string]editState
}

// New returns a Guard using sessions for cluster access and log for auditing.
func New(sessions Sessions, log *audit.Log) *Guard {
	osUser := "unknown"
	if u, err := user.Current(); err == nil {
		osUser = u.Username
	}
	return &Guard{
		sessions: sessions,
		audit:    log,
		now:      time.Now,
		osUser:   osUser,
		plans:    map[string]*Plan{},
		editing:  map[string]editState{},
	}
}

// EditingEnabled reports whether changes are switched on for a context.
func (g *Guard) EditingEnabled(contextName string, kind profile.Kind) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.editingLocked(contextName, kind)
}

func (g *Guard) editingLocked(contextName string, kind profile.Kind) bool {
	if st, ok := g.editing[contextName]; ok {
		return st.enabled
	}
	return profile.RulesFor(kind).EditingOnByDefault
}

// SetEditing switches changes on or off for a connected context until it
// disconnects. Switching on where the profile requires a reason needs one.
func (g *Guard) SetEditing(contextName string, enabled bool, reason string) error {
	s, ok := g.sessions.Session(contextName)
	if !ok {
		return ErrNotConnected
	}
	if err := validate.Reason(reason); err != nil {
		return err
	}
	rules := profile.RulesFor(s.Profile())
	if enabled && rules.RequireReason && strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	g.mu.Lock()
	g.editing[contextName] = editState{enabled: enabled, reason: reason}
	g.mu.Unlock()
	action := "editing-off"
	if enabled {
		action = "editing-on"
	}
	_, err := g.audit.Append(audit.Entry{
		Context:  contextName,
		Profile:  string(s.Profile()),
		OSUser:   g.osUser,
		KubeUser: s.UserName(),
		Action:   action,
		Reason:   reason,
		Outcome:  audit.OutcomeInfo,
	})
	return err
}

// Forget drops editing state and pending plans for a context, for example
// after it disconnects or its profile changes.
func (g *Guard) Forget(contextName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.editing, contextName)
	for id, p := range g.plans {
		if p.Context == contextName {
			delete(g.plans, id)
		}
	}
}

// Cancel discards a prepared plan.
func (g *Guard) Cancel(planID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.plans, planID)
}

// claim checks a plan against the user's confirmation and, if everything
// holds, marks it used and returns it with its session. Shell plans are not
// gated by the editing switch; every other change is.
func (g *Guard) claim(planID string, c Confirmation, shell bool) (*Plan, Session, error) {
	if err := validate.Reason(c.Reason); err != nil {
		return nil, nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dropExpiredLocked()
	p, ok := g.plans[planID]
	if !ok || p.used || (p.shell != nil) != shell {
		return nil, nil, ErrPlanNotFound
	}
	s, connected := g.sessions.Session(p.Context)
	switch {
	case !connected || s.Profile() != p.Profile:
		return nil, nil, ErrNotConnected
	case !p.Allowed:
		return nil, nil, fmt.Errorf("%w: %s", ErrNotAllowed, p.DeniedReason)
	case !shell && !g.editingLocked(p.Context, p.Profile):
		return nil, nil, fmt.Errorf("%w: editing is off for this cluster", ErrNotAllowed)
	case p.NeedsReason && strings.TrimSpace(c.Reason) == "":
		return nil, nil, ErrReasonRequired
	case p.ConfirmName != "" && c.TypedName != p.ConfirmName:
		return nil, nil, ErrConfirmName
	}
	p.used = true
	delete(g.plans, planID)
	return p, s, nil
}

// Commit runs a prepared plan.
func (g *Guard) Commit(ctx context.Context, planID string, c Confirmation) (*Result, error) {
	p, s, err := g.claim(planID, c, false)
	if err != nil {
		return nil, err
	}
	execErr := p.exec(ctx, s.Mutator())
	entry := audit.Entry{
		Context:    p.Context,
		Profile:    string(p.Profile),
		OSUser:     g.osUser,
		KubeUser:   s.UserName(),
		Action:     string(p.Action),
		Resource:   p.Resource(),
		Reason:     c.Reason,
		DiffSHA256: p.diffHash,
		Outcome:    audit.OutcomeApplied,
	}
	if execErr != nil {
		entry.Outcome = audit.OutcomeFailed
		entry.Error = execErr.Error()
	}
	logged, auditErr := g.audit.Append(entry)
	if execErr != nil {
		return nil, fmt.Errorf("%s failed: %w", p.Action, execErr)
	}
	if auditErr != nil {
		return nil, fmt.Errorf("change applied but the audit log could not be written: %w", auditErr)
	}
	return &Result{Outcome: audit.OutcomeApplied, Message: fmt.Sprintf("%s: %s", actionPastTense(p.Action), p.Resource()), AuditSeq: logged.Seq}, nil
}

// store registers a plan and fills in its ID, expiry and diff hash.
func (g *Guard) store(p *Plan) *Plan {
	p.ID = newID()
	p.ExpiresAt = g.now().Add(PlanTTL)
	sum := sha256.Sum256([]byte(p.Diff))
	p.diffHash = hex.EncodeToString(sum[:])
	g.mu.Lock()
	g.dropExpiredLocked()
	g.plans[p.ID] = p
	g.mu.Unlock()
	return p
}

func (g *Guard) dropExpiredLocked() {
	now := g.now()
	for id, p := range g.plans {
		if now.After(p.ExpiresAt) {
			delete(g.plans, id)
		}
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func actionPastTense(a Action) string {
	switch a {
	case ActionApply:
		return "Applied"
	case ActionDelete:
		return "Deleted"
	case ActionScale:
		return "Scaled"
	case ActionRestart:
		return "Restarted"
	}
	return string(a)
}
