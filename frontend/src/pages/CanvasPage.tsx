import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Background, Controls, MiniMap, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { RefreshCw, Search } from "lucide-react";
import { CanvasNode, NamespaceFrame, type CanvasNodeData } from "../components/CanvasNode";
import { DetailPanel } from "../components/DetailPanel";
import { Button, Notice } from "../components/ui";
import { CanvasService, errorMessage, type TableRow } from "../lib/api";
import { FLAG_TEXT, dependents, exposed, neighbourhood, securityLevel, type GraphEdge, type GraphNode, type Overlay } from "../lib/canvas";
import { layout, type Positioned } from "../lib/canvasLayout";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";

const REFRESH_MS = 15_000;
const LARGE = 400;
const nodeTypes = { kupi: CanvasNode, frame: NamespaceFrame };

const MINIMAP_COLOR: Record<string, string> = { good: "#3ccf91", warn: "#f2b84b", bad: "#f06a6a", unknown: "#9aa3b2" };

const EDGE_STYLE: Record<string, { stroke: string; dash?: string }> = {
  routes: { stroke: "var(--kupi-accent)" },
  selects: { stroke: "var(--kupi-muted)" },
  uses: { stroke: "var(--kupi-muted)", dash: "5 4" },
  mounts: { stroke: "var(--kupi-muted)", dash: "5 4" },
  scales: { stroke: "var(--kupi-muted)", dash: "2 4" },
};

interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
  warnings: string[];
  truncated: boolean;
}

export function CanvasPage() {
  return (
    <ReactFlowProvider>
      <Canvas />
    </ReactFlowProvider>
  );
}

/** Live map of how the objects in a namespace connect. */
function Canvas() {
  const { active, namespace } = useClusters();
  const { findType } = useNav();
  const { fitView } = useReactFlow();
  const [graph, setGraph] = useState<GraphData | null>(null);
  const [positions, setPositions] = useState<Positioned[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [overlay, setOverlay] = useState<Overlay>("health");
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const layoutKey = useRef("");
  const contextName = active?.name ?? "";

  const load = useCallback(async () => {
    if (!contextName) return;
    setLoading(true);
    try {
      const g = await CanvasService.Graph(contextName, namespace);
      if (!g) return;
      const data: GraphData = { nodes: g.nodes ?? [], edges: g.edges ?? [], warnings: g.warnings ?? [], truncated: g.truncated };
      setGraph(data);
      setError(null);
      // Re-layout only when the set of objects changes, so the map doesn't jump on every refresh.
      const key = data.nodes.map((n) => n.id).join("|") + "#" + data.edges.map((e) => e.id).join("|");
      if (key !== layoutKey.current) {
        layoutKey.current = key;
        setPositions(await layout(data.nodes, data.edges, namespace === ""));
        window.setTimeout(() => void fitView({ padding: 0.15, duration: 300 }), 50);
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [contextName, namespace, fitView]);

  useEffect(() => {
    layoutKey.current = "";
    setSelected(null);
    void load();
    const timer = window.setInterval(() => void load(), REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  const nodesById = useMemo(() => new Map((graph?.nodes ?? []).map((n) => [n.id, n])), [graph]);
  const exposedSet = useMemo(() => (graph ? exposed(graph.nodes, graph.edges) : new Set<string>()), [graph]);
  const focus = useMemo(() => (selected && graph ? neighbourhood(selected, graph.edges, 2) : null), [selected, graph]);
  const q = query.trim().toLowerCase();

  const flowNodes: Node[] = useMemo(() => {
    const out: Node[] = [];
    for (const p of positions) {
      if (p.id.startsWith("ns:")) {
        out.push({ id: p.id, type: "frame", position: { x: p.x, y: p.y }, data: { label: p.id.slice(3) }, style: { width: p.width, height: p.height }, selectable: false, draggable: false, zIndex: -1 });
        continue;
      }
      const n = nodesById.get(p.id);
      if (!n) continue;
      let ring: CanvasNodeData["ring"] = null;
      let dimmed = false;
      if (overlay === "exposure") {
        ring = exposedSet.has(n.id) ? "accent" : null;
        dimmed = !exposedSet.has(n.id);
      } else if (overlay === "security") {
        const level = securityLevel(n);
        ring = level === "serious" ? "bad" : level === "risky" ? "warn" : null;
        dimmed = level === "ok";
      } else if (n.health === "bad" || n.health === "warn") {
        ring = n.health;
      }
      if (focus) dimmed = !focus.has(n.id);
      if (q) dimmed = !(n.name.toLowerCase().includes(q) || n.kind.toLowerCase().includes(q));
      const data: CanvasNodeData = { node: n, ring, dimmed, selected: n.id === selected };
      out.push({ id: n.id, type: "kupi", position: { x: p.x, y: p.y }, data, parentId: p.parent, extent: p.parent ? "parent" : undefined, draggable: true });
    }
    return out;
  }, [positions, nodesById, overlay, exposedSet, focus, q, selected]);

  const flowEdges: Edge[] = useMemo(
    () =>
      (graph?.edges ?? []).map((e) => {
        const style = EDGE_STYLE[e.kind] ?? EDGE_STYLE.selects;
        const lit = !focus || (focus.has(e.source) && focus.has(e.target));
        return {
          id: e.id,
          source: e.source,
          target: e.target,
          label: e.kind === "routes" || e.kind === "selects" ? undefined : e.kind,
          labelStyle: { fill: "var(--kupi-muted)", fontSize: 10 },
          labelBgStyle: { fill: "var(--kupi-bg)" },
          style: { stroke: style.stroke, strokeDasharray: style.dash, strokeWidth: 1.4, opacity: lit ? 1 : 0.15 },
          animated: e.kind === "routes" && lit,
        };
      }),
    [graph, focus],
  );

  if (!active) return null;
  const sel = selected ? nodesById.get(selected) : undefined;
  const selType = sel ? findType(sel.group, sel.resource) : undefined;
  const selRow: TableRow | null = sel
    ? { name: sel.name, namespace: sel.namespace, uid: sel.uid, created: sel.created, labels: sel.labels ?? {}, ownerKind: "", ownerName: "", cells: [] }
    : null;
  const affected = sel && ["ConfigMap", "Secret", "PersistentVolumeClaim"].includes(sel.kind) && graph ? dependents(sel.id, graph.edges) : [];

  return (
    <div className="flex h-full min-h-0 gap-4">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">Map</h1>
          <span className="text-[12px] text-muted">
            {namespace ? `namespace ${namespace}` : "all namespaces"} · {graph ? `${graph.nodes.length} objects` : "…"} · refreshes every 15 s
          </span>
          <div className="ml-auto flex items-center gap-2">
            <div role="tablist" aria-label="Overlay" className="flex rounded-md border border-line p-0.5">
              {(["health", "exposure", "security"] as Overlay[]).map((o) => (
                <button
                  key={o}
                  role="tab"
                  aria-selected={overlay === o}
                  onClick={() => setOverlay(o)}
                  className={`rounded px-2.5 py-1 text-[12px] capitalize ${overlay === o ? "bg-panel-2 font-medium text-fg" : "text-muted hover:text-fg"}`}
                >
                  {o}
                </button>
              ))}
            </div>
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
              <input
                aria-label="Find on the map"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Find"
                className="h-8 w-40 rounded-md border border-line bg-bg pr-2 pl-7 text-[13px] focus:border-accent focus:outline-none"
              />
            </div>
            <Button variant="ghost" busy={loading} icon={<RefreshCw className="size-4" />} aria-label="Refresh" onClick={() => void load()} />
          </div>
        </div>
        {error && <Notice tone="bad">{error}</Notice>}
        {graph?.warnings.map((w) => <Notice key={w} tone="warn">{w}</Notice>)}
        {graph && graph.nodes.length > LARGE && (
          <Notice tone="warn">This map has {graph.nodes.length} objects. Pick a namespace in the top bar for a clearer view.</Notice>
        )}
        <div className="relative min-h-0 flex-1 overflow-hidden rounded-lg border border-line bg-bg">
          {graph && graph.nodes.length === 0 && <p className="p-4 text-[13px] text-muted">Nothing here yet.</p>}
          <ReactFlow
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={nodeTypes}
            onNodeClick={(_, n) => n.type === "kupi" && setSelected((cur) => (cur === n.id ? null : n.id))}
            onPaneClick={() => setSelected(null)}
            minZoom={0.1}
            proOptions={{ hideAttribution: true }}
            colorMode="dark"
            fitView
          >
            <Background gap={24} color="var(--kupi-line)" />
            <Controls showInteractive={false} />
            <MiniMap
              pannable
              zoomable
              nodeColor={(n) => MINIMAP_COLOR[(n.data as Partial<CanvasNodeData>).node?.health ?? ""] ?? "transparent"}
              nodeStrokeWidth={0}
              style={{ background: "var(--kupi-panel)", width: 150, height: 96 }}
              maskColor="rgba(0,0,0,0.35)"
            />
          </ReactFlow>
          <Legend overlay={overlay} />
        </div>
      </div>
      {sel && (
        <div className="flex w-[46%] max-w-[760px] min-w-[420px] flex-col gap-2">
          {(sel.flags ?? []).length > 0 && (
            <div className="rounded-lg border border-line bg-panel px-3 py-2 text-[12px]">
              {(sel.flags ?? []).map((f) => (
                <div key={f}>• {FLAG_TEXT[f] ?? f}</div>
              ))}
            </div>
          )}
          {affected.length > 0 && (
            <Notice tone="warn">
              Changing this {sel.kind} affects {affected.length} {affected.length === 1 ? "workload" : "workloads"}:{" "}
              {affected.map((id) => nodesById.get(id)?.name ?? id).join(", ")}.
            </Notice>
          )}
          {selType && selRow && !(sel.flags ?? []).includes("missing") ? (
            <div className="flex min-h-0 flex-1">
              <DetailPanel key={sel.id} className="w-full" contextName={active.name} type={selType} row={selRow} onClose={() => setSelected(null)} />
            </div>
          ) : (
            <Notice tone="bad">
              {sel.kind} {sel.name} doesn't exist, but something on the map refers to it.
            </Notice>
          )}
        </div>
      )}
    </div>
  );
}

function Legend({ overlay }: { overlay: Overlay }) {
  const items: [string, string][] =
    overlay === "health"
      ? [["bg-good", "healthy"], ["bg-warn", "degraded"], ["bg-bad", "failing or missing"]]
      : overlay === "exposure"
        ? [["bg-accent", "reachable from outside the cluster"]]
        : [["bg-bad", "privileged, host network or host path"], ["bg-warn", "may run as root or no memory limit"]];
  return (
    <div className="absolute top-3 left-3 z-10 flex flex-col gap-1 rounded-md border border-line bg-panel/90 px-2.5 py-2 text-[11px] text-muted">
      {items.map(([c, label]) => (
        <span key={label} className="flex items-center gap-1.5">
          <span className={`size-2 rounded-full ${c}`} /> {label}
        </span>
      ))}
      <span className="mt-1 border-t border-line pt-1">Click an object to see what it connects to.</span>
    </div>
  );
}
