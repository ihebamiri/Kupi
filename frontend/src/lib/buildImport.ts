// Opening a live namespace on the Build canvas: the Map's graph says what
// exists and how it connects, and each object's YAML fills in a block. Blocks
// keep the live object, so only what's changed on the canvas is written back.

import type { Block, BlockKind, Link, Sidecar } from "./build";
import type { GraphEdge, GraphNode } from "./canvas";

type Obj = Record<string, unknown>;

const IMPORTABLE = new Set<BlockKind>(["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Service", "Ingress", "ConfigMap", "Secret", "PersistentVolumeClaim", "HorizontalPodAutoscaler"]);

/** Whether the canvas has a block for this kind of object. */
export function importable(kind: string): kind is BlockKind {
  return IMPORTABLE.has(kind as BlockKind);
}

export interface ImportResult {
  blocks: Block[];
  links: Link[];
  /** Objects left off the canvas, and why. */
  skipped: string[];
  /** Block id for each graph node id that became a block. */
  idOf: Map<string, string>;
}

function o(v: unknown): Obj {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : {};
}

function arr(v: unknown): Obj[] {
  return Array.isArray(v) ? (v.filter((x) => x && typeof x === "object") as Obj[]) : [];
}

function str(v: unknown): string {
  return v === undefined || v === null ? "" : String(v);
}

function podSpec(kind: string, obj: Obj): Obj {
  const spec = o(obj.spec);
  return kind === "CronJob" ? o(o(o(o(spec.jobTemplate).spec).template).spec) : o(o(spec.template).spec);
}

/** A command the canvas can show and write back without losing anything. */
function commandText(c: Obj): { text: string; exact: boolean } {
  const cmd = Array.isArray(c.command) ? c.command.map(String) : [];
  return { text: cmd.join(" "), exact: !cmd.some((w) => /\s/.test(w) || w === "") };
}

/** Which tool manages an object, judging by the labels and annotations those tools set. */
export function managedBy(obj: Obj): string | undefined {
  const meta = o(obj.metadata);
  const labels = o(meta.labels) as Record<string, string>;
  const notes = o(meta.annotations) as Record<string, string>;
  if (labels["app.kubernetes.io/managed-by"] === "Helm" || notes["meta.helm.sh/release-name"]) return "Helm";
  if (Object.keys(notes).some((k) => k.startsWith("argocd.argoproj.io/")) || labels["argocd.argoproj.io/instance"]) return "Argo CD";
  if (labels["kustomize.toolkit.fluxcd.io/name"] || labels["helm.toolkit.fluxcd.io/name"]) return "Flux";
  return undefined;
}

/** The canvas fields of a live object, plus fields the canvas can't round-trip faithfully. */
export function fieldsOf(kind: BlockKind, obj: Obj): { fields: Record<string, string>; sidecars?: Sidecar[]; locked: string[] } {
  const spec = o(obj.spec);
  const locked: string[] = [];
  switch (kind) {
    case "Deployment":
    case "StatefulSet":
    case "DaemonSet":
    case "Job":
    case "CronJob": {
      const containers = arr(podSpec(kind, obj).containers);
      const main = containers[0] ?? {};
      const cmd = commandText(main);
      if (!cmd.exact) locked.push("command");
      const ports = arr(main.ports);
      if (ports.some((p) => p.protocol && p.protocol !== "TCP")) locked.push("ports");
      const fields: Record<string, string> = {
        image: str(main.image),
        command: cmd.text,
      };
      if (kind === "Deployment" || kind === "StatefulSet" || kind === "DaemonSet")
        fields.ports = ports.map((p) => (p.name ? `${str(p.name)}=${str(p.containerPort)}` : str(p.containerPort))).join("\n");
      if (kind === "Deployment" || kind === "StatefulSet") fields.replicas = spec.replicas === undefined ? "" : str(spec.replicas);
      if (kind === "CronJob") fields.schedule = str(spec.schedule);
      const sidecars = containers.slice(1).map((c) => {
        const sc = commandText(c);
        if (!sc.exact) locked.push("sidecars");
        return { name: str(c.name), image: str(c.image), command: sc.text };
      });
      return { fields, sidecars, locked };
    }
    case "Service": {
      const ports = arr(spec.ports);
      if (ports.some((p) => p.protocol && p.protocol !== "TCP")) locked.push("ports");
      return {
        fields: { ports: ports.map((p) => `${str(p.port)}:${str(p.targetPort ?? p.port)}`).join("\n"), type: str(spec.type) || "ClusterIP" },
        locked,
      };
    }
    case "Ingress": {
      const rules = arr(spec.rules);
      if (rules.length > 1) locked.push("host");
      return { fields: { host: str(rules[0]?.host), className: str(spec.ingressClassName) }, locked };
    }
    case "ConfigMap": {
      const data = o(obj.data) as Record<string, string>;
      // KEY=value lines can't hold multi-line values.
      if (Object.values(data).some((v) => /[\r\n]/.test(String(v)))) locked.push("data");
      return { fields: { data: Object.entries(data).map(([k, v]) => `${k}=${v}`).join("\n") }, locked };
    }
    case "PersistentVolumeClaim":
      return { fields: { size: str(o(o(spec.resources).requests).storage), storageClass: str(spec.storageClassName) }, locked };
    case "HorizontalPodAutoscaler": {
      const cpu = arr(spec.metrics).find((m) => m.type === "Resource" && o(m.resource).name === "cpu");
      const percent = o(o(cpu?.resource).target).averageUtilization;
      if (percent === undefined) locked.push("cpu");
      return { fields: { min: str(spec.minReplicas ?? 1), max: str(spec.maxReplicas), cpu: str(percent) }, locked };
    }
    case "Secret":
      return { fields: {}, locked };
  }
}

// Annotations Kubernetes' own controllers keep up to date. Writing them back
// would claim them for Kupi and conflict with the controller later.
const CONTROLLER_ANNOTATIONS = ["deployment.kubernetes.io/revision"];

function withoutControllerFields(obj: Obj | undefined): Obj | undefined {
  if (!obj) return undefined;
  const out = structuredClone(obj);
  const meta = o(out.metadata);
  const notes = { ...(o(meta.annotations) as Record<string, string>) };
  for (const k of CONTROLLER_ANNOTATIONS) delete notes[k];
  if (Object.keys(notes).length) meta.annotations = notes;
  else delete meta.annotations;
  return out;
}

/** Claims a StatefulSet made for its copies: <template>-<statefulset>-<n>. */
function perCopyClaims(nodes: GraphNode[], objects: Map<string, Obj>): Map<string, string> {
  const owner = new Map<string, string>();
  for (const n of nodes) {
    if (n.kind !== "StatefulSet") continue;
    for (const t of arr(o(objects.get(n.id)?.spec).volumeClaimTemplates)) {
      const prefix = `${str(o(t.metadata).name)}-${n.name}-`;
      for (const c of nodes) if (c.kind === "PersistentVolumeClaim" && c.name.startsWith(prefix) && /^\d+$/.test(c.name.slice(prefix.length))) owner.set(c.id, n.name);
    }
  }
  return owner;
}

/**
 * Blocks and links for a live namespace. objects holds each object's
 * cleaned JSON by graph node id (Secrets have none: they become references).
 */
export function importGraph(nodes: GraphNode[], edges: GraphEdge[], objects: Map<string, Obj>, newId: () => string): ImportResult {
  const blocks: Block[] = [];
  const links: Link[] = [];
  const skipped: string[] = [];
  const blockOf = new Map<string, Block>();
  const claimOwner = perCopyClaims(nodes, objects);

  for (const n of nodes) {
    if ((n.flags ?? []).includes("missing")) {
      skipped.push(`${n.kind} ${n.name}: referred to, but doesn't exist`);
      continue;
    }
    if (!importable(n.kind)) {
      skipped.push(`${n.kind} ${n.name}: no block for this kind yet`);
      continue;
    }
    if (claimOwner.has(n.id)) {
      skipped.push(`${n.kind} ${n.name}: a per-copy claim of StatefulSet ${claimOwner.get(n.id)}`);
      continue;
    }
    const kind = n.kind;
    if (kind === "Secret") {
      const b: Block = { id: newId(), kind, name: n.name, fields: {}, source: { apiVersion: "v1", kind: "Secret", metadata: { name: n.name, namespace: n.namespace } }, origFields: {} };
      blocks.push(b);
      blockOf.set(n.id, b);
      continue;
    }
    const obj = withoutControllerFields(objects.get(n.id));
    if (!obj) {
      skipped.push(`${kind} ${n.name}: couldn't be read`);
      continue;
    }
    const { fields, sidecars, locked } = fieldsOf(kind, obj);
    const b: Block = {
      id: newId(),
      kind,
      name: n.name,
      fields: { ...fields },
      source: obj,
      origFields: { ...fields },
      locked,
      managedBy: managedBy(obj),
    };
    if (sidecars) {
      b.sidecars = sidecars.map((s) => ({ ...s }));
      b.origSidecars = sidecars.map((s) => ({ ...s }));
    }
    blocks.push(b);
    blockOf.set(n.id, b);
  }

  for (const e of edges) {
    const from = blockOf.get(e.source);
    const to = blockOf.get(e.target);
    if (!from || !to) continue;
    const kind = e.kind === "uses" ? "env" : e.kind;
    if (!["selects", "routes", "env", "mounts", "scales"].includes(kind)) continue;
    const link: Link = { id: newId(), from: from.id, to: to.id, kind: kind as Link["kind"], imported: true };
    if (kind === "routes") {
      const path = arr(o(from.source?.spec).rules)
        .flatMap((r) => arr(o(r.http).paths))
        .find((p) => str(o(o(p.backend).service).name) === to.name);
      const port = o(o(o(path?.backend).service).port);
      link.fields = { path: str(path?.path) || "/", port: port.number !== undefined ? str(port.number) : "" };
      link.origFields = { ...link.fields };
    }
    links.push(link);
  }
  return { blocks, links, skipped, idOf: new Map([...blockOf.entries()].map(([nid, b]) => [nid, b.id])) };
}
