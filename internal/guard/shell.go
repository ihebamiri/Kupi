package guard

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/validate"
)

// ActionExec opens an interactive shell in a container.
const ActionExec Action = "exec"

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// defaultShell prefers bash and falls back to sh, inside the container.
var defaultShell = []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}

// ShellRequest asks for an interactive shell in a container.
type ShellRequest struct {
	Context   string
	Namespace string
	Pod       string
	// Container defaults to the pod's first container.
	Container string
	// Command defaults to bash, falling back to sh.
	Command []string
}

// PrepareShell checks that a shell can be opened and what the profile
// requires. Shells are not blocked by read-only mode, because they are the
// main debugging tool, but production clusters require a written reason and
// every session start is audited.
func (g *Guard) PrepareShell(ctx context.Context, req ShellRequest) (*Plan, error) {
	s, ok := g.sessions.Session(req.Context)
	if !ok {
		return nil, ErrNotConnected
	}
	if err := validate.Namespace(req.Namespace); err != nil {
		return nil, err
	}
	if err := validate.Name(req.Pod); err != nil {
		return nil, err
	}
	reader := s.Reader()
	pod, err := reader.Get(ctx, podsGVR, req.Namespace, req.Pod)
	if err != nil {
		return nil, fmt.Errorf("read the pod: %w", err)
	}
	container, err := pickContainer(pod, req.Container)
	if err != nil {
		return nil, err
	}
	rules := profile.RulesFor(s.Profile())
	p := &Plan{
		Context: s.ContextName(), Profile: s.Profile(), Action: ActionExec,
		APIVersion: "v1", Kind: "Pod", Namespace: req.Namespace, Name: req.Pod,
		Allowed: true, NeedsReason: rules.RequireReason,
	}
	if phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase"); phase != "Running" {
		p.deny(fmt.Sprintf("The pod isn't running (phase %s), so there's no container to open a shell in.", phase))
	}
	g.checkRBACSub(ctx, p, reader, "create", podsGVR, "exec", req.Namespace, req.Pod)
	p.Summary = []string{fmt.Sprintf("Opens a shell in container %s of pod %s/%s.", container, req.Namespace, req.Pod)}
	if rules.RequireReason {
		p.Warnings = append(p.Warnings, "Commands you run can change the container. The session start is recorded in the audit log.")
	}
	command := req.Command
	if len(command) == 0 {
		command = defaultShell
	}
	p.shell = &ShellRequest{Context: req.Context, Namespace: req.Namespace, Pod: req.Pod, Container: container, Command: command}
	return g.store(p), nil
}

// OpenShell claims a prepared shell plan and writes its audit entry. It
// returns the context name and a function that runs the shell until it exits
// or its ctx is cancelled. If the audit entry can't be written, no shell opens.
func (g *Guard) OpenShell(planID string, c Confirmation) (string, func(ctx context.Context, streams kube.ExecStreams) error, error) {
	p, s, err := g.claim(planID, c, true)
	if err != nil {
		return "", nil, err
	}
	req := *p.shell
	if _, err := g.audit.Append(audit.Entry{
		Context:  p.Context,
		Profile:  string(p.Profile),
		OSUser:   g.osUser,
		KubeUser: s.UserName(),
		Action:   string(ActionExec),
		Resource: fmt.Sprintf("%s container %s", p.Resource(), req.Container),
		Reason:   c.Reason,
		Outcome:  audit.OutcomeApplied,
	}); err != nil {
		return "", nil, fmt.Errorf("the audit log could not be written, so the shell was not opened: %w", err)
	}
	mutator := s.Mutator()
	return p.Context, func(ctx context.Context, streams kube.ExecStreams) error {
		return mutator.Exec(ctx, req.Namespace, req.Pod, req.Container, req.Command, streams)
	}, nil
}

// StartShell opens a prepared shell and blocks until it exits.
func (g *Guard) StartShell(ctx context.Context, planID string, c Confirmation, streams kube.ExecStreams) error {
	_, run, err := g.OpenShell(planID, c)
	if err != nil {
		return err
	}
	return run(ctx, streams)
}

func pickContainer(pod *unstructured.Unstructured, want string) (string, error) {
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	var names []string
	for _, c := range containers {
		if m, ok := c.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("the pod has no containers")
	}
	if want == "" {
		return names[0], nil
	}
	for _, n := range names {
		if n == want {
			return n, nil
		}
	}
	return "", fmt.Errorf("the pod has no container named %q", want)
}

func (g *Guard) checkRBACSub(ctx context.Context, p *Plan, r kube.Reader, verb string, gvr schema.GroupVersionResource, sub, ns, name string) {
	allowed, reason, err := r.CanI(ctx, verb, gvr, sub, ns, name)
	if err != nil {
		p.Warnings = append(p.Warnings, "Couldn't check your permissions: "+err.Error())
		return
	}
	if !allowed {
		msg := fmt.Sprintf("Your account can't open shells (%s/%s) in namespace %s.", gvr.Resource, sub, ns)
		if reason != "" {
			msg += " " + reason
		}
		p.deny(msg)
	}
}
