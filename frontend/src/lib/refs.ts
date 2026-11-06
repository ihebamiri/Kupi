// Which form fields name an existing object, so the form can suggest names.
import { getIn, type Path } from "./objpath";

/** Object types the backend can suggest (FormService.Suggest). */
export type RefType =
  | "namespaces"
  | "configmaps"
  | "secrets"
  | "serviceaccounts"
  | "services"
  | "persistentvolumeclaims"
  | "persistentvolumes"
  | "nodes"
  | "deployments"
  | "statefulsets"
  | "replicasets"
  | "storageclasses"
  | "ingressclasses"
  | "priorityclasses"
  | "roles"
  | "clusterroles";

export const CLUSTER_WIDE: ReadonlySet<RefType> = new Set(["namespaces", "persistentvolumes", "nodes", "storageclasses", "ingressclasses", "priorityclasses", "clusterroles"]);

export interface Ref {
  /** Names of objects of this type are suggested. */
  type: RefType;
  /** Suggest the keys of this ConfigMap or Secret instead of object names. */
  keysOf?: { type: "configmaps" | "secrets"; name: string };
  /** Namespace to look in, when the field itself says (RBAC subjects). */
  namespace?: string;
}

type Rule = { pattern: string; type: RefType | ((root: unknown, path: Path) => RefType | null) } | { pattern: string; keys: "configmaps" | "secrets" };

// Patterns match the end of the field path, with "*" for any array index.
const RULES: Rule[] = [
  { pattern: "metadata.namespace", type: "namespaces" },
  { pattern: "configMapKeyRef.name", type: "configmaps" },
  { pattern: "configMapKeyRef.key", keys: "configmaps" },
  { pattern: "configMapRef.name", type: "configmaps" },
  { pattern: "configMap.name", type: "configmaps" },
  { pattern: "secretKeyRef.name", type: "secrets" },
  { pattern: "secretKeyRef.key", keys: "secrets" },
  { pattern: "secretRef.name", type: "secrets" },
  { pattern: "secret.secretName", type: "secrets" },
  { pattern: "imagePullSecrets.*.name", type: "secrets" },
  { pattern: "tls.*.secretName", type: "secrets" },
  { pattern: "persistentVolumeClaim.claimName", type: "persistentvolumeclaims" },
  { pattern: "serviceAccountName", type: "serviceaccounts" },
  { pattern: "storageClassName", type: "storageclasses" },
  { pattern: "ingressClassName", type: "ingressclasses" },
  { pattern: "priorityClassName", type: "priorityclasses" },
  { pattern: "nodeName", type: "nodes" },
  { pattern: "spec.volumeName", type: "persistentvolumes" },
  { pattern: "backend.service.name", type: "services" },
  { pattern: "defaultBackend.service.name", type: "services" },
  { pattern: "spec.serviceName", type: "services" },
  {
    pattern: "scaleTargetRef.name",
    type: (root, path) => kindToType(getIn(root as never, [...path.slice(0, -1), "kind"])),
  },
  {
    pattern: "roleRef.name",
    type: (root, path) => (getIn(root as never, [...path.slice(0, -1), "kind"]) === "ClusterRole" ? "clusterroles" : "roles"),
  },
  {
    pattern: "subjects.*.name",
    type: (root, path) => (getIn(root as never, [...path.slice(0, -1), "kind"]) === "ServiceAccount" ? "serviceaccounts" : null),
  },
];

function kindToType(kind: unknown): RefType | null {
  switch (kind) {
    case "Deployment":
      return "deployments";
    case "StatefulSet":
      return "statefulsets";
    case "ReplicaSet":
      return "replicasets";
    default:
      return null;
  }
}

function matches(path: Path, pattern: string): boolean {
  const parts = pattern.split(".");
  if (parts.length > path.length) return false;
  const tail = path.slice(path.length - parts.length);
  return parts.every((p, i) => (p === "*" ? typeof tail[i] === "number" : tail[i] === p));
}

/** The reference a string field holds, or null when it names nothing. */
export function referenceFor(path: Path, root: unknown): Ref | null {
  // metadata.namespace only at the top: nested metadata (pod templates) has none.
  if (path.length === 2 && path[0] === "metadata" && path[1] === "namespace") return { type: "namespaces" };
  for (const rule of RULES) {
    if (rule.pattern === "metadata.namespace" || !matches(path, rule.pattern)) continue;
    if ("keys" in rule) {
      const name = getIn(root as never, [...path.slice(0, -1), "name"]);
      if (typeof name !== "string" || name === "") return null;
      return { type: rule.keys, keysOf: { type: rule.keys, name } };
    }
    const type = typeof rule.type === "function" ? rule.type(root, path) : rule.type;
    if (!type) return null;
    if (rule.pattern === "subjects.*.name") {
      const ns = getIn(root as never, [...path.slice(0, -1), "namespace"]);
      return { type, namespace: typeof ns === "string" && ns ? ns : undefined };
    }
    return { type };
  }
  return null;
}

/** Which slider, if any, a field gets. */
export function sliderFor(path: Path, kind: string): "cpu" | "memory" | "storage" | "replicas" | null {
  const last = path[path.length - 1];
  const parent = path[path.length - 2];
  const grand = path[path.length - 3];
  if ((parent === "requests" || parent === "limits") && grand === "resources") {
    if (last === "cpu") return "cpu";
    if (last === "memory") return "memory";
    if (last === "storage" || last === "ephemeral-storage") return "storage";
  }
  if (last === "replicas" && path.length === 2 && path[0] === "spec") return "replicas";
  if ((last === "minReplicas" || last === "maxReplicas") && kind === "HorizontalPodAutoscaler") return "replicas";
  return null;
}
