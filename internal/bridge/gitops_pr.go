package bridge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/audit"
	"github.com/ihebamiri/kupi/internal/gitops"
	"github.com/ihebamiri/kupi/internal/gitpr"
	"github.com/ihebamiri/kupi/internal/validate"
)

// The GitHub token lives in the vault, like registry logins.
const (
	githubItem  = "github-token"
	proposalTTL = 15 * time.Minute
	maxPRTitle  = 200
	maxPRBody   = 10000
)

var (
	githubTokenRe = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)
	proposalIDRe  = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
)

// GitHubStatus says whether Kupi can open pull requests.
type GitHubStatus struct {
	Connected   bool   `json:"connected"`
	Login       string `json:"login"`
	VaultLocked bool   `json:"vaultLocked"`
}

type storedProposal struct {
	p       *gitpr.Proposal
	id      gitpr.ObjectID
	context string
	expires time.Time
}

type proposalStore struct {
	mu    sync.Mutex
	items map[string]*storedProposal
}

func (s *proposalStore) put(sp *storedProposal) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.items {
		if now.After(v.expires) {
			delete(s.items, k)
		}
	}
	id := randomID()
	s.items[id] = sp
	return id
}

func (s *proposalStore) take(id string) (*storedProposal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.items[id]
	delete(s.items, id)
	if !ok || time.Now().After(sp.expires) {
		return nil, false
	}
	return sp, true
}

// GitHubStatus reports the saved token's account. The vault must be unlocked.
func (s *GitOpsService) GitHubStatus(ctx context.Context) GitHubStatus {
	vals, err := s.d.Vault.Values(githubItem)
	if err != nil {
		return GitHubStatus{VaultLocked: !s.d.Vault.Status().Unlocked}
	}
	return GitHubStatus{Connected: vals["token"] != "", Login: vals["login"]}
}

// SaveGitHubToken checks a token with GitHub and keeps it in the vault.
func (s *GitOpsService) SaveGitHubToken(ctx context.Context, token string) (GitHubStatus, error) {
	token = strings.TrimSpace(token)
	if !githubTokenRe.MatchString(token) {
		return GitHubStatus{}, errors.New("that doesn't look like a GitHub token (ghp_… or github_pat_…)")
	}
	login, err := s.d.Git.User(ctx, token)
	if err != nil {
		return GitHubStatus{}, err
	}
	if err := s.d.Vault.Put(githubItem, "GitHub token for pull requests", map[string]string{"token": token, "login": login}, nil); err != nil {
		return GitHubStatus{}, vaultErr(err)
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "github-token-saved", Resource: login, Outcome: audit.OutcomeInfo})
	return GitHubStatus{Connected: true, Login: login}, nil
}

// ForgetGitHubToken removes the saved token.
func (s *GitOpsService) ForgetGitHubToken() error {
	if err := s.d.Vault.Delete(githubItem); err != nil {
		return vaultErr(err)
	}
	_, _ = s.d.Audit.Append(audit.Entry{Action: "github-token-deleted", Outcome: audit.OutcomeInfo})
	return nil
}

// GitOpsOwner is the app that manages an object, with where it deploys from.
type GitOpsOwner struct {
	Owner      gitops.Owner `json:"owner"`
	RepoURL    string       `json:"repoUrl"`
	Path       string       `json:"path"`
	Target     string       `json:"target"`
	CanPropose bool         `json:"canPropose"`
	// Why explains when a pull request can't be proposed.
	Why string `json:"why"`
}

// GitSource returns the Argo CD or Flux app managing an object and where it
// deploys from, or nil when no app manages it.
func (s *GitOpsService) GitSource(ctx context.Context, contextName string, ref ResourceRef) (*GitOpsOwner, error) {
	req, err := targetRequest(contextName, ref)
	if err != nil {
		return nil, err
	}
	sess, err := s.d.session(contextName)
	if err != nil {
		return nil, err
	}
	obj, err := sess.Reader().Get(ctx, req.GVR, ref.Namespace, ref.Name)
	if err != nil {
		return nil, err
	}
	o := gitops.OwnerOf(obj)
	if o == nil {
		return nil, nil
	}
	out := &GitOpsOwner{Owner: *o}
	app, err := gitops.Find(ctx, sess.Reader(), o)
	if err != nil {
		out.Why = fmt.Sprintf("Kupi can't read the %s %s that manages it.", o.Kind, o.Name)
		return out, nil
	}
	out.RepoURL, out.Path, out.Target = app.RepoURL, app.Path, app.Target
	switch {
	case obj.GetKind() == "Secret":
		out.Why = "Secrets don't belong in Git as plain text. Use Sealed Secrets, SOPS or External Secrets instead."
	case o.Kind == "HelmRelease":
		out.Why = "This object comes from a Helm chart; change the HelmRelease's values in Git instead."
	default:
		if _, err := gitpr.ParseGitHub(app.RepoURL); err != nil {
			out.Why = err.Error() + "."
		} else {
			out.CanPropose = true
		}
	}
	return out, nil
}

// ProposalView is a prepared pull request, shown before it's opened.
type ProposalView struct {
	ID       string   `json:"id"`
	Repo     string   `json:"repo"`
	Base     string   `json:"base"`
	File     string   `json:"file"`
	FileURL  string   `json:"fileUrl"`
	Diff     string   `json:"diff"`
	Warnings []string `json:"warnings"`
	Title    string   `json:"title"`
}

// ProposeChange finds the object's file in its app's Git repository and
// prepares a pull request that puts the edited manifest there. Nothing is
// sent to GitHub but reads until OpenPullRequest.
func (s *GitOpsService) ProposeChange(ctx context.Context, contextName string, ref ResourceRef, manifest string) (ProposalView, error) {
	if err := validate.Manifest(manifest); err != nil {
		return ProposalView{}, err
	}
	owner, err := s.GitSource(ctx, contextName, ref)
	if err != nil {
		return ProposalView{}, err
	}
	if owner == nil {
		return ProposalView{}, errors.New("Argo CD or Flux doesn't manage this object; apply the change directly")
	}
	if !owner.CanPropose {
		return ProposalView{}, errors.New(owner.Why)
	}
	obj, err := parseSingleObject(manifest)
	if err != nil {
		return ProposalView{}, err
	}
	if obj.GetName() != ref.Name || (obj.GetNamespace() != "" && obj.GetNamespace() != ref.Namespace) {
		return ProposalView{}, errors.New("the edited manifest names a different object")
	}
	if obj.GetKind() == "Secret" {
		return ProposalView{}, errors.New("Secrets don't belong in Git as plain text")
	}
	doc, err := gitDocument(obj)
	if err != nil {
		return ProposalView{}, err
	}
	token, err := s.token()
	if err != nil {
		return ProposalView{}, err
	}
	id := gitpr.ObjectID{APIVersion: obj.GetAPIVersion(), Kind: obj.GetKind(), Namespace: ref.Namespace, Name: ref.Name}
	p, err := s.d.Git.Prepare(ctx, token, owner.RepoURL, owner.Target, owner.Path, id, doc)
	if err != nil {
		return ProposalView{}, err
	}
	pid := s.proposals.put(&storedProposal{p: p, id: id, context: contextName, expires: time.Now().Add(proposalTTL)})
	warnings := append([]string{}, p.Warnings...)
	return ProposalView{
		ID: pid, Repo: p.Repo.String(), Base: p.Base, File: p.File, FileURL: p.FileURL, Diff: p.Diff, Warnings: warnings,
		Title: fmt.Sprintf("Update %s %s", obj.GetKind(), ref.Name),
	}, nil
}

// OpenPullRequest opens a prepared proposal on GitHub and returns its address.
func (s *GitOpsService) OpenPullRequest(ctx context.Context, id, title, body string) (string, error) {
	if !proposalIDRe.MatchString(id) {
		return "", errors.New("invalid proposal")
	}
	title = strings.TrimSpace(title)
	if title == "" || len(title) > maxPRTitle || strings.ContainsAny(title, "\r\n") {
		return "", fmt.Errorf("give the pull request a one-line title (up to %d characters)", maxPRTitle)
	}
	if len(body) > maxPRBody {
		return "", fmt.Errorf("the description is limited to %d characters", maxPRBody)
	}
	sp, ok := s.proposals.take(id)
	if !ok {
		return "", errors.New("this proposal expired; prepare it again")
	}
	token, err := s.token()
	if err != nil {
		return "", err
	}
	if body == "" {
		body = "Proposed with Kupi."
	}
	url, err := s.d.Git.Open(ctx, token, sp.p, sp.id, title, body)
	resource := sp.id.Kind + " " + sp.id.Namespace + "/" + sp.id.Name
	entry := audit.Entry{Context: sp.context, Action: "gitops-pr-opened", Resource: resource, Reason: url, Outcome: audit.OutcomeInfo}
	if err != nil {
		entry.Outcome, entry.Error, entry.Reason = audit.OutcomeFailed, err.Error(), ""
	}
	_, _ = s.d.Audit.Append(entry)
	return url, err
}

func (s *GitOpsService) token() (string, error) {
	vals, err := s.d.Vault.Values(githubItem)
	if err != nil {
		if !s.d.Vault.Status().Unlocked {
			return "", errors.New("unlock the vault first: the GitHub token is kept in it")
		}
		return "", errors.New("save a GitHub token first (GitOps → GitHub)")
	}
	if vals["token"] == "" {
		return "", errors.New("save a GitHub token first (GitOps → GitHub)")
	}
	return vals["token"], nil
}

// gitDocument renders an object the way it belongs in Git: without status
// or anything the cluster adds.
func gitDocument(obj *unstructured.Unstructured) (string, error) {
	o := obj.DeepCopy()
	for _, f := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "selfLink"} {
		unstructured.RemoveNestedField(o.Object, "metadata", f)
	}
	unstructured.RemoveNestedField(o.Object, "status")
	for _, k := range []string{"kubectl.kubernetes.io/last-applied-configuration", "deployment.kubernetes.io/revision"} {
		unstructured.RemoveNestedField(o.Object, "metadata", "annotations", k)
	}
	// Tracking labels and annotations are added by Argo CD and Flux at sync.
	for _, k := range []string{"argocd.argoproj.io/instance", "kustomize.toolkit.fluxcd.io/name", "kustomize.toolkit.fluxcd.io/namespace"} {
		unstructured.RemoveNestedField(o.Object, "metadata", "labels", k)
	}
	unstructured.RemoveNestedField(o.Object, "metadata", "annotations", "argocd.argoproj.io/tracking-id")
	for _, f := range []string{"labels", "annotations"} {
		if m, found, _ := unstructured.NestedMap(o.Object, "metadata", f); found && len(m) == 0 {
			unstructured.RemoveNestedField(o.Object, "metadata", f)
		}
	}
	out, err := yaml.Marshal(o.Object)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
