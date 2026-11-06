// Package incident answers "what is broken, and what changed right before?"
// in one report: failing resources, the changes Kupi recorded in the time
// window, warning events, and error lines from the logs of failing pods.
package incident

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ihebamiri/kupi/internal/history"
	"github.com/ihebamiri/kupi/internal/kube"
)

// Severities.
const (
	Critical = "critical"
	Warning  = "warning"
)

const (
	maxProblems   = 200
	maxLogPods    = 6
	logTail       = 300
	maxLogLines   = 12
	maxLineRunes  = 400
	pendingGrace  = 2 * time.Minute
	notReadyGrace = 5 * time.Minute
)

var (
	podsGVR        = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	nodesGVR       = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
	pvcsGVR        = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	servicesGVR    = schema.GroupVersionResource{Version: "v1", Resource: "services"}
	eventsGVR      = schema.GroupVersionResource{Version: "v1", Resource: "events"}
	slicesGVR      = schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}
	deploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	statefulGVR    = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	daemonGVR      = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	jobsGVR        = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
)

// Problem is something failing right now.
type Problem struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Problem   string `json:"problem"`
	Detail    string `json:"detail"`
	// Since is when the problem started, if known (RFC 3339).
	Since string `json:"since"`
	// Container is set for pod problems tied to one container.
	Container string `json:"container"`

	logPrevious bool
}

// LogExcerpt is the interesting end of a failing container's log.
type LogExcerpt struct {
	Namespace string   `json:"namespace"`
	Pod       string   `json:"pod"`
	Container string   `json:"container"`
	Previous  bool     `json:"previous"`
	Lines     []string `json:"lines"`
	// Matched: Lines are error lines; otherwise they are the last lines.
	Matched bool   `json:"matched"`
	Error   string `json:"error"`
}

// Report is everything the incident view shows.
type Report struct {
	GeneratedAt string           `json:"generatedAt"`
	Minutes     int              `json:"minutes"`
	Namespace   string           `json:"namespace"`
	Problems    []Problem        `json:"problems"`
	Changes     []history.Change `json:"changes"`
	Events      []history.Event  `json:"events"`
	Logs        []LogExcerpt     `json:"logs"`
	// Skipped lists what Kupi couldn't check.
	Skipped []string `json:"skipped"`
	// HistorySince is when Kupi started recording changes for this
	// cluster; changes before that are unknown. Empty when not recording.
	HistorySince string `json:"historySince"`
}

// Input is what Build needs.
type Input struct {
	Reader       kube.Reader
	Store        *history.Store
	Context      string
	Namespace    string
	Window       time.Duration
	Now          time.Time
	HistorySince string
}

// Build gathers the report.
func Build(ctx context.Context, in Input) (*Report, error) {
	since := in.Now.Add(-in.Window)
	rep := &Report{
		GeneratedAt:  in.Now.UTC().Format(time.RFC3339),
		Minutes:      int(in.Window / time.Minute),
		Namespace:    in.Namespace,
		Problems:     []Problem{},
		Logs:         []LogExcerpt{},
		Skipped:      []string{},
		HistorySince: in.HistorySince,
	}
	list := func(kind string, gvr schema.GroupVersionResource, namespaced bool) []unstructured.Unstructured {
		ns := in.Namespace
		if !namespaced {
			ns = ""
		}
		l, err := in.Reader.List(ctx, gvr, ns, metav1.ListOptions{})
		if err != nil {
			if apierrors.IsForbidden(err) {
				rep.Skipped = append(rep.Skipped, kind+" (no permission to list)")
			} else if !apierrors.IsNotFound(err) {
				rep.Skipped = append(rep.Skipped, kind+" ("+err.Error()+")")
			}
			return nil
		}
		return l.Items
	}

	for _, p := range list("Pod", podsGVR, true) {
		rep.Problems = append(rep.Problems, podProblems(&p, in.Now, since)...)
	}
	for _, d := range list("Deployment", deploymentsGVR, true) {
		rep.Problems = append(rep.Problems, deploymentProblems(&d)...)
	}
	for _, s := range list("StatefulSet", statefulGVR, true) {
		rep.Problems = append(rep.Problems, replicaProblem(&s, "StatefulSet", "replicas", "readyReplicas")...)
	}
	for _, d := range list("DaemonSet", daemonGVR, true) {
		rep.Problems = append(rep.Problems, replicaProblem(&d, "DaemonSet", "", "")...)
	}
	for _, j := range list("Job", jobsGVR, true) {
		rep.Problems = append(rep.Problems, jobProblems(&j, in.Now)...)
	}
	for _, c := range list("PersistentVolumeClaim", pvcsGVR, true) {
		rep.Problems = append(rep.Problems, pvcProblems(&c, in.Now)...)
	}
	for _, n := range list("Node", nodesGVR, false) {
		rep.Problems = append(rep.Problems, nodeProblems(&n)...)
	}
	rep.Problems = append(rep.Problems, serviceProblems(list("Service", servicesGVR, true), list("EndpointSlice", slicesGVR, true))...)

	sort.SliceStable(rep.Problems, func(i, j int) bool {
		a, b := rep.Problems[i], rep.Problems[j]
		if a.Severity != b.Severity {
			return a.Severity == Critical
		}
		return a.Namespace+"/"+a.Kind+"/"+a.Name < b.Namespace+"/"+b.Kind+"/"+b.Name
	})
	if len(rep.Problems) > maxProblems {
		rep.Problems = rep.Problems[:maxProblems]
	}

	q := history.Query{Since: since, Namespace: in.Namespace, Limit: 500}
	changes, err := in.Store.Changes(in.Context, q)
	if err != nil {
		rep.Skipped = append(rep.Skipped, "recorded changes ("+err.Error()+")")
	}
	rep.Changes = changes
	rep.Events = mergeEvents(in.Store, in.Context, q, list("Event", eventsGVR, true), since)

	rep.Logs = logExcerpts(ctx, in.Reader, rep.Problems)
	return rep, nil
}

// waitingFailures are container waiting reasons that mean it can't run.
var waitingFailures = map[string]bool{
	"CrashLoopBackOff": true, "ImagePullBackOff": true, "ErrImagePull": true, "CreateContainerConfigError": true,
	"CreateContainerError": true, "InvalidImageName": true, "RunContainerError": true, "ErrImageNeverPull": true,
}

func podProblems(p *unstructured.Unstructured, now, since time.Time) []Problem {
	phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
	if phase == "Succeeded" {
		return nil
	}
	base := Problem{Kind: "Pod", Namespace: p.GetNamespace(), Name: p.GetName()}
	var out []Problem
	if phase == "Failed" {
		reason, _, _ := unstructured.NestedString(p.Object, "status", "reason")
		msg, _, _ := unstructured.NestedString(p.Object, "status", "message")
		pr := base
		pr.Severity, pr.Problem, pr.Detail = Critical, "Failed"+suffix(reason), msg
		return append(out, pr)
	}
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		statuses, _, _ := unstructured.NestedSlice(p.Object, "status", field)
		for _, s := range statuses {
			cs, _ := s.(map[string]interface{})
			name, _ := cs["name"].(string)
			restarts, _, _ := unstructured.NestedInt64(cs, "restartCount")
			reason, _, _ := unstructured.NestedString(cs, "state", "waiting", "reason")
			msg, _, _ := unstructured.NestedString(cs, "state", "waiting", "message")
			lastReason, _, _ := unstructured.NestedString(cs, "lastState", "terminated", "reason")
			lastExit, _, _ := unstructured.NestedInt64(cs, "lastState", "terminated", "exitCode")
			lastEnd := timeAt(cs, "lastState", "terminated", "finishedAt")
			if waitingFailures[reason] {
				pr := base
				pr.Severity, pr.Container = Critical, name
				pr.Problem = fmt.Sprintf("%s in container %s", reason, name)
				if restarts > 0 {
					pr.Problem += fmt.Sprintf(" (restarted %d times)", restarts)
				}
				detail := msg
				if lastReason != "" {
					detail = strings.TrimSpace(fmt.Sprintf("Last run ended: %s, exit code %d. %s", lastReason, lastExit, msg))
				}
				pr.Detail = detail
				pr.logPrevious = restarts > 0
				out = append(out, pr)
				continue
			}
			if lastReason == "OOMKilled" && lastEnd.After(since) {
				pr := base
				pr.Severity, pr.Container = Warning, name
				pr.Problem = fmt.Sprintf("Container %s ran out of memory and was restarted", name)
				pr.Detail = "Raise its memory limit or find what uses the memory."
				pr.Since = lastEnd.UTC().Format(time.RFC3339)
				pr.logPrevious = true
				out = append(out, pr)
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	conds := conditions(p)
	created := p.GetCreationTimestamp().Time
	if phase == "Pending" && now.Sub(created) > pendingGrace {
		pr := base
		pr.Severity, pr.Since = Warning, created.UTC().Format(time.RFC3339)
		if c := conds["PodScheduled"]; c.status == "False" {
			pr.Severity, pr.Problem, pr.Detail = Critical, "Can't be scheduled", c.message
		} else {
			pr.Problem = "Pending for " + roundDuration(now.Sub(created))
		}
		return append(out, pr)
	}
	if phase == "Running" {
		if c := conds["Ready"]; c.status == "False" && now.Sub(c.since) > notReadyGrace {
			pr := base
			pr.Severity, pr.Problem = Warning, "Running but not ready for "+roundDuration(now.Sub(c.since))
			pr.Detail = "Usually a failing readiness probe. " + c.message
			pr.Since = c.since.UTC().Format(time.RFC3339)
			out = append(out, pr)
		}
	}
	return out
}

func deploymentProblems(d *unstructured.Unstructured) []Problem {
	out := replicaProblem(d, "Deployment", "replicas", "availableReplicas")
	if c := conditions(d)["Progressing"]; c.status == "False" && c.reason == "ProgressDeadlineExceeded" {
		out = append(out, Problem{
			Severity: Critical, Kind: "Deployment", Namespace: d.GetNamespace(), Name: d.GetName(),
			Problem: "Rollout is stuck", Detail: c.message, Since: c.since.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// replicaProblem reports a workload with fewer ready copies than wanted.
// DaemonSets use their own status fields.
func replicaProblem(o *unstructured.Unstructured, kind, wantField, haveField string) []Problem {
	var want, have int64
	if kind == "DaemonSet" {
		want, _, _ = unstructured.NestedInt64(o.Object, "status", "desiredNumberScheduled")
		have, _, _ = unstructured.NestedInt64(o.Object, "status", "numberReady")
	} else {
		var ok bool
		want, ok, _ = unstructured.NestedInt64(o.Object, "spec", wantField)
		if !ok {
			want = 1
		}
		have, _, _ = unstructured.NestedInt64(o.Object, "status", haveField)
	}
	if want == 0 || have >= want {
		return nil
	}
	sev := Warning
	if have == 0 {
		sev = Critical
	}
	return []Problem{{
		Severity: sev, Kind: kind, Namespace: o.GetNamespace(), Name: o.GetName(),
		Problem: fmt.Sprintf("%d of %d ready", have, want),
	}}
}

func jobProblems(j *unstructured.Unstructured, now time.Time) []Problem {
	c := conditions(j)["Failed"]
	if c.status != "True" || now.Sub(c.since) > 24*time.Hour {
		return nil
	}
	return []Problem{{
		Severity: Warning, Kind: "Job", Namespace: j.GetNamespace(), Name: j.GetName(),
		Problem: "Failed" + suffix(c.reason), Detail: c.message, Since: c.since.UTC().Format(time.RFC3339),
	}}
}

func pvcProblems(c *unstructured.Unstructured, now time.Time) []Problem {
	phase, _, _ := unstructured.NestedString(c.Object, "status", "phase")
	created := c.GetCreationTimestamp().Time
	switch {
	case phase == "Lost":
		return []Problem{{Severity: Critical, Kind: "PersistentVolumeClaim", Namespace: c.GetNamespace(), Name: c.GetName(), Problem: "Its volume is lost"}}
	case phase == "Pending" && now.Sub(created) > time.Minute:
		return []Problem{{
			Severity: Warning, Kind: "PersistentVolumeClaim", Namespace: c.GetNamespace(), Name: c.GetName(),
			Problem: "Not bound to a volume for " + roundDuration(now.Sub(created)), Detail: "Check its storage class and the events below.",
			Since: created.UTC().Format(time.RFC3339),
		}}
	}
	return nil
}

func nodeProblems(n *unstructured.Unstructured) []Problem {
	var out []Problem
	conds := conditions(n)
	if c := conds["Ready"]; c.status != "True" {
		out = append(out, Problem{Severity: Critical, Kind: "Node", Name: n.GetName(), Problem: "Not ready", Detail: c.message, Since: c.since.UTC().Format(time.RFC3339)})
	}
	for _, p := range []string{"MemoryPressure", "DiskPressure", "PIDPressure"} {
		if c := conds[p]; c.status == "True" {
			out = append(out, Problem{Severity: Warning, Kind: "Node", Name: n.GetName(), Problem: p, Detail: c.message, Since: c.since.UTC().Format(time.RFC3339)})
		}
	}
	return out
}

// serviceProblems finds Services with a selector but no ready endpoint.
func serviceProblems(services, slices []unstructured.Unstructured) []Problem {
	if slices == nil {
		return nil
	}
	ready := map[string]int{}
	for _, s := range slices {
		svc := s.GetLabels()["kubernetes.io/service-name"]
		eps, _, _ := unstructured.NestedSlice(s.Object, "endpoints")
		for _, e := range eps {
			em, _ := e.(map[string]interface{})
			if r, ok, _ := unstructured.NestedBool(em, "conditions", "ready"); !ok || r {
				ready[s.GetNamespace()+"/"+svc]++
			}
		}
	}
	var out []Problem
	for _, svc := range services {
		sel, _, _ := unstructured.NestedStringMap(svc.Object, "spec", "selector")
		typ, _, _ := unstructured.NestedString(svc.Object, "spec", "type")
		if len(sel) == 0 || typ == "ExternalName" || ready[svc.GetNamespace()+"/"+svc.GetName()] > 0 {
			continue
		}
		out = append(out, Problem{
			Severity: Warning, Kind: "Service", Namespace: svc.GetNamespace(), Name: svc.GetName(),
			Problem: "No ready pods behind it", Detail: "Requests to this Service fail. Check that its selector matches running, ready pods.",
		})
	}
	return out
}

// mergeEvents combines recorded warnings with the cluster's current ones.
func mergeEvents(store *history.Store, contextName string, q history.Query, live []unstructured.Unstructured, since time.Time) []history.Event {
	byUID := map[string]history.Event{}
	if recorded, err := store.Events(contextName, q); err == nil {
		for _, e := range recorded {
			byUID[e.UID] = e
		}
	}
	for i := range live {
		if t, _, _ := unstructured.NestedString(live[i].Object, "type"); t != "Warning" {
			continue
		}
		e := history.EventFrom(&live[i])
		if e.Time.Before(since) {
			continue
		}
		if prev, ok := byUID[e.UID]; !ok || e.Count >= prev.Count {
			byUID[e.UID] = e
		}
	}
	out := make([]history.Event, 0, len(byUID))
	for _, e := range byUID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > 500 {
		out = out[:500]
	}
	return out
}

var errorLineRe = regexp.MustCompile(`(?i)\b(error|err|fatal|panic|exception|failed|failure|refused|denied|forbidden|timeout|timed out|traceback|unable to|cannot|can't)\b`)

// logExcerpts reads the end of the logs of the first failing containers.
func logExcerpts(ctx context.Context, r kube.Reader, problems []Problem) []LogExcerpt {
	out := []LogExcerpt{}
	seen := map[string]bool{}
	for _, p := range problems {
		if len(out) >= maxLogPods {
			break
		}
		if p.Kind != "Pod" || p.Container == "" || seen[p.Namespace+"/"+p.Name] {
			continue
		}
		if strings.HasPrefix(p.Problem, "ImagePull") || strings.HasPrefix(p.Problem, "ErrImage") || strings.HasPrefix(p.Problem, "InvalidImageName") {
			continue // never started, so there's no log
		}
		seen[p.Namespace+"/"+p.Name] = true
		ex := LogExcerpt{Namespace: p.Namespace, Pod: p.Name, Container: p.Container, Previous: p.logPrevious, Lines: []string{}}
		lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rc, err := r.Logs(lctx, p.Namespace, p.Name, kube.LogOptions{Container: p.Container, Previous: p.logPrevious, TailLines: logTail, LimitBytes: 256 << 10})
		if err != nil {
			cancel()
			ex.Error = err.Error()
			out = append(out, ex)
			continue
		}
		var all, matched []string
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := clip(sc.Text())
			all = append(all, line)
			if errorLineRe.MatchString(line) {
				matched = append(matched, line)
			}
		}
		_ = rc.Close()
		cancel()
		if len(matched) > 0 {
			ex.Lines, ex.Matched = tail(matched, maxLogLines), true
		} else {
			ex.Lines = tail(all, 8)
		}
		out = append(out, ex)
	}
	return out
}

type condition struct {
	status, reason, message string
	since                   time.Time
}

func conditions(o *unstructured.Unstructured) map[string]condition {
	out := map[string]condition{}
	list, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range list {
		cm, _ := c.(map[string]interface{})
		t, _ := cm["type"].(string)
		st, _ := cm["status"].(string)
		reason, _ := cm["reason"].(string)
		msg, _ := cm["message"].(string)
		out[t] = condition{status: st, reason: reason, message: msg, since: timeAt(cm, "lastTransitionTime")}
	}
	return out
}

func timeAt(m map[string]interface{}, path ...string) time.Time {
	s, _, _ := unstructured.NestedString(m, path...)
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func suffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

func roundDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

func clip(s string) string {
	if r := []rune(s); len(r) > maxLineRunes {
		return string(r[:maxLineRunes]) + "…"
	}
	return s
}

func tail(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}
