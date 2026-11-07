/**
 * Turns a Kubernetes field manager name (from managedFields) into words
 * people recognise. Unknown managers are shown as they are.
 */
const KNOWN: [RegExp, string][] = [
  [/^kupi$/, "Kupi"],
  [/^kubectl-client-side-apply$/, "kubectl apply"],
  // kubectl names itself after its executable unless a command sets a
  // manager: server-side apply, scale and others all show as plain kubectl.
  [/^kubectl(\.exe)?$/, "kubectl"],
  [/^kubectl-(edit|patch|scale|rollout|label|annotate|set|create|replace|expose|autoscale)$/, "kubectl $1"],
  [/^helm$/, "Helm"],
  [/^argocd-(application-)?controller$/, "Argo CD"],
  [/^argocd/, "Argo CD"],
  [/^kustomize-controller$/, "Flux"],
  [/^helm-controller$/, "Flux (Helm)"],
  [/^kube-controller-manager$/, "Kubernetes"],
  [/^k3s$/, "k3s"],
  [/^Go-http-client$/, "a Go program"],
  [/^(terraform|tf-)/i, "Terraform"],
  [/^pulumi/i, "Pulumi"],
  [/^Mozilla/, "Kubernetes Dashboard or a browser"],
];

export function managerLabel(manager: string): string {
  if (!manager) return "unknown";
  for (const [re, label] of KNOWN) {
    const m = manager.match(re);
    if (m) return label.replace("$1", m[1] ?? "");
  }
  return manager;
}

/** API group and resource for the kinds Kupi links to. */
export const KIND_TYPES: Record<string, { group: string; resource: string }> = {
  Pod: { group: "", resource: "pods" },
  Service: { group: "", resource: "services" },
  ConfigMap: { group: "", resource: "configmaps" },
  Secret: { group: "", resource: "secrets" },
  PersistentVolumeClaim: { group: "", resource: "persistentvolumeclaims" },
  Node: { group: "", resource: "nodes" },
  Deployment: { group: "apps", resource: "deployments" },
  StatefulSet: { group: "apps", resource: "statefulsets" },
  DaemonSet: { group: "apps", resource: "daemonsets" },
  ReplicaSet: { group: "apps", resource: "replicasets" },
  Job: { group: "batch", resource: "jobs" },
  CronJob: { group: "batch", resource: "cronjobs" },
  Ingress: { group: "networking.k8s.io", resource: "ingresses" },
  NetworkPolicy: { group: "networking.k8s.io", resource: "networkpolicies" },
  HorizontalPodAutoscaler: { group: "autoscaling", resource: "horizontalpodautoscalers" },
};

/** Kinds Kupi records history for (internal/history Types). */
export const RECORDED_KINDS = [
  "Deployment",
  "StatefulSet",
  "DaemonSet",
  "CronJob",
  "Service",
  "Ingress",
  "NetworkPolicy",
  "ConfigMap",
  "Secret",
  "HorizontalPodAutoscaler",
  "PersistentVolumeClaim",
];
