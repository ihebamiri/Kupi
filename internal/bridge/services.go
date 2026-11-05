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
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/buildmode"
	"github.com/iheb-amiri/kupi/internal/forward"
	"github.com/iheb-amiri/kupi/internal/guard"
	"github.com/iheb-amiri/kupi/internal/kube"
	"github.com/iheb-amiri/kupi/internal/kubeconfig"
	"github.com/iheb-amiri/kupi/internal/live"
	"github.com/iheb-amiri/kupi/internal/metrics"
	"github.com/iheb-amiri/kupi/internal/profile"
	"github.com/iheb-amiri/kupi/internal/trust"
	"github.com/iheb-amiri/kupi/internal/validate"
	"github.com/iheb-amiri/kupi/internal/version"
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
	ConfigDir string
}

// stopContext ends every stream, sampler and forward of a context.
func (d Deps) stopContext(contextName string) {
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
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	sess, err := s.d.Kube.Connect(ctx, contextName)
	if err != nil {
		var nt *kube.NotTrustedError
		if errors.As(err, &nt) {
			return Connection{}, fmt.Errorf("trust this kubeconfig first: %w", err)
		}
		return Connection{}, err
	}
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

// PrepareApply previews applying a single-object YAML or JSON manifest.
func (s *GuardService) PrepareApply(ctx context.Context, contextName, manifest string, forceConflicts bool) (PlanView, error) {
	if err := validate.ContextName(contextName); err != nil {
		return PlanView{}, err
	}
	if err := validate.Manifest(manifest); err != nil {
		return PlanView{}, err
	}
	obj, err := parseSingleObject(manifest)
	if err != nil {
		return PlanView{}, err
	}
	plan, err := s.d.Guard.PrepareApply(ctx, guard.ApplyRequest{Context: contextName, Object: obj, ForceConflicts: forceConflicts})
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
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)
	var first map[string]interface{}
	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		if len(doc) == 0 {
			continue
		}
		if first != nil {
			return nil, fmt.Errorf("the manifest holds more than one object; apply them one at a time")
		}
		first = doc
	}
	if first == nil {
		return nil, fmt.Errorf("the manifest is empty")
	}
	return &unstructured.Unstructured{Object: first}, nil
}

func planView(p *guard.Plan) PlanView {
	return PlanView{
		ID: p.ID, Context: p.Context, Profile: string(p.Profile), Action: string(p.Action),
		APIVersion: p.APIVersion, Kind: p.Kind, Namespace: p.Namespace, Name: p.Name,
		Diff: p.Diff, Summary: nonNil(p.Summary), Warnings: nonNil(p.Warnings), Conflicts: nonNil(p.Conflicts),
		Allowed: p.Allowed, DeniedReason: p.DeniedReason, NeedsReason: p.NeedsReason,
		ConfirmName: p.ConfirmName, ExpiresAt: p.ExpiresAt.Format(time.RFC3339),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
