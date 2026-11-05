// Pure helpers for the Observe canvas: overlays and neighbourhoods. Layout
// lives in canvasLayout.ts so these stay testable without the layout engine.

export interface GraphNode {
  id: string;
  kind: string;
  group: string;
  version: string;
  resource: string;
  name: string;
  namespace: string;
  uid: string;
  created: string;
  labels: { [key: string]: string | undefined } | null;
  health: string;
  summary: string;
  pods: number;
  ready: number;
  restarts: number;
  flags: string[] | null;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  kind: string;
}

export type Overlay = "health" | "exposure" | "security";

const SERIOUS = new Set(["privileged", "hostNetwork", "hostPath"]);
const RISKY = new Set(["mayRunAsRoot", "noMemoryLimit"]);

/** Nodes reachable from the internet: public entry points and everything they route or select. */
export function exposed(nodes: GraphNode[], edges: GraphEdge[]): Set<string> {
  const out = new Set<string>();
  const queue = nodes.filter((n) => (n.flags ?? []).includes("public")).map((n) => n.id);
  queue.forEach((id) => out.add(id));
  const next = new Map<string, string[]>();
  for (const e of edges) {
    if (e.kind !== "routes" && e.kind !== "selects") continue;
    next.set(e.source, [...(next.get(e.source) ?? []), e.target]);
  }
  while (queue.length > 0) {
    const id = queue.shift()!;
    for (const t of next.get(id) ?? []) {
      if (!out.has(t)) {
        out.add(t);
        queue.push(t);
      }
    }
  }
  return out;
}

/** Security level of a node for the security overlay. */
export function securityLevel(node: GraphNode): "serious" | "risky" | "ok" {
  const flags = node.flags ?? [];
  if (flags.some((f) => SERIOUS.has(f))) return "serious";
  if (flags.some((f) => RISKY.has(f))) return "risky";
  return "ok";
}

/** Everything within `hops` links of a node, in either direction: what it affects and what affects it. */
export function neighbourhood(id: string, edges: GraphEdge[], hops = 2): Set<string> {
  const adj = new Map<string, string[]>();
  for (const e of edges) {
    adj.set(e.source, [...(adj.get(e.source) ?? []), e.target]);
    adj.set(e.target, [...(adj.get(e.target) ?? []), e.source]);
  }
  const seen = new Set([id]);
  let frontier = [id];
  for (let i = 0; i < hops; i++) {
    const nextFrontier: string[] = [];
    for (const n of frontier) {
      for (const m of adj.get(n) ?? []) {
        if (!seen.has(m)) {
          seen.add(m);
          nextFrontier.push(m);
        }
      }
    }
    frontier = nextFrontier;
  }
  return seen;
}

/** Workloads that use a ConfigMap, Secret or claim: what a change to it affects. */
export function dependents(id: string, edges: GraphEdge[]): string[] {
  return edges.filter((e) => e.target === id && (e.kind === "uses" || e.kind === "mounts")).map((e) => e.source);
}

/** Readable explanation of a flag. */
export const FLAG_TEXT: Record<string, string> = {
  public: "Reachable from outside the cluster",
  privileged: "Runs a privileged container",
  hostNetwork: "Uses the node's network",
  hostPath: "Mounts a path from the node",
  mayRunAsRoot: "May run as root",
  noMemoryLimit: "Has a container with no memory limit",
  missing: "Referenced but doesn't exist",
};
