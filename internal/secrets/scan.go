package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/iheb-amiri/kupi/internal/kube"
)

// Severity of a scan finding.
const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
)

// Finding is a secret Kupi found stored in the open. It names where the
// value is, never the value itself.
type Finding struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Where     string `json:"where"`
	Problem   string `json:"problem"`
	Fix       string `json:"fix"`
}

// ScanResult is the outcome of a hygiene scan.
type ScanResult struct {
	Findings []Finding `json:"findings"`
	Scanned  int       `json:"scanned"`
	// Skipped lists types that could not be read.
	Skipped []string `json:"skipped"`
}

// valuePatterns recognise credentials by their shape.
var valuePatterns = []struct {
	what string
	re   *regexp.Regexp
}{
	{"a private key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`)},
	{"an AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"a GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{"a GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"a Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"a Stripe live key", regexp.MustCompile(`\b[rs]k_live_[A-Za-z0-9]{20,}`)},
	{"a Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"a JSON web token", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
}

// urlPasswordRe finds user:password@ in connection strings.
var urlPasswordRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:([^/\s@]+)@`)

// sensitiveNameRe matches names that usually hold a credential.
var sensitiveNameRe = regexp.MustCompile(`(?i)(passw(or)?d|pwd|secret|token|api[_.-]?key|private[_.-]?key|access[_.-]?key|credential|auth[_.-]?key)`)

// referenceSuffixRe matches names that point at a credential rather than
// hold one, such as DB_PASSWORD_FILE or TOKEN_SECRET_NAME.
var referenceSuffixRe = regexp.MustCompile(`(?i)[_.-](file|path|name|ref|key[_.-]?name|enabled|ttl|length|header|url|endpoint|mode|type|field|dir|location)$`)

// sensitiveFlagRe finds credentials passed as command-line flags.
var sensitiveFlagRe = regexp.MustCompile(`(?i)^--?[a-z0-9-]*(password|passwd|token|secret|api-key|apikey)[a-z0-9-]*=(.+)$`)

// identifyValue says what kind of credential a value looks like, if any.
func identifyValue(v string) string {
	for _, p := range valuePatterns {
		if p.re.MatchString(v) {
			return p.what
		}
	}
	for _, m := range urlPasswordRe.FindAllStringSubmatch(v, -1) {
		if !isPlaceholder(m[1]) {
			return "a password inside a connection URL"
		}
	}
	return ""
}

// looksLikeSecretByName reports a credential-like name with a value that
// could be the credential itself.
func looksLikeSecretByName(name, value string) bool {
	if !sensitiveNameRe.MatchString(name) || referenceSuffixRe.MatchString(name) {
		return false
	}
	v := strings.TrimSpace(value)
	if len(v) < 6 || isPlaceholder(v) || strings.HasPrefix(v, "/") {
		return false
	}
	switch strings.ToLower(v) {
	case "true", "false", "enabled", "disabled", "required", "optional", "none", "null":
		return false
	}
	return true
}

func isPlaceholder(v string) bool {
	for _, p := range []string{"$", "{{", "<", "%", "***", "xxx", "changeme", "placeholder", "example", "redacted"} {
		if strings.HasPrefix(strings.ToLower(v), p) {
			return true
		}
	}
	return false
}

// Scan looks through a namespace ("" for all namespaces) for credentials
// stored where they shouldn't be: ConfigMaps, plain environment variables,
// command-line flags, and Secret values copied into annotations by kubectl.
func Scan(ctx context.Context, r kube.Reader, namespace string) (ScanResult, error) {
	res := ScanResult{Findings: []Finding{}, Skipped: []string{}}
	opts := metav1.ListOptions{Limit: listLimit}

	if list, err := r.List(ctx, configMapsGVR, namespace, opts); err != nil {
		res.Skipped = appendSkip(res.Skipped, "ConfigMap", err)
	} else {
		for _, cm := range list.Items {
			res.Scanned++
			res.Findings = append(res.Findings, scanConfigMap(&cm)...)
		}
	}
	for _, t := range workloadTypes {
		list, err := r.List(ctx, t.gvr, namespace, opts)
		if err != nil {
			res.Skipped = appendSkip(res.Skipped, t.kind, err)
			continue
		}
		for i := range list.Items {
			obj := &list.Items[i]
			if (t.kind == "Pod" || t.kind == "Job") && metav1.GetControllerOf(obj) != nil {
				continue
			}
			res.Scanned++
			res.Findings = append(res.Findings, scanPodSpec(obj, t.kind)...)
		}
	}
	// Secret metadata only: the table API never returns values.
	table, err := r.Table(ctx, SecretsGVR, true, namespace, kube.TableOptions{Limit: listLimit})
	if err != nil {
		res.Skipped = appendSkip(res.Skipped, "Secret", err)
	} else {
		for _, row := range table.Rows {
			var m metav1.PartialObjectMetadata
			if len(row.Object.Raw) == 0 || json.Unmarshal(row.Object.Raw, &m) != nil {
				continue
			}
			res.Scanned++
			if valuesInAnnotation(m.Annotations) {
				res.Findings = append(res.Findings, Finding{
					Severity: SeverityMedium, Kind: "Secret", Namespace: m.Namespace, Name: m.Name,
					Where:   "annotation kubectl.kubernetes.io/last-applied-configuration",
					Problem: "kubectl apply copied this Secret's values into an annotation, where tools that only read metadata can see them.",
					Fix:     "Remove the annotation, and apply Secrets with server-side apply (kubectl apply --server-side) or Kupi.",
				})
			}
		}
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity == SeverityHigh
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Kind+a.Name < b.Kind+b.Name
	})
	return res, nil
}

func scanConfigMap(cm *unstructured.Unstructured) []Finding {
	var out []Finding
	data, _, _ := unstructured.NestedStringMap(cm.Object, "data")
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const fix = "Move the value into a Secret and reference it from there."
	for _, k := range keys {
		v := data[k]
		if what := identifyValue(v); what != "" {
			out = append(out, Finding{
				Severity: SeverityHigh, Kind: "ConfigMap", Namespace: cm.GetNamespace(), Name: cm.GetName(),
				Where: "key " + k, Problem: "The value contains " + what + ". ConfigMaps aren't meant for secrets and are readable by more people.", Fix: fix,
			})
		} else if looksLikeSecretByName(k, v) {
			out = append(out, Finding{
				Severity: SeverityMedium, Kind: "ConfigMap", Namespace: cm.GetNamespace(), Name: cm.GetName(),
				Where: "key " + k, Problem: "The key name suggests a credential stored in plain text.", Fix: fix,
			})
		}
	}
	return out
}

func scanPodSpec(obj *unstructured.Unstructured, kind string) []Finding {
	spec := podSpec(obj, kind)
	if spec == nil {
		return nil
	}
	var out []Finding
	add := func(sev, where, problem, fix string) {
		out = append(out, Finding{Severity: sev, Kind: kind, Namespace: obj.GetNamespace(), Name: obj.GetName(), Where: where, Problem: problem, Fix: fix})
	}
	const envFix = "Store the value in a Secret and use valueFrom.secretKeyRef."
	for _, field := range []string{"initContainers", "containers"} {
		list, _, _ := unstructured.NestedSlice(spec, field)
		for _, c := range list {
			cm, _ := c.(map[string]interface{})
			cname, _ := cm["name"].(string)
			vars, _, _ := unstructured.NestedSlice(cm, "env")
			for _, e := range vars {
				em, _ := e.(map[string]interface{})
				name, _ := em["name"].(string)
				value, ok := em["value"].(string)
				if !ok {
					continue
				}
				where := fmt.Sprintf("container %s, environment variable %s", cname, name)
				if what := identifyValue(value); what != "" {
					add(SeverityHigh, where, "The value contains "+what+", written into the spec in plain text.", envFix)
				} else if looksLikeSecretByName(name, value) {
					add(SeverityMedium, where, "The name suggests a credential written into the spec in plain text.", envFix)
				}
			}
			for _, field := range []string{"command", "args"} {
				args, _, _ := unstructured.NestedStringSlice(cm, field)
				for _, a := range args {
					where := fmt.Sprintf("container %s, %s", cname, field)
					if m := sensitiveFlagRe.FindStringSubmatch(a); m != nil && !isPlaceholder(m[2]) {
						flag, _, _ := strings.Cut(a, "=")
						add(SeverityHigh, where, "The flag "+flag+" passes a credential on the command line, visible to anyone who can read the spec or the process list.",
							"Read it from a Secret through an environment variable or a mounted file.")
					} else if what := identifyValue(a); what != "" {
						add(SeverityHigh, where, "An argument contains "+what+".", "Read it from a Secret through an environment variable or a mounted file.")
					}
				}
			}
		}
	}
	return out
}
