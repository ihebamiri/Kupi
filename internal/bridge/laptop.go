package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/iheb-amiri/kupi/internal/audit"
	"github.com/iheb-amiri/kupi/internal/engine"
	"github.com/iheb-amiri/kupi/internal/laptop"
	"github.com/iheb-amiri/kupi/internal/localk8s"
	"github.com/iheb-amiri/kupi/internal/profile"
)

// NewLaptop opens laptop mode with hooks into the rest of Kupi: a cluster
// counts as busy while it has port-forwards, and an idle stop disconnects
// its session and is audited.
func NewLaptop(path string, d Deps) (*laptop.Manager, error) {
	return laptop.Open(path, laptop.Hooks{
		Running: func(ctx context.Context, name string) (bool, error) {
			clusters, err := localk8s.ListK3d(ctx)
			if err != nil {
				return false, err
			}
			for _, c := range clusters {
				if c.Name == name {
					return c.Running, nil
				}
			}
			return false, nil
		},
		Stop: func(ctx context.Context, name string) error {
			contextName := "k3d-" + name
			if _, ok := d.Kube.Session(contextName); ok {
				d.stopContext(contextName)
				d.Kube.Disconnect(contextName)
				d.Guard.Forget(contextName)
			}
			_, err := localk8s.StopK3d(ctx, name)
			return err
		},
		Busy: func(contextName string) bool {
			for _, f := range d.Forwards.List() {
				if f.Context == contextName {
					return true
				}
			}
			return false
		},
		Stopped: func(name string, err error) {
			e := audit.Entry{Context: "k3d-" + name, Profile: string(profile.Local), Action: "local-cluster-idle-stop", Resource: "k3d cluster " + name, Outcome: audit.OutcomeApplied}
			if err != nil {
				e.Outcome, e.Error = audit.OutcomeFailed, err.Error()
			}
			_, _ = d.Audit.Append(e)
		},
	})
}

// Usage measures how much memory and CPU a k3d cluster takes from this machine.
func (s *LocalClusterService) Usage(ctx context.Context, name string) (localk8s.Usage, error) {
	if err := localk8s.ValidName(name); err != nil {
		return localk8s.Usage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	eng, err := engine.Detect(ctx)
	if err != nil {
		return localk8s.Usage{}, fmt.Errorf("no container engine is running (Docker or Podman): %w", err)
	}
	return localk8s.K3dUsage(ctx, eng, name)
}

// SetLimits caps each node of a k3d cluster: memory in MB and CPUs; 0 lifts a cap.
func (s *LocalClusterService) SetLimits(ctx context.Context, name string, memoryMB int, cpus float64) error {
	if err := localk8s.ValidName(name); err != nil {
		return err
	}
	if memoryMB < 0 || cpus < 0 || memoryMB > 1<<20 || cpus > 1024 {
		return fmt.Errorf("invalid limits")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	eng, err := engine.Detect(ctx)
	if err != nil {
		return fmt.Errorf("no container engine is running (Docker or Podman): %w", err)
	}
	err = localk8s.SetK3dLimits(ctx, eng, name, memoryMB, cpus)
	e := audit.Entry{Context: "k3d-" + name, Profile: string(profile.Local), Action: "local-cluster-limits", Resource: fmt.Sprintf("k3d cluster %s (memory %d MB, CPUs %.1f per node; 0 = no cap)", name, memoryMB, cpus), Outcome: audit.OutcomeApplied}
	if err != nil {
		e.Outcome, e.Error = audit.OutcomeFailed, err.Error()
	}
	_, _ = s.d.Audit.Append(e)
	return err
}

// LaptopStatus returns a cluster's laptop-mode settings.
func (s *LocalClusterService) LaptopStatus(name string) (laptop.Status, error) {
	if err := localk8s.ValidName(name); err != nil {
		return laptop.Status{}, err
	}
	return s.d.Laptop.Status(name), nil
}

// SetIdleStop stops a cluster after minutes without Kupi using it; 0 turns it off.
func (s *LocalClusterService) SetIdleStop(name string, minutes int) error {
	if err := localk8s.ValidName(name); err != nil {
		return err
	}
	return s.d.Laptop.SetIdleStop(name, minutes)
}
