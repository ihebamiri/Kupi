// Package bridge holds the Wails services: the only Go code the UI can call.
// Every method validates its inputs before doing anything else; the UI is
// treated as untrusted.
package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/buildmode"
	"github.com/ihebamiri/kupi/internal/charts"
	"github.com/ihebamiri/kupi/internal/clipboard"
	"github.com/ihebamiri/kupi/internal/forward"
	"github.com/ihebamiri/kupi/internal/gitpr"
	"github.com/ihebamiri/kupi/internal/guard"
	"github.com/ihebamiri/kupi/internal/history"
	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/laptop"
	"github.com/ihebamiri/kupi/internal/live"
	"github.com/ihebamiri/kupi/internal/localk8s"
	"github.com/ihebamiri/kupi/internal/metrics"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/settings"
	"github.com/ihebamiri/kupi/internal/terminal"
	"github.com/ihebamiri/kupi/internal/trust"
	"github.com/ihebamiri/kupi/internal/validate"
	"github.com/ihebamiri/kupi/internal/vault"
	"github.com/ihebamiri/kupi/internal/version"
)

const connectTimeout = 30 * time.Second

var planIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Deps are the packages the services call into.
type Deps struct {
	Kube      *kube.Manager
	Guard     *guard.Guard
	Audit     *audit.Log
	Trust     *trust.Store
	Profiles  *profile.Store
	Live      *live.Manager
	Metrics   *metrics.Manager
	Forwards  *forward.Manager
	Emit      live.Emitter
	Clipboard *clipboard.Secure
	Vault     *vault.Vault
	// History records changes and warnings while a context is connected.
	History      *history.Recorder
	HistoryStore *history.Store
	// Laptop stops idle local clusters and wakes them on connect.
	Laptop    *laptop.Manager
	ConfigDir string
	// Settings are the user's preferences.
	Settings *settings.Store
	// Terminals runs the integrated terminal's shells.
	Terminals *terminal.Manager
	// Charts finds and downloads Helm charts.
	Charts *charts.Client
	// Git opens pull requests for GitOps-managed objects.
	Git *gitpr.Client
	// Native asks the user in a native OS dialog, which page scripts can't
	// answer. It reports false when no dialog could be shown.
	Native *Native
}

// Native holds functions that need the running Wails app, set once it starts.
type Native struct {
	mu      sync.RWMutex
	confirm func(title, message, accept string) bool
	openURL func(url string) error
}

// Set installs the native functions.
func (n *Native) Set(confirm func(title, message, accept string) bool, openURL func(url string) error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.confirm, n.openURL = confirm, openURL
}

// Confirm asks a yes/no question in a native dialog.
func (n *Native) Confirm(title, message, accept string) bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	f := n.confirm
	n.mu.RUnlock()
	return f != nil && f(title, message, accept)
}

// OpenURL opens a web page in the default browser.
func (n *Native) OpenURL(url string) error {
	if n == nil {
		return fmt.Errorf("can't open links yet")
	}
	n.mu.RLock()
	f := n.openURL
	n.mu.RUnlock()
	if f == nil {
		return fmt.Errorf("can't open links yet")
	}
	return f(url)
}

// clipboardTTL is how long a copied secret stays on the clipboard.
func (d Deps) clipboardTTL() time.Duration {
	if d.Settings == nil {
		return 30 * time.Second
	}
	return time.Duration(d.Settings.Get().ClipboardClearSeconds) * time.Second
}

// stopContext ends every stream, sampler and forward of a context.
func (d Deps) stopContext(contextName string) {
	d.History.Stop(contextName)
	d.Live.StopContext(contextName)
	d.Metrics.Stop(contextName)
	d.Forwards.StopContext(contextName)
}

// AppService reports facts about the running app.
type AppService struct{ d Deps }

// NewAppService returns the app service.
func NewAppService(d Deps) *AppService { return &AppService{d: d} }

// Info returns the version and platform.
func (s *AppService) Info() AppInfo {
	return AppInfo{Version: version.Version, Platform: runtime.GOOS, Production: buildmode.Production, ConfigDir: s.d.ConfigDir}
}

// ClusterService lists contexts, manages trust and profiles, and connects.
type ClusterService struct{ d Deps }

// NewClusterService returns the cluster service.
func NewClusterService(d Deps) *ClusterService { return &ClusterService{d: d} }

// ListContexts describes every kubeconfig context.
func (s *ClusterService) ListContexts() ([]ContextInfo, error) {
	contexts, err := s.d.Kube.Loader().Contexts()
	if err != nil {
		return nil, err
	}
	out := make([]ContextInfo, 0, len(contexts))
	for _, c := range contexts {
		kind, overridden := s.d.Profiles.Resolve(c.Name, c.Cluster, c.Server)
		info := ContextInfo{
			Name:                  c.Name,
			Cluster:               c.Cluster,
			User:                  c.User,
			Namespace:             c.Namespace,
			Server:                c.Server,
			Current:               c.Current,
			Profile:               string(kind),
			ProfileOverridden:     overridden,
			DetectedProfile:       string(profile.Detect(c.Name, c.Cluster, c.Server)),
			File:                  c.UserFile,
			AuthProvider:          c.AuthProvider,
			InsecureSkipTLSVerify: c.InsecureSkipTLSVerify,
			EditingEnabled:        profile.RulesFor(kind).EditingOnByDefault,
		}
		if c.Exec != nil {
			info.NeedsTrust = true
			info.ExecCommand = c.Exec.Command
			info.ExecArgs = c.Exec.Args
			info.ExecEnvNames = c.Exec.EnvNames
			if hash, err := kubeconfig.FileHash(c.UserFile); err == nil {
				info.Trusted = s.d.Trust.IsTrusted(c.UserFile, hash)
			}
		}
		if sess, ok := s.d.Kube.Session(c.Name); ok {
			info.Connected = true
			info.ServerVersion = sess.ServerVersion()
			info.EditingEnabled = s.d.Guard.EditingEnabled(c.Name, sess.Profile())
		}
		out = append(out, info)
	}
	return out, nil
}

// TrustKubeconfig trusts the credential plugins in one of the loaded
// kubeconfig files, pinned to its current contents.
func (s *ClusterService) TrustKubeconfig(file string) error {
	path, err := s.loadedFile(file)
	if err != nil {
		return err
	}
	hash, err := kubeconfig.FileHash(path)
	if err != nil {
		return err
	}
	contexts, err := s.d.Kube.Loader().Contexts()
	if err != nil {
		return err
	}
	var commands []string
	for _, c := range contexts {
		if c.Exec != nil && sameFile(c.UserFile, path) {
			commands = append(commands, strings.TrimSpace(c.Exec.Command+" "+strings.Join(c.Exec.Args, " ")))
		}
	}
	if err := s.d.Trust.Trust(path, hash, commands); err != nil {
		return err
	}
	_, err = s.d.Audit.Append(audit.Entry{Action: "kubeconfig-trusted", Resource: path, Outcome: audit.OutcomeInfo, Reason: strings.Join(commands, "; ")})
	return err
}

// RevokeTrust forgets a trusted kubeconfig file.
func (s *ClusterService) RevokeTrust(file string) error {
	path, err := s.loadedFile(file)
	if err != nil {
		return err
	}
	if err := s.d.Trust.Revoke(path); err != nil {
		return err
	}
	_, err = s.d.Audit.Append(audit.Entry{Action: "kubeconfig-trust-revoked", Resource: path, Outcome: audit.OutcomeInfo})
	return err
}

// Connect opens a session to a context.
func (s *ClusterService) Connect(ctx context.Context, contextName string) (Connection, error) {
	if err := validate.ContextName(contextName); err != nil {
		return Connection{}, err
	}
	if name, ok := s.d.Laptop.ShouldWake(contextName); ok {
		// Laptop mode stopped this cluster while idle; start it again.
		wctx, wcancel := context.WithTimeout(ctx, localk8s.ActionTimeout)
		_, err := localk8s.StartK3d(wctx, name)
		wcancel()
		if err != nil {
			return Connection{}, fmt.Errorf("start the idle-stopped cluster %s: %w", name, err)
		}
		s.d.Laptop.Woke(name)
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	sess, err := s.d.Kube.Connect(ctx, contextName)
	if err != nil {
		var nt *kube.NotTrustedError
		if errors.As(err, &nt) {
			return Connection{}, fmt.Errorf("trust this kubeconfig first: %w", err)
		}
		if info, ierr := s.d.Kube.Loader().Context(contextName); ierr == nil {
			return Connection{}, explainConnectError(info.Server, err)
		}
		return Connection{}, err
	}
	s.d.History.Start(contextName, sess.Reader(), sess.DefaultNamespace())
	return Connection{
		Context:          contextName,
		Profile:          string(sess.Profile()),
		ServerVersion:    sess.ServerVersion(),
		DefaultNamespace: sess.DefaultNamespace(),
		EditingEnabled:   s.d.Guard.EditingEnabled(contextName, sess.Profile()),
	}, nil
}

// Disconnect closes a session and drops its editing state and pending plans.
func (s *ClusterService) Disconnect(contextName string) error {
	if err := validate.ContextName(contextName); err != nil {
		return err
	}
	s.d.stopContext(contextName)
	s.d.Kube.Disconnect(contextName)
	s.d.Guard.Forget(contextName)
	return nil
}

// SetProfile sets a context's profile. An empty kind returns to detection.
// The context is disconnected so the new rules apply from the next connect.
func (s *ClusterService) SetProfile(contextName, kind string) error {
	if err := validate.ContextName(contextName); err != nil {
		return err
	}
	k := profile.Kind(kind)
	if k != "" && !k.Valid() {
		return fmt.Errorf("unknown profile %q", kind)
	}
	if _, err := s.d.Kube.Loader().Context(contextName); err != nil {
		return err
	}
	if err := s.d.Profiles.Set(contextName, k); err != nil {
		return err
	}
	s.d.stopContext(contextName)
	s.d.Kube.Disconnect(contextName)
	s.d.Guard.Forget(contextName)
	label := kind
	if label == "" {
		label = "auto-detect"
	}
	_, err := s.d.Audit.Append(audit.Entry{Context: contextName, Action: "profile-set", Resource: label, Outcome: audit.OutcomeInfo})
	return err
}

// SetEditing switches changes on or off for a connected context.
func (s *ClusterService) SetEditing(contextName string, enabled bool, reason string) error {
	if err := validate.ContextName(contextName); err != nil {
		return err
	}
	return s.d.Guard.SetEditing(contextName, enabled, reason)
}

func (s *ClusterService) loadedFile(file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file is required")
	}
	for _, f := range s.d.Kube.Loader().Files() {
		if sameFile(f, file) {
			return f, nil
		}
	}
	return "", fmt.Errorf("%s is not one of the loaded kubeconfig files", file)
}

func sameFile(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// GuardService prepares and commits cluster changes.
type GuardService struct{ d Deps }

// NewGuardService returns the guard service.
func NewGuardService(d Deps) *GuardService { return &GuardService{d: d} }

// PrepareApply previews applying a YAML or JSON manifest. A manifest with
// several objects (separated by ---, or a v1 List) is previewed object by
// object and applied together.
func (s *GuardService) PrepareApply(ctx context.Context, contextName, manifest string, forceConflicts bool) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := validate.Manifest(manifest); err != nil {
		return PlanView{}, err
	}
	objs, err := parseObjects(manifest, guard.MaxBatchObjects)
	if err != nil {
		return PlanView{}, err
	}
	var plan *guard.Plan
	if len(objs) == 1 {
		plan, err = s.d.Guard.PrepareApply(ctx, guard.ApplyRequest{Context: contextName, Object: objs[0], ForceConflicts: forceConflicts})
	} else {
		plan, err = s.d.Guard.PrepareApplyBatch(ctx, guard.ApplyBatchRequest{Context: contextName, Objects: objs, ForceConflicts: forceConflicts})
	}
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareDelete previews deleting an object.
func (s *GuardService) PrepareDelete(ctx context.Context, contextName string, ref ResourceRef) (PlanView, error) {
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareDelete(ctx, req)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareScale previews changing a workload's replica count.
func (s *GuardService) PrepareScale(ctx context.Context, contextName string, ref ResourceRef, replicas int32) (PlanView, error) {
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareScale(ctx, req, replicas)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// PrepareRestart previews a rolling restart.
func (s *GuardService) PrepareRestart(ctx context.Context, contextName string, ref ResourceRef) (PlanView, error) {
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareRestart(ctx, req)
	if err != nil {
		return PlanView{}, err
	}
	return planView(plan), nil
}

// Commit runs a prepared change.
func (s *GuardService) Commit(ctx context.Context, planID, reason, typedName string) (CommitResult, error) {
	if !planIDRe.MatchString(planID) {
		return CommitResult{}, fmt.Errorf("invalid plan id")
	}
	if len(typedName) > validate.MaxContextNameLen {
		return CommitResult{}, fmt.Errorf("confirmation text is too long")
	}
	res, err := s.d.Guard.Commit(ctx, planID, guard.Confirmation{Reason: reason, TypedName: typedName})
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Outcome: res.Outcome, Message: res.Message, AuditSeq: res.AuditSeq}, nil
}

// Cancel discards a prepared change.
func (s *GuardService) Cancel(planID string) {
	if planIDRe.MatchString(planID) {
		s.d.Guard.Cancel(planID)
	}
}

// AuditService reads the audit log.
type AuditService struct{ d Deps }

// NewAuditService returns the audit service.
func NewAuditService(d Deps) *AuditService { return &AuditService{d: d} }

// Recent returns the newest entries first.
func (s *AuditService) Recent(limit int) ([]AuditEntry, error) {
	limit = max(1, min(limit, 1000))
	entries, err := s.d.Audit.Recent(limit)
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, AuditEntry{
			Seq: e.Seq, Time: e.Time.Format(time.RFC3339), Context: e.Context, Profile: e.Profile,
			OSUser: e.OSUser, KubeUser: e.KubeUser, Action: e.Action, Resource: e.Resource,
			Reason: e.Reason, Outcome: e.Outcome, Error: e.Error, Hash: e.Hash,
		})
	}
	return out, nil
}

// Verify checks the audit chain.
func (s *AuditService) Verify() (AuditVerification, error) {
	res, err := s.d.Audit.Verify()
	if err != nil {
		return AuditVerification{}, err
	}
	return AuditVerification{OK: res.OK, Entries: res.Entries, BrokenAt: res.BrokenAt, Problem: res.Problem}, nil
}

func targetRequest(contextName string, ref ResourceRef) (guard.TargetRequest, error) {
	if err := validate.ContextName(contextName); err != nil {
		return guard.TargetRequest{}, err
	}
	if err := validate.GroupVersionResource(ref.Group, ref.Version, ref.Resource); err != nil {
		return guard.TargetRequest{}, err
	}
	if err := validate.OptionalNamespace(ref.Namespace); err != nil {
		return guard.TargetRequest{}, err
	}
	if err := validate.Name(ref.Name); err != nil {
		return guard.TargetRequest{}, err
	}
	return guard.TargetRequest{
		Context:   contextName,
		GVR:       schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource},
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}, nil
}

// parseSingleObject decodes exactly one object from YAML or JSON.
func parseSingleObject(manifest string) (*unstructured.Unstructured, error) {
	objs, err := parseObjects(manifest, 1)
	if err != nil {
		return nil, err
	}
	return objs[0], nil
}

// parseObjects decodes up to limit objects from YAML or JSON. Documents are
// separated by ---; a List (kind: List, or any *List) is expanded into its
// items. Empty documents and comments are skipped.
func parseObjects(manifest string, limit int) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)
	var out []*unstructured.Unstructured
	add := func(doc map[string]interface{}, n int) error {
		if len(out) >= limit {
			if limit == 1 {
				return fmt.Errorf("this manifest holds more than one object; the form edits one at a time, so use the YAML view")
			}
			return fmt.Errorf("the manifest holds more than %d objects", limit)
		}
		if _, ok := doc["kind"].(string); !ok {
			return fmt.Errorf("object %d has no kind", n)
		}
		out = append(out, &unstructured.Unstructured{Object: doc})
		return nil
	}
	for n := 1; ; n++ {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse manifest (document %d): %w", n, err)
		}
		if len(doc) == 0 {
			continue
		}
		kind, _ := doc["kind"].(string)
		items, isList := doc["items"].([]interface{})
		if isList && strings.HasSuffix(kind, "List") {
			for _, item := range items {
				m, ok := item.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("document %d: list items must be objects", n)
				}
				if err := add(m, n); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := add(doc, n); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the manifest is empty")
	}
	return out, nil
}

func planView(p *guard.Plan) PlanView {
	items := make([]PlanItemView, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, PlanItemView{
			APIVersion: it.APIVersion, Kind: it.Kind, Namespace: it.Namespace, Name: it.Name,
			State: it.State, Note: it.Note, Diff: it.Diff, Summary: nonNil(it.Summary),
			Warnings: nonNil(it.Warnings), Conflicts: nonNil(it.Conflicts),
		})
	}
	return PlanView{
		ID: p.ID, Context: p.Context, Profile: string(p.Profile), Action: string(p.Action),
		APIVersion: p.APIVersion, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name,
		Diff: p.Diff, Summary: nonNil(p.Summary), Warnings: nonNil(p.Warnings), Conflicts: nonNil(p.Conflicts),
		Allowed: p.Allowed, DeniedReason: p.DeniedReason, NeedsReason: p.NeedsReason,
		ConfirmName: p.ConfirmName, ExpiresAt: p.ExpiresAt.Format(time.RFC3339), Items: items,
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// explainConnectError adds advice for a known trap: k3d on Windows writes
// https://host.docker.internal:<port> into the kubeconfig, and that name
// can resolve to a stale address, so connections just time out.
func explainConnectError(server string, err error) error {
	u, perr := url.Parse(server)
	if perr != nil || !strings.EqualFold(u.Hostname(), "host.docker.internal") {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, "timeout") && !strings.Contains(msg, "deadline exceeded") && !strings.Contains(msg, "refused") && !strings.Contains(msg, "no such host") {
		return err
	}
	return fmt.Errorf("%w. This cluster's address is %s, and host.docker.internal often points to an old address on Windows. "+
		"Change the server in your kubeconfig to https://127.0.0.1:%s, or create k3d clusters with --api-port 127.0.0.1:<port> (Kupi does this for clusters it creates)", err, server, u.Port())
}

// OpenURL opens an http(s) link in the default browser, for example a link
// clicked in the terminal. The UI asks the user first.
func (s *AppService) OpenURL(link string) error {
	if len(link) > 2048 {
		return fmt.Errorf("the link is too long")
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("only http and https links can be opened")
	}
	return s.d.Native.OpenURL(u.String())
}
