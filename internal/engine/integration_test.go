//go:build integration

package engine

import (
	"context"
	"testing"
	"time"
)

func TestLocalEngine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := Detect(ctx)
	if err != nil {
		t.Skipf("no local container engine: %v", err)
	}
	v, err := c.Version(ctx)
	if err != nil || v.Version == "" {
		t.Fatalf("version: %+v %v", v, err)
	}
	t.Logf("engine %s %s at %s", v.Kind(), v.Version, c.Endpoint)
	if _, err := c.Images(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DiskUsage(ctx); err != nil {
		t.Fatal(err)
	}
	var last PullProgress
	if err := c.Pull(ctx, "busybox:1.36", nil, func(p PullProgress) { last = p }); err != nil {
		t.Fatalf("pull: %v (last %+v)", err, last)
	}
	d, err := c.Inspect(ctx, "busybox:1.36")
	if err != nil || d.Size == 0 {
		t.Fatalf("inspect: %+v %v", d, err)
	}
	layers, err := c.History(ctx, "busybox:1.36")
	if err != nil || len(layers) == 0 {
		t.Fatalf("history: %d layers, %v", len(layers), err)
	}
}
