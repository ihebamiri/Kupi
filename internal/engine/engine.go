// Package engine is a small client for the Docker Engine API, which Docker
// and Podman both serve. It only talks to local endpoints (a Unix socket or
// a Windows named pipe); unauthenticated TCP endpoints are refused because
// anyone on the network who can reach one gets root on the machine.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client talks to one engine endpoint.
type Client struct {
	Endpoint string
	http     *http.Client
}

// ErrTCPRefused is returned for DOCKER_HOST values using plain TCP.
var ErrTCPRefused = errors.New("TCP engine endpoints aren't supported: they are often unauthenticated and give root access to anyone who can reach them")

// New returns a client for an endpoint such as unix:///var/run/docker.sock
// or npipe:////./pipe/docker_engine.
func New(endpoint string) (*Client, error) {
	scheme, path, ok := strings.Cut(endpoint, "://")
	if !ok {
		return nil, fmt.Errorf("invalid engine endpoint %q", endpoint)
	}
	var dial func(ctx context.Context) (net.Conn, error)
	switch scheme {
	case "unix":
		dial = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}
	case "npipe":
		dial = func(ctx context.Context) (net.Conn, error) { return dialPipe(ctx, path) }
	case "tcp", "http", "https":
		return nil, ErrTCPRefused
	default:
		return nil, fmt.Errorf("unsupported engine endpoint %q", endpoint)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		Proxy:       nil,
	}
	return &Client{Endpoint: endpoint, http: &http.Client{Transport: transport}}, nil
}

// Candidates lists the endpoints to try, in order.
func Candidates() []string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return []string{h}
	}
	return defaultEndpoints()
}

// Detect returns a client for the first endpoint that answers.
func Detect(ctx context.Context) (*Client, error) {
	var lastErr error
	for _, ep := range Candidates() {
		c, err := New(ep)
		if err != nil {
			lastErr = err
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = c.Ping(pctx)
		cancel()
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no container engine endpoint found")
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, headers map[string]string) (*http.Response, error) {
	u := "http://engine" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var msg struct {
			Message string `json:"message"`
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if json.Unmarshal(data, &msg) == nil && msg.Message != "" {
			return nil, &APIError{Status: resp.StatusCode, Message: msg.Message}
		}
		return nil, &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	return resp, nil
}

// APIError is an error reply from the engine.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, query, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping checks the engine answers.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/_ping", nil, nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Version describes the engine.
type Version struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	OS         string `json:"Os"`
	Arch       string `json:"Arch"`
	Components []struct {
		Name string `json:"Name"`
	} `json:"Components"`
}

// Kind returns "podman" or "docker".
func (v Version) Kind() string {
	for _, c := range v.Components {
		if strings.Contains(strings.ToLower(c.Name), "podman") {
			return "podman"
		}
	}
	return "docker"
}

// Version returns the engine's version.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	err := c.getJSON(ctx, "/version", nil, &v)
	return v, err
}

// Image is one local image.
type Image struct {
	ID         string   `json:"Id"`
	RepoTags   []string `json:"RepoTags"`
	Created    int64    `json:"Created"`
	Size       int64    `json:"Size"`
	Containers int64    `json:"Containers"`
}

// Images lists local images.
func (c *Client) Images(ctx context.Context) ([]Image, error) {
	var out []Image
	err := c.getJSON(ctx, "/images/json", nil, &out)
	return out, err
}

// ImageDetail is the part of an image's inspect output Kupi shows.
type ImageDetail struct {
	ID           string   `json:"Id"`
	RepoTags     []string `json:"RepoTags"`
	RepoDigests  []string `json:"RepoDigests"`
	Created      string   `json:"Created"`
	Size         int64    `json:"Size"`
	Architecture string   `json:"Architecture"`
	Os           string   `json:"Os"`
	Config       struct {
		User         string            `json:"User"`
		Env          []string          `json:"Env"`
		Entrypoint   []string          `json:"Entrypoint"`
		Cmd          []string          `json:"Cmd"`
		ExposedPorts map[string]any    `json:"ExposedPorts"`
		Labels       map[string]string `json:"Labels"`
		WorkingDir   string            `json:"WorkingDir"`
	} `json:"Config"`
}

// Inspect returns one image's details.
func (c *Client) Inspect(ctx context.Context, ref string) (ImageDetail, error) {
	var d ImageDetail
	err := c.getJSON(ctx, "/images/"+url.PathEscape(ref)+"/json", nil, &d)
	return d, err
}

// Layer is one step of an image's history.
type Layer struct {
	Created   int64  `json:"Created"`
	CreatedBy string `json:"CreatedBy"`
	Size      int64  `json:"Size"`
	Comment   string `json:"Comment"`
}

// History returns an image's layers, newest first.
func (c *Client) History(ctx context.Context, ref string) ([]Layer, error) {
	var out []Layer
	err := c.getJSON(ctx, "/images/"+url.PathEscape(ref)+"/history", nil, &out)
	return out, err
}

// SearchResult is one registry search hit.
type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
	IsOfficial  bool   `json:"is_official"`
}

// Search searches the engine's default registry (Docker Hub).
func (c *Client) Search(ctx context.Context, term string, limit int) ([]SearchResult, error) {
	var out []SearchResult
	err := c.getJSON(ctx, "/images/search", url.Values{"term": {term}, "limit": {fmt.Sprint(limit)}}, &out)
	return out, err
}

// PullProgress reports a pull's progress.
type PullProgress struct {
	Status  string
	Percent int
	Done    bool
	Error   string
}

type pullMessage struct {
	Status         string `json:"status"`
	ID             string `json:"id"`
	Error          string `json:"error"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
}

// Pull downloads an image, calling onProgress as layers arrive.
func (c *Client) Pull(ctx context.Context, ref string, onProgress func(PullProgress)) error {
	image, tag := splitTag(ref)
	q := url.Values{"fromImage": {image}}
	if tag != "" {
		q.Set("tag", tag)
	}
	resp, err := c.do(ctx, http.MethodPost, "/images/create", q, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	type layer struct{ current, total int64 }
	layers := map[string]*layer{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	last := time.Time{}
	for scanner.Scan() {
		var m pullMessage
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue
		}
		if m.Error != "" {
			return errors.New(m.Error)
		}
		if m.ID != "" && m.ProgressDetail.Total > 0 && strings.HasPrefix(m.Status, "Downloading") {
			layers[m.ID] = &layer{m.ProgressDetail.Current, m.ProgressDetail.Total}
		}
		if m.ID != "" && (m.Status == "Download complete" || m.Status == "Pull complete" || m.Status == "Already exists") {
			if l, ok := layers[m.ID]; ok {
				l.current = l.total
			}
		}
		if time.Since(last) > 200*time.Millisecond {
			last = time.Now()
			var cur, total int64
			for _, l := range layers {
				cur += l.current
				total += l.total
			}
			pct := 0
			if total > 0 {
				pct = int(cur * 100 / total)
			}
			onProgress(PullProgress{Status: m.Status, Percent: pct})
		}
	}
	return scanner.Err()
}

// splitTag splits "repo:tag" without confusing a registry port for a tag.
func splitTag(ref string) (string, string) {
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

// Remove deletes a local image.
func (c *Client) Remove(ctx context.Context, ref string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	resp, err := c.do(ctx, http.MethodDelete, "/images/"+url.PathEscape(ref), q, nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PruneImages removes dangling images, or every unused image when all is set,
// and returns the bytes reclaimed.
func (c *Client) PruneImages(ctx context.Context, all bool) (int64, error) {
	filters := `{"dangling":["true"]}`
	if all {
		filters = `{"dangling":["false"]}`
	}
	resp, err := c.do(ctx, http.MethodPost, "/images/prune", url.Values{"filters": {filters}}, nil, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out.SpaceReclaimed, err
}

// PruneBuildCache removes unused build cache and returns the bytes reclaimed.
func (c *Client) PruneBuildCache(ctx context.Context) (int64, error) {
	resp, err := c.do(ctx, http.MethodPost, "/build/prune", nil, nil, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out.SpaceReclaimed, err
}

// DiskUsage summarises space used by the engine.
type DiskUsage struct {
	Images            int64 `json:"images"`
	ImagesReclaimable int64 `json:"imagesReclaimable"`
	Containers        int64 `json:"containers"`
	Volumes           int64 `json:"volumes"`
	BuildCache        int64 `json:"buildCache"`
}

// DiskUsage returns the engine's disk use.
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	var raw struct {
		Images []struct {
			Size       int64 `json:"Size"`
			Containers int64 `json:"Containers"`
		} `json:"Images"`
		Containers []struct {
			SizeRw int64 `json:"SizeRw"`
		} `json:"Containers"`
		Volumes []struct {
			UsageData struct {
				Size int64 `json:"Size"`
			} `json:"UsageData"`
		} `json:"Volumes"`
		BuildCache []struct {
			Size  int64 `json:"Size"`
			InUse bool  `json:"InUse"`
		} `json:"BuildCache"`
	}
	if err := c.getJSON(ctx, "/system/df", nil, &raw); err != nil {
		return DiskUsage{}, err
	}
	var d DiskUsage
	for _, i := range raw.Images {
		d.Images += i.Size
		if i.Containers == 0 {
			d.ImagesReclaimable += i.Size
		}
	}
	for _, ct := range raw.Containers {
		d.Containers += ct.SizeRw
	}
	for _, v := range raw.Volumes {
		if v.UsageData.Size > 0 {
			d.Volumes += v.UsageData.Size
		}
	}
	for _, b := range raw.BuildCache {
		d.BuildCache += b.Size
	}
	return d, nil
}
