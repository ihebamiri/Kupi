//go:build !windows

package engine

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
)

func dialPipe(context.Context, string) (net.Conn, error) {
	return nil, errors.New("named pipes are only available on Windows")
}

func defaultEndpoints() []string {
	eps := []string{"unix:///var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		eps = append(eps, "unix://"+filepath.Join(home, ".docker", "desktop", "docker.sock"))
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		eps = append(eps, "unix://"+filepath.Join(runtimeDir, "podman", "podman.sock"))
	}
	return eps
}
