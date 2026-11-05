package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardAddress is the only address port-forwards listen on.
const ForwardAddress = "127.0.0.1"

// PortForward forwards 127.0.0.1:localPort to remotePort on a pod. localPort
// 0 lets the OS pick one. onReady receives the local port once listening. It
// blocks until ctx is cancelled or the connection to the pod is lost.
func (s *Session) PortForward(ctx context.Context, namespace, pod string, localPort, remotePort int, onReady func(local int)) error {
	if s.cfg == nil {
		return errors.New("port-forward is not available in this session")
	}
	reqURL := s.cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward").URL()
	transport, upgrader, err := spdy.RoundTripperFor(s.cfg)
	if err != nil {
		return fmt.Errorf("create spdy transport: %w", err)
	}
	spdyDialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, reqURL)
	wsDialer, err := portforward.NewSPDYOverWebsocketDialer(reqURL, s.cfg)
	if err != nil {
		return fmt.Errorf("create websocket dialer: %w", err)
	}
	dialer := portforward.NewFallbackDialer(wsDialer, spdyDialer, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})

	errOut := &lastLine{}
	ready := make(chan struct{})
	fw, err := portforward.NewOnAddressesWithContext(ctx, dialer, []string{ForwardAddress},
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)}, ready, discard{}, errOut)
	if err != nil {
		return fmt.Errorf("create port-forward: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- fw.ForwardPorts() }()
	select {
	case <-ready:
		if ports, err := fw.GetPorts(); err == nil && len(ports) > 0 && onReady != nil {
			onReady(int(ports[0].Local))
		}
	case err := <-done:
		return withDetail(err, errOut)
	case <-ctx.Done():
		fw.Close()
		return ctx.Err()
	}
	select {
	case err := <-done:
		return withDetail(err, errOut)
	case <-ctx.Done():
		fw.Close()
		<-done
		return ctx.Err()
	}
}

func withDetail(err error, out *lastLine) error {
	if detail := out.get(); detail != "" {
		if err == nil {
			return errors.New(detail)
		}
		return fmt.Errorf("%w (%s)", err, detail)
	}
	return err
}

// LastForwardError reports the most recent per-connection error, such as
// "connection refused" when nothing listens on the remote port.
type lastLine struct {
	mu   sync.Mutex
	line string
}

func (l *lastLine) Write(p []byte) (int, error) {
	if s := strings.TrimSpace(string(p)); s != "" {
		l.mu.Lock()
		l.line = s
		l.mu.Unlock()
	}
	return len(p), nil
}

func (l *lastLine) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
