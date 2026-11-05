package graph

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// autoInjectedCA is the ConfigMap every namespace gets for service-account tokens.
const autoInjectedCA = "kube-root-ca.crt"

// ref is something a pod template uses.
type ref struct {
	kind     string
	name     string
	optional bool
}

// podTemplate returns the pod spec and pod labels an object runs.
func podTemplate(obj *unstructured.Unstructured, kind string) (map[string]interface{}, map[string]string) {
	var specPath, labelPath []string
	switch kind {
	case "Pod":
		spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
		return spec, obj.GetLabels()
	case "CronJob":
		specPath = []string{"spec", "jobTemplate", "spec", "template", "spec"}
		labelPath = []string{"spec", "jobTemplate", "spec", "template", "metadata", "labels"}
	default:
		specPath = []string{"spec", "template", "spec"}
		labelPath = []string{"spec", "template", "metadata", "labels"}
	}
	spec, _, _ := unstructured.NestedMap(obj.Object, specPath...)
	labels, _, _ := unstructured.NestedStringMap(obj.Object, labelPath...)
	return spec, labels
}

func containers(spec map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, field := range []string{"initContainers", "containers"} {
		list, _, _ := unstructured.NestedSlice(spec, field)
		for _, c := range list {
			if m, ok := c.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func boolAt(m map[string]interface{}, path ...string) bool {
	v, _, _ := unstructured.NestedBool(m, path...)
	return v
}

func str(m map[string]interface{}, path ...string) string {
	v, _, _ := unstructured.NestedString(m, path...)
	return v
}

// podRefs lists the ConfigMaps, Secrets and claims a pod spec references.
func podRefs(spec map[string]interface{}) []ref {
	if spec == nil {
		return nil
	}
	var out []ref
	add := func(kind, name string, optional bool) {
		if name != "" {
			out = append(out, ref{kind: kind, name: name, optional: optional})
		}
	}
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	for _, v := range volumes {
		vm, _ := v.(map[string]interface{})
		add("ConfigMap", str(vm, "configMap", "name"), boolAt(vm, "configMap", "optional"))
		add("Secret", str(vm, "secret", "secretName"), boolAt(vm, "secret", "optional"))
		add("PersistentVolumeClaim", str(vm, "persistentVolumeClaim", "claimName"), false)
		sources, _, _ := unstructured.NestedSlice(vm, "projected", "sources")
		for _, s := range sources {
			sm, _ := s.(map[string]interface{})
			// Kubernetes injects the cluster CA bundle into every pod's token
			// volume; it would only add noise to the map.
			if name := str(sm, "configMap", "name"); name != autoInjectedCA {
				add("ConfigMap", name, boolAt(sm, "configMap", "optional"))
			}
			add("Secret", str(sm, "secret", "name"), boolAt(sm, "secret", "optional"))
		}
	}
	for _, c := range containers(spec) {
		envFrom, _, _ := unstructured.NestedSlice(c, "envFrom")
		for _, e := range envFrom {
			em, _ := e.(map[string]interface{})
			add("ConfigMap", str(em, "configMapRef", "name"), boolAt(em, "configMapRef", "optional"))
			add("Secret", str(em, "secretRef", "name"), boolAt(em, "secretRef", "optional"))
		}
		env, _, _ := unstructured.NestedSlice(c, "env")
		for _, e := range env {
			em, _ := e.(map[string]interface{})
			add("ConfigMap", str(em, "valueFrom", "configMapKeyRef", "name"), boolAt(em, "valueFrom", "configMapKeyRef", "optional"))
			add("Secret", str(em, "valueFrom", "secretKeyRef", "name"), boolAt(em, "valueFrom", "secretKeyRef", "optional"))
		}
	}
	pullSecrets, _, _ := unstructured.NestedSlice(spec, "imagePullSecrets")
	for _, p := range pullSecrets {
		pm, _ := p.(map[string]interface{})
		add("Secret", str(pm, "name"), false)
	}
	return out
}

// securityFlags reports risky settings in a pod spec.
func securityFlags(spec map[string]interface{}) []string {
	if spec == nil {
		return nil
	}
	var flags []string
	if boolAt(spec, "hostNetwork") {
		flags = append(flags, FlagHostNetwork)
	}
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	for _, v := range volumes {
		if vm, _ := v.(map[string]interface{}); vm != nil {
			if _, ok := vm["hostPath"]; ok {
				flags = append(flags, FlagHostPath)
				break
			}
		}
	}
	podNonRoot := boolAt(spec, "securityContext", "runAsNonRoot")
	if uid, ok, _ := unstructured.NestedInt64(spec, "securityContext", "runAsUser"); ok && uid > 0 {
		podNonRoot = true
	}
	privileged, root, noLimits := false, false, false
	for _, c := range containers(spec) {
		if boolAt(c, "securityContext", "privileged") {
			privileged = true
		}
		nonRoot := podNonRoot || boolAt(c, "securityContext", "runAsNonRoot")
		if uid, ok, _ := unstructured.NestedInt64(c, "securityContext", "runAsUser"); ok {
			nonRoot = uid > 0
		}
		if !nonRoot {
			root = true
		}
		if str(c, "resources", "limits", "memory") == "" {
			noLimits = true
		}
	}
	if privileged {
		flags = append(flags, FlagPrivileged)
	}
	if root {
		flags = append(flags, FlagRoot)
	}
	if noLimits {
		flags = append(flags, FlagNoLimits)
	}
	return flags
}
