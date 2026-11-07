// Package prom reads pod history from a Prometheus running in the cluster,
// through the API server's service proxy. Kupi only runs its own fixed
// queries (CPU and memory of one pod); names are validated before they go
// into a query, so nothing the UI sends becomes PromQL.
package prom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/ihebamiri/kupi/internal/kube"
	"github.com/ihebamiri/kupi/internal/metrics"
	"github.com/ihebamiri/kupi/internal/validate"
)

var servicesGVR = schema.GroupVersionResource{Version: "v1", Resource: "services"}

// ErrNotFound means no Prometheus was found in the cluster.
var ErrNotFound = errors.New("no Prometheus found in the cluster")

// Endpoint is a Prometheus Service.
type Endpoint struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      string `json:"port"`
}

func (e Endpoint) String() string { return e.Namespace + "/" + e.Service + ":" + e.Port }

// Find looks for a Prometheus Service: the usual names and labels of the
// Prometheus Operator, kube-prometheus-stack and the prometheus chart.
func Find(ctx context.Context, r kube.Reader) (*Endpoint, error) {
	l, err := r.List(ctx, servicesGVR, "", metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Services: %w", err)
	}
	var best *Endpoint
	bestScore := 0
	for i := range l.Items {
		svc := &l.Items[i]
		score := scoreService(svc)
		if score == 0 {
			continue
		}
		port := pickPort(svc)
		if port == "" {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = &Endpoint{Namespace: svc.GetNamespace(), Service: svc.GetName(), Port: port}
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}

func scoreService(svc *unstructured.Unstructured) int {
	name, lbl := svc.GetName(), svc.GetLabels()
	switch {
	case name == "prometheus-operated":
		return 5
	case lbl["app.kubernetes.io/name"] == "prometheus" && lbl["app.kubernetes.io/component"] != "alertmanager":
		return 4
	case name == "prometheus-server" || name == "prometheus":
		return 3
	case strings.HasSuffix(name, "-prometheus") && !strings.Contains(name, "operator") && !strings.Contains(name, "node-exporter"):
		return 2
	}
	return 0
}

func pickPort(svc *unstructured.Unstructured) string {
	ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	fallback := ""
	for _, p := range ports {
		m, _ := p.(map[string]any)
		n, _ := m["name"].(string)
		num, _, _ := unstructured.NestedInt64(m, "port")
		if n == "web" || n == "http" || n == "http-web" || num == 9090 {
			return strconv.FormatInt(num, 10)
		}
		if fallback == "" && num == 80 {
			fallback = "80"
		}
	}
	return fallback
}

// PodHistory returns a pod's CPU (millicores) and memory (bytes) over the
// last window, in about 120 points.
func PodHistory(ctx context.Context, r kube.Reader, ep *Endpoint, namespace, pod string, window time.Duration) ([]metrics.Sample, error) {
	if err := validate.Namespace(namespace); err != nil {
		return nil, err
	}
	// Pod names are DNS subdomains: no quotes or braces can reach the query.
	if msgs := validation.IsDNS1123Subdomain(pod); len(msgs) > 0 {
		return nil, fmt.Errorf("invalid pod name %q", pod)
	}
	if window < time.Hour || window > 31*24*time.Hour {
		return nil, errors.New("choose between 1 hour and 31 days")
	}
	sel := fmt.Sprintf(`namespace=%q,pod=%q,container!="",container!="POD"`, namespace, pod)
	cpuQ := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m])) * 1000`, sel)
	memQ := fmt.Sprintf(`sum(container_memory_working_set_bytes{%s})`, sel)
	end := time.Now()
	start := end.Add(-window)
	step := window / 120
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	cpu, err := queryRange(ctx, r, ep, cpuQ, start, end, step)
	if err != nil {
		return nil, err
	}
	mem, err := queryRange(ctx, r, ep, memQ, start, end, step)
	if err != nil {
		return nil, err
	}
	byTime := map[int64]*metrics.Sample{}
	for t, v := range cpu {
		byTime[t] = &metrics.Sample{T: t, CPU: int64(v)}
	}
	for t, v := range mem {
		s, ok := byTime[t]
		if !ok {
			s = &metrics.Sample{T: t}
			byTime[t] = s
		}
		s.Mem = int64(v)
	}
	out := make([]metrics.Sample, 0, len(byTime))
	for _, s := range byTime {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

func queryRange(ctx context.Context, r kube.Reader, ep *Endpoint, q string, start, end time.Time, step time.Duration) (map[int64]float64, error) {
	params := map[string]string{
		"query": q,
		"start": strconv.FormatInt(start.Unix(), 10),
		"end":   strconv.FormatInt(end.Unix(), 10),
		"step":  strconv.Itoa(int(step.Seconds())),
	}
	data, err := r.ServiceProxyGet(ctx, ep.Namespace, ep.Service, ep.Port, "api/v1/query_range", params)
	if err != nil {
		return nil, fmt.Errorf("ask Prometheus (%s): %w", ep, err)
	}
	return parseMatrix(data)
}

func parseMatrix(data []byte) (map[int64]float64, error) {
	var res struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Values [][2]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("read Prometheus' answer: %w", err)
	}
	if res.Status != "success" {
		return nil, fmt.Errorf("Prometheus: %s", res.Error)
	}
	out := map[int64]float64{}
	for _, series := range res.Data.Result {
		for _, pair := range series.Values {
			ts, ok := pair[0].(float64)
			s, ok2 := pair[1].(string)
			if !ok || !ok2 {
				continue
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				continue
			}
			out[int64(ts)] += v
		}
	}
	return out, nil
}
