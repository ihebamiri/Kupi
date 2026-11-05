// Package forward runs the port-forward manager: forwards that pick a ready
// pod behind a Service or workload, listen on 127.0.0.1 only, and reconnect
// on the same local port when the pod restarts or is replaced.
package forward

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Event is sent to the UI with the full list whenever a forward changes.
const Event = "kupi:forwards"

// Status of a forward.
const (
	StatusStarting     = "starting"
	StatusActive       = "active"
	StatusReconnecting = "reconnecting"
)

const (
	podCheckEvery = 3 * time.Second
	maxBackoff    = 10 * time.Second
)

var (
	podsGVR     = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	servicesGVR = schema.GroupVersionResource{Version: "v1", Resource: "services"}
	workloads   = map[string]schema.GroupVersionResource{
		"Deployment":  {Group: "apps", Version: "v1", Resource: "deployments"},
		"StatefulSet": {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"DaemonSet":   {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"ReplicaSet":  {Group: "apps", Version: "v1", Resource: "replicasets"},
	}
)

// Target is what to forward to: a Pod, Service or workload, and its port
// (the Service port for Services, the container port otherwise).
type Target struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Port      int    `json:"port"`
}

// Forward is one forward as the UI shows it.
type Forward struct {
	ID         string `json:"id"`
	Context    string `json:"context"`
	Target     Target `json:"target"`
	LocalPort  int    `json:"localPort"`
	Pod        string `json:"pod"`
	PodPort    int    `json:"podPort"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Since      string `json:"since"`
	Reconnects int    `json:"reconnects"`
}

// Changed carries the full list of forwards.
type Changed struct {
	Forwards []Forward `json:"forwards"`
}

// Session is what a forward needs from a cluster connection.
type Session interface {
	Reader() kube.Reader
	PortForward(ctx context.Context, namespace, pod string, localPort, remotePort int, onReady func(local int)) error
}

// Emitter sends events to the UI.
type Emitter interface {
	Emit(name string, data any)
}

type entry struct {
	Forward
	cancel context.CancelFunc
}

// Manager owns all forwards.
type Manager struct {
	emit Emitter

	mu       sync.Mutex
	forwards map[string]*entry
}

// New returns a manager.
func New(emit Emitter) *Manager {
	return &Manager{emit: emit, forwards: map[string]*entry{}}
}

// Start begins forwarding target. localPort 0 picks the remote port number
// when it is free and above 1023, otherwise any free port.
func (m *Manager) Start(contextName string, s Session, t Target, localPort int) (Forward, error) {
	if t.Port < 1 || t.Port > 65535 {
		return Forward{}, fmt.Errorf("port must be between 1 and 65535")
	}
	if localPort < 0 || localPort > 65535 {
		return Forward{}, fmt.Errorf("local port must be between 0 and 65535")
	}
	if t.Kind != "Pod" && t.Kind != "Service" {
		if _, ok := workloads[t.Kind]; !ok {
			return Forward{}, fmt.Errorf("can't forward to a %s", t.Kind)
		}
	}
	if localPort == 0 {
		localPort = pickLocal(t.Port)
	} else if !portFree(localPort) {
		return Forward{}, fmt.Errorf("local port %d is already in use", localPort)
	}

	// Resolve once up front so obvious mistakes fail immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, _, err := resolve(ctx, s.Reader(), t)
	cancel()
	if err != nil {
		return Forward{}, err
	}

	runCtx, stop := context.WithCancel(context.Background())
	e := &entry{
		Forward: Forward{ID: newID(), Context: contextName, Target: t, LocalPort: localPort, Status: StatusStarting, Since: now()},
		cancel:  stop,
	}
	m.mu.Lock()
	m.forwards[e.ID] = e
	m.mu.Unlock()
	m.changed()
	go m.run(runCtx, e.ID, s, t)
	return e.Forward, nil
}

// Stop ends a forward.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	e, ok := m.forwards[id]
	delete(m.forwards, id)
	m.mu.Unlock()
	if ok {
		e.cancel()
		m.changed()
	}
}

// StopContext ends every forward of a context.
func (m *Manager) StopContext(contextName string) {
	m.mu.Lock()
	var ids []string
	for id, e := range m.forwards {
		if e.Context == contextName {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// StopAll ends every forward.
func (m *Manager) StopAll() {
	m.mu.Lock()
	var ids []string
	for id := range m.forwards {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// List returns all forwards, oldest first.
func (m *Manager) List() []Forward {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Forward, 0, len(m.forwards))
	for _, e := range m.forwards {
		out = append(out, e.Forward)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Since < out[j].Since || (out[i].Since == out[j].Since && out[i].ID < out[j].ID)
	})
	return out
}

func (m *Manager) update(id string, fn func(f *Forward)) {
	m.mu.Lock()
	e, ok := m.forwards[id]
	if ok {
		fn(&e.Forward)
	}
	m.mu.Unlock()
	if ok {
		m.changed()
	}
}

func (m *Manager) changed() {
	if m.emit != nil {
		m.emit.Emit(Event, Changed{Forwards: m.List()})
	}
}

func (m *Manager) run(ctx context.Context, id string, s Session, t Target) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		pod, podPort, err := resolve(ctx, s.Reader(), t)
		if err != nil {
			m.update(id, func(f *Forward) { f.Status, f.Error = StatusReconnecting, err.Error() })
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		m.update(id, func(f *Forward) {
			f.Pod, f.PodPort = pod, podPort
			if !first {
				f.Reconnects++
			}
		})
		first = false

		local := m.localPort(id)
		fctx, cancel := context.WithCancel(ctx)
		go watchPod(fctx, cancel, s.Reader(), t.Namespace, pod)
		err = s.PortForward(fctx, t.Namespace, pod, local, podPort, func(actual int) {
			backoff = time.Second
			m.update(id, func(f *Forward) { f.Status, f.Error, f.LocalPort = StatusActive, "", actual })
		})
		cancel()
		if ctx.Err() != nil {
			return
		}
		msg := "the connection to the pod was lost"
		if err != nil && !errors.Is(err, context.Canceled) {
			msg = err.Error()
		}
		m.update(id, func(f *Forward) { f.Status, f.Error = StatusReconnecting, msg })
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (m *Manager) localPort(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.forwards[id]; ok {
		return e.LocalPort
	}
	return 0
}

// watchPod cancels the forward when its pod stops running, so the manager
// reconnects to a replacement instead of holding a dead tunnel.
func watchPod(ctx context.Context, cancel context.CancelFunc, r kube.Reader, ns, name string) {
	t := time.NewTicker(podCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pod, err := r.Get(ctx, podsGVR, ns, name)
			if apierrors.IsNotFound(err) || (err == nil && !podUsable(pod)) {
				cancel()
				return
			}
		}
	}
}

// resolve finds the pod and container port behind a target.
func resolve(ctx context.Context, r kube.Reader, t Target) (pod string, port int, err error) {
	switch t.Kind {
	case "Pod":
		p, err := r.Get(ctx, podsGVR, t.Namespace, t.Name)
		if err != nil {
			return "", 0, fmt.Errorf("pod %s: %w", t.Name, err)
		}
		if !podUsable(p) {
			return "", 0, fmt.Errorf("pod %s isn't running", t.Name)
		}
		return t.Name, t.Port, nil
	case "Service":
		svc, err := r.Get(ctx, servicesGVR, t.Namespace, t.Name)
		if err != nil {
			return "", 0, fmt.Errorf("service %s: %w", t.Name, err)
		}
		selector, _, _ := unstructured.NestedStringMap(svc.Object, "spec", "selector")
		if len(selector) == 0 {
			return "", 0, fmt.Errorf("service %s has no selector, so there are no pods to forward to", t.Name)
		}
		target, err := serviceTargetPort(svc, t.Port)
		if err != nil {
			return "", 0, err
		}
		p, err := readyPod(ctx, r, t.Namespace, labels.SelectorFromSet(selector).String())
		if err != nil {
			return "", 0, fmt.Errorf("service %s: %w", t.Name, err)
		}
		port, err := containerPort(p, target)
		return p.GetName(), port, err
	default:
		gvr := workloads[t.Kind]
		w, err := r.Get(ctx, gvr, t.Namespace, t.Name)
		if err != nil {
			return "", 0, fmt.Errorf("%s %s: %w", t.Kind, t.Name, err)
		}
		matchLabels, _, _ := unstructured.NestedStringMap(w.Object, "spec", "selector", "matchLabels")
		if len(matchLabels) == 0 {
			return "", 0, fmt.Errorf("%s %s has no matchLabels selector", t.Kind, t.Name)
		}
		p, err := readyPod(ctx, r, t.Namespace, labels.SelectorFromSet(matchLabels).String())
		if err != nil {
			return "", 0, fmt.Errorf("%s %s: %w", t.Kind, t.Name, err)
		}
		return p.GetName(), t.Port, nil
	}
}

// serviceTargetPort returns the targetPort (number or name) for a Service port.
func serviceTargetPort(svc *unstructured.Unstructured, port int) (string, error) {
	ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	for _, item := range ports {
		p, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		n, _, _ := unstructured.NestedInt64(p, "port")
		if int(n) != port {
			continue
		}
		switch tp := p["targetPort"].(type) {
		case string:
			return tp, nil
		case int64:
			return strconv.FormatInt(tp, 10), nil
		case float64:
			return strconv.Itoa(int(tp)), nil
		}
		return strconv.Itoa(port), nil
	}
	return "", fmt.Errorf("service %s has no port %d", svc.GetName(), port)
}

// containerPort turns a targetPort (number or port name) into a number.
func containerPort(pod *unstructured.Unstructured, target string) (int, error) {
	if n, err := strconv.Atoi(target); err == nil {
		return n, nil
	}
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		ports, _, _ := unstructured.NestedSlice(cm, "ports")
		for _, p := range ports {
			pm, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			if name, _ := pm["name"].(string); name == target {
				n, _, _ := unstructured.NestedInt64(pm, "containerPort")
				return int(n), nil
			}
		}
	}
	return 0, fmt.Errorf("pod %s has no port named %q", pod.GetName(), target)
}

func readyPod(ctx context.Context, r kube.Reader, ns, selector string) (*unstructured.Unstructured, error) {
	list, err := r.List(ctx, podsGVR, ns, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	var running *unstructured.Unstructured
	for i := range list.Items {
		p := &list.Items[i]
		if !podUsable(p) {
			continue
		}
		if podReady(p) {
			return p, nil
		}
		if running == nil {
			running = p
		}
	}
	if running != nil {
		return running, nil
	}
	return nil, errors.New("no running pods right now")
}

func podUsable(p *unstructured.Unstructured) bool {
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	return phase == "Running" && p.GetDeletionTimestamp() == nil
}

func podReady(p *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(p.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]interface{}); ok && m["type"] == "Ready" && m["status"] == "True" {
			return true
		}
	}
	return false
}

func pickLocal(remote int) int {
	if remote > 1023 && portFree(remote) {
		return remote
	}
	return 0
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort(kube.ForwardAddress, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
