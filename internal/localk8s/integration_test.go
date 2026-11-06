//go:build integration

package localk8s

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/ihebamiri/kupi/internal/engine"
)

// TestLoadImageIntoK3d loads a local-only tag into the kupi-test cluster and
// relies on LoadImage's own check that the node really has it.
func TestLoadImageIntoK3d(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not installed")
	}
	if out, err := exec.CommandContext(ctx, "docker", "pull", "busybox:1.36").CombinedOutput(); err != nil {
		t.Fatalf("pull: %v %s", err, out)
	}
	tag := "kupi-local/it:" + time.Now().Format("150405")
	if out, err := exec.CommandContext(ctx, "docker", "tag", "busybox:1.36", tag).CombinedOutput(); err != nil {
		t.Fatalf("tag: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", tag).Run() })
	if _, err := LoadImage(ctx, "k3d-kupi-test", tag); err != nil {
		t.Fatal(err)
	}
}

// TestK3dUsageAndLimits measures kupi-test and caps then uncaps its nodes.
func TestK3dUsageAndLimits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	eng, err := engine.Detect(ctx)
	if err != nil {
		t.Skip("no container engine")
	}
	u, err := K3dUsage(ctx, eng, "kupi-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Nodes) == 0 || u.MemUsage <= 0 || u.HostMem <= 0 || u.HostCPUs <= 0 {
		t.Fatalf("usage = %+v", u)
	}
	t.Logf("kupi-test uses %d MB and %.2f cores of %d MB / %d CPUs", u.MemUsage>>20, u.CPUCores, u.HostMem>>20, u.HostCPUs)
	t.Cleanup(func() { _ = SetK3dLimits(context.Background(), eng, "kupi-test", 0, 0) })
	if err := SetK3dLimits(ctx, eng, "kupi-test", 2048, 1.5); err != nil {
		t.Fatal(err)
	}
	capped, _ := K3dUsage(ctx, eng, "kupi-test")
	if n := capped.Nodes[0]; n.MemCap != 2048<<20 || n.CPUCap != 1.5 {
		t.Errorf("caps not applied: %+v", n)
	}
	if err := SetK3dLimits(ctx, eng, "kupi-test", 100, 0); err == nil {
		t.Error("a cap below the minimum was accepted")
	}
}
