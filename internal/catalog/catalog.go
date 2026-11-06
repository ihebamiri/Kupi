// Package catalog sorts resource types into the groups the UI shows:
// workloads, network, config, storage, access, cluster and custom resources.
package catalog

import (
	"sort"
	"strings"

	"github.com/ihebamiri/kupi/internal/kube"
)

// Category is a sidebar group.
type Category string

const (
	Workloads Category = "workloads"
	Network   Category = "network"
	Config    Category = "config"
	Storage   Category = "storage"
	Access    Category = "access"
	Cluster   Category = "cluster"
	Custom    Category = "custom"
)

// Order is the sidebar order of the categories.
var Order = []Category{Workloads, Network, Config, Storage, Access, Cluster, Custom}

// known maps "group/resource" to a category; the list order is the order
// within the category.
var known = []struct {
	key string
	cat Category
}{
	{"/pods", Workloads}, {"apps/deployments", Workloads}, {"apps/statefulsets", Workloads},
	{"apps/daemonsets", Workloads}, {"apps/replicasets", Workloads}, {"batch/jobs", Workloads},
	{"batch/cronjobs", Workloads}, {"autoscaling/horizontalpodautoscalers", Workloads},
	{"policy/poddisruptionbudgets", Workloads},

	{"/services", Network}, {"networking.k8s.io/ingresses", Network},
	{"networking.k8s.io/networkpolicies", Network}, {"discovery.k8s.io/endpointslices", Network},
	{"networking.k8s.io/ingressclasses", Network}, {"/endpoints", Network},

	{"/configmaps", Config}, {"/secrets", Config}, {"/resourcequotas", Config}, {"/limitranges", Config},
	{"scheduling.k8s.io/priorityclasses", Config},

	{"/persistentvolumeclaims", Storage}, {"/persistentvolumes", Storage}, {"storage.k8s.io/storageclasses", Storage},
	{"storage.k8s.io/volumeattachments", Storage}, {"storage.k8s.io/csidrivers", Storage}, {"storage.k8s.io/csinodes", Storage},

	{"/serviceaccounts", Access}, {"rbac.authorization.k8s.io/roles", Access},
	{"rbac.authorization.k8s.io/rolebindings", Access}, {"rbac.authorization.k8s.io/clusterroles", Access},
	{"rbac.authorization.k8s.io/clusterrolebindings", Access},

	{"/nodes", Cluster}, {"/namespaces", Cluster}, {"/events", Cluster},
	{"apiextensions.k8s.io/customresourcedefinitions", Cluster}, {"coordination.k8s.io/leases", Cluster},
	{"node.k8s.io/runtimeclasses", Cluster}, {"apiregistration.k8s.io/apiservices", Cluster},
	{"admissionregistration.k8s.io/mutatingwebhookconfigurations", Cluster},
	{"admissionregistration.k8s.io/validatingwebhookconfigurations", Cluster},
	{"admissionregistration.k8s.io/validatingadmissionpolicies", Cluster},
	{"admissionregistration.k8s.io/validatingadmissionpolicybindings", Cluster},
	{"certificates.k8s.io/certificatesigningrequests", Cluster},
}

// groupCategories places whole API groups.
var groupCategories = map[string]Category{
	"gateway.networking.k8s.io":    Network,
	"snapshot.storage.k8s.io":      Storage,
	"flowcontrol.apiserver.k8s.io": Cluster,
	"metrics.k8s.io":               Cluster,
	"resource.k8s.io":              Cluster,
}

// hidden types duplicate others or are deprecated.
var hidden = map[string]bool{
	"events.k8s.io/events":    true, // same objects as /events
	"/componentstatuses":      true, // deprecated
	"/podtemplates":           true,
	"/replicationcontrollers": true, // superseded by Deployments
}

var position = func() map[string]int {
	m := make(map[string]int, len(known))
	for i, k := range known {
		m[k.key] = i
	}
	return m
}()

// Entry is a categorised resource type.
type Entry struct {
	kube.ResourceType
	Category Category
	Label    string
}

// Build categorises, filters and sorts resource types for the sidebar.
func Build(types []kube.ResourceType) []Entry {
	var out []Entry
	for _, t := range types {
		key := t.Group + "/" + t.Resource
		if hidden[key] {
			continue
		}
		cat := Custom
		for _, k := range known {
			if k.key == key {
				cat = k.cat
				break
			}
		}
		if cat == Custom {
			if c, ok := groupCategories[t.Group]; ok {
				cat = c
			}
		}
		out = append(out, Entry{ResourceType: t, Category: cat, Label: Plural(t.Kind)})
	}
	rank := map[Category]int{}
	for i, c := range Order {
		rank[c] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Category != b.Category {
			return rank[a.Category] < rank[b.Category]
		}
		pa, oka := position[a.Group+"/"+a.Resource]
		pb, okb := position[b.Group+"/"+b.Resource]
		switch {
		case oka && okb:
			return pa < pb
		case oka != okb:
			return oka
		case a.Group != b.Group:
			return a.Group < b.Group
		}
		return a.Kind < b.Kind
	})
	return out
}

// Plural turns a Kind into a sidebar label: Deployment -> Deployments,
// NetworkPolicy -> NetworkPolicies, Ingress -> Ingresses.
func Plural(kind string) string {
	switch {
	case kind == "" || kind == "Endpoints":
		return kind
	case strings.HasSuffix(kind, "y") && len(kind) > 1 && !strings.ContainsRune("aeiou", rune(kind[len(kind)-2])):
		return kind[:len(kind)-1] + "ies"
	case strings.HasSuffix(kind, "ss"), strings.HasSuffix(kind, "x"), strings.HasSuffix(kind, "ch"), strings.HasSuffix(kind, "sh"):
		return kind + "es"
	case strings.HasSuffix(kind, "s"):
		return kind
	}
	return kind + "s"
}
