// Package kube manages connections to Kubernetes clusters.
//
// A Session exposes two capabilities:
//   - Reader: get, list, watch, resolve types and check permissions. Any
//     package may use it.
//   - Mutator: apply, patch and delete. Only internal/guard may call
//     Session.Mutator(); internal/archtest fails the build otherwise. This
//     makes the Guard the single checkpoint for every cluster change.
//
// This package is the only one allowed to import client-go's dynamic,
// kubernetes, rest, remotecommand and portforward packages.
package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	apiversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	"github.com/ihebamiri/kupi/internal/kubeconfig"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/trust"
	"github.com/ihebamiri/kupi/internal/version"
)

// FieldManager is the server-side apply field manager for every Kupi change.
const FieldManager = "kupi"

// ErrNotTrusted is returned when a context uses a credential plugin from a
// kubeconfig file the user has not trusted yet.
var ErrNotTrusted = errors.New("kubeconfig credential plugin is not trusted")

// NotTrustedError says which file and command need the user's approval.
type NotTrustedError struct {
	Context string
	File    string
	Command string
}

func (e *NotTrustedError) Error() string {
	return fmt.Sprintf("context %q uses a credential plugin (%s) from %s, which you haven't trusted yet", e.Context, e.Command, e.File)
}

func (e *NotTrustedError) Unwrap() error { return ErrNotTrusted }

// Reader is the read-only view of a cluster.
type Reader interface {
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
	Watch(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (watch.Interface, error)
	// Resolve maps a kind to its resource and reports whether it is namespaced.
	Resolve(gvk schema.GroupVersionKind) (gvr schema.GroupVersionResource, namespaced bool, err error)
	// CanI asks the API server whether the current user may perform verb.
	// subresource is "" for the object itself, or e.g. "exec" or "log".
	CanI(ctx context.Context, verb string, gvr schema.GroupVersionResource, subresource, namespace, name string) (allowed bool, reason string, err error)
	// Table lists objects in the server's own table format: the same columns
	// kubectl get shows, for every type including custom resources.
	Table(ctx context.Context, gvr schema.GroupVersionResource, namespaced bool, namespace string, opts TableOptions) (*metav1.Table, error)
	// ResourceTypes lists every listable resource type the server offers.
	ResourceTypes(ctx context.Context) ([]ResourceType, error)
	// Logs streams a container's log.
	Logs(ctx context.Context, namespace, pod string, opts LogOptions) (io.ReadCloser, error)
	// WatchMetadata watches object metadata only. It never transfers object
	// contents, so Secret values stay on the server.
	WatchMetadata(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (watch.Interface, error)
	// OpenAPI returns the server's OpenAPI v3 document for one group/version.
	OpenAPI(ctx context.Context, gv schema.GroupVersion) ([]byte, error)
	// ServiceProxyGet reads a path from a Service through the API server's
	// service proxy (needs the services/proxy permission). Used for
	// Prometheus queries, so no port-forward is needed.
	ServiceProxyGet(ctx context.Context, namespace, service, port, path string, params map[string]string) ([]byte, error)
}

// ApplyOptions controls a server-side apply.
type ApplyOptions struct {
	DryRun bool
	// Force takes ownership of fields managed by other tools. Only set when
	// the user explicitly chose to take over those fields.
	Force bool
}

// Mutator changes a cluster. Reserved for internal/guard.
type Mutator interface {
	Apply(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, opts ApplyOptions) (*unstructured.Unstructured, error)
	MergePatch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patch []byte, dryRun bool) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name, resourceVersion string, dryRun bool) error
	// Exec runs a command in a container, attached to streams, until it exits
	// or ctx is cancelled.
	Exec(ctx context.Context, namespace, pod, container string, command []string, streams ExecStreams) error
}

// Clients are the API clients a session uses.
type Clients struct {
	Config   *rest.Config
	Dynamic  dynamic.Interface
	Typed    kubernetes.Interface
	Metadata metadata.Interface
	Mapper   meta.ResettableRESTMapper
}

// Session is a live connection to one kubeconfig context.
type Session struct {
	name             string
	profile          profile.Kind
	user             string
	defaultNamespace string
	serverVersion    string

	cfg    *rest.Config
	dyn    dynamic.Interface
	cs     kubernetes.Interface
	meta   metadata.Interface
	mapper meta.ResettableRESTMapper

	openapiMu    sync.Mutex
	openapiCache map[string]cachedDoc
}

type cachedDoc struct {
	data    []byte
	fetched time.Time
}

// NewSession wires a session from already-built clients. Manager.Connect is
// the normal way to get one; tests use this directly.
func NewSession(name string, kind profile.Kind, user, defaultNamespace, serverVersion string, c Clients) *Session {
	if defaultNamespace == "" {
		defaultNamespace = metav1.NamespaceDefault
	}
	return &Session{
		name: name, profile: kind, user: user, defaultNamespace: defaultNamespace, serverVersion: serverVersion,
		cfg: c.Config, dyn: c.Dynamic, cs: c.Typed, meta: c.Metadata, mapper: c.Mapper,
	}
}

func (s *Session) ContextName() string      { return s.name }
func (s *Session) Profile() profile.Kind    { return s.profile }
func (s *Session) UserName() string         { return s.user }
func (s *Session) DefaultNamespace() string { return s.defaultNamespace }
func (s *Session) ServerVersion() string    { return s.serverVersion }

// Reader returns the read-only view of the cluster.
func (s *Session) Reader() Reader { return &reader{s: s} }

// Mutator returns the write capability. Only internal/guard may call this;
// internal/archtest enforces it.
func (s *Session) Mutator() Mutator { return &mutator{s: s} }

func (s *Session) resource(gvr schema.GroupVersionResource, namespace string) dynamic.ResourceInterface {
	if namespace == "" {
		return s.dyn.Resource(gvr)
	}
	return s.dyn.Resource(gvr).Namespace(namespace)
}

type reader struct{ s *Session }

func (r *reader) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	return r.s.resource(gvr, namespace).Get(ctx, name, metav1.GetOptions{})
}

func (r *reader) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return r.s.resource(gvr, namespace).List(ctx, opts)
}

func (r *reader) Watch(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return r.s.resource(gvr, namespace).Watch(ctx, opts)
}

func (r *reader) Resolve(gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool, error) {
	mapping, err := r.s.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		// The type may be new (a CRD installed since we connected): refresh once.
		r.s.mapper.Reset()
		mapping, err = r.s.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return schema.GroupVersionResource{}, false, fmt.Errorf("unknown resource type %s: %w", gvk.String(), err)
		}
	}
	return mapping.Resource, mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
}

func (r *reader) CanI(ctx context.Context, verb string, gvr schema.GroupVersionResource, subresource, namespace, name string) (bool, string, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace:   namespace,
				Verb:        verb,
				Group:       gvr.Group,
				Version:     gvr.Version,
				Resource:    gvr.Resource,
				Subresource: subresource,
				Name:        name,
			},
		},
	}
	res, err := r.s.cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, "", fmt.Errorf("check permissions: %w", err)
	}
	return res.Status.Allowed, res.Status.Reason, nil
}

type mutator struct{ s *Session }

func dryRun(on bool) []string {
	if on {
		return []string{metav1.DryRunAll}
	}
	return nil
}

func (m *mutator) Apply(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, opts ApplyOptions) (*unstructured.Unstructured, error) {
	return m.s.resource(gvr, namespace).Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: FieldManager,
		Force:        opts.Force,
		DryRun:       dryRun(opts.DryRun),
	})
}

func (m *mutator) MergePatch(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, patch []byte, dry bool) (*unstructured.Unstructured, error) {
	return m.s.resource(gvr, namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{
		FieldManager: FieldManager,
		DryRun:       dryRun(dry),
	})
}

func (m *mutator) Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name, resourceVersion string, dry bool) error {
	policy := metav1.DeletePropagationBackground
	opts := metav1.DeleteOptions{DryRun: dryRun(dry), PropagationPolicy: &policy}
	if resourceVersion != "" {
		opts.Preconditions = &metav1.Preconditions{ResourceVersion: &resourceVersion}
	}
	return m.s.resource(gvr, namespace).Delete(ctx, name, opts)
}

// Manager connects to contexts and keeps the live sessions.
type Manager struct {
	loader   *kubeconfig.Loader
	trust    *trust.Store
	profiles *profile.Store

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns a manager reading contexts from loader.
func NewManager(loader *kubeconfig.Loader, trustStore *trust.Store, profiles *profile.Store) *Manager {
	return &Manager{loader: loader, trust: trustStore, profiles: profiles, sessions: map[string]*Session{}}
}

// Loader returns the kubeconfig loader.
func (m *Manager) Loader() *kubeconfig.Loader { return m.loader }

// Session returns the live session for a context, if connected.
func (m *Manager) Session(contextName string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[contextName]
	return s, ok
}

// Disconnect drops the session for a context.
func (m *Manager) Disconnect(contextName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, contextName)
}

// Connect opens a session to a context. Contexts that use a credential plugin
// need their kubeconfig file trusted first (NotTrustedError).
func (m *Manager) Connect(ctx context.Context, contextName string) (*Session, error) {
	info, err := m.loader.Context(contextName)
	if err != nil {
		return nil, err
	}
	if info.Exec != nil {
		hash, err := kubeconfig.FileHash(info.UserFile)
		if err != nil {
			return nil, err
		}
		if !m.trust.IsTrusted(info.UserFile, hash) {
			return nil, &NotTrustedError{Context: contextName, File: info.UserFile, Command: info.Exec.Command}
		}
	}
	kind, _ := m.profiles.Resolve(info.Name, info.Cluster, info.Server)
	rules := profile.RulesFor(kind)

	cfg, err := m.loader.ClientConfig(contextName).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build client config: %w", err)
	}
	cfg.UserAgent = version.UserAgent()
	cfg.QPS = rules.QPS
	cfg.Burst = rules.Burst

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	// Built-in types decode faster as protobuf.
	typedCfg := rest.CopyConfig(cfg)
	typedCfg.ContentType = "application/vnd.kubernetes.protobuf"
	typedCfg.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	cs, err := kubernetes.NewForConfig(typedCfg)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	serverVersion, err := fetchVersion(ctx, cs.Discovery())
	if err != nil {
		return nil, fmt.Errorf("reach API server: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(cs.Discovery()))
	metaClient, err := metadata.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create metadata client: %w", err)
	}

	session := NewSession(contextName, kind, info.User, info.Namespace, serverVersion, Clients{
		Config: cfg, Dynamic: dyn, Typed: cs, Metadata: metaClient, Mapper: mapper,
	})
	m.mu.Lock()
	m.sessions[contextName] = session
	m.mu.Unlock()
	return session, nil
}

func fetchVersion(ctx context.Context, disc discovery.DiscoveryInterface) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	body, err := disc.RESTClient().Get().AbsPath("/version").Do(ctx).Raw()
	if err != nil {
		return "", err
	}
	var info apiversion.Info
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parse server version: %w", err)
	}
	return info.GitVersion, nil
}
