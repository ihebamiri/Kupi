import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import type { GraphEdge, GraphNode } from "./canvas";

export const NODE_W = 230;
export const NODE_H = 68;

const elk = new ELK();

export interface Positioned {
  id: string;
  x: number;
  y: number;
  /** Set for namespace frames (when showing all namespaces). */
  width?: number;
  height?: number;
  parent?: string;
}

/**
 * Lays the graph out left to right: entry points, Services, workloads, then
 * the config and storage they use. With several namespaces, each becomes a
 * frame around its own objects.
 */
export async function layout(nodes: GraphNode[], edges: GraphEdge[], groupByNamespace: boolean): Promise<Positioned[]> {
  const layoutOptions = {
    "elk.algorithm": "layered",
    "elk.direction": "RIGHT",
    "elk.layered.spacing.nodeNodeBetweenLayers": "70",
    "elk.spacing.nodeNode": "22",
    "elk.layered.nodePlacement.strategy": "BRANDES_KOEPF",
    "elk.hierarchyHandling": "INCLUDE_CHILDREN",
    "elk.padding": "[top=36,left=16,bottom=16,right=16]",
  };
  const leaf = (n: GraphNode): ElkNode => ({ id: n.id, width: NODE_W, height: NODE_H });
  const elkEdges: ElkExtendedEdge[] = edges.map((e) => ({ id: e.id, sources: [e.source], targets: [e.target] }));
  let children: ElkNode[];
  if (groupByNamespace) {
    const byNs = new Map<string, GraphNode[]>();
    for (const n of nodes) byNs.set(n.namespace || "(cluster)", [...(byNs.get(n.namespace || "(cluster)") ?? []), n]);
    children = [...byNs.entries()].map(([ns, list]) => ({ id: `ns:${ns}`, layoutOptions, children: list.map(leaf) }));
  } else {
    children = nodes.map(leaf);
  }
  const result = await elk.layout({ id: "root", layoutOptions, children, edges: elkEdges });
  const out: Positioned[] = [];
  for (const c of result.children ?? []) {
    if (c.children) {
      out.push({ id: c.id, x: c.x ?? 0, y: c.y ?? 0, width: c.width, height: c.height });
      for (const g of c.children) out.push({ id: g.id, x: g.x ?? 0, y: g.y ?? 0, parent: c.id });
    } else {
      out.push({ id: c.id, x: c.x ?? 0, y: c.y ?? 0 });
    }
  }
  return out;
}
