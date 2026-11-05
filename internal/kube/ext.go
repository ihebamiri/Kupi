package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	tablePageSize = 500
	// DefaultTableLimit caps how many rows one list returns.
	DefaultTableLimit = 5000
)

// TableOptions filters and caps a table list.
type TableOptions struct {
	LabelSelector string
	FieldSelector string
	// Limit caps the rows returned; Kupi pages through the list up to it.
	Limit int64
}

// ResourceType is one listable resource the API server offers.
type ResourceType struct {
	Group      string
	Version    string
	Resource   string
	Kind       string
	Namespaced bool
	Verbs      []string
	ShortNames []string
	Categories []string
}

// GVR returns the type's group, version and resource.
func (t ResourceType) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: t.Group, Version: t.Version, Resource: t.Resource}
}

// LogOptions selects which part of a container's log to stream.
type LogOptions struct {
	Container    string
	Follow       bool
	Previous     bool
	Timestamps   bool
	TailLines    int64
	SinceSeconds int64
	LimitBytes   int64
}

// TerminalSize is a terminal's size in characters.
type TerminalSize struct {
	Width  uint16
	Height uint16
}

// ExecStreams attaches a command in a container to local streams.
type ExecStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	// Stderr is merged into Stdout when TTY is set.
	Stderr io.Writer
	TTY    bool
	Resize <-chan TerminalSize
}

func (r *reader) Table(ctx context.Context, gvr schema.GroupVersionResource, namespaced bool, namespace string, opts TableOptions) (*metav1.Table, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultTableLimit
	}
	path := resourcePath(gvr, namespaced, namespace)
	var out *metav1.Table
	cont := ""
	for {
		got := int64(0)
		if out != nil {
			got = int64(len(out.Rows))
		}
		req := r.s.cs.Discovery().RESTClient().Get().AbsPath(path).
			SetHeader("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io,application/json").
			Param("includeObject", "Metadata").
			Param("limit", strconv.FormatInt(min(tablePageSize, limit-got), 10))
		if cont != "" {
			req = req.Param("continue", cont)
		}
		if opts.LabelSelector != "" {
			req = req.Param("labelSelector", opts.LabelSelector)
		}
		if opts.FieldSelector != "" {
			req = req.Param("fieldSelector", opts.FieldSelector)
		}
		body, err := req.Do(ctx).Raw()
		if err != nil {
			return nil, err
		}
		var page metav1.Table
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("parse table: %w", err)
		}
		if page.Kind != "Table" {
			return nil, errors.New("the API server did not return a table for this resource")
		}
		if out == nil {
			out = &page
		} else {
			out.Rows = append(out.Rows, page.Rows...)
		}
		out.Continue = page.Continue
		out.ResourceVersion = page.ResourceVersion
		if page.Continue == "" || int64(len(out.Rows)) >= limit {
			return out, nil
		}
		cont = page.Continue
	}
}

// resourcePath builds the REST path for a list. Callers validate the parts.
func resourcePath(gvr schema.GroupVersionResource, namespaced bool, namespace string) string {
	p := "/apis/" + gvr.Group + "/" + gvr.Version
	if gvr.Group == "" {
		p = "/api/" + gvr.Version
	}
	if namespaced && namespace != "" {
		p += "/namespaces/" + namespace
	}
	return p + "/" + gvr.Resource
}

func (r *reader) ResourceTypes(ctx context.Context) ([]ResourceType, error) {
	type result struct {
		lists []*metav1.APIResourceList
		err   error
	}
	done := make(chan result, 1)
	go func() {
		lists, err := r.s.cs.Discovery().ServerPreferredResources()
		done <- result{lists, err}
	}()
	var res result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res = <-done:
	}
	// A broken aggregated API (for example metrics-server down) fails only
	// its own group; keep everything else.
	if res.err != nil && !discovery.IsGroupDiscoveryFailedError(res.err) {
		return nil, fmt.Errorf("discover resource types: %w", res.err)
	}
	var out []ResourceType
	for _, list := range res.lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, ar := range list.APIResources {
			if containsSlash(ar.Name) || !hasVerb(ar.Verbs, "list") {
				continue
			}
			out = append(out, ResourceType{
				Group: gv.Group, Version: gv.Version, Resource: ar.Name, Kind: ar.Kind,
				Namespaced: ar.Namespaced, Verbs: ar.Verbs, ShortNames: ar.ShortNames, Categories: ar.Categories,
			})
		}
	}
	return out, nil
}

func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

func hasVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

func (r *reader) Logs(ctx context.Context, namespace, pod string, o LogOptions) (io.ReadCloser, error) {
	opts := &corev1.PodLogOptions{Container: o.Container, Follow: o.Follow, Previous: o.Previous, Timestamps: o.Timestamps}
	if o.TailLines > 0 {
		opts.TailLines = &o.TailLines
	}
	if o.SinceSeconds > 0 {
		opts.SinceSeconds = &o.SinceSeconds
	}
	if o.LimitBytes > 0 {
		opts.LimitBytes = &o.LimitBytes
	}
	return r.s.cs.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
}

func (r *reader) WatchMetadata(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	if namespace == "" {
		return r.s.meta.Resource(gvr).Watch(ctx, opts)
	}
	return r.s.meta.Resource(gvr).Namespace(namespace).Watch(ctx, opts)
}

func (m *mutator) Exec(ctx context.Context, namespace, pod, container string, command []string, st ExecStreams) error {
	if m.s.cfg == nil {
		return errors.New("exec is not available in this session")
	}
	req := m.s.cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     st.Stdin != nil,
			Stdout:    st.Stdout != nil,
			Stderr:    st.Stderr != nil && !st.TTY,
			TTY:       st.TTY,
		}, scheme.ParameterCodec)
	ws, err := remotecommand.NewWebSocketExecutor(m.s.cfg, "GET", req.URL().String())
	if err != nil {
		return fmt.Errorf("create websocket executor: %w", err)
	}
	spdy, err := remotecommand.NewSPDYExecutor(m.s.cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create spdy executor: %w", err)
	}
	executor, err := remotecommand.NewFallbackExecutor(ws, spdy, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})
	if err != nil {
		return fmt.Errorf("create executor: %w", err)
	}
	opts := remotecommand.StreamOptions{Stdin: st.Stdin, Stdout: st.Stdout, Tty: st.TTY}
	if !st.TTY {
		opts.Stderr = st.Stderr
	}
	if st.Resize != nil {
		opts.TerminalSizeQueue = sizeQueue{ctx: ctx, ch: st.Resize}
	}
	return executor.StreamWithContext(ctx, opts)
}

type sizeQueue struct {
	ctx context.Context
	ch  <-chan TerminalSize
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case <-q.ctx.Done():
		return nil
	case s, ok := <-q.ch:
		if !ok {
			return nil
		}
		return &remotecommand.TerminalSize{Width: s.Width, Height: s.Height}
	}
}
