import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Background, Controls, MiniMap, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { RefreshCw, Search } from "lucide-react";
import { CanvasNode, NamespaceFrame, POD_CHIP_H, POD_CHIP_W, PodChip, type CanvasNodeData, type PodChipData } from "../components/CanvasNode";
import { DetailPanel } from "../components/DetailPanel";
import { PodList } from "../components/PodList";
import { Button, Notice } from "../components/ui";
import { CanvasService, errorMessage, type PodSummary, type PodsResult, type TableRow } from "../lib/api";
import { FLAG_TEXT, dependents, exposed, neighbourhood, securityLevel, type GraphEdge, type GraphNode, type Overlay } from "../lib/canvas";
import { NODE_H, NODE_W, layout, type Positioned } from "../lib/canvasLayout";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";
import { effectiveTheme, useSettings } from "../state/settings";


const LARGE = 400;
const nodeTypes = { kupi: CanvasNode, frame: NamespaceFrame, pod: PodChip };
/** Kinds whose pods fan out under them when selected. */
const POD_KINDS = new Set(["Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob", "Service"]);
const CHIPS_SHOWN = 12;
const NO_PODS: PodSummary[] = [];
const CHIP_GAP = 8;

/** Where the pod chips go: a grid centred under the selected node. */
export function chipPositions(count: number, anchor: { x: number; y: number }): { x: number; y: number }[] {
  const cols = Math.min(4, Math.max(1, count));
  const width = cols * POD_CHIP_W + (cols - 1) * CHIP_GAP;
  const left = anchor.x + NODE_W / 2 - width / 2;
  return Array.from({ length: count }, (_, i) => ({
    x: left + (i % cols) * (POD_CHIP_W + CHIP_GAP),
    y: anchor.y + NODE_H + 36 + Math.floor(i / cols) * (POD_CHIP_H + CHIP_GAP),
  }));
}

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
  const { settings } = useSettings();
  const refreshMs = settings.mapRefreshSeconds * 1000;
  const { findType } = useNav();
  const { fitView } = useReactFlow();
  const [graph, setGraph] = useState<GraphData | null>(null);
  const [positions, setPositions] = useState<Positioned[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [overlay, setOverlay] = useState<Overlay>("health");
  const [selected, setSelected] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [pods, setPods] = useState<{ id: string; result: PodsResult } | null>(null);
  const [podsError, setPodsError] = useState<string | null>(null);
  const [selectedPod, setSelectedPod] = useState<PodSummary | null>(null);
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
    const timer = window.setInterval(() => void load(), refreshMs);
    return () => window.clearInterval(timer);
  }, [load, refreshMs]);

  const nodesById = useMemo(() => new Map((graph?.nodes ?? []).map((n) => [n.id, n])), [graph]);

  // The selected workload's pods, refreshed with the map.
  const selNode = selected ? nodesById.get(selected) : undefined;
  const podsFor = selNode && POD_KINDS.has(selNode.kind) && !(selNode.flags ?? []).includes("missing") ? selNode : undefined;
  useEffect(() => {
    setSelectedPod(null);
    setPodsError(null);
    if (!podsFor) setPods(null);
  }, [podsFor?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!podsFor || !contextName) return;
    let cancelled = false;
    CanvasService.Pods(contextName, podsFor.kind, podsFor.namespace, podsFor.name)
      .then((r) => {
        if (!cancelled && r) {
          setPods({ id: podsFor.id, result: { ...r, pods: r.pods ?? [] } });
          setPodsError(null);
        }
      })
      .catch((err) => !cancelled && setPodsError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
    // Re-read whenever the map refreshes (graph changes) or the selection moves.
  }, [contextName, podsFor?.id, graph]); // eslint-disable-line react-hooks/exhaustive-deps
  const podsForId = podsFor?.id;
  const podList = useMemo(() => (pods && podsForId && pods.id === podsForId ? (pods.result.pods ?? NO_PODS) : NO_PODS), [pods, podsForId]);

  // When a workload's pods first appear, bring the workload and its pods into view.
  const fitted = useRef("");
  useEffect(() => {
    if (!podsForId || !settings.mapShowPods || podList.length === 0 || fitted.current === podsForId) return;
    fitted.current = podsForId;
    const ids = [podsForId, ...podList.slice(0, CHIPS_SHOWN).map((p) => `pod:${p.uid}`)];
    const t = window.setTimeout(() => void fitView({ nodes: ids.map((id) => ({ id })), padding: 0.35, duration: 350, maxZoom: 1.2 }), 60);
    return () => window.clearTimeout(t);
  }, [podsForId, podList, settings.mapShowPods, fitView]);
  useEffect(() => {
    if (!podsForId) fitted.current = "";
  }, [podsForId]);
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
      if (n.id === selected && settings.mapShowPods && podList.length > 0) {
        const shown = podList.slice(0, CHIPS_SHOWN);
        const extra = podList.length - shown.length;
        const spots = chipPositions(shown.length + (extra > 0 ? 1 : 0), p);
        shown.forEach((pod, i) => {
          const chip: PodChipData = { pod, selected: selectedPod?.uid === pod.uid };
          out.push({ id: `pod:${pod.uid}`, type: "pod", position: spots[i], data: chip, parentId: p.parent, draggable: false, zIndex: 20 });
        });
        if (extra > 0) {
          out.push({ id: "pod:more", type: "pod", position: spots[shown.length], data: { pod: shown[0], selected: false, more: extra }, parentId: p.parent, draggable: false, selectable: false, zIndex: 20 });
        }
      }
    }
    return out;
  }, [positions, nodesById, overlay, exposedSet, focus, q, selected, podList, selectedPod, settings.mapShowPods]);

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
        } as Edge;
      }).concat(
        selected
          ? podList.slice(0, CHIPS_SHOWN).map((pod) => ({
              id: `runs:${pod.uid}`,
              source: selected,
              sourceHandle: "pods",
              target: `pod:${pod.uid}`,
              style: { stroke: pod.health === "bad" ? "var(--kupi-bad)" : "var(--kupi-muted)", strokeDasharray: "2 3", strokeWidth: 1.2 },
              zIndex: 19,
            }))
          : [],
      ),
    [graph, focus, selected, podList],
  );

  if (!active) return null;
  const sel = selected ? nodesById.get(selected) : undefined;
  const selType = sel ? findType(sel.group, sel.resource) : undefined;
  const selRow: TableRow | null = sel
    ? { name: sel.name, namespace: sel.namespace, uid: sel.uid, created: sel.created, labels: sel.labels ?? {}, ownerKind: "", ownerName: "", cells: [] }
    : null;
  const affected = sel && ["ConfigMap", "Secret", "PersistentVolumeClaim"].includes(sel.kind) && graph ? dependents(sel.id, graph.edges) : [];
  const podType = findType("", "pods");
  const podRow: TableRow | null = selectedPod
    ? { name: selectedPod.name, namespace: selectedPod.namespace, uid: selectedPod.uid, created: selectedPod.created, labels: selectedPod.labels ?? {}, ownerKind: "", ownerName: "", cells: [] }
    : null;

  return (
    <div className="flex h-full min-h-0 gap-4">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">Map</h1>
          <span className="text-[12px] text-muted">
            {namespace ? `namespace ${namespace}` : "all namespaces"} · {graph ? `${graph.nodes.length} objects` : "…"} · refreshes every {settings.mapRefreshSeconds} s
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
            onNodeClick={(_, n) => {
              if (n.type === "kupi") setSelected((cur) => (cur === n.id ? null : n.id));
              else if (n.type === "pod" && !(n.data as PodChipData).more) setSelectedPod((n.data as PodChipData).pod);
            }}
            onPaneClick={() => setSelected(null)}
            minZoom={0.1}
            proOptions={{ hideAttribution: true }}
            colorMode={effectiveTheme(settings.theme)}
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
          {podsFor && !selectedPod && (
            <PodList
              owner={`${sel.kind} ${sel.name}`}
              pods={podList}
              note={pods?.id === podsFor.id ? pods.result.note : ""}
              truncated={pods?.id === podsFor.id && pods.result.truncated}
              loading={!pods || pods.id !== podsFor.id}
              error={podsError}
              onOpen={setSelectedPod}
            />
          )}
          {selectedPod && podType && podRow ? (
            <div className="flex min-h-0 flex-1 flex-col gap-2">
              <button className="w-fit text-[12px] text-accent hover:underline" onClick={() => setSelectedPod(null)}>
                ← Back to {sel.kind} {sel.name}
              </button>
              <div className="flex min-h-0 flex-1">
                <DetailPanel key={selectedPod.uid} className="w-full" contextName={active.name} type={podType} row={podRow} onClose={() => setSelectedPod(null)} />
              </div>
            </div>
          ) : selType && selRow && !(sel.flags ?? []).includes("missing") ? (
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
