// Package charts finds and downloads Helm charts: from Helm repositories over
// https, from OCI registries (oci://), and through Artifact Hub search. It
// never talks to a cluster; installing and upgrading go through the Guard.
//
// Charts are code-like input from the internet, so downloads are size-capped,
// only https and OCI sources are accepted, credentials in URLs are refused,
// and a chart archive must match the digest its repository index publishes.
package charts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
	repo "helm.sh/helm/v4/pkg/repo/v1"
	"sigs.k8s.io/yaml"
)

// Limits.
const (
	maxIndexBytes = 64 << 20 // big public repositories have large indexes
	maxChartBytes = 32 << 20
	maxAPIBytes   = 4 << 20
	indexTTL      = 10 * time.Minute
	maxVersions   = 200
)

// ArtifactHub is the public chart catalogue Kupi searches.
const ArtifactHub = "https://artifacthub.io"

var (
	chartNameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$`)
	versionRe   = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)
)

// Source names a chart: a repository (https://… or oci://host/path) and the
// chart's name in it.
type Source struct {
	Repo  string `json:"repo"`
	Chart string `json:"chart"`
}

// Label is "chart from repo", for reviews and the audit trail.
func (s Source) Label() string { return s.Chart + " from " + s.Repo }

func (s Source) isOCI() bool { return strings.HasPrefix(s.Repo, "oci://") }

// Check validates a source before anything is fetched.
func (s Source) Check() error {
	if !chartNameRe.MatchString(s.Chart) {
		return fmt.Errorf("invalid chart name %q", s.Chart)
	}
	if len(s.Repo) > 512 {
		return errors.New("the repository address is too long")
	}
	u, err := url.Parse(s.Repo)
	if err != nil {
		return fmt.Errorf("invalid repository address: %w", err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "oci":
		return errors.New("the repository address must start with https:// or oci://")
	case u.Host == "":
		return errors.New("the repository address has no host")
	case u.User != nil:
		return errors.New("don't put a user name or password in the repository address")
	case u.RawQuery != "" || u.Fragment != "":
		return errors.New("the repository address can't have a query or fragment")
	}
	return nil
}

func checkVersion(v string) error {
	if v != "" && !versionRe.MatchString(v) {
		return fmt.Errorf("invalid chart version %q", v)
	}
	return nil
}

// Version is one published version of a chart.
type Version struct {
	Version    string `json:"version"`
	AppVersion string `json:"appVersion"`
	Created    string `json:"created"`
	Deprecated bool   `json:"deprecated"`
}

// Info describes a downloaded chart.
type Info struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	AppVersion   string   `json:"appVersion"`
	Description  string   `json:"description"`
	Home         string   `json:"home"`
	Sources      []string `json:"sources"`
	Deprecated   bool     `json:"deprecated"`
	KubeVersion  string   `json:"kubeVersion"`
	Dependencies []string `json:"dependencies"`
	// Values is the chart's own values.yaml, comments included.
	Values string `json:"values"`
	// Digest is the archive's sha256 (verified against the index when the
	// repository publishes one).
	Digest string `json:"digest"`
	Source Source `json:"source"`
}

// Client fetches charts. It keeps repository indexes for a few minutes.
type Client struct {
	http *http.Client
	hub  string
	now  func() time.Time

	mu      sync.Mutex
	indexes map[string]cachedIndex
}

type cachedIndex struct {
	at  time.Time
	idx *repo.IndexFile
}

// New returns a client that uses Artifact Hub for search.
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 2 * time.Minute},
		hub:     ArtifactHub,
		now:     time.Now,
		indexes: map[string]cachedIndex{},
	}
}

// get fetches an https URL with a size cap.
func (c *Client) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("only https downloads are allowed: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kupi")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", u.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", u.Redacted(), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", u.Host, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d MB", u.Redacted(), limit>>20)
	}
	return data, nil
}

// index returns a repository's index.yaml, cached for indexTTL.
func (c *Client) index(ctx context.Context, repoURL string) (*repo.IndexFile, error) {
	base := strings.TrimSuffix(repoURL, "/")
	c.mu.Lock()
	if ci, ok := c.indexes[base]; ok && c.now().Sub(ci.at) < indexTTL {
		c.mu.Unlock()
		return ci.idx, nil
	}
	c.mu.Unlock()

	data, err := c.get(ctx, base+"/index.yaml", maxIndexBytes)
	if err != nil {
		return nil, err
	}
	idx := &repo.IndexFile{}
	if err := yaml.Unmarshal(data, idx); err != nil {
		return nil, fmt.Errorf("read the repository index: %w", err)
	}
	if idx.Entries == nil {
		return nil, errors.New("the repository index lists no charts")
	}
	idx.SortEntries()
	c.mu.Lock()
	c.indexes[base] = cachedIndex{at: c.now(), idx: idx}
	c.mu.Unlock()
	return idx, nil
}

// Versions lists a chart's versions, newest first.
func (c *Client) Versions(ctx context.Context, s Source) ([]Version, error) {
	if err := s.Check(); err != nil {
		return nil, err
	}
	var out []Version
	if s.isOCI() {
		rc, err := c.registry()
		if err != nil {
			return nil, err
		}
		tags, err := rc.Tags(ociRef(s, ""))
		if err != nil {
			return nil, fmt.Errorf("list versions: %w", err)
		}
		for _, t := range tags {
			out = append(out, Version{Version: t})
		}
	} else {
		idx, err := c.index(ctx, s.Repo)
		if err != nil {
			return nil, err
		}
		entries, ok := idx.Entries[s.Chart]
		if !ok {
			return nil, fmt.Errorf("the repository has no chart named %s", s.Chart)
		}
		for _, cv := range entries {
			if cv == nil || cv.Metadata == nil || cv.Removed {
				continue
			}
			v := Version{Version: cv.Version, AppVersion: cv.AppVersion, Deprecated: cv.Deprecated}
			if !cv.Created.IsZero() {
				v.Created = cv.Created.UTC().Format(time.RFC3339)
			}
			out = append(out, v)
		}
	}
	if len(out) > maxVersions {
		out = out[:maxVersions]
	}
	return out, nil
}

// Load downloads one version of a chart ("" for the newest) and returns it
// with a description for the UI.
func (c *Client) Load(ctx context.Context, s Source, version string) (*chartv2.Chart, Info, error) {
	if err := s.Check(); err != nil {
		return nil, Info{}, err
	}
	if err := checkVersion(version); err != nil {
		return nil, Info{}, err
	}
	var archive []byte
	var digest string
	if s.isOCI() {
		if version == "" {
			vs, err := c.Versions(ctx, s)
			if err != nil {
				return nil, Info{}, err
			}
			if len(vs) == 0 {
				return nil, Info{}, errors.New("the registry has no versions of this chart")
			}
			version = vs[0].Version
		}
		rc, err := c.registry()
		if err != nil {
			return nil, Info{}, err
		}
		res, err := rc.Pull(ociRef(s, version), registry.PullOptWithChart(true))
		if err != nil {
			return nil, Info{}, fmt.Errorf("pull the chart: %w", err)
		}
		if res.Chart == nil || len(res.Chart.Data) == 0 {
			return nil, Info{}, errors.New("the registry returned no chart")
		}
		if len(res.Chart.Data) > maxChartBytes {
			return nil, Info{}, fmt.Errorf("the chart is larger than %d MB", maxChartBytes>>20)
		}
		archive, digest = res.Chart.Data, strings.TrimPrefix(res.Chart.Digest, "sha256:")
	} else {
		idx, err := c.index(ctx, s.Repo)
		if err != nil {
			return nil, Info{}, err
		}
		cv, err := idx.Get(s.Chart, version)
		if err != nil {
			return nil, Info{}, fmt.Errorf("the repository has no %s %s", s.Chart, version)
		}
		if len(cv.URLs) == 0 {
			return nil, Info{}, errors.New("the repository index gives no download address")
		}
		chartURL, err := resolve(s.Repo, cv.URLs[0])
		if err != nil {
			return nil, Info{}, err
		}
		if archive, err = c.get(ctx, chartURL, maxChartBytes); err != nil {
			return nil, Info{}, err
		}
		sum := sha256.Sum256(archive)
		digest = hex.EncodeToString(sum[:])
		if cv.Digest != "" && !strings.EqualFold(strings.TrimPrefix(cv.Digest, "sha256:"), digest) {
			return nil, Info{}, errors.New("the downloaded chart doesn't match the digest in the repository index; it may have been tampered with")
		}
	}

	ch, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, Info{}, fmt.Errorf("read the chart: %w", err)
	}
	if ch.Metadata == nil || ch.Metadata.Name != s.Chart {
		return nil, Info{}, fmt.Errorf("the archive holds a different chart than %s", s.Chart)
	}
	return ch, describe(ch, s, digest), nil
}

func describe(ch *chartv2.Chart, s Source, digest string) Info {
	md := ch.Metadata
	info := Info{
		Name: md.Name, Version: md.Version, AppVersion: md.AppVersion, Description: md.Description,
		Home: md.Home, Sources: md.Sources, Deprecated: md.Deprecated, KubeVersion: md.KubeVersion,
		Digest: digest, Source: s,
	}
	if info.Sources == nil {
		info.Sources = []string{}
	}
	info.Dependencies = []string{}
	for _, d := range md.Dependencies {
		info.Dependencies = append(info.Dependencies, strings.TrimSpace(d.Name+" "+d.Version))
	}
	for _, f := range ch.Raw {
		if f.Name == "values.yaml" {
			info.Values = string(f.Data)
		}
	}
	return info
}

// resolve turns an index URL (often relative) into an absolute https URL.
func resolve(repoURL, ref string) (string, error) {
	base, err := url.Parse(strings.TrimSuffix(repoURL, "/") + "/")
	if err != nil {
		return "", err
	}
	u, err := base.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid chart address in the index: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("the index points to a non-https download (%s)", u.Scheme)
	}
	return u.String(), nil
}

func ociRef(s Source, version string) string {
	ref := strings.TrimSuffix(strings.TrimPrefix(s.Repo, "oci://"), "/") + "/" + s.Chart
	if version != "" {
		ref += ":" + version
	}
	return ref
}

func (c *Client) registry() (*registry.Client, error) {
	return registry.NewClient(registry.ClientOptHTTPClient(c.http), registry.ClientOptWriter(io.Discard))
}

// Package is one Artifact Hub search result.
type Package struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	AppVersion        string `json:"appVersion"`
	Description       string `json:"description"`
	Deprecated        bool   `json:"deprecated"`
	Signed            bool   `json:"signed"`
	Stars             int    `json:"stars"`
	RepoName          string `json:"repoName"`
	RepoDisplay       string `json:"repoDisplay"`
	RepoURL           string `json:"repoUrl"`
	VerifiedPublisher bool   `json:"verifiedPublisher"`
	Official          bool   `json:"official"`
	// Page is the chart's Artifact Hub page.
	Page string `json:"page"`
}

// Search asks Artifact Hub for Helm charts matching query.
func (c *Client) Search(ctx context.Context, query string) ([]Package, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 100 {
		return nil, errors.New("type 1 to 100 characters to search")
	}
	q := url.Values{"ts_query_web": {query}, "kind": {"0"}, "limit": {"30"}, "offset": {"0"}, "facets": {"false"}, "sort": {"relevance"}}
	data, err := c.get(ctx, c.hub+"/api/v1/packages/search?"+q.Encode(), maxAPIBytes)
	if err != nil {
		return nil, err
	}
	var res struct {
		Packages []struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			AppVersion  string `json:"app_version"`
			Description string `json:"description"`
			Deprecated  bool   `json:"deprecated"`
			Signed      bool   `json:"signed"`
			Stars       int    `json:"stars"`
			Official    bool   `json:"official"`
			Repository  struct {
				Name              string `json:"name"`
				DisplayName       string `json:"display_name"`
				URL               string `json:"url"`
				VerifiedPublisher bool   `json:"verified_publisher"`
				Official          bool   `json:"official"`
			} `json:"repository"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("read Artifact Hub's answer: %w", err)
	}
	out := make([]Package, 0, len(res.Packages))
	for _, p := range res.Packages {
		pkg := Package{
			Name: p.Name, Version: p.Version, AppVersion: p.AppVersion, Description: p.Description,
			Deprecated: p.Deprecated, Signed: p.Signed, Stars: p.Stars,
			RepoName: p.Repository.Name, RepoDisplay: p.Repository.DisplayName, RepoURL: p.Repository.URL,
			VerifiedPublisher: p.Repository.VerifiedPublisher, Official: p.Official || p.Repository.Official,
			Page: c.hub + "/packages/helm/" + url.PathEscape(p.Repository.Name) + "/" + url.PathEscape(p.Name),
		}
		// Only results Kupi can fetch.
		if (Source{Repo: pkg.RepoURL, Chart: pkg.Name}).Check() != nil {
			continue
		}
		out = append(out, pkg)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Deprecated != out[j].Deprecated {
			return !out[i].Deprecated
		}
		return false
	})
	return out, nil
}
