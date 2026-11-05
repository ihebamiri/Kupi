package bridge

import (
	"errors"
	"strings"
	"testing"
)

func TestExplainConnectError(t *testing.T) {
	timeout := errors.New("reach API server: Get \"https://host.docker.internal:6550/version\": dial tcp 172.16.1.233:6550: i/o timeout")
	got := explainConnectError("https://host.docker.internal:6550", timeout)
	if !errors.Is(got, timeout) || !strings.Contains(got.Error(), "https://127.0.0.1:6550") {
		t.Errorf("got %v", got)
	}
	if got := explainConnectError("https://10.0.0.5:6443", timeout); got != timeout {
		t.Errorf("other servers changed: %v", got)
	}
	forbidden := errors.New("Unauthorized")
	if got := explainConnectError("https://host.docker.internal:6550", forbidden); got != forbidden {
		t.Errorf("unrelated error changed: %v", got)
	}
}
