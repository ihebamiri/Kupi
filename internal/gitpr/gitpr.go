// Package gitpr proposes changes to GitOps-managed objects as GitHub pull
// requests. Argo CD and Flux revert edits made directly in the cluster, so
// the lasting way to change such an object is to change its file in Git.
//
// Prepare finds the YAML document that defines the object under the app's
// path and builds the edited file; Open creates a branch, commits the file
// and opens the pull request. Only GitHub is supported, through its REST
// API with a token the user saves in Kupi's vault.
package gitpr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/diff"
)

// GitHubAPI is GitHub's REST API.
const GitHubAPI = "https://api.github.com"

// Limits on what Kupi reads from a repository.
const (
	maxFiles     = 400
	maxFileBytes = 1 << 20
	maxAPIBytes  = 16 << 20
)

// ErrUnsupported means the repository isn't on GitHub.
var ErrUnsupported = errors.New("pull requests are only supported for GitHub repositories for now")

// Repo is a GitHub repository.
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

var (
	ghPathRe  = regexp.MustCompile(`^/?([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100}?)(\.git)?/?$`)
	ghSSHRe   = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100}?)(\.git)?$`)
	shaLikeRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// ParseGitHub reads a repository address: https://github.com/org/repo,
// with or without .git, or the SSH forms git@github.com:org/repo.git and
// ssh://git@github.com/org/repo.git.
func ParseGitHub(repoURL string) (Repo, error) {
	repoURL = strings.TrimSpace(repoURL)
	if m := ghSSHRe.FindStringSubmatch(repoURL); m != nil {
		return Repo{Owner: m[1], Name: m[2]}, nil
	}
	u, err := url.Parse(repoURL)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return Repo{}, ErrUnsupported
	}
	m := ghPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return Repo{}, fmt.Errorf("can't read a GitHub repository from %s", repoURL)
	}
	return Repo{Owner: m[1], Name: m[2]}, nil
}

// ObjectID names the object to find.
type ObjectID struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
}

// Proposal is a prepared change: the file, its new content and a diff.
type Proposal struct {
	Repo     Repo     `json:"repo"`
	Base     string   `json:"base"`
	File     string   `json:"file"`
	Diff     string   `json:"diff"`
	Before   string   `json:"-"`
	After    string   `json:"-"`
	FileSHA  string   `json:"-"`
	BaseSHA  string   `json:"-"`
	FileURL  string   `json:"fileUrl"`
	Warnings []string `json:"warnings"`
}

// Client talks to GitHub.
type Client struct {
	http *http.Client
	api  string
	now  func() time.Time
}

// New returns a GitHub client.
func New() *Client {
	return &Client{http: &http.Client{Timeout: time.Minute}, api: GitHubAPI, now: time.Now}
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("GitHub: %s (%d)", e.message, e.status) }

func (c *Client) call(ctx context.Context, token, method, p string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+p, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "kupi")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Message
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			msg = "the saved token was refused; save a new one"
		case http.StatusForbidden, http.StatusNotFound:
			if msg == "" {
				msg = "not found, or the token can't see this repository"
			}
		}
		return &apiError{status: resp.StatusCode, message: msg}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// User returns the login a token belongs to, to check it works.
func (c *Client) User(ctx context.Context, token string) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.call(ctx, token, http.MethodGet, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

func repoPath(r Repo) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

// Prepare finds the object's document under dir in the repository and
// builds the file with newDoc in its place. branch is the branch the app
// follows; tags, commits and HEAD fall back to the default branch.
func (c *Client) Prepare(ctx context.Context, token, repoURL, branch, dir string, id ObjectID, newDoc string) (*Proposal, error) {
	repo, err := ParseGitHub(repoURL)
	if err != nil {
		return nil, err
	}
	var info struct {
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
	}
	if err := c.call(ctx, token, http.MethodGet, repoPath(repo), nil, &info); err != nil {
		return nil, err
	}
	p := &Proposal{Repo: repo, Warnings: []string{}}
	base := branchOf(branch)
	if base == "" {
		base = info.DefaultBranch
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.call(ctx, token, http.MethodGet, repoPath(repo)+"/git/ref/heads/"+url.PathEscape(base), nil, &ref); err != nil {
		if base == info.DefaultBranch {
			return nil, err
		}
		p.Warnings = append(p.Warnings, fmt.Sprintf("The app follows %q, which isn't a branch; the pull request goes to %s.", branch, info.DefaultBranch))
		base = info.DefaultBranch
		if err := c.call(ctx, token, http.MethodGet, repoPath(repo)+"/git/ref/heads/"+url.PathEscape(base), nil, &ref); err != nil {
			return nil, err
		}
	}
	p.Base, p.BaseSHA = base, ref.Object.SHA

	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int    `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := c.call(ctx, token, http.MethodGet, repoPath(repo)+"/git/trees/"+p.BaseSHA+"?recursive=1", nil, &tree); err != nil {
		return nil, err
	}
	prefix := strings.Trim(path.Clean("/"+dir), "/")
	scanned := 0
	for _, e := range tree.Tree {
		if e.Type != "blob" || e.Size > maxFileBytes || !isYAML(e.Path) || !under(e.Path, prefix) {
			continue
		}
		if scanned++; scanned > maxFiles {
			p.Warnings = append(p.Warnings, fmt.Sprintf("Only the first %d YAML files under %s were searched.", maxFiles, dirLabel(prefix)))
			break
		}
		var blob struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		if err := c.call(ctx, token, http.MethodGet, repoPath(repo)+"/git/blobs/"+e.SHA, nil, &blob); err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
		if err != nil {
			continue
		}
		after, ok := replaceDocument(string(raw), id, newDoc)
		if !ok {
			continue
		}
		p.File, p.FileSHA, p.Before, p.After = e.Path, e.SHA, string(raw), after
		p.Diff = diff.Unified("a/"+e.Path, "b/"+e.Path, p.Before, p.After, 3)
		p.FileURL = info.HTMLURL + "/blob/" + url.PathEscape(base) + "/" + e.Path
		if p.Diff == "" {
			return nil, errors.New("the file in Git already matches this object; nothing to propose")
		}
		return p, nil
	}
	return nil, fmt.Errorf("no YAML file under %s in %s defines %s %s. It may come from a Helm chart or a Kustomize base outside that folder", dirLabel(prefix), repo, id.Kind, id.Name)
}

// Open creates a branch from the base, commits the edited file to it and
// opens a pull request. It returns the pull request's web address.
func (c *Client) Open(ctx context.Context, token string, p *Proposal, id ObjectID, title, body string) (string, error) {
	branch := fmt.Sprintf("kupi/%s-%s-%s", slug(id.Kind), slug(id.Name), c.now().UTC().Format("20060102-150405"))
	if err := c.call(ctx, token, http.MethodPost, repoPath(p.Repo)+"/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": p.BaseSHA}, nil); err != nil {
		return "", fmt.Errorf("create the branch: %w", err)
	}
	put := map[string]string{
		"message": title,
		"content": base64.StdEncoding.EncodeToString([]byte(p.After)),
		"sha":     p.FileSHA,
		"branch":  branch,
	}
	if err := c.call(ctx, token, http.MethodPut, repoPath(p.Repo)+"/contents/"+escapePath(p.File), put, nil); err != nil {
		return "", fmt.Errorf("commit the change: %w", err)
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := c.call(ctx, token, http.MethodPost, repoPath(p.Repo)+"/pulls", map[string]string{"title": title, "head": branch, "base": p.Base, "body": body}, &pr); err != nil {
		return "", fmt.Errorf("open the pull request: %w", err)
	}
	return pr.HTMLURL, nil
}

// branchOf turns what an app follows into a branch name: Flux revisions look
// like "main@sha1:abc…"; HEAD, tags and commits mean "use the default".
func branchOf(target string) string {
	target = strings.TrimSpace(target)
	if b, _, found := strings.Cut(target, "@sha"); found {
		target = b
	}
	target = strings.TrimPrefix(target, "refs/heads/")
	if target == "" || target == "HEAD" || shaLikeRe.MatchString(target) || strings.HasPrefix(target, "refs/tags/") || strings.HasPrefix(target, "v") && strings.Count(target, ".") >= 1 {
		return ""
	}
	return target
}

func isYAML(p string) bool {
	return strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
}

func under(p, prefix string) bool {
	return prefix == "" || p == prefix || strings.HasPrefix(p, prefix+"/")
}

func dirLabel(prefix string) string {
	if prefix == "" {
		return "the repository root"
	}
	return prefix
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

var separatorRe = regexp.MustCompile(`^---(\s.*)?$`)

// replaceDocument swaps the YAML document that defines id for newDoc,
// keeping every other document, comment and separator as it was.
func replaceDocument(file string, id ObjectID, newDoc string) (string, bool) {
	lines := strings.SplitAfter(file, "\n")
	type doc struct{ start, end int } // line range [start, end)
	var docs []doc
	start := 0
	for i, l := range lines {
		if separatorRe.MatchString(strings.TrimRight(l, "\r\n")) {
			docs = append(docs, doc{start, i})
			start = i + 1
		}
	}
	docs = append(docs, doc{start, len(lines)})
	for _, d := range docs {
		text := strings.Join(lines[d.start:d.end], "")
		var m struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if yaml.Unmarshal([]byte(text), &m) != nil || m.Kind != id.Kind || m.Metadata.Name != id.Name {
			continue
		}
		if m.Metadata.Namespace != "" && id.Namespace != "" && m.Metadata.Namespace != id.Namespace {
			continue
		}
		if group(m.APIVersion) != group(id.APIVersion) {
			continue
		}
		replacement := strings.TrimRight(newDoc, "\n") + "\n"
		// Keep comments that open the document (licence headers, notes).
		lead := ""
		for _, l := range lines[d.start:d.end] {
			t := strings.TrimSpace(l)
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			lead += l
		}
		var b strings.Builder
		b.WriteString(strings.Join(lines[:d.start], ""))
		b.WriteString(lead)
		b.WriteString(replacement)
		b.WriteString(strings.Join(lines[d.end:], ""))
		return b.String(), true
	}
	return "", false
}

func group(apiVersion string) string {
	g, _, found := strings.Cut(apiVersion, "/")
	if !found {
		return ""
	}
	return g
}
