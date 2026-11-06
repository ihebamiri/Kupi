package guard

import (
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/ihebamiri/kupi/internal/diff"
	"github.com/ihebamiri/kupi/internal/redact"
)

const (
	maxSummary    = 12
	maxValueRunes = 60
)

// render returns the redacted YAML diff and a plain-language summary of the
// change from before to after. Either may be nil (create or delete).
func render(before, after *unstructured.Unstructured) (string, []string) {
	b, a := redact.SecretPair(normalize(before), normalize(after))
	return diff.Unified("live", "after change", toYAML(b), toYAML(a), 3), summarize(b, a)
}

// normalize drops fields the server manages, which would only add noise.
func normalize(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	out := obj.DeepCopy()
	stripServerFields(out)
	return out
}

func toYAML(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	data, err := yaml.Marshal(obj.Object)
	if err != nil {
		return fmt.Sprintf("# could not render this object: %v\n", err)
	}
	return string(data)
}

// summarize lists changed fields in plain language, capped at maxSummary.
func summarize(before, after *unstructured.Unstructured) []string {
	if before == nil || after == nil {
		return nil
	}
	old, cur := map[string]string{}, map[string]string{}
	flatten("", before.Object, old)
	flatten("", after.Object, cur)

	paths := map[string]bool{}
	for p := range old {
		paths[p] = true
	}
	for p := range cur {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var out []string
	changes := 0
	for _, p := range sorted {
		ov, inOld := old[p]
		nv, inNew := cur[p]
		var line string
		switch {
		case inOld && inNew && ov != nv:
			line = fmt.Sprintf("%s: %s → %s", p, short(ov), short(nv))
		case !inOld && inNew:
			line = fmt.Sprintf("Adds %s: %s", p, short(nv))
		case inOld && !inNew:
			line = fmt.Sprintf("Removes %s", p)
		default:
			continue
		}
		changes++
		if len(out) < maxSummary {
			out = append(out, line)
		}
	}
	if changes > maxSummary {
		out = append(out, fmt.Sprintf("…and %d more changes in the diff.", changes-maxSummary))
	}
	return out
}

func flatten(prefix string, v interface{}, out map[string]string) {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 0 && prefix != "" {
			out[prefix] = "{}"
			return
		}
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case []interface{}:
		if len(t) == 0 {
			out[prefix] = "[]"
			return
		}
		for i, child := range t {
			flatten(prefix+"["+strconv.Itoa(i)+"]", child, out)
		}
	default:
		out[prefix] = fmt.Sprint(t)
	}
}

func short(s string) string {
	if utf8.RuneCountInString(s) <= maxValueRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxValueRunes]) + "…"
}
