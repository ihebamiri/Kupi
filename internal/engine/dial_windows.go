//go:build windows

package engine

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialPipe(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}

func defaultEndpoints() []string {
	return []string{
		"npipe:////./pipe/docker_engine",
		"npipe:////./pipe/dockerDesktopLinuxEngine",
		"npipe:////./pipe/podman-machine-default",
	}
}
