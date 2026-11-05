// Starter objects and the "common fields first" layout for the main kinds.
// Workload templates start with secure defaults: non-root, read-only root
// filesystem, no privilege escalation, all capabilities dropped, seccomp,
// and resource requests and limits.

import type { Path } from "./objpath";

export interface Essential {
  path: Path;
  label: string;
  hint?: string;
}

export interface KindInfo {
  apiVersion: string;
  kind: string;
  label: string;
  namespaced: boolean;
  essentials: Essential[];
  template?: (namespace: string) => Record<string, unknown>;
}

const container = ["spec", "template", "spec", "containers", 0];
const jobContainer = ["spec", "template", "spec", "containers", 0];
const cronContainer = ["spec", "jobTemplate", "spec", "template", "spec", "containers", 0];

const podSecurity = { runAsNonRoot: true, seccompProfile: { type: "RuntimeDefault" } };
const containerSecurity = {
  allowPrivilegeEscalation: false,
  readOnlyRootFilesystem: true,
  capabilities: { drop: ["ALL"] },
};

const nameAndNamespace: Essential[] = [
  { path: ["metadata", "name"], label: "Name" },
  { path: ["metadata", "namespace"], label: "Namespace" },
];

export const KINDS: KindInfo[] = [
  {
    apiVersion: "apps/v1",
    kind: "Deployment",
    label: "Deployment (app)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: ["spec", "replicas"], label: "Copies running", hint: "How many identical pods run at once." },
      { path: [...container, "image"], label: "Image", hint: "Container image, ideally pinned to a version or digest." },
      { path: [...container, "ports"], label: "Container ports" },
      { path: [...container, "env"], label: "Environment variables" },
      { path: [...container, "resources", "requests"], label: "Requests", hint: "Guaranteed CPU and memory, e.g. 100m and 128Mi." },
      { path: [...container, "resources", "limits"], label: "Limits", hint: "The most it may use; going over the memory limit restarts the container." },
      { path: ["spec", "template", "metadata", "labels"], label: "Pod labels", hint: "The selector must match these labels." },
    ],
    template: (ns) => ({
      apiVersion: "apps/v1",
      kind: "Deployment",
      metadata: { name: "web", namespace: ns, labels: { app: "web" } },
      spec: {
        replicas: 2,
        selector: { matchLabels: { app: "web" } },
        template: {
          metadata: { labels: { app: "web" } },
          spec: {
            securityContext: podSecurity,
            containers: [
              {
                name: "web",
                image: "nginxinc/nginx-unprivileged:1.27",
                ports: [{ name: "http", containerPort: 8080 }],
                resources: { requests: { cpu: "100m", memory: "128Mi" }, limits: { memory: "256Mi" } },
                readinessProbe: { httpGet: { path: "/", port: "http" }, periodSeconds: 10 },
                securityContext: containerSecurity,
                volumeMounts: [{ name: "tmp", mountPath: "/tmp" }],
              },
            ],
            volumes: [{ name: "tmp", emptyDir: {} }],
          },
        },
      },
    }),
  },
  {
    apiVersion: "v1",
    kind: "Service",
    label: "Service (stable address)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: ["spec", "type"], label: "Type", hint: "ClusterIP is reachable inside the cluster only." },
      { path: ["spec", "selector"], label: "Sends traffic to pods with labels" },
      { path: ["spec", "ports"], label: "Ports" },
    ],
    template: (ns) => ({
      apiVersion: "v1",
      kind: "Service",
      metadata: { name: "web", namespace: ns },
      spec: { type: "ClusterIP", selector: { app: "web" }, ports: [{ name: "http", port: 80, targetPort: "http" }] },
    }),
  },
  {
    apiVersion: "v1",
    kind: "ConfigMap",
    label: "ConfigMap (settings)",
    namespaced: true,
    essentials: [...nameAndNamespace, { path: ["data"], label: "Keys and values" }],
    template: (ns) => ({ apiVersion: "v1", kind: "ConfigMap", metadata: { name: "settings", namespace: ns }, data: { LOG_LEVEL: "info" } }),
  },
  {
    apiVersion: "v1",
    kind: "Namespace",
    label: "Namespace",
    namespaced: false,
    essentials: [{ path: ["metadata", "name"], label: "Name" }, { path: ["metadata", "labels"], label: "Labels" }],
    template: () => ({ apiVersion: "v1", kind: "Namespace", metadata: { name: "my-app" } }),
  },
  {
    apiVersion: "batch/v1",
    kind: "Job",
    label: "Job (run once)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: [...jobContainer, "image"], label: "Image" },
      { path: [...jobContainer, "command"], label: "Command" },
      { path: ["spec", "backoffLimit"], label: "Retries", hint: "How many times to retry before marking the job failed." },
    ],
    template: (ns) => ({
      apiVersion: "batch/v1",
      kind: "Job",
      metadata: { name: "once", namespace: ns },
      spec: {
        backoffLimit: 2,
        template: {
          spec: {
            restartPolicy: "Never",
            securityContext: podSecurity,
            containers: [
              {
                name: "task",
                image: "busybox:1.36",
                command: ["sh", "-c", "echo done"],
                resources: { requests: { cpu: "50m", memory: "32Mi" }, limits: { memory: "64Mi" } },
                securityContext: { ...containerSecurity, runAsUser: 65534 },
              },
            ],
          },
        },
      },
    }),
  },
  {
    apiVersion: "batch/v1",
    kind: "CronJob",
    label: "CronJob (scheduled)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: ["spec", "schedule"], label: "Schedule", hint: "Cron format, e.g. 0 3 * * * for every day at 03:00." },
      { path: [...cronContainer, "image"], label: "Image" },
      { path: [...cronContainer, "command"], label: "Command" },
    ],
    template: (ns) => ({
      apiVersion: "batch/v1",
      kind: "CronJob",
      metadata: { name: "nightly", namespace: ns },
      spec: {
        schedule: "0 3 * * *",
        jobTemplate: {
          spec: {
            template: {
              spec: {
                restartPolicy: "OnFailure",
                securityContext: podSecurity,
                containers: [
                  {
                    name: "task",
                    image: "busybox:1.36",
                    command: ["sh", "-c", "date"],
                    resources: { requests: { cpu: "50m", memory: "32Mi" }, limits: { memory: "64Mi" } },
                    securityContext: { ...containerSecurity, runAsUser: 65534 },
                  },
                ],
              },
            },
          },
        },
      },
    }),
  },
  {
    apiVersion: "v1",
    kind: "PersistentVolumeClaim",
    label: "PersistentVolumeClaim (storage)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: ["spec", "resources", "requests", "storage"], label: "Size", hint: "For example 1Gi." },
      { path: ["spec", "accessModes"], label: "Access modes" },
      { path: ["spec", "storageClassName"], label: "Storage class" },
    ],
    template: (ns) => ({
      apiVersion: "v1",
      kind: "PersistentVolumeClaim",
      metadata: { name: "data", namespace: ns },
      spec: { accessModes: ["ReadWriteOnce"], resources: { requests: { storage: "1Gi" } } },
    }),
  },
  {
    apiVersion: "networking.k8s.io/v1",
    kind: "Ingress",
    label: "Ingress",
    namespaced: true,
    essentials: [...nameAndNamespace, { path: ["spec", "ingressClassName"], label: "Ingress class" }, { path: ["spec", "rules"], label: "Rules" }],
  },
  {
    apiVersion: "gateway.networking.k8s.io/v1",
    kind: "HTTPRoute",
    label: "HTTPRoute (Gateway API)",
    namespaced: true,
    essentials: [
      ...nameAndNamespace,
      { path: ["spec", "parentRefs"], label: "Gateways" },
      { path: ["spec", "hostnames"], label: "Hostnames" },
      { path: ["spec", "rules"], label: "Rules" },
    ],
  },
];

export function kindInfo(apiVersion: string, kind: string): KindInfo | undefined {
  return KINDS.find((k) => k.apiVersion === apiVersion && k.kind === kind);
}

/** Essentials for any kind: the curated list, or name and namespace. */
export function essentialsFor(apiVersion: string, kind: string): Essential[] {
  return kindInfo(apiVersion, kind)?.essentials ?? nameAndNamespace;
}

export function splitApiVersion(apiVersion: string): { group: string; version: string } {
  const i = apiVersion.indexOf("/");
  return i < 0 ? { group: "", version: apiVersion } : { group: apiVersion.slice(0, i), version: apiVersion.slice(i + 1) };
}

/** Warnings about selectors that don't match their own pod template. */
export function selectorWarnings(obj: unknown): string[] {
  const o = obj as { kind?: string; spec?: { selector?: { matchLabels?: Record<string, string> }; template?: { metadata?: { labels?: Record<string, string> } } } };
  if (!o || !["Deployment", "StatefulSet", "DaemonSet", "ReplicaSet"].includes(o.kind ?? "")) return [];
  const selector = o.spec?.selector?.matchLabels ?? {};
  const labels = o.spec?.template?.metadata?.labels ?? {};
  const missing = Object.entries(selector).filter(([k, v]) => labels[k] !== v);
  if (Object.keys(selector).length === 0) return ["The selector is empty. Add labels under spec.selector.matchLabels that match the pod labels."];
  return missing.length > 0
    ? [`The selector doesn't match the pod labels (${missing.map(([k, v]) => `${k}=${v}`).join(", ")}). The cluster will reject this.`]
    : [];
}
