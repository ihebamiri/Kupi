package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

// HostInfo is the engine host's capacity. With Docker Desktop this is the
// Linux VM's share of the machine.
type HostInfo struct {
	MemTotal int64 `json:"memTotal"`
	NCPU     int   `json:"ncpu"`
}

// Info returns the engine host's memory and CPU count.
func (c *Client) Info(ctx context.Context) (HostInfo, error) {
	var raw struct {
		MemTotal int64
		NCPU     int
	}
	if err := c.getJSON(ctx, "/info", nil, &raw); err != nil {
		return HostInfo{}, err
	}
	return HostInfo{MemTotal: raw.MemTotal, NCPU: raw.NCPU}, nil
}

// Container is one container from the engine's list.
type Container struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
}

var labelRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+=[A-Za-z0-9._-]+$`)

// Containers lists containers (running or not) carrying a key=value label.
func (c *Client) Containers(ctx context.Context, label string) ([]Container, error) {
	if !labelRe.MatchString(label) {
		return nil, fmt.Errorf("invalid label filter %q", label)
	}
	filters, _ := json.Marshal(map[string][]string{"label": {label}})
	var raw []struct {
		ID     string `json:"Id"`
		Names  []string
		State  string
		Labels map[string]string
	}
	if err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		name := ""
		if len(r.Names) > 0 {
			name = r.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}
		out = append(out, Container{ID: r.ID, Name: name, State: r.State, Labels: r.Labels})
	}
	return out, nil
}

var idRe = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

// Stats is a container's current resource use and limits.
type Stats struct {
	MemUsage int64 `json:"memUsage"`
	MemLimit int64 `json:"memLimit"`
	// CPUCores is CPU use in cores (1.5 = one and a half cores busy).
	CPUCores float64 `json:"cpuCores"`
}

// Stats samples a running container's use. It takes about a second, as the
// engine measures CPU over an interval.
func (c *Client) Stats(ctx context.Context, id string) (Stats, error) {
	if !idRe.MatchString(id) {
		return Stats{}, fmt.Errorf("invalid container id")
	}
	var raw struct {
		CPUStats    cpuStats `json:"cpu_stats"`
		PreCPUStats cpuStats `json:"precpu_stats"`
		MemoryStats struct {
			Usage int64            `json:"usage"`
			Limit int64            `json:"limit"`
			Stats map[string]int64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/stats", url.Values{"stream": {"false"}}, &raw); err != nil {
		return Stats{}, err
	}
	s := Stats{MemUsage: raw.MemoryStats.Usage, MemLimit: raw.MemoryStats.Limit}
	// Page cache the kernel can drop isn't really used (same as docker stats).
	if v, ok := raw.MemoryStats.Stats["inactive_file"]; ok && v < s.MemUsage {
		s.MemUsage -= v
	} else if v, ok := raw.MemoryStats.Stats["total_inactive_file"]; ok && v < s.MemUsage {
		s.MemUsage -= v
	}
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
	online := float64(raw.CPUStats.OnlineCPUs)
	if online == 0 {
		online = float64(len(raw.CPUStats.CPUUsage.PerCPU))
	}
	if cpuDelta > 0 && sysDelta > 0 {
		s.CPUCores = cpuDelta / sysDelta * online
	}
	return s, nil
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64   `json:"total_usage"`
		PerCPU     []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs  int    `json:"online_cpus"`
}

// Limits are a container's memory (bytes) and CPU (billionths of a core)
// caps; 0 means none.
type Limits struct {
	Memory   int64 `json:"memory"`
	NanoCPUs int64 `json:"nanoCpus"`
}

// ContainerLimits reads a container's current caps.
func (c *Client) ContainerLimits(ctx context.Context, id string) (Limits, error) {
	if !idRe.MatchString(id) {
		return Limits{}, fmt.Errorf("invalid container id")
	}
	var raw struct {
		HostConfig struct {
			Memory   int64
			NanoCpus int64
		}
	}
	if err := c.getJSON(ctx, "/containers/"+id+"/json", nil, &raw); err != nil {
		return Limits{}, err
	}
	return Limits{Memory: raw.HostConfig.Memory, NanoCPUs: raw.HostConfig.NanoCpus}, nil
}

// SetLimits changes a container's caps while it runs. Swap is kept equal to
// the memory cap, so a capped cluster doesn't thrash the disk instead.
func (c *Client) SetLimits(ctx context.Context, id string, l Limits) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("invalid container id")
	}
	body := map[string]int64{"NanoCpus": l.NanoCPUs}
	if l.Memory > 0 {
		body["Memory"] = l.Memory
		body["MemorySwap"] = l.Memory
	}
	data, _ := json.Marshal(body)
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/update", nil, bytes.NewReader(data), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
