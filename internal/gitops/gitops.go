// Package gitops reads Argo CD and Flux: which applications exist, whether
// they are in sync with Git, and which of them owns a given object. Live
// edits to objects these tools own are reverted at the next sync, so Kupi
// points people at the source instead.
package gitops

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Tools.
const (
	ArgoCD = "Argo CD"
	Flux   = "Flux"
)

// Types of the objects Kupi reads, by kind.
var (
	ApplicationGVR   = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	KustomizationGVR = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	HelmReleaseGVR   = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	sources          = map[string]schema.GroupVersionResource{
		"GitRepository":  {Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"},
		"OCIRepository":  {Group: "source.toolkit.fluxcd.io", Version: "v1beta2", Resource: "ocirepositories"},
		"HelmRepository": {Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmrepositories"},
		"Bucket":         {Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "buckets"},
	}
)

// GVRFor returns the type of an application kind.
func GVRFor(kind string) (schema.GroupVersionResource, bool) {
	switch kind {
	case "Application":
		return ApplicationGVR, true
	case "Kustomization":
		return KustomizationGVR, true
	case "HelmRelease":
		return HelmReleaseGVR, true
	}
	return schema.GroupVersionResource{}, false
}

// App is an Argo CD Application, Flux Kustomization or Flux HelmRelease.
type App struct {
	Tool      string `json:"tool"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Source is what the app deploys from, in words (repo, path, chart).
	Source string `json:"source"`
	// RepoURL is a web address for the repository when Kupi can work one out.
	RepoURL string `json:"repoUrl"`
	Path    string `json:"path"`
	// Target is the branch, tag or version the app follows; Revision is the
	// one last applied.
	Target   string `json:"target"`
	Revision string `json:"revision"`
	// Sync is Synced, OutOfSync or Unknown (Argo CD), or Ready, NotReady or
	// Unknown (Flux).
	Sync      string `json:"sync"`
	Health    string `json:"health"`
	Message   string `json:"message"`
	Suspended bool   `json:"suspended"`
	AutoSync  bool   `json:"autoSync"`
	// Operation is Argo CD's running or last operation phase.
	Operation   string `json:"operation"`
	LastSync    string `json:"lastSync"`
	Destination string `json:"destination"`
	Resources   int    `json:"resources"`
	OutOfSync   int    `json:"outOfSync"`
}

// Overview lists the GitOps apps in a cluster.
type Overview struct {
	Apps []App `json:"apps"`
	// Installed names the tools whose resource types the cluster serves.
	Installed []string `json:"installed"`
	Skipped   []string `json:"skipped"`
}

// List reads every Argo CD and Flux app the user can see.
func List(ctx context.Context, r kube.Reader) (*Overview, error) {
	ov := &Overview{Apps: []App{}, Installed: []string{}, Skipped: []string{}}
	list := func(what string, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, bool) {
		l, err := r.List(ctx, gvr, "", metav1.ListOptions{Limit: 2000})
		switch {
		case apierrors.IsNotFound(err):
			return nil, false
		case apierrors.IsForbidden(err):
			ov.Skipped = append(ov.Skipped, what+" (no permission to list)")
			return nil, true
		case err != nil:
			ov.Skipped = append(ov.Skipped, what+" ("+err.Error()+")")
			return nil, true
		}
		return l.Items, true
	}
	if items, ok := list("Argo CD Applications", ApplicationGVR); ok {
		ov.Installed = append(ov.Installed, ArgoCD)
		for i := range items {
			ov.Apps = append(ov.Apps, argoApp(&items[i]))
		}
	}
	kustomizations, kOK := list("Flux Kustomizations", KustomizationGVR)
	releases, hOK := list("Flux HelmReleases", HelmReleaseGVR)
	if kOK || hOK {
		ov.Installed = append(ov.Installed, Flux)
		repos := map[string]string{}
		for kind, gvr := range sources {
			items, _ := list("Flux "+kind+"s", gvr)
			for _, s := range items {
				url, _, _ := unstructured.NestedString(s.Object, "spec", "url")
				repos[kind+"/"+s.GetNamespace()+"/"+s.GetName()] = url
			}
		}
		for i := range kustomizations {
			ov.Apps = append(ov.Apps, fluxKustomization(&kustomizations[i], repos))
		}
		for i := range releases {
			ov.Apps = append(ov.Apps, fluxHelmRelease(&releases[i], repos))
		}
	}
	sort.SliceStable(ov.Apps, func(i, j int) bool {
		a, b := ov.Apps[i], ov.Apps[j]
		if ra, rb := attention(a), attention(b); ra != rb {
			return ra
		}
		return a.Tool+a.Namespace+a.Name < b.Tool+b.Namespace+b.Name
	})
	return ov, nil
}

// attention is true for apps that need a look.
func attention(a App) bool {
	switch {
	case a.Sync == "OutOfSync" || a.Sync == "NotReady":
		return true
	case a.Health == "Degraded" || a.Health == "Missing":
		return true
	case a.Operation == "Failed" || a.Operation == "Error":
		return true
	}
	return false
}

func str(o *unstructured.Unstructured, path ...string) string {
	v, _, _ := unstructured.NestedString(o.Object, path...)
	return v
}

func argoApp(o *unstructured.Unstructured) App {
	a := App{Tool: ArgoCD, Kind: "Application", Namespace: o.GetNamespace(), Name: o.GetName()}
	src, _, _ := unstructured.NestedMap(o.Object, "spec", "source")
	if src == nil {
		if list, _, _ := unstructured.NestedSlice(o.Object, "spec", "sources"); len(list) > 0 {
			src, _ = list[0].(map[string]interface{})
		}
	}
	repo, _ := src["repoURL"].(string)
	a.Path, _ = src["path"].(string)
	a.Target, _ = src["targetRevision"].(string)
	chart, _ := src["chart"].(string)
	a.Source = describeSource(repo, a.Path, chart)
	a.RepoURL = BrowseURL(repo)
	dest := str(o, "spec", "destination", "server")
	if dest == "" {
		dest = str(o, "spec", "destination", "name")
	}
	if ns := str(o, "spec", "destination", "namespace"); ns != "" {
		dest = strings.TrimSpace(dest + " / " + ns)
	}
	a.Destination = dest
	_, a.AutoSync, _ = unstructured.NestedMap(o.Object, "spec", "syncPolicy", "automated")
	a.Sync = str(o, "status", "sync", "status")
	a.Revision = str(o, "status", "sync", "revision")
	a.Health = str(o, "status", "health", "status")
	a.Operation = str(o, "status", "operationState", "phase")
	a.Message = str(o, "status", "operationState", "message")
	if conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions"); len(conds) > 0 {
		if c, ok := conds[0].(map[string]interface{}); ok {
			if msg, _ := c["message"].(string); msg != "" {
				a.Message = msg
			}
		}
	}
	a.LastSync = str(o, "status", "operationState", "finishedAt")
	if a.LastSync == "" {
		a.LastSync = str(o, "status", "reconciledAt")
	}
	res, _, _ := unstructured.NestedSlice(o.Object, "status", "resources")
	a.Resources = len(res)
	for _, r := range res {
		if rm, ok := r.(map[string]interface{}); ok && rm["status"] == "OutOfSync" {
			a.OutOfSync++
		}
	}
	return a
}

func fluxReady(o *unstructured.Unstructured, a *App) {
	a.Sync, a.Health = "Unknown", "Unknown"
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, _ := c.(map[string]interface{})
		if cm["type"] != "Ready" {
			continue
		}
		switch cm["status"] {
		case "True":
			a.Sync, a.Health = "Ready", "Healthy"
		case "False":
			a.Sync, a.Health = "NotReady", "Degraded"
		default:
			a.Health = "Progressing"
		}
		a.Message, _ = cm["message"].(string)
		a.LastSync, _ = cm["lastTransitionTime"].(string)
	}
	a.Suspended, _, _ = unstructured.NestedBool(o.Object, "spec", "suspend")
	a.AutoSync = !a.Suspended
}

func fluxKustomization(o *unstructured.Unstructured, repos map[string]string) App {
	a := App{Tool: Flux, Kind: "Kustomization", Namespace: o.GetNamespace(), Name: o.GetName()}
	fluxReady(o, &a)
	kind, name, ns := str(o, "spec", "sourceRef", "kind"), str(o, "spec", "sourceRef", "name"), str(o, "spec", "sourceRef", "namespace")
	if ns == "" {
		ns = a.Namespace
	}
	repo := repos[kind+"/"+ns+"/"+name]
	a.Path = str(o, "spec", "path")
	a.Source = describeSource(repo, a.Path, "")
	if repo == "" {
		a.Source = fmt.Sprintf("%s %s/%s, path %s", kind, ns, name, a.Path)
	}
	a.RepoURL = BrowseURL(repo)
	a.Revision = str(o, "status", "lastAppliedRevision")
	a.Target = str(o, "status", "lastAttemptedRevision")
	a.Destination = str(o, "spec", "targetNamespace")
	entries, _, _ := unstructured.NestedSlice(o.Object, "status", "inventory", "entries")
	a.Resources = len(entries)
	return a
}

func fluxHelmRelease(o *unstructured.Unstructured, repos map[string]string) App {
	a := App{Tool: Flux, Kind: "HelmRelease", Namespace: o.GetNamespace(), Name: o.GetName()}
	fluxReady(o, &a)
	chart := str(o, "spec", "chart", "spec", "chart")
	kind, name, ns := str(o, "spec", "chart", "spec", "sourceRef", "kind"), str(o, "spec", "chart", "spec", "sourceRef", "name"), str(o, "spec", "chart", "spec", "sourceRef", "namespace")
	if chart == "" {
		kind, name, ns = str(o, "spec", "chartRef", "kind"), str(o, "spec", "chartRef", "name"), str(o, "spec", "chartRef", "namespace")
	}
	if ns == "" {
		ns = a.Namespace
	}
	repo := repos[kind+"/"+ns+"/"+name]
	a.Source = describeSource(repo, "", chart)
	a.RepoURL = BrowseURL(repo)
	a.Target = str(o, "spec", "chart", "spec", "version")
	a.Revision = str(o, "status", "lastAttemptedRevision")
	if hist, _, _ := unstructured.NestedSlice(o.Object, "status", "history"); len(hist) > 0 {
		if h, ok := hist[0].(map[string]interface{}); ok {
			if v, _ := h["chartVersion"].(string); v != "" {
				a.Revision = v
			}
		}
	}
	a.Destination = str(o, "spec", "targetNamespace")
	return a
}

func describeSource(repo, path, chart string) string {
	parts := []string{}
	if repo != "" {
		parts = append(parts, repo)
	}
	if chart != "" {
		parts = append(parts, "chart "+chart)
	}
	if path != "" && path != "." && path != "./" {
		parts = append(parts, "path "+path)
	}
	return strings.Join(parts, ", ")
}

var scpLikeRe = regexp.MustCompile(`^git@([A-Za-z0-9.-]+):([A-Za-z0-9._/-]+?)(\.git)?$`)

// BrowseURL turns a Git or Helm repository address into a web address the
// user can open, or "" when there's no safe one. Only https is returned.
func BrowseURL(repo string) string {
	repo = strings.TrimSpace(repo)
	if m := scpLikeRe.FindStringSubmatch(repo); m != nil {
		return "https://" + m[1] + "/" + m[2]
	}
	for _, prefix := range []string{"ssh://git@", "git+ssh://git@"} {
		if rest, ok := strings.CutPrefix(repo, prefix); ok {
			host, path, _ := strings.Cut(rest, "/")
			host, _, _ = strings.Cut(host, ":")
			return "https://" + host + "/" + strings.TrimSuffix(path, ".git")
		}
	}
	if strings.HasPrefix(repo, "https://") && !strings.ContainsAny(repo, " \"'<>") {
		return strings.TrimSuffix(repo, ".git")
	}
	return ""
}

// Owner is the GitOps app that manages an object.
type Owner struct {
	Tool      string `json:"tool"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// OwnerOf reads the labels and annotations Argo CD and Flux put on the
// objects they manage. Argo CD's namespace isn't recorded on the object,
// so it is left empty unless the app runs outside Argo CD's own namespace.
func OwnerOf(obj *unstructured.Unstructured) *Owner {
	labels, ann := obj.GetLabels(), obj.GetAnnotations()
	if name := labels["kustomize.toolkit.fluxcd.io/name"]; name != "" {
		return &Owner{Tool: Flux, Kind: "Kustomization", Namespace: labels["kustomize.toolkit.fluxcd.io/namespace"], Name: name}
	}
	if name := labels["helm.toolkit.fluxcd.io/name"]; name != "" {
		return &Owner{Tool: Flux, Kind: "HelmRelease", Namespace: labels["helm.toolkit.fluxcd.io/namespace"], Name: name}
	}
	if id := ann["argocd.argoproj.io/tracking-id"]; id != "" {
		app, _, _ := strings.Cut(id, ":")
		ns, name, found := strings.Cut(app, "_")
		if !found {
			ns, name = "", app
		}
		return &Owner{Tool: ArgoCD, Kind: "Application", Namespace: ns, Name: name}
	}
	if name := labels["argocd.argoproj.io/instance"]; name != "" {
		return &Owner{Tool: ArgoCD, Kind: "Application", Name: name}
	}
	return nil
}

// Find returns the app an owner points at, looking it up by name across
// namespaces when the owner doesn't say which.
func Find(ctx context.Context, r kube.Reader, o *Owner) (*App, error) {
	gvr, ok := GVRFor(o.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown kind %s", o.Kind)
	}
	var obj *unstructured.Unstructured
	if o.Namespace != "" {
		got, err := r.Get(ctx, gvr, o.Namespace, o.Name)
		if err != nil {
			return nil, err
		}
		obj = got
	} else {
		l, err := r.List(ctx, gvr, "", metav1.ListOptions{FieldSelector: "metadata.name=" + o.Name})
		if err != nil {
			return nil, err
		}
		if len(l.Items) == 0 {
			return nil, apierrors.NewNotFound(gvr.GroupResource(), o.Name)
		}
		obj = &l.Items[0]
	}
	var a App
	switch o.Kind {
	case "Application":
		a = argoApp(obj)
	case "Kustomization":
		a = fluxKustomization(obj, map[string]string{})
	default:
		a = fluxHelmRelease(obj, map[string]string{})
	}
	return &a, nil
}
