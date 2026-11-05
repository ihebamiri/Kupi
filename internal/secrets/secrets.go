// Package secrets reads Kubernetes Secrets for the UI. Values stay in Go:
// callers get key names, sizes, certificate details and who uses a Secret,
// and a value leaves this package only through Value or EnvFile, which the
// bridge calls when the user explicitly asks to reveal or copy one.
package secrets

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Secret types with special handling.
const (
	TypeOpaque           = "Opaque"
	TypeTLS              = "kubernetes.io/tls"
	TypeDockerConfigJSON = "kubernetes.io/dockerconfigjson"
	TypeDockerCfg        = "kubernetes.io/dockercfg"
	TypeServiceAccount   = "kubernetes.io/service-account-token"
	TypeBasicAuth        = "kubernetes.io/basic-auth"
	TypeSSHAuth          = "kubernetes.io/ssh-auth"
	TypeHelmRelease      = "helm.sh/release.v1"
)

// listLimit caps how many objects of one type a lookup reads.
const listLimit = 2000

var (
	SecretsGVR      = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	configMapsGVR   = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	podsGVR         = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	serviceAcctsGVR = schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}
	ingressesGVR    = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
)

// workloadTypes are the kinds that run pods from a template, in display order.
var workloadTypes = []struct {
	kind string
	gvr  schema.GroupVersionResource
}{
	{"Deployment", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
	{"StatefulSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}},
	{"DaemonSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
	{"CronJob", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}},
	{"Job", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}},
	{"Pod", podsGVR},
}

// Key describes one entry of a Secret without its value.
type Key struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	Binary bool   `json:"binary"`
}

// Usage is an object that uses a Secret.
type Usage struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// How lists each place it is used, in plain words.
	How []string `json:"how"`
	// NeedsRestart: the Secret is read into environment variables, which
	// running pods only pick up after a restart.
	NeedsRestart bool `json:"needsRestart"`
	// Restartable: Kupi can roll the workload's pods.
	Restartable bool `json:"restartable"`
}

// Details describes a Secret for the UI. It never holds a value.
type Details struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Immutable bool              `json:"immutable"`
	Created   string            `json:"created"`
	Labels    map[string]string `json:"labels"`
	Keys      []Key             `json:"keys"`
	// ManagedBy names the tool that writes this Secret; ManagedNote says
	// what that means for editing it here.
	ManagedBy   string   `json:"managedBy"`
	ManagedNote string   `json:"managedNote"`
	TLS         *TLSInfo `json:"tls"`
	Registries  []string `json:"registries"`
	UsedBy      []Usage  `json:"usedBy"`
	// UsageSkipped lists types Kupi could not read while looking for users,
	// so UsedBy may be incomplete.
	UsageSkipped []string `json:"usageSkipped"`
	// ValuesInAnnotation: kubectl apply copied the values into the
	// last-applied-configuration annotation.
	ValuesInAnnotation bool `json:"valuesInAnnotation"`
}

// Describe reads a Secret and reports everything about it except its values.
func Describe(ctx context.Context, r kube.Reader, namespace, name string, now time.Time) (*Details, error) {
	obj, err := r.Get(ctx, SecretsGVR, namespace, name)
	if err != nil {
		return nil, err
	}
	data, err := decodeData(obj)
	if err != nil {
		return nil, err
	}
	d := &Details{
		Namespace: namespace,
		Name:      name,
		Type:      typeOf(obj),
		Labels:    obj.GetLabels(),
		Keys:      []Key{},
	}
	d.Immutable, _, _ = unstructured.NestedBool(obj.Object, "immutable")
	if ts := obj.GetCreationTimestamp(); !ts.IsZero() {
		d.Created = ts.UTC().Format(time.RFC3339)
	}
	for k, v := range data {
		d.Keys = append(d.Keys, Key{Name: k, Size: len(v), Binary: !utf8.Valid(v)})
	}
	sort.Slice(d.Keys, func(i, j int) bool { return d.Keys[i].Name < d.Keys[j].Name })
	d.ManagedBy, d.ManagedNote = managedBy(obj)
	if crt, ok := data["tls.crt"]; ok {
		info := CertInfo(crt, data["tls.key"], now)
		d.TLS = &info
	}
	switch d.Type {
	case TypeDockerConfigJSON:
		d.Registries = Registries(data[".dockerconfigjson"], true)
	case TypeDockerCfg:
		d.Registries = Registries(data[".dockercfg"], false)
	}
	d.ValuesInAnnotation = valuesInAnnotation(obj.GetAnnotations())
	d.UsedBy, d.UsageSkipped = UsedBy(ctx, r, namespace, name)
	return d, nil
}

// Value returns one key's value.
func Value(ctx context.Context, r kube.Reader, namespace, name, key string) ([]byte, error) {
	obj, err := r.Get(ctx, SecretsGVR, namespace, name)
	if err != nil {
		return nil, err
	}
	data, err := decodeData(obj)
	if err != nil {
		return nil, err
	}
	v, ok := data[key]
	if !ok {
		return nil, fmt.Errorf("the Secret has no key %q", key)
	}
	return v, nil
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// EnvFile renders a Secret as a .env file. Keys that are not valid variable
// names and binary values are left out and listed in skipped.
func EnvFile(ctx context.Context, r kube.Reader, namespace, name string) (text string, count int, skipped []string, err error) {
	obj, err := r.Get(ctx, SecretsGVR, namespace, name)
	if err != nil {
		return "", 0, nil, err
	}
	data, err := decodeData(obj)
	if err != nil {
		return "", 0, nil, err
	}
	return FormatEnv(data)
}

// FormatEnv renders values as a .env file, sorted by key.
func FormatEnv(data map[string][]byte) (text string, count int, skipped []string, err error) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := data[k]
		if !envNameRe.MatchString(k) || !utf8.Valid(v) {
			skipped = append(skipped, k)
			continue
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(quoteEnv(string(v)))
		b.WriteByte('\n')
		count++
	}
	return b.String(), count, skipped, nil
}

var plainEnvRe = regexp.MustCompile(`^[A-Za-z0-9_./:@+=,%-]*$`)

// quoteEnv quotes a value the way common .env readers (Docker Compose,
// dotenv libraries) understand it.
func quoteEnv(v string) string {
	if plainEnvRe.MatchString(v) {
		return v
	}
	if !strings.ContainsAny(v, "'\n\r") {
		return "'" + v + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "$", `\$`)
	return `"` + r.Replace(v) + `"`
}

// decodeData returns a Secret's values decoded from base64.
func decodeData(obj *unstructured.Unstructured) (map[string][]byte, error) {
	raw, _, err := unstructured.NestedMap(obj.Object, "data")
	if err != nil {
		return nil, fmt.Errorf("read Secret data: %w", err)
	}
	out := make(map[string][]byte, len(raw))
	for k, v := range raw {
		s, _ := v.(string)
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("key %q is not valid base64", k)
		}
		out[k] = b
	}
	return out, nil
}

func typeOf(obj *unstructured.Unstructured) string {
	t, _, _ := unstructured.NestedString(obj.Object, "type")
	if t == "" {
		return TypeOpaque
	}
	return t
}

// managedBy names the controller or tool that writes a Secret, if Kupi can
// tell, and what that means for editing it.
func managedBy(obj *unstructured.Unstructured) (string, string) {
	const overwritten = "Changes made here will be overwritten. Change the source instead."
	for _, o := range obj.GetOwnerReferences() {
		switch o.Kind {
		case "ExternalSecret":
			return "External Secrets Operator (ExternalSecret " + o.Name + ")", "Values come from an external store. " + overwritten
		case "PushSecret":
			return "External Secrets Operator (PushSecret " + o.Name + ")", overwritten
		case "SealedSecret":
			return "Sealed Secrets (SealedSecret " + o.Name + ")", "Values come from an encrypted SealedSecret, usually kept in Git. " + overwritten
		case "Certificate":
			return "cert-manager (Certificate " + o.Name + ")", "cert-manager renews this certificate. " + overwritten
		}
	}
	ann := obj.GetAnnotations()
	labels := obj.GetLabels()
	switch {
	case ann["cert-manager.io/certificate-name"] != "":
		return "cert-manager (Certificate " + ann["cert-manager.io/certificate-name"] + ")", "cert-manager renews this certificate. " + overwritten
	case ann["reconcile.external-secrets.io/data-hash"] != "":
		return "External Secrets Operator", overwritten
	case typeOf(obj) == TypeServiceAccount:
		return "Kubernetes (token for service account " + ann["kubernetes.io/service-account.name"] + ")", "Kubernetes fills in this token. Don't edit it."
	case typeOf(obj) == TypeHelmRelease:
		return "Helm (release record)", "This is Helm's record of a release, not application data. Editing it can break helm upgrade and rollback."
	case labels["kustomize.toolkit.fluxcd.io/name"] != "":
		return "Flux (Kustomization " + labels["kustomize.toolkit.fluxcd.io/namespace"] + "/" + labels["kustomize.toolkit.fluxcd.io/name"] + ")",
			"Flux applies this from Git, possibly decrypted from a SOPS file. " + overwritten
	case labels["argocd.argoproj.io/instance"] != "" || ann["argocd.argoproj.io/tracking-id"] != "":
		app := labels["argocd.argoproj.io/instance"]
		if app == "" {
			app, _, _ = strings.Cut(ann["argocd.argoproj.io/tracking-id"], ":")
		}
		return "Argo CD (Application " + app + ")", "Argo CD syncs this from Git. " + overwritten
	case strings.EqualFold(labels["app.kubernetes.io/managed-by"], "Helm"):
		return "Helm (release " + ann["meta.helm.sh/release-name"] + ")", "The next helm upgrade will overwrite changes made here."
	case labels["app.kubernetes.io/managed-by"] != "":
		return labels["app.kubernetes.io/managed-by"], "Another tool says it manages this Secret; it may overwrite changes made here."
	}
	return "", ""
}

// valuesInAnnotation reports whether kubectl apply left the Secret's values
// in the last-applied-configuration annotation.
func valuesInAnnotation(ann map[string]string) bool {
	last := ann["kubectl.kubernetes.io/last-applied-configuration"]
	return strings.Contains(last, `"data"`) || strings.Contains(last, `"stringData"`)
}

// UsedBy finds the workloads, ingresses and service accounts in a namespace
// that use a Secret. Types the user cannot list are reported in skipped.
func UsedBy(ctx context.Context, r kube.Reader, namespace, secret string) (out []Usage, skipped []string) {
	out = []Usage{}
	opts := metav1.ListOptions{Limit: listLimit}
	for _, t := range workloadTypes {
		list, err := r.List(ctx, t.gvr, namespace, opts)
		if err != nil {
			skipped = appendSkip(skipped, t.kind, err)
			continue
		}
		for i := range list.Items {
			obj := &list.Items[i]
			// Pods and Jobs created by a controller are covered by it.
			if (t.kind == "Pod" || t.kind == "Job") && metav1.GetControllerOf(obj) != nil {
				continue
			}
			spec := podSpec(obj, t.kind)
			how, env := templateUses(spec, secret)
			if len(how) == 0 {
				continue
			}
			out = append(out, Usage{
				Kind: t.kind, Namespace: obj.GetNamespace(), Name: obj.GetName(), How: how,
				NeedsRestart: env && t.kind != "CronJob" && t.kind != "Job",
				Restartable:  t.kind == "Deployment" || t.kind == "StatefulSet" || t.kind == "DaemonSet",
			})
		}
	}
	if list, err := r.List(ctx, ingressesGVR, namespace, opts); err != nil {
		skipped = appendSkip(skipped, "Ingress", err)
	} else {
		for _, ing := range list.Items {
			tls, _, _ := unstructured.NestedSlice(ing.Object, "spec", "tls")
			for _, t := range tls {
				tm, _ := t.(map[string]interface{})
				if n, _ := tm["secretName"].(string); n == secret {
					out = append(out, Usage{Kind: "Ingress", Namespace: ing.GetNamespace(), Name: ing.GetName(), How: []string{"TLS certificate"}})
					break
				}
			}
		}
	}
	if list, err := r.List(ctx, serviceAcctsGVR, namespace, opts); err != nil {
		skipped = appendSkip(skipped, "ServiceAccount", err)
	} else {
		for _, sa := range list.Items {
			var how []string
			for _, field := range []string{"imagePullSecrets", "secrets"} {
				refs, _, _ := unstructured.NestedSlice(sa.Object, field)
				for _, ref := range refs {
					rm, _ := ref.(map[string]interface{})
					if n, _ := rm["name"].(string); n == secret {
						if field == "imagePullSecrets" {
							how = append(how, "image pull secret for every pod using this account")
						} else {
							how = append(how, "listed in the account's secrets")
						}
					}
				}
			}
			if len(how) > 0 {
				out = append(out, Usage{Kind: "ServiceAccount", Namespace: sa.GetNamespace(), Name: sa.GetName(), How: how})
			}
		}
	}
	return out, skipped
}

func appendSkip(skipped []string, kind string, err error) []string {
	switch {
	case apierrors.IsForbidden(err):
		return append(skipped, kind+" (no permission to list)")
	case apierrors.IsNotFound(err):
		return skipped // the API isn't served on this cluster
	default:
		return append(skipped, kind+" ("+err.Error()+")")
	}
}

// podSpec returns the pod spec an object runs.
func podSpec(obj *unstructured.Unstructured, kind string) map[string]interface{} {
	var path []string
	switch kind {
	case "Pod":
		path = []string{"spec"}
	case "CronJob":
		path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
	default:
		path = []string{"spec", "template", "spec"}
	}
	spec, _, _ := unstructured.NestedMap(obj.Object, path...)
	return spec
}

// templateUses lists where a pod spec uses a Secret, and whether any use is
// through environment variables.
func templateUses(spec map[string]interface{}, secret string) (how []string, env bool) {
	if spec == nil {
		return nil, false
	}
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	for _, v := range volumes {
		vm, _ := v.(map[string]interface{})
		vol, _ := vm["name"].(string)
		if n, _, _ := unstructured.NestedString(vm, "secret", "secretName"); n == secret {
			how = append(how, "mounted as volume "+vol)
		}
		sources, _, _ := unstructured.NestedSlice(vm, "projected", "sources")
		for _, s := range sources {
			sm, _ := s.(map[string]interface{})
			if n, _, _ := unstructured.NestedString(sm, "secret", "name"); n == secret {
				how = append(how, "mounted in projected volume "+vol)
			}
		}
	}
	for _, field := range []string{"initContainers", "containers"} {
		list, _, _ := unstructured.NestedSlice(spec, field)
		for _, c := range list {
			cm, _ := c.(map[string]interface{})
			cname, _ := cm["name"].(string)
			envFrom, _, _ := unstructured.NestedSlice(cm, "envFrom")
			for _, e := range envFrom {
				em, _ := e.(map[string]interface{})
				if n, _, _ := unstructured.NestedString(em, "secretRef", "name"); n == secret {
					prefix, _ := em["prefix"].(string)
					text := "every key as an environment variable in container " + cname
					if prefix != "" {
						text += " (prefix " + prefix + ")"
					}
					how = append(how, text)
					env = true
				}
			}
			vars, _, _ := unstructured.NestedSlice(cm, "env")
			for _, e := range vars {
				em, _ := e.(map[string]interface{})
				if n, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "name"); n == secret {
					key, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "key")
					vname, _ := em["name"].(string)
					how = append(how, fmt.Sprintf("key %s as environment variable %s in container %s", key, vname, cname))
					env = true
				}
			}
		}
	}
	pull, _, _ := unstructured.NestedSlice(spec, "imagePullSecrets")
	for _, p := range pull {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == secret {
			how = append(how, "image pull secret")
		}
	}
	return how, env
}
