package forward

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/kube"
)

type fakeReader struct {
	kube.Reader
	mu      sync.Mutex
	objects map[string]*unstructured.Unstructured
}

func (f *fakeReader) key(gvr schema.GroupVersionResource, ns, name string) string {
	return gvr.Resource + "/" + ns + "/" + name
}

func (f *fakeReader) put(gvr schema.GroupVersionResource, obj map[string]interface{}) {
	u := &unstructured.Unstructured{Object: obj}
	f.mu.Lock()
	f.objects[f.key(gvr, u.GetNamespace(), u.GetName())] = u
	f.mu.Unlock()
}

func (f *fakeReader) remove(gvr schema.GroupVersionResource, ns, name string) {
	f.mu.Lock()
	delete(f.objects, f.key(gvr, ns, name))
	f.mu.Unlock()
}

func (f *fakeReader) Get(_ context.Context, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o, ok := f.objects[f.key(gvr, ns, name)]; ok {
		return o.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(gvr.GroupResource(), name)
}

func (f *fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, ns string, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &unstructured.UnstructuredList{}
	for k, o := range f.objects {
		if strings.HasPrefix(k, gvr.Resource+"/"+ns+"/") {
			out.Items = append(out.Items, *o.DeepCopy())
		}
	}
	return out, nil
}

func pod(name string, ready bool) map[string]interface{} {
	status := "False"
	if ready {
		status = "True"
	}
	return map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]interface{}{"name": name, "namespace": "shop", "labels": map[string]interface{}{"app": "web"}},
		"spec": map[string]interface{}{"containers": []interface{}{map[string]interface{}{
			"name": "web", "ports": []interface{}{map[string]interface{}{"name": "http", "containerPort": int64(8080)}},
		}}},
		"status": map[string]interface{}{"phase": "Running", "conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": status}}},
	}
}

func service() map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]interface{}{"name": "web", "namespace": "shop"},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{"app": "web"},
			"ports":    []interface{}{map[string]interface{}{"port": int64(80), "targetPort": "http"}},
		},
	}
}

func TestResolveServiceToReadyPodAndNamedPort(t *testing.T) {
	r := &fakeReader{objects: map[string]*unstructured.Unstructured{}}
	r.put(servicesGVR, service())
	r.put(podsGVR, pod("web-unready", false))
	r.put(podsGVR, pod("web-ready", true))
	name, port, err := resolve(context.Background(), r, Target{Kind: "Service", Namespace: "shop", Name: "web", Port: 80})
	if err != nil {
		t.Fatal(err)
	}
	if name != "web-ready" || port != 8080 {
		t.Errorf("resolved to %s:%d, want web-ready:8080", name, port)
	}
	if _, _, err := resolve(context.Background(), r, Target{Kind: "Service", Namespace: "shop", Name: "web", Port: 443}); err == nil {
		t.Error("unknown service port accepted")
	}
}

// fakeSession forwards by listening locally, then drops when told to.
type fakeSession struct {
	r     *fakeReader
	mu    sync.Mutex
	calls []string
	drop  chan struct{}
}

func (s *fakeSession) Reader() kube.Reader { return s.r }

func (s *fakeSession) PortForward(ctx context.Context, _ string, pod string, local, remote int, onReady func(int)) error {
	s.mu.Lock()
	s.calls = append(s.calls, pod)
	s.mu.Unlock()
	l, err := net.Listen("tcp", net.JoinHostPort(kube.ForwardAddress, strconv.Itoa(local)))
	if err != nil {
		return err
	}
	defer l.Close()
	onReady(l.Addr().(*net.TCPAddr).Port)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.drop:
		return errors.New("lost connection to pod")
	}
}

type nopEmitter struct{}

func (nopEmitter) Emit(string, any) {}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestReconnectsOnSameLocalPort(t *testing.T) {
	r := &fakeReader{objects: map[string]*unstructured.Unstructured{}}
	r.put(servicesGVR, service())
	r.put(podsGVR, pod("web-1", true))
	s := &fakeSession{r: r, drop: make(chan struct{})}
	m := New(nopEmitter{})

	f, err := m.Start("ctx", s, Target{Kind: "Service", Namespace: "shop", Name: "web", Port: 80}, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.List()[0].Status == StatusActive })
	first := m.List()[0]
	if first.LocalPort == 0 || first.Pod != "web-1" || first.PodPort != 8080 {
		t.Fatalf("first forward = %+v", first)
	}

	// The pod is replaced and the tunnel drops: the forward moves to the new pod.
	r.remove(podsGVR, "shop", "web-1")
	r.put(podsGVR, pod("web-2", true))
	close(s.drop)
	waitFor(t, func() bool {
		l := m.List()
		return len(l) == 1 && l[0].Pod == "web-2" && l[0].Reconnects == 1
	})
	if got := m.List()[0].LocalPort; got != first.LocalPort {
		t.Errorf("reconnected on port %d, want the same port %d", got, first.LocalPort)
	}

	m.Stop(f.ID)
	if len(m.List()) != 0 {
		t.Error("Stop should remove the forward")
	}
}

func TestStartRejectsBadInput(t *testing.T) {
	r := &fakeReader{objects: map[string]*unstructured.Unstructured{}}
	m := New(nopEmitter{})
	s := &fakeSession{r: r, drop: make(chan struct{})}
	if _, err := m.Start("ctx", s, Target{Kind: "Secret", Namespace: "a", Name: "b", Port: 80}, 0); err == nil {
		t.Error("forwarding to a Secret accepted")
	}
	if _, err := m.Start("ctx", s, Target{Kind: "Pod", Namespace: "a", Name: "b", Port: 0}, 0); err == nil {
		t.Error("port 0 accepted")
	}
	if _, err := m.Start("ctx", s, Target{Kind: "Pod", Namespace: "a", Name: "missing", Port: 80}, 0); err == nil {
		t.Error("missing pod accepted")
	}
}
