// Package procutil starts helper programs (kubectl, docker, k3d) directly,
// with an argument list and never through a shell. On Windows it also keeps
// them from opening a console window over Kupi.
package procutil

import (
	"context"
	"os/exec"
)

// Command is exec.CommandContext plus hide(), which keeps a console program
// from flashing a window on Windows. path should be absolute (exec.LookPath).
func Command(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	hide(cmd)
	return cmd
}
