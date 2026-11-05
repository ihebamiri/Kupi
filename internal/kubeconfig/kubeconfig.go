// Package kubeconfig loads the user's kubeconfig files and describes each
// context, including any credential plugin that would run a command on the
// user's machine when Kupi connects.
package kubeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ContextInfo describes one kubeconfig context. It never carries credentials.
type ContextInfo struct {
	Name                  string
	Cluster               string
	User                  string
	Namespace             string
	Server                string
	Current               bool
	UserFile              string
	Exec                  *ExecPlugin
	AuthProvider          string
	InsecureSkipTLSVerify bool
}

// ExecPlugin is a credential plugin from a kubeconfig user. Plugins run an
// arbitrary command when a client connects, so Kupi asks the user to trust
// the kubeconfig file before using one. Environment variable values are not
// kept because they can hold secrets.
type ExecPlugin struct {
	Command  string
	Args     []string
	EnvNames []string
}

// Loader reads kubeconfig files using the same rules as kubectl: the
// KUBECONFIG environment variable, or ~/.kube/config.
type Loader struct {
	rules *clientcmd.ClientConfigLoadingRules
}

// NewLoader returns a loader that follows kubectl's lookup rules.
func NewLoader() *Loader {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	// Never let client-go migrate or rewrite the user's kubeconfig files.
	rules.MigrationRules = nil
	return &Loader{rules: rules}
}

// NewLoaderForFiles returns a loader that reads only the given files, in order.
func NewLoaderForFiles(files ...string) *Loader {
	return &Loader{rules: &clientcmd.ClientConfigLoadingRules{Precedence: files}}
}

// Files returns the kubeconfig files that exist, in precedence order, as
// absolute paths.
func (l *Loader) Files() []string {
	var files []string
	for _, f := range l.rules.GetLoadingPrecedence() {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && st.Mode().IsRegular() {
			files = append(files, abs)
		}
	}
	return files
}

// Load returns the merged kubeconfig.
func (l *Loader) Load() (*clientcmdapi.Config, error) {
	cfg, err := l.rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}

// Contexts describes every context in the merged kubeconfig, sorted by name.
func (l *Loader) Contexts() ([]ContextInfo, error) {
	cfg, err := l.Load()
	if err != nil {
		return nil, err
	}
	out := make([]ContextInfo, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		info := ContextInfo{
			Name:      name,
			Cluster:   c.Cluster,
			User:      c.AuthInfo,
			Namespace: c.Namespace,
			Current:   name == cfg.CurrentContext,
		}
		if cluster, ok := cfg.Clusters[c.Cluster]; ok && cluster != nil {
			info.Server = cluster.Server
			info.InsecureSkipTLSVerify = cluster.InsecureSkipTLSVerify
		}
		if user, ok := cfg.AuthInfos[c.AuthInfo]; ok && user != nil {
			info.UserFile = absOrEmpty(user.LocationOfOrigin)
			if user.Exec != nil {
				plugin := &ExecPlugin{Command: user.Exec.Command, Args: append([]string(nil), user.Exec.Args...)}
				for _, env := range user.Exec.Env {
					plugin.EnvNames = append(plugin.EnvNames, env.Name)
				}
				info.Exec = plugin
			}
			if user.AuthProvider != nil {
				info.AuthProvider = user.AuthProvider.Name
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Context returns the description of one context.
func (l *Loader) Context(name string) (ContextInfo, error) {
	contexts, err := l.Contexts()
	if err != nil {
		return ContextInfo{}, err
	}
	for _, c := range contexts {
		if c.Name == name {
			return c, nil
		}
	}
	return ContextInfo{}, fmt.Errorf("context %q not found in kubeconfig", name)
}

// ClientConfig returns a client config pinned to one context.
func (l *Loader) ClientConfig(contextName string) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(l.rules, &clientcmd.ConfigOverrides{CurrentContext: contextName})
}

// FileHash returns the SHA-256 of a kubeconfig file's contents, used to pin
// trust to the exact file the user reviewed.
func FileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func absOrEmpty(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
