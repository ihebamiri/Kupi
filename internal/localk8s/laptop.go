package localk8s

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/ihebamiri/kupi/internal/engine"
)

// Minimum caps: below these k3s can't run reliably.
const (
	MinNodeMemoryMB = 768
	MinNodeCPUs     = 0.5
)

// NodeUsage is one k3d node container's use of the machine.
type NodeUsage struct {
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Running  bool    `json:"running"`
	MemUsage int64   `json:"memUsage"`
	CPUCores float64 `json:"cpuCores"`
	// MemCap and CPUCap are the node's caps; 0 means none.
	MemCap int64   `json:"memCap"`
	CPUCap float64 `json:"cpuCap"`
}

// Usage is how much of the machine a k3d cluster uses.
type Usage struct {
	Cluster  string      `json:"cluster"`
	Nodes    []NodeUsage `json:"nodes"`
	MemUsage int64       `json:"memUsage"`
	CPUCores float64     `json:"cpuCores"`
	HostMem  int64       `json:"hostMem"`
	HostCPUs int         `json:"hostCpus"`
}

// k3dNodes lists a cluster's server and agent containers.
func k3dNodes(ctx context.Context, eng *engine.Client, name string) ([]engine.Container, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	all, err := eng.Containers(ctx, "k3d.cluster="+name)
	if err != nil {
		return nil, err
	}
	var nodes []engine.Container
	for _, c := range all {
		if role := c.Labels["k3d.role"]; role == "server" || role == "agent" {
			nodes = append(nodes, c)
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes found for k3d cluster %s", name)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

// K3dUsage measures a k3d cluster's memory and CPU use, sampling its nodes
// in parallel.
func K3dUsage(ctx context.Context, eng *engine.Client, name string) (Usage, error) {
	nodes, err := k3dNodes(ctx, eng, name)
	if err != nil {
		return Usage{}, err
	}
	host, err := eng.Info(ctx)
	if err != nil {
		return Usage{}, err
	}
	u := Usage{Cluster: name, Nodes: make([]NodeUsage, len(nodes)), HostMem: host.MemTotal, HostCPUs: host.NCPU}
	var wg sync.WaitGroup
	for i, n := range nodes {
		u.Nodes[i] = NodeUsage{Name: n.Name, Role: n.Labels["k3d.role"], Running: n.State == "running"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l, err := eng.ContainerLimits(ctx, n.ID); err == nil {
				u.Nodes[i].MemCap, u.Nodes[i].CPUCap = l.Memory, float64(l.NanoCPUs)/1e9
			}
			if !u.Nodes[i].Running {
				return
			}
			if s, err := eng.Stats(ctx, n.ID); err == nil {
				u.Nodes[i].MemUsage, u.Nodes[i].CPUCores = s.MemUsage, s.CPUCores
			}
		}()
	}
	wg.Wait()
	for _, n := range u.Nodes {
		u.MemUsage += n.MemUsage
		u.CPUCores += n.CPUCores
	}
	return u, nil
}

// SetK3dLimits caps every node of a k3d cluster. memoryMB or cpus of 0
// lift that cap (it is set to the whole machine). The caps survive the
// cluster being stopped and started.
func SetK3dLimits(ctx context.Context, eng *engine.Client, name string, memoryMB int, cpus float64) error {
	if memoryMB != 0 && memoryMB < MinNodeMemoryMB {
		return fmt.Errorf("each node needs at least %d MB", MinNodeMemoryMB)
	}
	if cpus != 0 && cpus < MinNodeCPUs {
		return fmt.Errorf("each node needs at least %.1f CPUs", MinNodeCPUs)
	}
	nodes, err := k3dNodes(ctx, eng, name)
	if err != nil {
		return err
	}
	host, err := eng.Info(ctx)
	if err != nil {
		return err
	}
	if cpus > float64(host.NCPU) {
		return fmt.Errorf("this machine has %d CPUs", host.NCPU)
	}
	if int64(memoryMB)<<20 > host.MemTotal {
		return fmt.Errorf("this machine has %d MB", host.MemTotal>>20)
	}
	l := engine.Limits{Memory: int64(memoryMB) << 20, NanoCPUs: int64(cpus * 1e9)}
	if memoryMB == 0 {
		l.Memory = host.MemTotal
	}
	if cpus == 0 {
		l.NanoCPUs = int64(host.NCPU) * 1e9
	}
	for _, n := range nodes {
		if err := eng.SetLimits(ctx, n.ID, l); err != nil {
			return fmt.Errorf("node %s: %w", n.Name, err)
		}
	}
	return nil
}
