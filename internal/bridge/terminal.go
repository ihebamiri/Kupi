package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/appdir"
	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/procutil"
	"github.com/ihebamiri/kupi/internal/terminal"
	"github.com/ihebamiri/kupi/internal/validate"
)

// ErrTerminalOff is returned while the terminal is switched off.
var ErrTerminalOff = errors.New("the terminal is off; turn it on in Settings")

var (
	terminalIDRe = regexp.MustCompile(`^[0-9a-f]{24}$`)
	shellIDRe    = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)
	// Environment variables Kupi sets itself, or that must not leak into
	// the user's shell.
	droppedEnv = map[string]bool{"KUBECONFIG": true, "KUPI_DEV_AUTOCONFIRM": true}
)

// TerminalSession describes an open terminal.
type TerminalSession struct {
	ID        string `json:"id"`
	ShellID   string `json:"shellId"`
	Shell     string `json:"shell"`
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Profile   string `json:"profile"`
	// FollowsCluster: kubectl and helm in this terminal use Context and
	// Namespace without changing the kubeconfig's current context.
	FollowsCluster bool `json:"followsCluster"`
}

// TerminalService runs the integrated terminal.
type TerminalService struct{ d Deps }

// NewTerminalService returns the terminal service.
func NewTerminalService(d Deps) *TerminalService { return &TerminalService{d: d} }

// CleanTerminalFiles removes kubeconfig overlays left by a previous run.
func CleanTerminalFiles(configDir string) {
	matches, _ := filepath.Glob(filepath.Join(configDir, "terminal", "*.kubeconfig"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// Shells lists the shells installed on this computer.
func (s *TerminalService) Shells() []terminal.Shell {
	out := terminal.Shells()
	if out == nil {
		return []terminal.Shell{}
	}
	return out
}

// Open starts a shell. With a context (and the "follow cluster" setting on),
// kubectl and helm in it use that context and namespace.
func (s *TerminalService) Open(shellID, contextName, namespace string, cols, rows int) (TerminalSession, error) {
	st := s.d.Settings.Get()
	if !st.TerminalEnabled {
		return TerminalSession{}, ErrTerminalOff
	}
	if !shellIDRe.MatchString(shellID) {
		return TerminalSession{}, fmt.Errorf("invalid shell")
	}
	if shellID == "" {
		shellID = st.TerminalShell
	}
	sh, err := terminal.Find(shellID)
	if err != nil {
		return TerminalSession{}, err
	}
	if err := validate.OptionalNamespace(namespace); err != nil {
		return TerminalSession{}, err
	}

	env := baseEnv()
	info := TerminalSession{ShellID: sh.ID, Shell: sh.Name}
	var overlay string
	if contextName != "" {
		if err := validate.ContextName(contextName); err != nil {
			return TerminalSession{}, err
		}
		c, err := s.d.Kube.Loader().Context(contextName)
		if err != nil {
			return TerminalSession{}, err
		}
		kind, _ := s.d.Profiles.Resolve(c.Name, c.Cluster, c.Server)
		if sess, ok := s.d.Kube.Session(contextName); ok {
			kind = sess.Profile()
		}
		ns := namespace
		if ns == "" {
			ns = c.Namespace
		}
		info.Context, info.Namespace, info.Profile = contextName, ns, string(kind)
		env = append(env, "KUPI_CONTEXT="+contextName, "KUPI_PROFILE="+string(kind))
		if ns != "" {
			env = append(env, "KUPI_NAMESPACE="+ns)
		}
		if st.TerminalFollowCluster {
			overlay, err = s.writeOverlay(contextName, c.Cluster, c.User, ns)
			if err != nil {
				return TerminalSession{}, err
			}
			files := append([]string{overlay}, s.d.Kube.Loader().Files()...)
			env = append(env, "KUBECONFIG="+strings.Join(files, string(os.PathListSeparator)))
			info.FollowsCluster = true
		}
	}
	if !info.FollowsCluster {
		if kc := os.Getenv("KUBECONFIG"); kc != "" {
			env = append(env, "KUBECONFIG="+kc)
		}
	}

	dir, err := os.UserHomeDir()
	if err != nil {
		dir = ""
	}
	id, err := s.d.Terminals.Open(terminal.OpenRequest{
		Shell: sh, Cols: cols, Rows: rows, Dir: dir, Env: env,
		OnExit: func() {
			if overlay != "" {
				_ = os.Remove(overlay)
			}
		},
	})
	if err != nil {
		if overlay != "" {
			_ = os.Remove(overlay)
		}
		return TerminalSession{}, err
	}
	info.ID = id
	_, _ = s.d.Audit.Append(audit.Entry{Context: info.Context, Profile: info.Profile, Action: "terminal-opened", Resource: sh.Name, Outcome: audit.OutcomeInfo})
	return info, nil
}

// baseEnv is Kupi's environment minus what it sets itself, plus terminal
// capabilities.
func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if droppedEnv[strings.ToUpper(k)] || strings.HasPrefix(strings.ToUpper(k), "KUPI_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Kupi", "KUPI_TERMINAL=1")
}

// writeOverlay writes a small kubeconfig that only selects a context and
// namespace. It goes first in KUBECONFIG; the real files follow and supply
// clusters and credentials, which are never copied. kubectl config
// commands typed in the terminal change this file, not the user's.
func (s *TerminalService) writeOverlay(contextName, cluster, user, namespace string) (string, error) {
	dir := filepath.Join(s.d.ConfigDir, "terminal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	ctx := map[string]interface{}{"cluster": cluster, "user": user}
	if namespace != "" {
		ctx["namespace"] = namespace
	}
	doc := map[string]interface{}{
		"apiVersion":      "v1",
		"kind":            "Config",
		"current-context": contextName,
		"contexts":        []interface{}{map[string]interface{}{"name": contextName, "context": ctx}},
		"preferences":     map[string]interface{}{},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(b)+".kubeconfig")
	if err := appdir.WriteFileAtomic(path, data); err != nil {
		return "", fmt.Errorf("write terminal kubeconfig: %w", err)
	}
	return path, nil
}

// Input sends keystrokes or pasted text to a terminal.
func (s *TerminalService) Input(id, data string) error {
	if !terminalIDRe.MatchString(id) {
		return fmt.Errorf("invalid terminal id")
	}
	return s.d.Terminals.Write(id, data)
}

// Resize changes a terminal's size.
func (s *TerminalService) Resize(id string, cols, rows int) {
	if terminalIDRe.MatchString(id) {
		_ = s.d.Terminals.Resize(id, cols, rows)
	}
}

// Close ends a terminal's shell.
func (s *TerminalService) Close(id string) {
	if terminalIDRe.MatchString(id) {
		s.d.Terminals.Close(id)
	}
}

// ToolInfo is one command-line tool and its version.
type ToolInfo struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// ToolsReport lists the tools a DevOps terminal usually needs, and warns
// when kubectl is too far from the cluster's version.
type ToolsReport struct {
	Tools         []ToolInfo `json:"tools"`
	ServerVersion string     `json:"serverVersion"`
	Skew          string     `json:"skew"`
}

var toolChecks = []struct {
	name string
	args []string
	pick func(string) string
}{
	{"kubectl", []string{"version", "--client", "-o", "json"}, func(out string) string {
		var v struct {
			ClientVersion struct {
				GitVersion string `json:"gitVersion"`
			} `json:"clientVersion"`
		}
		if json.Unmarshal([]byte(out), &v) == nil {
			return v.ClientVersion.GitVersion
		}
		return firstLine(out)
	}},
	{"helm", []string{"version", "--short"}, firstLine},
	{"docker", []string{"version", "--format", "{{.Client.Version}}"}, firstLine},
	{"k3d", []string{"version"}, firstLine},
	{"kind", []string{"version"}, firstLine},
	{"minikube", []string{"version", "--short"}, firstLine},
	{"flux", []string{"version", "--client"}, firstLine},
	{"argocd", []string{"version", "--client", "--short"}, firstLine},
}

var toolsCache struct {
	mu  sync.Mutex
	at  time.Time
	out []ToolInfo
}

// Tools checks which tools are installed (cached for a minute).
func (s *TerminalService) Tools(ctx context.Context, contextName string) (ToolsReport, error) {
	toolsCache.mu.Lock()
	tools := toolsCache.out
	fresh := time.Since(toolsCache.at) < time.Minute
	toolsCache.mu.Unlock()
	if !fresh {
		tools = detectTools(ctx)
		toolsCache.mu.Lock()
		toolsCache.out, toolsCache.at = tools, time.Now()
		toolsCache.mu.Unlock()
	}
	rep := ToolsReport{Tools: tools}
	if contextName != "" {
		if err := validate.ContextName(contextName); err != nil {
			return rep, err
		}
		if sess, ok := s.d.Kube.Session(contextName); ok {
			rep.ServerVersion = sess.ServerVersion()
			for _, t := range tools {
				if t.Name == "kubectl" && t.Found {
					rep.Skew = versionSkew(t.Version, rep.ServerVersion)
				}
			}
		}
	}
	return rep, nil
}

func detectTools(ctx context.Context) []ToolInfo {
	out := make([]ToolInfo, len(toolChecks))
	var wg sync.WaitGroup
	for i, tc := range toolChecks {
		out[i] = ToolInfo{Name: tc.name}
		path, err := exec.LookPath(tc.name)
		if err != nil || !filepath.IsAbs(path) {
			continue
		}
		out[i].Found, out[i].Path = true, path
		wg.Add(1)
		go func(i int, path string, args []string, pick func(string) string) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			cmd := procutil.Command(cctx, path, args...)
			var buf bytes.Buffer
			cmd.Stdout = &buf
			if cmd.Run() == nil {
				v := pick(buf.String())
				if len(v) > 80 {
					v = v[:80]
				}
				out[i].Version = v
			}
		}(i, path, tc.args, tc.pick)
	}
	wg.Wait()
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

var minorRe = regexp.MustCompile(`v?(\d+)\.(\d+)`)

// versionSkew explains a kubectl that's more than one minor version away
// from the cluster, which Kubernetes doesn't support.
func versionSkew(client, server string) string {
	c, s := minorRe.FindStringSubmatch(client), minorRe.FindStringSubmatch(server)
	if c == nil || s == nil || c[1] != s[1] {
		return ""
	}
	cm, _ := strconv.Atoi(c[2])
	sm, _ := strconv.Atoi(s[2])
	if d := cm - sm; d > 1 || d < -1 {
		return fmt.Sprintf("kubectl %s.%s is more than one minor version away from the cluster (%s.%s). Kubernetes only supports one version of difference; some commands may misbehave.", c[1], c[2], s[1], s[2])
	}
	return ""
}
