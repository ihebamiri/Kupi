package kube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	helmkube "helm.sh/helm/v4/pkg/kube"
	rspb "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// helmTimeout bounds one Helm operation.
const helmTimeout = 5 * time.Minute

func init() {
	// Helm names its server-side apply field manager after the running
	// program. Use the Helm CLI's name, so Kupi and helm share ownership of
	// release objects instead of conflicting over them.
	helmkube.ManagedFieldsManager = "helm"
}

// ErrReleaseChanged means a release got a new revision after it was reviewed.
var ErrReleaseChanged = errors.New("the release changed since this preview; prepare it again")

// HelmReader reads Helm releases. Helm stores releases in Secrets, so this
// needs permission to read Secrets in the release's namespace.
type HelmReader interface {
	// HelmReleases returns the latest revision of every release in a
	// namespace ("" for all namespaces).
	HelmReleases(ctx context.Context, namespace string) ([]*rspb.Release, error)
	// HelmHistory returns every stored revision of a release, newest first.
	HelmHistory(ctx context.Context, namespace, name string) ([]*rspb.Release, error)
	// HelmRelease returns one revision (0 for the latest).
	HelmRelease(ctx context.Context, namespace, name string, revision int) (*rspb.Release, error)
}

// HelmUpgrade upgrades a release with its own chart and new values.
type HelmUpgrade struct {
	Namespace string
	Name      string
	// Values is the complete set of user-supplied values; the chart's
	// defaults fill in the rest.
	Values map[string]any
	// FromRevision is the revision that was reviewed. The upgrade fails if
	// the release has moved on.
	FromRevision int
	// Chart, when set, replaces the release's chart (another version, or
	// another chart). Nil keeps the chart the release has.
	Chart *chartv2.Chart
}

// HelmInstall installs a chart as a new release.
type HelmInstall struct {
	Namespace string
	Name      string
	Chart     *chartv2.Chart
	// Values are the user-supplied values; the chart's defaults fill in the rest.
	Values          map[string]any
	CreateNamespace bool
}

// HelmMutator changes Helm releases. Reserved for internal/guard, like
// Mutator; internal/archtest enforces it.
type HelmMutator interface {
	// HelmUpgrade runs an upgrade, or a server-side dry run of one.
	HelmUpgrade(ctx context.Context, req HelmUpgrade, dryRun bool) (*rspb.Release, error)
	// HelmInstall installs a new release; dryRun renders and validates it
	// against the cluster without creating anything.
	HelmInstall(ctx context.Context, req HelmInstall, dryRun bool) (*rspb.Release, error)
	HelmRollback(ctx context.Context, namespace, name string, revision, fromRevision int) error
	HelmUninstall(ctx context.Context, namespace, name string, fromRevision int) error
}

// Helm returns the read-only view of the cluster's Helm releases.
func (s *Session) Helm() HelmReader { return &helmClient{s: s} }

// HelmMutator returns the Helm write capability. Only internal/guard may
// call this; internal/archtest enforces it.
func (s *Session) HelmMutator() HelmMutator { return &helmClient{s: s} }

type helmClient struct{ s *Session }

// helmGetter hands Helm the session's own client configuration, so Helm
// never reads kubeconfig files or runs credential plugins itself.
type helmGetter struct {
	s         *Session
	namespace string
}

func (g helmGetter) ToRESTConfig() (*rest.Config, error) { return rest.CopyConfig(g.s.cfg), nil }

func (g helmGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(g.s.cfg))
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g helmGetter) ToRESTMapper() (meta.RESTMapper, error) { return g.s.mapper, nil }

func (g helmGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewDefaultClientConfig(*clientcmdapi.NewConfig(), &clientcmd.ConfigOverrides{Context: clientcmdapi.Context{Namespace: g.namespace}})
}

var _ genericclioptions.RESTClientGetter = helmGetter{}

func (h *helmClient) config(namespace string) (*action.Configuration, error) {
	if h.s.cfg == nil {
		return nil, errors.New("this session has no client configuration")
	}
	cfg := action.NewConfiguration()
	cfg.SetLogger(slog.DiscardHandler)
	if err := cfg.Init(helmGetter{s: h.s, namespace: namespace}, namespace, "secret"); err != nil {
		return nil, fmt.Errorf("set up Helm: %w", err)
	}
	return cfg, nil
}

func asRelease(r any) (*rspb.Release, error) {
	rel, ok := r.(*rspb.Release)
	if !ok || rel == nil {
		return nil, fmt.Errorf("unsupported Helm release format %T", r)
	}
	return rel, nil
}

func (h *helmClient) HelmReleases(_ context.Context, namespace string) ([]*rspb.Release, error) {
	cfg, err := h.config(namespace)
	if err != nil {
		return nil, err
	}
	list := action.NewList(cfg)
	list.All = true
	list.AllNamespaces = namespace == ""
	list.StateMask = action.ListAll
	items, err := list.Run()
	if err != nil {
		return nil, fmt.Errorf("list Helm releases: %w", err)
	}
	out := make([]*rspb.Release, 0, len(items))
	for _, it := range items {
		if rel, err := asRelease(it); err == nil {
			out = append(out, rel)
		}
	}
	return out, nil
}

func (h *helmClient) HelmHistory(_ context.Context, namespace, name string) ([]*rspb.Release, error) {
	cfg, err := h.config(namespace)
	if err != nil {
		return nil, err
	}
	hist := action.NewHistory(cfg)
	hist.Max = 256
	items, err := hist.Run(name)
	if err != nil {
		return nil, fmt.Errorf("read release history: %w", err)
	}
	out := make([]*rspb.Release, 0, len(items))
	for _, it := range items {
		if rel, err := asRelease(it); err == nil {
			out = append(out, rel)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (h *helmClient) HelmRelease(_ context.Context, namespace, name string, revision int) (*rspb.Release, error) {
	cfg, err := h.config(namespace)
	if err != nil {
		return nil, err
	}
	get := action.NewGet(cfg)
	get.Version = revision
	r, err := get.Run(name)
	if err != nil {
		return nil, fmt.Errorf("read release %s: %w", name, err)
	}
	return asRelease(r)
}

// checkRevision fails when the release's latest revision isn't the reviewed one.
func (h *helmClient) checkRevision(ctx context.Context, namespace, name string, want int) error {
	cur, err := h.HelmRelease(ctx, namespace, name, 0)
	if err != nil {
		return err
	}
	if cur.Version != want {
		return ErrReleaseChanged
	}
	return nil
}

func (h *helmClient) HelmUpgrade(ctx context.Context, req HelmUpgrade, dryRun bool) (*rspb.Release, error) {
	cur, err := h.HelmRelease(ctx, req.Namespace, req.Name, 0)
	if err != nil {
		return nil, err
	}
	if cur.Version != req.FromRevision {
		return nil, ErrReleaseChanged
	}
	ch := cur.Chart
	if req.Chart != nil {
		ch = req.Chart
	}
	if ch == nil {
		return nil, errors.New("the release has no stored chart")
	}
	cfg, err := h.config(req.Namespace)
	if err != nil {
		return nil, err
	}
	up := action.NewUpgrade(cfg)
	up.Namespace = req.Namespace
	up.ResetValues = true
	up.Timeout = helmTimeout
	up.WaitStrategy = helmkube.HookOnlyStrategy
	up.Description = "Upgraded by Kupi"
	if dryRun {
		up.DryRunStrategy = action.DryRunServer
	}
	values := req.Values
	if values == nil {
		values = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(ctx, helmTimeout)
	defer cancel()
	r, err := up.RunWithContext(ctx, req.Name, ch, values)
	if err != nil {
		return nil, fmt.Errorf("helm upgrade: %w", err)
	}
	return asRelease(r)
}

func (h *helmClient) HelmInstall(ctx context.Context, req HelmInstall, dryRun bool) (*rspb.Release, error) {
	if req.Chart == nil {
		return nil, errors.New("no chart to install")
	}
	cfg, err := h.config(req.Namespace)
	if err != nil {
		return nil, err
	}
	in := action.NewInstall(cfg)
	in.ReleaseName = req.Name
	in.Namespace = req.Namespace
	in.CreateNamespace = req.CreateNamespace
	in.Timeout = helmTimeout
	in.WaitStrategy = helmkube.HookOnlyStrategy
	in.Description = "Installed by Kupi"
	if dryRun {
		in.DryRunStrategy = action.DryRunServer
	}
	values := req.Values
	if values == nil {
		values = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(ctx, helmTimeout)
	defer cancel()
	r, err := in.RunWithContext(ctx, req.Chart, values)
	if err != nil {
		return nil, fmt.Errorf("helm install: %w", err)
	}
	return asRelease(r)
}

func (h *helmClient) HelmRollback(ctx context.Context, namespace, name string, revision, fromRevision int) error {
	if err := h.checkRevision(ctx, namespace, name, fromRevision); err != nil {
		return err
	}
	cfg, err := h.config(namespace)
	if err != nil {
		return err
	}
	rb := action.NewRollback(cfg)
	rb.Version = revision
	rb.Timeout = helmTimeout
	rb.WaitStrategy = helmkube.HookOnlyStrategy
	rb.Description = fmt.Sprintf("Rolled back to %d by Kupi", revision)
	if err := rb.Run(name); err != nil {
		return fmt.Errorf("helm rollback: %w", err)
	}
	return nil
}

func (h *helmClient) HelmUninstall(ctx context.Context, namespace, name string, fromRevision int) error {
	if err := h.checkRevision(ctx, namespace, name, fromRevision); err != nil {
		return err
	}
	cfg, err := h.config(namespace)
	if err != nil {
		return err
	}
	un := action.NewUninstall(cfg)
	un.Timeout = helmTimeout
	un.WaitStrategy = helmkube.HookOnlyStrategy
	un.Description = "Uninstalled by Kupi"
	if _, err := un.Run(name); err != nil {
		return fmt.Errorf("helm uninstall: %w", err)
	}
	return nil
}
