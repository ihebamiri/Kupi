// Package metrics samples CPU and memory from the cluster's metrics API
// (metrics-server) and keeps about an hour of history in memory. A sampler
// runs only while the UI asks for metrics and stops itself when idle, so an
// open but unused Kupi costs the API server nothing.
package metrics

import (
	"context"
	"sort"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
)

const (
	// Interval between samples; metrics-server refreshes about every 15 s.
	Interval = 10 * time.Second
	// Keep is the number of samples per series: one hour.
	Keep     = 360
	idleStop = 2 * time.Minute
	staleAge = 15 * time.Minute
)

var (
	nodeMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
	podMetricsGVR  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
	nodesGVR       = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
)

// Sample is one reading: CPU in millicores, memory in bytes.
type Sample struct {
	T   int64 `json:"t"`
	CPU int64 `json:"cpu"`
	Mem int64 `json:"mem"`
}

// NodeUsage is a node's current usage and allocatable capacity.
type NodeUsage struct {
	Name        string `json:"name"`
	CPU         int64  `json:"cpu"`
	Mem         int64  `json:"mem"`
	CPUCapacity int64  `json:"cpuCapacity"`
	MemCapacity int64  `json:"memCapacity"`
}

// PodUsage is a pod's current usage, summed over its containers.
type PodUsage struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CPU       int64  `json:"cpu"`
	Mem       int64  `json:"mem"`
}

// Status says whether metrics are available and why not.
type Status struct {
	Available  bool   `json:"available"`
	Reason     string `json:"reason"`
	LastSample string `json:"lastSample"`
}

type ring struct {
	buf      []Sample
	next     int
	full     bool
	lastSeen time.Time
}

func (r *ring) add(s Sample) {
	if r.buf == nil {
		r.buf = make([]Sample, Keep)
	}
	r.buf[r.next] = s
	r.next = (r.next + 1) % Keep
	if r.next == 0 {
		r.full = true
	}
}

func (r *ring) samples() []Sample {
	if !r.full {
		return append([]Sample(nil), r.buf[:r.next]...)
	}
	out := make([]Sample, 0, Keep)
	out = append(out, r.buf[r.next:]...)
	return append(out, r.buf[:r.next]...)
}

type sampler struct {
	reader kube.Reader
	now    func() time.Time

	mu      sync.Mutex
	series  map[string]*ring
	nodes   []NodeUsage
	pods    []PodUsage
	status  Status
	lastUse time.Time
}

// Manager keeps one sampler per connected context.
type Manager struct {
	now func() time.Time

	mu       sync.Mutex
	samplers map[string]*sampler
	cancels  map[string]context.CancelFunc
}

// New returns a manager.
func New() *Manager {
	return &Manager{now: time.Now, samplers: map[string]*sampler{}, cancels: map[string]context.CancelFunc{}}
}

// touch returns the context's sampler, starting it if needed.
func (m *Manager) touch(contextName string, r kube.Reader) *sampler {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.samplers[contextName]
	if !ok {
		s = &sampler{reader: r, now: m.now, series: map[string]*ring{}, status: Status{Reason: "Collecting the first sample…"}}
		ctx, cancel := context.WithCancel(context.Background())
		m.samplers[contextName] = s
		m.cancels[contextName] = cancel
		go m.loop(ctx, contextName, s)
	}
	s.mu.Lock()
	s.lastUse = m.now()
	s.mu.Unlock()
	return s
}

// Stop ends sampling for a context and drops its history.
func (m *Manager) Stop(contextName string) {
	m.mu.Lock()
	cancel, ok := m.cancels[contextName]
	delete(m.cancels, contextName)
	delete(m.samplers, contextName)
	m.mu.Unlock()
	if ok {
		cancel()
	}
}

// StopAll ends every sampler.
func (m *Manager) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.cancels))
	for n := range m.cancels {
		names = append(names, n)
	}
	m.mu.Unlock()
	for _, n := range names {
		m.Stop(n)
	}
}

func (m *Manager) loop(ctx context.Context, contextName string, s *sampler) {
	s.sample(ctx)
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			idle := m.now().Sub(s.lastUse) > idleStop
			s.mu.Unlock()
			if idle {
				m.Stop(contextName)
				return
			}
			s.sample(ctx)
		}
	}
}

func (s *sampler) sample(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	nodeMetrics, err := s.reader.List(ctx, nodeMetricsGVR, "", metav1.ListOptions{})
	if err != nil {
		reason := err.Error()
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			reason = "metrics-server isn't installed on this cluster, so there's no CPU or memory data."
		} else if apierrors.IsServiceUnavailable(err) {
			reason = "metrics-server is installed but not answering yet."
		}
		s.mu.Lock()
		s.status = Status{Available: false, Reason: reason}
		s.mu.Unlock()
		return
	}
	podMetrics, err := s.reader.List(ctx, podMetricsGVR, "", metav1.ListOptions{})
	if err != nil {
		podMetrics = &unstructured.UnstructuredList{}
	}
	capacity := map[string][2]int64{}
	if nodes, err := s.reader.List(ctx, nodesGVR, "", metav1.ListOptions{}); err == nil {
		for _, n := range nodes.Items {
			alloc, _, _ := unstructured.NestedStringMap(n.Object, "status", "allocatable")
			capacity[n.GetName()] = [2]int64{milli(alloc["cpu"]), bytes(alloc["memory"])}
		}
	}

	now := s.now()
	ts := now.Unix()
	var nodes []NodeUsage
	var pods []PodUsage
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range nodeMetrics.Items {
		cpu, mem := usage(n.Object)
		c := capacity[n.GetName()]
		nodes = append(nodes, NodeUsage{Name: n.GetName(), CPU: cpu, Mem: mem, CPUCapacity: c[0], MemCapacity: c[1]})
		s.record("node//"+n.GetName(), Sample{T: ts, CPU: cpu, Mem: mem}, now)
	}
	for _, p := range podMetrics.Items {
		var cpu, mem int64
		containers, _, _ := unstructured.NestedSlice(p.Object, "containers")
		for _, c := range containers {
			if cm, ok := c.(map[string]interface{}); ok {
				cc, mm := usage(cm)
				cpu += cc
				mem += mm
			}
		}
		pods = append(pods, PodUsage{Namespace: p.GetNamespace(), Name: p.GetName(), CPU: cpu, Mem: mem})
		s.record("pod/"+p.GetNamespace()+"/"+p.GetName(), Sample{T: ts, CPU: cpu, Mem: mem}, now)
	}
	for k, r := range s.series {
		if now.Sub(r.lastSeen) > staleAge {
			delete(s.series, k)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	s.nodes, s.pods = nodes, pods
	s.status = Status{Available: true, LastSample: now.UTC().Format(time.RFC3339)}
}

func (s *sampler) record(key string, sample Sample, now time.Time) {
	r, ok := s.series[key]
	if !ok {
		r = &ring{}
		s.series[key] = r
	}
	r.add(sample)
	r.lastSeen = now
}

func usage(obj map[string]interface{}) (cpu, mem int64) {
	u, _, _ := unstructured.NestedStringMap(obj, "usage")
	return milli(u["cpu"]), bytes(u["memory"])
}

func milli(q string) int64 {
	if q == "" {
		return 0
	}
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return 0
	}
	return v.MilliValue()
}

func bytes(q string) int64 {
	if q == "" {
		return 0
	}
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return 0
	}
	return v.Value()
}

// Overview is the cluster-wide view.
type Overview struct {
	Status Status      `json:"status"`
	Nodes  []NodeUsage `json:"nodes"`
	TopCPU []PodUsage  `json:"topCpu"`
	TopMem []PodUsage  `json:"topMem"`
}

// Overview returns nodes and the busiest pods, optionally in one namespace.
func (m *Manager) Overview(contextName string, r kube.Reader, namespace string, limit int) Overview {
	s := m.touch(contextName, r)
	s.mu.Lock()
	defer s.mu.Unlock()
	var pods []PodUsage
	for _, p := range s.pods {
		if namespace == "" || p.Namespace == namespace {
			pods = append(pods, p)
		}
	}
	top := func(less func(a, b PodUsage) bool) []PodUsage {
		cp := append([]PodUsage(nil), pods...)
		sort.Slice(cp, func(i, j int) bool { return less(cp[i], cp[j]) })
		if len(cp) > limit {
			cp = cp[:limit]
		}
		return cp
	}
	return Overview{
		Status: s.status,
		Nodes:  append([]NodeUsage(nil), s.nodes...),
		TopCPU: top(func(a, b PodUsage) bool { return a.CPU > b.CPU }),
		TopMem: top(func(a, b PodUsage) bool { return a.Mem > b.Mem }),
	}
}

// PodSeries returns a pod's history.
func (m *Manager) PodSeries(contextName string, r kube.Reader, namespace, pod string) (Status, []Sample) {
	return m.series(contextName, r, "pod/"+namespace+"/"+pod)
}

// NodeSeries returns a node's history.
func (m *Manager) NodeSeries(contextName string, r kube.Reader, node string) (Status, []Sample) {
	return m.series(contextName, r, "node//"+node)
}

func (m *Manager) series(contextName string, r kube.Reader, key string) (Status, []Sample) {
	s := m.touch(contextName, r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ring, ok := s.series[key]; ok {
		return s.status, ring.samples()
	}
	return s.status, []Sample{}
}
