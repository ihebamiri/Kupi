//go:build integration

package localk8s

import (
	"context"
	"os/exec"
	"testing"
	"time"
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
