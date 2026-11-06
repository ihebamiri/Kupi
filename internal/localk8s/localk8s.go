// Package localk8s manages local clusters through their own CLIs (k3d,
// kind, minikube): listing, creating, starting, stopping, deleting, and
// loading local images into them. Tools run directly from their absolute
// path with an argument list; user-supplied values are validated and placed
// after "--" so they can never be read as flags.
package localk8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/iheb-amiri/kupi/internal/procutil"
)

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
	// imageRe follows the Docker reference grammar closely enough to reject
	// anything that could be read as a flag or a shell fragment.
	imageRe   = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)*(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(@sha256:[a-f0-9]{64})?$`)
	versionRe = regexp.MustCompile(`^rancher/k3s:v[0-9]+\.[0-9]+\.[0-9]+-k3s[0-9]+$`)
)

const maxOutput = 64 * 1024

// ValidName checks a local cluster name.
func ValidName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("cluster names use lowercase letters, digits and dashes, up to 32 characters")
	}
	return nil
}

// ValidImage checks an image reference.
func ValidImage(ref string) error {
	if len(ref) > 255 || !imageRe.MatchString(ref) {
		return fmt.Errorf("%q isn't a valid image reference", ref)
	}
	return nil
}

// Tool is a local-cluster CLI found on this machine.
type Tool struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Tools reports which local-cluster CLIs are installed.
func Tools() []Tool {
	var out []Tool
	for _, name := range []string{"k3d", "kind", "minikube"} {
		if p, err := exec.LookPath(name); err == nil {
			out = append(out, Tool{Name: name, Path: p})
		}
	}
	return out
}

// run executes a tool directly (no shell) and returns its combined output.
func run(ctx context.Context, tool string, args ...string) (string, error) {
	path, err := exec.LookPath(tool)
	if err != nil {
		return "", fmt.Errorf("%s isn't installed", tool)
	}
	cmd := procutil.Command(ctx, path, args...)
	var out bytes.Buffer
	cmd.Stdout = &limited{w: &out}
	cmd.Stderr = &limited{w: &out}
	err = cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("%s timed out", tool)
		}
		return text, fmt.Errorf("%s failed: %s", tool, lastLines(text, 4))
	}
	return text, nil
}

type limited struct{ w *bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if room := maxOutput - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// Cluster is a k3d cluster.
type Cluster struct {
	Name           string `json:"name"`
	Context        string `json:"context"`
	Servers        int    `json:"servers"`
	ServersRunning int    `json:"serversRunning"`
	Agents         int    `json:"agents"`
	AgentsRunning  int    `json:"agentsRunning"`
	Running        bool   `json:"running"`
}

// ListK3d returns the k3d clusters on this machine.
func ListK3d(ctx context.Context) ([]Cluster, error) {
	out, err := run(ctx, "k3d", "cluster", "list", "-o", "json")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name           string `json:"name"`
		ServersCount   int    `json:"serversCount"`
		ServersRunning int    `json:"serversRunning"`
		AgentsCount    int    `json:"agentsCount"`
		AgentsRunning  int    `json:"agentsRunning"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("read k3d cluster list: %w", err)
	}
	clusters := make([]Cluster, 0, len(raw))
	for _, r := range raw {
		clusters = append(clusters, Cluster{
			Name: r.Name, Context: "k3d-" + r.Name,
			Servers: r.ServersCount, ServersRunning: r.ServersRunning,
			Agents: r.AgentsCount, AgentsRunning: r.AgentsRunning,
			Running: r.ServersCount > 0 && r.ServersRunning == r.ServersCount,
		})
	}
	return clusters, nil
}

// CreateOptions configures a new k3d cluster.
type CreateOptions struct {
	Name   string `json:"name"`
	Agents int    `json:"agents"`
	// Image pins the k3s version, e.g. rancher/k3s:v1.35.5-k3s1. Empty uses k3d's default.
	Image string `json:"image"`
}

// CreateK3d creates a cluster with its API on 127.0.0.1 and without
// switching the user's current kubectl context.
func CreateK3d(ctx context.Context, o CreateOptions) (string, error) {
	if err := ValidName(o.Name); err != nil {
		return "", err
	}
	if o.Agents < 0 || o.Agents > 5 {
		return "", fmt.Errorf("agents must be between 0 and 5")
	}
	if o.Image != "" && !versionRe.MatchString(o.Image) {
		return "", fmt.Errorf("the k3s image must look like rancher/k3s:v1.35.5-k3s1")
	}
	port, err := freePort()
	if err != nil {
		return "", err
	}
	args := []string{"cluster", "create", "--api-port", "127.0.0.1:" + strconv.Itoa(port), "--wait", "--kubeconfig-switch-context=false",
		"--agents", strconv.Itoa(o.Agents)}
	if o.Image != "" {
		args = append(args, "--image", o.Image)
	}
	args = append(args, "--", o.Name)
	return run(ctx, "k3d", args...)
}

// StartK3d, StopK3d and DeleteK3d manage a cluster's lifecycle.
func StartK3d(ctx context.Context, name string) (string, error) { return k3dAction(ctx, "start", name) }
func StopK3d(ctx context.Context, name string) (string, error)  { return k3dAction(ctx, "stop", name) }
func DeleteK3d(ctx context.Context, name string) (string, error) {
	return k3dAction(ctx, "delete", name)
}

func k3dAction(ctx context.Context, action, name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return run(ctx, "k3d", "cluster", action, "--", name)
}

// ErrNotLocal means a context isn't a cluster Kupi knows how to load images into.
var ErrNotLocal = errors.New("loading local images works with k3d, kind and minikube clusters")

// Target is the local cluster behind a context.
type Target struct {
	Tool string // k3d, kind or minikube
	Name string
	// NodeLabel selects the cluster's node containers (k3d and kind).
	NodeLabel string
}

// TargetOf identifies the local cluster behind a kubeconfig context.
func TargetOf(contextName string) (Target, error) {
	switch {
	case strings.HasPrefix(contextName, "k3d-"):
		name := strings.TrimPrefix(contextName, "k3d-")
		return Target{Tool: "k3d", Name: name, NodeLabel: "k3d.cluster=" + name}, ValidName(name)
	case strings.HasPrefix(contextName, "kind-"):
		name := strings.TrimPrefix(contextName, "kind-")
		return Target{Tool: "kind", Name: name, NodeLabel: "io.x-k8s.kind.cluster=" + name}, ValidName(name)
	case strings.HasPrefix(contextName, "minikube"):
		return Target{Tool: "minikube", Name: contextName}, ValidName(contextName)
	}
	return Target{}, ErrNotLocal
}

// LoadImage copies a local image into a local cluster's nodes, so pods can
// use it without a registry. For k3d and kind it exports only the nodes'
// platform and imports it into each node's containerd, then checks the image
// is really there: k3d's own import reports success even when the node
// rejects a multi-platform export from Docker's containerd image store.
func LoadImage(ctx context.Context, contextName, image string) (string, error) {
	if err := ValidImage(image); err != nil {
		return "", err
	}
	t, err := TargetOf(contextName)
	if err != nil {
		return "", err
	}
	if t.Tool == "minikube" {
		return run(ctx, "minikube", "image", "load", "-p", t.Name, "--", image)
	}
	nodes, err := nodeContainers(ctx, t)
	if err != nil {
		return "", err
	}
	platform := enginePlatform(ctx)
	var done []string
	for _, node := range nodes {
		if err := importInto(ctx, node, image, platform); err != nil {
			return strings.Join(done, "\n"), fmt.Errorf("node %s: %w", node, err)
		}
		done = append(done, "imported into "+node)
	}
	return strings.Join(done, "\n"), nil
}

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// nodeContainers lists the Kubernetes node containers of a k3d or kind
// cluster (not k3d's load balancer or tools containers).
func nodeContainers(ctx context.Context, t Target) ([]string, error) {
	out, err := run(ctx, "docker", "ps", "--filter", "label="+t.NodeLabel, "--format", `{{.Names}} {{.Label "k3d.role"}}`)
	if err != nil {
		return nil, err
	}
	var nodes []string
	for _, line := range strings.Split(out, "\n") {
		name, role, _ := strings.Cut(strings.TrimSpace(line), " ")
		if name == "" || !containerNameRe.MatchString(name) {
			continue
		}
		if t.Tool == "k3d" && role != "server" && role != "agent" {
			continue
		}
		nodes = append(nodes, name)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no running nodes found for %s cluster %s", t.Tool, t.Name)
	}
	return nodes, nil
}

// enginePlatform returns the engine's platform, such as linux/amd64.
func enginePlatform(ctx context.Context) string {
	out, err := run(ctx, "docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}")
	if err != nil || !regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`).MatchString(out) {
		return "linux/amd64"
	}
	return out
}

// importInto pipes `docker save` into the node's containerd, process to
// process with no shell, and verifies the result.
func importInto(ctx context.Context, node, image, platform string) error {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return errors.New("the docker CLI isn't installed")
	}
	save := procutil.Command(ctx, docker, "save", "--platform", platform, "--", image)
	imp := procutil.Command(ctx, docker, "exec", "-i", node, "ctr", "-n", "k8s.io", "images", "import", "--platform", platform, "-")
	pipe, err := save.StdoutPipe()
	if err != nil {
		return err
	}
	var saveErr, impOut bytes.Buffer
	save.Stderr = &limited{w: &saveErr}
	imp.Stdin = pipe
	imp.Stdout = &limited{w: &impOut}
	imp.Stderr = &limited{w: &impOut}
	if err := save.Start(); err != nil {
		return fmt.Errorf("export the image: %w", err)
	}
	if err := imp.Run(); err != nil {
		_ = save.Wait()
		return fmt.Errorf("import failed: %s", lastLines(impOut.String(), 3))
	}
	if err := save.Wait(); err != nil {
		return fmt.Errorf("export failed: %s", lastLines(saveErr.String(), 3))
	}
	list, err := run(ctx, "docker", "exec", node, "ctr", "-n", "k8s.io", "images", "ls", "-q")
	if err != nil {
		return err
	}
	want := normalizeRef(image)
	for _, line := range strings.Split(list, "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("the node didn't keep the image (%s missing after import)", want)
}

// normalizeRef expands a short reference the way containerd stores it:
// "busybox" -> "docker.io/library/busybox:latest".
func normalizeRef(ref string) string {
	name, tag := ref, ""
	if i := strings.Index(ref, "@"); i >= 0 {
		name, tag = ref[:i], ref[i:]
	} else if slash, colon := strings.LastIndex(ref, "/"), strings.LastIndex(ref, ":"); colon > slash {
		name, tag = ref[:colon], ref[colon:]
	}
	if tag == "" {
		tag = ":latest"
	}
	first := strings.Split(name, "/")[0]
	hasHost := strings.Contains(name, "/") && (strings.ContainsAny(first, ".:") || first == "localhost")
	switch {
	case hasHost:
	case !strings.Contains(name, "/"):
		name = "docker.io/library/" + name
	default:
		name = "docker.io/" + name
	}
	return name + tag
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Timeouts for slow operations.
const (
	CreateTimeout = 5 * time.Minute
	ActionTimeout = 2 * time.Minute
)
