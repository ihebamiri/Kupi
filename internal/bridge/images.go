package bridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/engine"
	"github.com/ihebamiri/kupi/internal/localk8s"
	"github.com/ihebamiri/kupi/internal/profile"
	"github.com/ihebamiri/kupi/internal/version"
)

// EventPull carries image pull progress.
const EventPull = "kupi:pull"

// PullEvent reports a pull's progress.
type PullEvent struct {
	ID      string `json:"id"`
	Ref     string `json:"ref"`
	Percent int    `json:"percent"`
	Status  string `json:"status"`
	Done    bool   `json:"done"`
	Error   string `json:"error"`
}

// EngineInfo describes the local container engine.
type EngineInfo struct {
	Available bool   `json:"available"`
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	Endpoint  string `json:"endpoint"`
	Reason    string `json:"reason"`
}

// ImageView is one local image.
type ImageView struct {
	ID         string   `json:"id"`
	Tags       []string `json:"tags"`
	Size       int64    `json:"size"`
	Created    string   `json:"created"`
	Dangling   bool     `json:"dangling"`
	Containers int64    `json:"containers"`
}

// LayerView is one layer of an image.
type LayerView struct {
	CreatedBy string `json:"createdBy"`
	Size      int64  `json:"size"`
	Created   string `json:"created"`
}

// ImageDetailView describes one image without exposing environment values,
// which can hold secrets baked in at build time.
type ImageDetailView struct {
	ID         string            `json:"id"`
	Tags       []string          `json:"tags"`
	Digests    []string          `json:"digests"`
	Created    string            `json:"created"`
	Size       int64             `json:"size"`
	Platform   string            `json:"platform"`
	User       string            `json:"user"`
	RunsAsRoot bool              `json:"runsAsRoot"`
	Entrypoint []string          `json:"entrypoint"`
	Cmd        []string          `json:"cmd"`
	Ports      []string          `json:"ports"`
	EnvNames   []string          `json:"envNames"`
	Labels     map[string]string `json:"labels"`
	Layers     []LayerView       `json:"layers"`
}

// SearchView is one registry search hit.
type SearchView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Official    bool   `json:"official"`
	// LooksLike names the official image this one resembles, a common
	// trick for passing off malicious images.
	LooksLike string `json:"looksLike"`
}

// TagView is one tag of a Docker Hub repository.
type TagView struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Updated string `json:"updated"`
}

// ImageService manages local images and registry lookups.
type ImageService struct {
	d Deps

	mu     sync.Mutex
	client *engine.Client
}

// NewImageService returns the image service.
func NewImageService(d Deps) *ImageService { return &ImageService{d: d} }

func (s *ImageService) engine(ctx context.Context) (*engine.Client, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.Ping(pctx)
		cancel()
		if err == nil {
			return c, nil
		}
	}
	c, err := engine.Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("no container engine is running (Docker or Podman): %w", err)
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	return c, nil
}

// Engine reports the local engine.
func (s *ImageService) Engine(ctx context.Context) EngineInfo {
	c, err := s.engine(ctx)
	if err != nil {
		return EngineInfo{Reason: err.Error()}
	}
	v, err := c.Version(ctx)
	if err != nil {
		return EngineInfo{Reason: err.Error()}
	}
	return EngineInfo{Available: true, Kind: v.Kind(), Version: v.Version, Endpoint: c.Endpoint}
}

// Images lists local images, largest first.
func (s *ImageService) Images(ctx context.Context) ([]ImageView, error) {
	c, err := s.engine(ctx)
	if err != nil {
		return nil, err
	}
	list, err := c.Images(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ImageView, 0, len(list))
	for _, i := range list {
		tags := []string{}
		for _, t := range i.RepoTags {
			if t != "<none>:<none>" {
				tags = append(tags, t)
			}
		}
		out = append(out, ImageView{
			ID: i.ID, Tags: tags, Size: i.Size, Created: time.Unix(i.Created, 0).UTC().Format(time.RFC3339),
			Dangling: len(tags) == 0, Containers: max(i.Containers, 0),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, nil
}

// Inspect describes one image and its layers.
func (s *ImageService) Inspect(ctx context.Context, ref string) (ImageDetailView, error) {
	if err := validImageOrID(ref); err != nil {
		return ImageDetailView{}, err
	}
	c, err := s.engine(ctx)
	if err != nil {
		return ImageDetailView{}, err
	}
	d, err := c.Inspect(ctx, ref)
	if err != nil {
		return ImageDetailView{}, err
	}
	v := ImageDetailView{
		ID: d.ID, Tags: d.RepoTags, Digests: d.RepoDigests, Created: d.Created, Size: d.Size,
		Platform: d.Os + "/" + d.Architecture, User: d.Config.User, Entrypoint: d.Config.Entrypoint, Cmd: d.Config.Cmd,
		Labels: d.Config.Labels,
	}
	v.RunsAsRoot = d.Config.User == "" || d.Config.User == "root" || d.Config.User == "0" || strings.HasPrefix(d.Config.User, "0:")
	for p := range d.Config.ExposedPorts {
		v.Ports = append(v.Ports, p)
	}
	sort.Strings(v.Ports)
	for _, e := range d.Config.Env {
		name, _, _ := strings.Cut(e, "=")
		v.EnvNames = append(v.EnvNames, name)
	}
	layers, err := c.History(ctx, ref)
	if err == nil {
		for _, l := range layers {
			v.Layers = append(v.Layers, LayerView{CreatedBy: cleanStep(l.CreatedBy), Size: l.Size, Created: time.Unix(l.Created, 0).UTC().Format(time.RFC3339)})
		}
	}
	return v, nil
}

func cleanStep(s string) string {
	s = strings.TrimPrefix(s, "/bin/sh -c #(nop) ")
	s = strings.TrimPrefix(s, "/bin/sh -c ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return strings.TrimSpace(s)
}

// popularOfficial helps catch look-alike names even when the official image
// isn't in the same search results.
var popularOfficial = []string{"nginx", "redis", "postgres", "mysql", "mariadb", "mongo", "node", "python", "golang", "alpine",
	"ubuntu", "debian", "busybox", "httpd", "traefik", "rabbitmq", "memcached", "registry", "openjdk", "php", "ruby", "wordpress"}

// Search finds images on Docker Hub through the engine.
func (s *ImageService) Search(ctx context.Context, term string) ([]SearchView, error) {
	term = strings.TrimSpace(term)
	if term == "" || len(term) > 100 || strings.ContainsAny(term, " \t\n") {
		return nil, fmt.Errorf("enter one search word")
	}
	c, err := s.engine(ctx)
	if err != nil {
		return nil, err
	}
	results, err := c.Search(ctx, term, 25)
	if err != nil {
		return nil, err
	}
	official := append([]string(nil), popularOfficial...)
	for _, r := range results {
		if r.IsOfficial {
			official = append(official, r.Name)
		}
	}
	out := make([]SearchView, 0, len(results))
	for _, r := range results {
		v := SearchView{Name: r.Name, Description: r.Description, Stars: r.StarCount, Official: r.IsOfficial}
		if !r.IsOfficial {
			v.LooksLike = lookalike(r.Name, official)
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Official != out[j].Official {
			return out[i].Official
		}
		return out[i].Stars > out[j].Stars
	})
	return out, nil
}

// lookalike returns an official name within two edits of name's repository
// part, or "".
func lookalike(name string, official []string) string {
	repo := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		repo = name[i+1:]
	}
	for _, o := range official {
		if repo == o {
			if strings.Contains(name, "/") {
				return o
			}
			continue
		}
		if len(repo) > 3 && levenshtein(repo, o) <= 2 {
			return o
		}
	}
	return ""
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

var hubClient = &http.Client{Timeout: 15 * time.Second}

// Tags lists the most recently updated tags of a Docker Hub repository.
func (s *ImageService) Tags(ctx context.Context, repo string) ([]TagView, error) {
	if err := localk8s.ValidImage(repo); err != nil || strings.Contains(repo, ":") || strings.Contains(repo, "@") {
		return nil, fmt.Errorf("enter a repository name without a tag, like nginx or bitnami/redis")
	}
	parts := strings.Split(repo, "/")
	if len(parts) > 2 || strings.Contains(parts[0], ".") {
		return nil, fmt.Errorf("tag lists are available for Docker Hub repositories; for other registries, type the full reference")
	}
	ns, name := "library", parts[0]
	if len(parts) == 2 {
		ns, name = parts[0], parts[1]
	}
	u := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s/tags?page_size=30&ordering=last_updated", url.PathEscape(ns), url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := hubClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach Docker Hub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Docker Hub answered %s", resp.Status)
	}
	var body struct {
		Results []struct {
			Name        string `json:"name"`
			FullSize    int64  `json:"full_size"`
			LastUpdated string `json:"last_updated"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("read Docker Hub tags: %w", err)
	}
	out := make([]TagView, 0, len(body.Results))
	for _, r := range body.Results {
		out = append(out, TagView{Name: r.Name, Size: r.FullSize, Updated: r.LastUpdated})
	}
	return out, nil
}

// Pull starts downloading an image; progress arrives as "kupi:pull" events.
func (s *ImageService) Pull(ctx context.Context, ref string) (string, error) {
	if err := localk8s.ValidImage(ref); err != nil {
		return "", err
	}
	c, err := s.engine(ctx)
	if err != nil {
		return "", err
	}
	id := randomID()
	go func() {
		pctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := c.Pull(pctx, ref, func(p engine.PullProgress) {
			s.d.Emit.Emit(EventPull, PullEvent{ID: id, Ref: ref, Percent: p.Percent, Status: p.Status})
		})
		ev := PullEvent{ID: id, Ref: ref, Percent: 100, Done: true, Status: "Pulled"}
		if err != nil {
			ev.Percent, ev.Status, ev.Error = 0, "Failed", err.Error()
		}
		s.d.Emit.Emit(EventPull, ev)
	}()
	return id, nil
}

// Remove deletes a local image.
func (s *ImageService) Remove(ctx context.Context, ref string, force bool) error {
	if err := validImageOrID(ref); err != nil {
		return err
	}
	c, err := s.engine(ctx)
	if err != nil {
		return err
	}
	return c.Remove(ctx, ref, force)
}

// Prune removes dangling images, or all unused images when all is set.
func (s *ImageService) Prune(ctx context.Context, all bool) (int64, error) {
	c, err := s.engine(ctx)
	if err != nil {
		return 0, err
	}
	return c.PruneImages(ctx, all)
}

// PruneBuildCache removes unused build cache.
func (s *ImageService) PruneBuildCache(ctx context.Context) (int64, error) {
	c, err := s.engine(ctx)
	if err != nil {
		return 0, err
	}
	return c.PruneBuildCache(ctx)
}

// DiskUsage reports the engine's disk use.
func (s *ImageService) DiskUsage(ctx context.Context) (engine.DiskUsage, error) {
	c, err := s.engine(ctx)
	if err != nil {
		return engine.DiskUsage{}, err
	}
	return c.DiskUsage(ctx)
}

// LoadIntoCluster copies a local image into a local cluster's nodes, so pods
// can run it without a registry. Only clusters labelled local qualify.
func (s *ImageService) LoadIntoCluster(ctx context.Context, contextName, ref string) (string, error) {
	sess, err := s.d.session(contextName)
	if err != nil {
		return "", err
	}
	if sess.Profile() != profile.Local {
		return "", fmt.Errorf("loading local images is only for clusters labelled local")
	}
	ctx, cancel := context.WithTimeout(ctx, localk8s.ActionTimeout)
	defer cancel()
	out, err := localk8s.LoadImage(ctx, contextName, ref)
	entry := audit.Entry{Context: contextName, Profile: string(sess.Profile()), KubeUser: sess.UserName(), Action: "image-load", Resource: ref, Outcome: audit.OutcomeApplied}
	if err != nil {
		entry.Outcome, entry.Error = audit.OutcomeFailed, err.Error()
	}
	_, _ = s.d.Audit.Append(entry)
	return out, err
}

var imageIDRe = func() func(string) bool {
	return func(s string) bool {
		if !strings.HasPrefix(s, "sha256:") || len(s) != 71 {
			return false
		}
		_, err := hex.DecodeString(s[7:])
		return err == nil
	}
}()

func validImageOrID(ref string) error {
	if imageIDRe(ref) {
		return nil
	}
	return localk8s.ValidImage(ref)
}

func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// LocalClusterService creates and manages k3d clusters.
type LocalClusterService struct{ d Deps }

// NewLocalClusterService returns the local cluster service.
func NewLocalClusterService(d Deps) *LocalClusterService { return &LocalClusterService{d: d} }

// Tools reports which local-cluster CLIs are installed.
func (s *LocalClusterService) Tools() []localk8s.Tool { return localk8s.Tools() }

// List returns the k3d clusters on this machine.
func (s *LocalClusterService) List(ctx context.Context) ([]localk8s.Cluster, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return localk8s.ListK3d(ctx)
}

// Create makes a new k3d cluster. It adds a context to the kubeconfig but
// never switches the current kubectl context.
func (s *LocalClusterService) Create(ctx context.Context, o localk8s.CreateOptions) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, localk8s.CreateTimeout)
	defer cancel()
	out, err := localk8s.CreateK3d(ctx, o)
	s.audit("local-cluster-create", o.Name, err)
	return out, err
}

// Start starts a stopped k3d cluster.
func (s *LocalClusterService) Start(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, localk8s.ActionTimeout)
	defer cancel()
	out, err := localk8s.StartK3d(ctx, name)
	if err == nil {
		s.d.Laptop.Woke(name)
	}
	return out, err
}

// Stop stops a k3d cluster, freeing its memory and CPU.
func (s *LocalClusterService) Stop(ctx context.Context, name string) (string, error) {
	s.disconnect(name)
	ctx, cancel := context.WithTimeout(ctx, localk8s.ActionTimeout)
	defer cancel()
	return localk8s.StopK3d(ctx, name)
}

// Delete removes a k3d cluster and everything in it.
func (s *LocalClusterService) Delete(ctx context.Context, name string) (string, error) {
	s.disconnect(name)
	ctx, cancel := context.WithTimeout(ctx, localk8s.ActionTimeout)
	defer cancel()
	out, err := localk8s.DeleteK3d(ctx, name)
	s.audit("local-cluster-delete", name, err)
	return out, err
}

func (s *LocalClusterService) disconnect(name string) {
	contextName := "k3d-" + name
	if _, ok := s.d.Kube.Session(contextName); ok {
		s.d.stopContext(contextName)
		s.d.Kube.Disconnect(contextName)
		s.d.Guard.Forget(contextName)
	}
}

func (s *LocalClusterService) audit(action, name string, err error) {
	e := audit.Entry{Context: "k3d-" + name, Profile: string(profile.Local), Action: action, Resource: "k3d cluster " + name, Outcome: audit.OutcomeApplied}
	if err != nil {
		e.Outcome, e.Error = audit.OutcomeFailed, err.Error()
	}
	_, _ = s.d.Audit.Append(e)
}
