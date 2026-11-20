import { useCallback, useEffect, useMemo, useState, type DragEvent } from "react";
import {
  Background,
  ConnectionMode,
  Controls,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Activity, Check, CircleAlert, Cloud, Copy, FileCode, Pencil, Play, Trash2, TriangleAlert } from "lucide-react";
import { BLOCK_ICONS, BuildNode, type BuildNodeData, type ScanBadge } from "../components/BuildNode";
import { BlockEditor, CapacityMeter, LinkEditor, LivePanel, imagesOf, linkLabel, type Need, type ScanResult } from "../components/BuildPanels";
import { ReviewDialog } from "../components/ReviewDialog";
import { Button, Notice, Segmented, Switch, inputClass } from "../components/ui";
import { CanvasService, FormService, GuardService, ImageService, errorMessage, type ClusterCapacity, type PlanView } from "../lib/api";
import { copyText } from "../lib/bytes";
import { BLOCKS, WORKLOADS, blockChanged, buildObjects, buildPlan, check, linkFor, newBlock, rebase, type Block, type BlockKind, type BuildModel, type Issue, type Link } from "../lib/build";
import { importGraph, importable } from "../lib/buildImport";
import { useLive } from "../lib/buildLive";
import type { GraphEdge, GraphNode } from "../lib/canvas";
import { evaluatePod, podRequests, podSpecOf, type PodSecurity } from "../lib/podsecurity";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";
import { effectiveTheme, useSettings } from "../state/settings";

const DRAG_TYPE = "application/x-kupi-block";
const nodeTypes = { block: BuildNode };

interface Draft {
  model: BuildModel;
  positions: Record<string, { x: number; y: number }>;
}

// The draft and scan results live for the app session, so leaving the page
// and coming back keeps the work. Nothing is written to disk.
let saved: Draft | null = null;
const scanCache = new Map<string, ScanResult>();
let nextId = 1;
const id = () => `b${nextId++}`;

export function BuildPage({ importNamespace }: { importNamespace?: string }) {
  return (
    <ReactFlowProvider>
      <Builder importNamespace={importNamespace} />
    </ReactFlowProvider>
  );
}

type Panel = "object" | "checks" | "yaml" | "live";

async function toYAML(objects: Record<string, unknown>[]): Promise<string> {
  return (await Promise.all(objects.map((o) => FormService.ToYAML(JSON.stringify(o))))).join("---\n");
}

/** Drag blocks together, or open what's running, and apply it through the Guard review. */
function Builder({ importNamespace }: { importNamespace?: string }) {
  const { active, namespace, setNamespace, reloadNamespaces } = useClusters();
  const { navigate } = useNav();
  const { settings } = useSettings();
  const { screenToFlowPosition, fitView } = useReactFlow();
  const contextName = active?.name ?? "";

  const [model, setModel] = useState<BuildModel>(() => saved?.model ?? { namespace: namespace || "default", createNamespace: false, blocks: [], links: [], ingressNamespace: "kube-system" });
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>(() => saved?.positions ?? {});
  // Sizes React Flow measured; edges can't be drawn without them.
  const [sizes, setSizes] = useState<Record<string, { width: number; height: number }>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [selectedLink, setSelectedLink] = useState<string | null>(null);
  const [panel, setPanel] = useState<Panel>("checks");
  const [refusal, setRefusal] = useState<string | null>(null);
  const [yaml, setYaml] = useState("");
  const [yamlError, setYamlError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [review, setReview] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [live, setLive] = useState<string | null>(null);
  const [scans, setScans] = useState<Map<string, ScanResult>>(() => new Map(scanCache));
  const [scanning, setScanning] = useState(false);
  const [canScan, setCanScan] = useState(false);
  const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);
  const [importing, setImporting] = useState(false);
  const [skipped, setSkipped] = useState<string[]>([]);

  useEffect(() => {
    saved = { model, positions };
  }, [model, positions]);

  useEffect(() => {
    if (!refusal) return;
    const t = window.setTimeout(() => setRefusal(null), 6000);
    return () => window.clearTimeout(t);
  }, [refusal]);

  useEffect(() => {
    void ImageService.ScanTools()
      .then((t) => setCanScan(!!t?.trivy))
      .catch(() => setCanScan(false));
  }, []);

  const loadCapacity = useCallback(() => {
    if (!contextName) return setCapacity(null);
    CanvasService.Capacity(contextName)
      .then((c) => setCapacity(c ?? null))
      .catch(() => setCapacity(null));
  }, [contextName]);
  useEffect(loadCapacity, [loadCapacity]);

  const { states: liveStates, error: liveError } = useLive(contextName, live, model.blocks);

  // --- What the canvas means -------------------------------------------------

  const issues = useMemo(() => check(model), [model]);
  const plan = useMemo(() => buildPlan(model), [model]);
  const objects = plan.objects;

  const security = useMemo(() => {
    const m = new Map<string, PodSecurity>();
    for (const b of model.blocks) {
      if (!WORKLOADS.has(b.kind)) continue;
      const w = plan.byBlock.get(b.id)?.find((o) => o.kind === b.kind);
      const spec = w && podSpecOf(w);
      if (spec) m.set(b.id, evaluatePod(spec));
    }
    return m;
  }, [model.blocks, plan]);

  const scanBadges = useMemo(() => {
    const m = new Map<string, ScanBadge>();
    for (const b of model.blocks) {
      const reports = imagesOf(b)
        .map((i) => scans.get(i))
        .filter((r): r is Exclude<ScanResult, { error: string }> => !!r && !("error" in r));
      if (reports.length) m.set(b.id, { critical: reports.reduce((s, r) => s + (r.counts?.CRITICAL ?? 0), 0), high: reports.reduce((s, r) => s + (r.counts?.HIGH ?? 0), 0) });
    }
    return m;
  }, [model.blocks, scans]);

  // What new workloads ask the scheduler for. Opened ones already run, so they're counted in the cluster's numbers.
  const need = useMemo<Need>(() => {
    const n: Need = { cpu: 0, mem: 0, podCpu: 0, podMem: 0 };
    for (const b of model.blocks) {
      if (b.source || !WORKLOADS.has(b.kind)) continue;
      const w = plan.byBlock.get(b.id)?.find((o) => o.kind === b.kind);
      const spec = w && podSpecOf(w);
      if (!spec) continue;
      const r = podRequests(spec);
      const copies = b.kind === "DaemonSet" ? (capacity?.nodes ?? 1) : b.kind === "Deployment" || b.kind === "StatefulSet" ? Math.max(0, Number(b.fields.replicas) || 0) : 1;
      n.cpu += r.cpu * copies;
      n.mem += r.mem * copies;
      n.podCpu = Math.max(n.podCpu, r.cpu);
      n.podMem = Math.max(n.podMem, r.mem);
    }
    return n;
  }, [model.blocks, plan, capacity]);

  const allIssues = useMemo<Issue[]>(() => {
    const extra: Issue[] = [];
    if (capacity && capacity.nodes > 0) {
      const freeCpu = capacity.cpuAllocatable - capacity.cpuRequested;
      const freeMem = capacity.memAllocatable - capacity.memRequested;
      if (need.cpu > freeCpu || need.mem > freeMem)
        extra.push({ level: "warn", text: "The new workloads request more CPU or memory than the cluster has free. Some pods will wait in Pending until room frees up or a node is added." });
      else if (need.podCpu > capacity.largestFreeCpu || need.podMem > capacity.largestFreeMem)
        extra.push({ level: "warn", text: "There's room in total, but no single node has enough free for the biggest new pod, so it would wait in Pending." });
    }
    for (const [bid, s] of security)
      if (s.level === "privileged") {
        const b = model.blocks.find((x) => x.id === bid);
        if (b && blockChanged(b, model.links)) extra.push({ level: "warn", block: bid, text: `${b.kind} ${b.name} doesn't meet the baseline Pod Security Standard: ${s.problems[0]}.` });
      }
    return [...issues, ...extra];
  }, [issues, capacity, need, security, model.blocks, model.links]);
  const errors = allIssues.filter((i) => i.level === "error");
  const problemOf = useMemo(() => {
    const m = new Map<string, "error" | "warn">();
    for (const i of allIssues) if (i.block && m.get(i.block) !== "error") m.set(i.block, i.level);
    return m;
  }, [allIssues]);
  const badLinks = useMemo(() => new Set(errors.flatMap((i) => (i.link ? [i.link] : []))), [errors]);

  // The manifest, converted to YAML in Go like the editor does.
  useEffect(() => {
    let cancelled = false;
    const t = window.setTimeout(() => {
      toYAML(objects)
        .then((y) => {
          if (cancelled) return;
          setYaml(y);
          setYamlError(null);
        })
        .catch((err) => !cancelled && setYamlError(errorMessage(err)));
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(t);
    };
  }, [objects]);

  // --- Editing -----------------------------------------------------------------

  const addBlock = useCallback((kind: BlockKind, at?: { x: number; y: number }) => {
    const bid = id();
    setModel((m) => ({ ...m, blocks: [...m.blocks, newBlock(kind, bid, m.blocks)] }));
    setPositions((p) => {
      if (at) return { ...p, [bid]: at };
      // Below everything already on the canvas, so a new block never covers one.
      const ys = Object.values(p).map((q) => q.y);
      const left = Object.values(p).length ? Math.min(...Object.values(p).map((q) => q.x)) : 40;
      return { ...p, [bid]: { x: left, y: ys.length ? Math.max(...ys) + 120 : 40 } };
    });
    setSelected(bid);
    setSelectedLink(null);
    setPanel("object");
  }, []);

  const removeBlocks = useCallback((ids: string[]) => {
    const gone = new Set(ids);
    setModel((m) => ({ ...m, blocks: m.blocks.filter((b) => !gone.has(b.id)), links: m.links.filter((l) => !gone.has(l.from) && !gone.has(l.to)) }));
    setPositions((p) => Object.fromEntries(Object.entries(p).filter(([k]) => !gone.has(k))));
    setSelected((s) => (s && gone.has(s) ? null : s));
  }, []);

  const updateBlock = (bid: string, patch: Partial<Block>) =>
    setModel((m) => ({ ...m, blocks: m.blocks.map((b) => (b.id === bid ? { ...b, ...patch, fields: { ...b.fields, ...(patch.fields ?? {}) } } : b)) }));

  const resetBlock = (bid: string) =>
    setModel((m) => ({
      ...m,
      blocks: m.blocks.map((b) => (b.id === bid && b.origFields ? { ...b, fields: { ...b.origFields }, sidecars: b.origSidecars?.map((s) => ({ ...s })) } : b)),
      links: m.links.map((l) => (l.from === bid && l.imported && l.origFields ? { ...l, fields: { ...l.origFields } } : l)),
    }));

  const updateLink = (lid: string, fields: Record<string, string>) => setModel((m) => ({ ...m, links: m.links.map((l) => (l.id === lid ? { ...l, fields: { ...l.fields, ...fields } } : l)) }));

  const removeLink = (lid: string) => {
    setModel((m) => ({ ...m, links: m.links.filter((l) => l.id !== lid || l.imported) }));
    setSelectedLink(null);
  };

  const selectBlock = (bid: string | null) => {
    setSelected(bid);
    setSelectedLink(null);
    if (bid && !live) setPanel("object");
  };

  const selectLink = (lid: string) => {
    setSelectedLink(lid);
    setSelected(null);
    if (!live) setPanel("object");
  };

  const scan = async (images: string[]) => {
    setScanning(true);
    for (const img of images) {
      try {
        const r = await ImageService.Scan(img);
        if (r) scanCache.set(img, r);
      } catch (err) {
        scanCache.set(img, { error: errorMessage(err) });
      }
      setScans(new Map(scanCache));
    }
    setScanning(false);
  };

  // --- Canvas ------------------------------------------------------------------

  const nodes: Node[] = useMemo(
    () =>
      model.blocks.map((b) => {
        const data: BuildNodeData = {
          block: b,
          problem: problemOf.get(b.id) ?? null,
          security: security.get(b.id),
          scan: scanBadges.get(b.id) ?? null,
          inCluster: !!b.source,
          changed: !!b.source && blockChanged(b, model.links),
          live: live ? (liveStates.get(b.id) ?? null) : null,
          liveMode: !!live,
        };
        return { id: b.id, type: "block", position: positions[b.id] ?? { x: 0, y: 0 }, data, selected: b.id === selected, measured: sizes[b.id] };
      }),
    [model.blocks, model.links, positions, problemOf, security, scanBadges, live, liveStates, selected, sizes],
  );

  const edges: Edge[] = useMemo(
    () =>
      model.links.map((l) => {
        // Leave from the side that faces the other block, wherever they've been moved.
        const leftToRight = (positions[l.from]?.x ?? 0) <= (positions[l.to]?.x ?? 0);
        const traffic = l.kind === "routes" || l.kind === "selects";
        const stroke = badLinks.has(l.id) ? "var(--kupi-bad)" : l.kind === "talks" ? "var(--kupi-good)" : traffic ? "var(--kupi-accent)" : "var(--kupi-muted)";
        return {
          id: l.id,
          source: l.from,
          target: l.to,
          sourceHandle: leftToRight ? "r" : "l",
          targetHandle: leftToRight ? "l" : "r",
          // A route shows its path, so several routes from one Ingress can be told apart.
          label: l.kind === "routes" ? `${(l.fields?.path ?? "/").trim() || "/"}${l.fields?.port ? ` :${l.fields.port}` : ""}` : l.kind === "talks" && l.fields?.port ? `may call :${l.fields.port}` : linkLabel(l),
          labelStyle: { fill: badLinks.has(l.id) ? "var(--kupi-bad)" : "var(--kupi-muted)", fontSize: l.kind === "routes" ? 11 : 10, fontFamily: l.kind === "routes" ? "var(--font-mono, monospace)" : undefined },
          labelBgStyle: { fill: "var(--kupi-bg)" },
          markerEnd: { type: MarkerType.ArrowClosed, color: "var(--kupi-muted)" },
          selected: l.id === selectedLink,
          deletable: !l.imported && !live,
          style: {
            stroke,
            strokeDasharray: l.kind === "env" || l.kind === "mounts" ? "5 4" : l.kind === "talks" ? "8 4" : undefined,
            strokeWidth: l.id === selectedLink ? 3 : 1.6,
            opacity: l.imported ? 0.7 : 1,
          },
          animated: traffic || l.kind === "talks",
        } as Edge;
      }),
    [model.links, positions, badLinks, selectedLink, live],
  );

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const removed = changes.filter((c) => c.type === "remove").map((c) => c.id);
      if (removed.length) removeBlocks(removed);
      const measured = changes.flatMap((c) => (c.type === "dimensions" && c.dimensions ? [[c.id, c.dimensions] as const] : []));
      if (measured.length) setSizes((s) => ({ ...s, ...Object.fromEntries(measured) }));
      const moves = changes.filter((c) => c.type === "position");
      if (moves.length) {
        setPositions((p) => {
          const moved = applyNodeChanges(moves, Object.entries(p).map(([nid, position]) => ({ id: nid, position, data: {} })));
          return Object.fromEntries(moved.map((n) => [n.id, n.position]));
        });
      }
    },
    [removeBlocks],
  );

  const onEdgesChange = useCallback((changes: EdgeChange[]) => {
    const removed = new Set(changes.filter((c) => c.type === "remove").map((c) => c.id));
    if (!removed.size) return;
    // Connections that exist in the cluster stay: removing them takes an edit of the object.
    setModel((m) => ({ ...m, links: m.links.filter((l) => !removed.has(l.id) || l.imported) }));
    setSelectedLink((s) => (s && removed.has(s) ? null : s));
  }, []);

  const onConnect = (c: Connection) => {
    const a = model.blocks.find((b) => b.id === c.source);
    const b = model.blocks.find((x) => x.id === c.target);
    if (!a || !b || live) return;
    const r = linkFor(a, b, model.links);
    if (!r.ok) {
      setRefusal(r.reason);
      return;
    }
    setRefusal(null);
    const link: Link = { id: `l${nextId++}`, ...r.link };
    setModel((m) => ({ ...m, links: [...m.links, link] }));
    // Routes and calls have settings of their own: show them right away.
    if (link.kind === "routes" || link.kind === "talks") selectLink(link.id);
  };

  const onDrop = (e: DragEvent) => {
    const kind = e.dataTransfer.getData(DRAG_TYPE) as BlockKind;
    if (!BLOCKS.some((b) => b.kind === kind) || live) return;
    e.preventDefault();
    const p = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    addBlock(kind, { x: p.x - 110, y: p.y - 32 });
  };

  const clear = () => {
    if (model.blocks.length && !window.confirm("Remove every block from the canvas? Nothing in the cluster is deleted.")) return;
    setModel((m) => ({ ...m, blocks: [], links: [] }));
    setPositions({});
    setSelected(null);
    setSelectedLink(null);
    setSkipped([]);
    setDone(null);
  };

  // --- Opening what's running --------------------------------------------------

  const openNamespace = useCallback(
    async (ns: string) => {
      if (!contextName || !ns) return;
      if (saved?.model.blocks.length && !window.confirm(`Replace the canvas with what's running in ${ns}?`)) return;
      setImporting(true);
      setError(null);
      setDone(null);
      try {
        const g = await CanvasService.Graph(contextName, ns);
        const gnodes = (g?.nodes ?? []) as GraphNode[];
        const gedges = (g?.edges ?? []) as GraphEdge[];
        const objectsById = new Map<string, Record<string, unknown>>();
        await Promise.all(
          gnodes
            .filter((n) => importable(n.kind) && n.kind !== "Secret" && !(n.flags ?? []).includes("missing"))
            .map(async (n) => {
              try {
                const y = await FormService.Object(contextName, { group: n.group, version: n.version, resource: n.resource, namespace: n.namespace, name: n.name });
                const r = await FormService.FromYAML(y);
                if (!r.error && r.json) objectsById.set(n.id, JSON.parse(r.json));
              } catch {
                // Listed as "couldn't be read" below.
              }
            }),
        );
        const res = importGraph(gnodes, gedges, objectsById, id);
        const keep = gnodes.filter((n) => res.idOf.has(n.id));
        const { layout } = await import("../lib/canvasLayout");
        const laid = await layout(
          keep,
          gedges.filter((e) => res.idOf.has(e.source) && res.idOf.has(e.target)),
          false,
        );
        setModel((m) => ({ namespace: ns, createNamespace: false, blocks: res.blocks, links: res.links, lockdown: false, ingressNamespace: m.ingressNamespace ?? "kube-system" }));
        setPositions(Object.fromEntries(laid.map((p) => [res.idOf.get(p.id)!, { x: p.x, y: p.y }])));
        setSizes({});
        setSelected(null);
        setSelectedLink(null);
        setLive(null);
        setSkipped(res.skipped);
        setPanel("checks");
        window.setTimeout(() => void fitView({ padding: 0.15, duration: 300 }), 120);
      } catch (err) {
        setError(errorMessage(err));
      } finally {
        setImporting(false);
      }
    },
    [contextName, fitView],
  );

  useEffect(() => {
    if (importNamespace) void openNamespace(importNamespace);
    // Only when the page is opened for a namespace.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [importNamespace]);

  // --- Applying ----------------------------------------------------------------

  const prepare = async (force: boolean) => {
    if (!active) return;
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      if (review) void GuardService.Cancel(review.id);
      setReview(await GuardService.PrepareApply(active.name, await toYAML(buildObjects(model)), force));
    } catch (err) {
      setReview(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const openInEditor = async () => navigate({ page: "apply", manifest: await toYAML(buildObjects(model)) });

  const sel = model.blocks.find((b) => b.id === selected);
  const selLink = model.links.find((l) => l.id === selectedLink);
  const canApply = errors.length === 0 && objects.length > 0;
  const hasImported = model.blocks.some((b) => b.source);
  const routed = model.links.some((l) => l.kind === "routes");
  const applyTitle = !active ? "Connect to a cluster to apply" : errors.length ? "Fix the problems under Checks first" : objects.length === 0 ? "Nothing has changed yet" : undefined;

  return (
    <div className="flex h-full min-h-0 gap-4">
      <aside aria-label="Blocks" className={`flex w-52 shrink-0 flex-col gap-1 overflow-y-auto ${live ? "pointer-events-none opacity-40" : ""}`}>
        <h2 className="px-1 pb-1 text-[11px] font-semibold tracking-wide text-muted uppercase">Blocks</h2>
        {BLOCKS.map((b) => {
          const Icon = BLOCK_ICONS[b.kind];
          return (
            <button
              key={b.kind}
              draggable
              onDragStart={(e) => {
                e.dataTransfer.setData(DRAG_TYPE, b.kind);
                e.dataTransfer.effectAllowed = "copy";
              }}
              onClick={() => addBlock(b.kind)}
              title={`${b.kind}: drag onto the canvas, or click to add one`}
              className="flex cursor-grab items-center gap-2 rounded-md border border-line bg-panel px-2 py-1.5 text-left hover:border-accent/60 active:cursor-grabbing"
            >
              <Icon className="size-4 shrink-0 text-muted" aria-hidden />
              <span className="min-w-0">
                <span className="block truncate text-[12.5px] font-medium">{b.label}</span>
                <span className="block truncate text-[11px] text-muted">{b.description}</span>
              </span>
            </button>
          );
        })}
        <p className="mt-2 px-1 text-[11.5px] leading-5 text-muted">
          Drag blocks onto the canvas, then drag from a dot on one block to another to connect them. Drag from one workload to another to say it may call it. Click a link to see what it does. Select a link or block and press Delete to remove it.
        </p>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="mb-2 flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">Build</h1>
          <span className="text-[12px] text-muted">
            {model.blocks.length} {model.blocks.length === 1 ? "block" : "blocks"} · {objects.length} {objects.length === 1 ? "object" : "objects"} to apply
          </span>
          {!live && <CapacityMeter need={need} cap={capacity} />}
          <div className="ml-auto flex items-center gap-2">
            {live ? (
              <Button icon={<Pencil className="size-4" />} onClick={() => setLive(null)}>
                Back to editing
              </Button>
            ) : (
              <>
                <Button
                  variant="ghost"
                  busy={importing}
                  icon={<Cloud className="size-4" />}
                  disabled={!active || !namespace}
                  title={!active ? "Connect to a cluster first" : !namespace ? "Pick a namespace in the top bar" : `Put what's running in ${namespace} on the canvas`}
                  onClick={() => void openNamespace(namespace)}
                >
                  Open {namespace || "namespace"}
                </Button>
                <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={clear} disabled={model.blocks.length === 0}>
                  Clear
                </Button>
                <Button icon={<FileCode className="size-4" />} disabled={!canApply || !active} title={applyTitle ?? "Open the manifest in Create or edit"} onClick={() => void openInEditor()}>
                  Review in editor
                </Button>
                <Button variant="primary" busy={busy} icon={<Play className="size-4" />} disabled={!canApply || !active} title={applyTitle ?? "Preview the change, apply it, and watch it come alive"} onClick={() => void prepare(false)}>
                  Apply
                </Button>
              </>
            )}
          </div>
        </div>
        {!live && (
          <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-2 text-[12px] text-muted">
            <label className="flex items-center gap-1.5">
              Namespace
              <input
                aria-label="Namespace"
                value={model.namespace}
                disabled={hasImported}
                title={hasImported ? "Objects opened from the cluster stay in their namespace" : undefined}
                onChange={(e) => setModel((m) => ({ ...m, namespace: e.target.value }))}
                className={`${inputClass} h-7 w-36 font-mono`}
                spellCheck={false}
              />
            </label>
            {!hasImported && (
              <label className="flex items-center gap-1.5">
                <Switch label="Create the namespace too" checked={model.createNamespace} onChange={(v) => setModel((m) => ({ ...m, createNamespace: v }))} />
                Create it
              </label>
            )}
            <label className="flex items-center gap-1.5" title="Add NetworkPolicies so only the connections you drew get in">
              <Switch label="Lock down traffic" checked={!!model.lockdown} onChange={(v) => setModel((m) => ({ ...m, lockdown: v }))} />
              Lock down traffic
            </label>
            {model.lockdown && routed && (
              <label className="flex items-center gap-1.5" title="Lockdown lets the Ingress controller's pods in from this namespace">
                Ingress controller in
                <input
                  aria-label="Ingress controller namespace"
                  value={model.ingressNamespace ?? ""}
                  onChange={(e) => setModel((m) => ({ ...m, ingressNamespace: e.target.value }))}
                  className={`${inputClass} h-7 w-32 font-mono`}
                  spellCheck={false}
                />
              </label>
            )}
          </div>
        )}
        <div className="flex flex-col gap-2">
          {live && (
            <Notice tone="info" icon={<Activity className="size-4 text-accent" />}>
              Live: watching {live}. Blocks show what the cluster reports, and each square is a pod.
            </Notice>
          )}
          {done && !live && <Notice tone="good">{done}</Notice>}
          {error && <Notice tone="bad">{error}</Notice>}
          {refusal && <Notice tone="warn">{refusal}</Notice>}
          {skipped.length > 0 && !live && (
            <Notice>
              Left off the canvas: {skipped.slice(0, 4).join("; ")}
              {skipped.length > 4 ? ` and ${skipped.length - 4} more` : ""}.
            </Notice>
          )}
        </div>
        <div className="relative mt-2 min-h-0 flex-1 overflow-hidden rounded-lg border border-line bg-bg" onDragOver={(e) => e.preventDefault()} onDrop={onDrop}>
          {model.blocks.length === 0 && (
            <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center">
              <p className="max-w-sm text-center text-[13px] text-muted">
                Drag a Deployment here to start, or open a namespace that's already running. Connect blocks and Kupi fills in the selectors, ports and routes.
              </p>
            </div>
          )}
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodesConnectable={!live}
            connectionMode={ConnectionMode.Loose}
            deleteKeyCode={live ? null : ["Delete", "Backspace"]}
            onNodeClick={(_, n) => selectBlock(n.id)}
            onEdgeClick={(_, e) => selectLink(e.id)}
            onPaneClick={() => selectBlock(null)}
            minZoom={0.2}
            proOptions={{ hideAttribution: true }}
            colorMode={effectiveTheme(settings.theme)}
          >
            <Background gap={24} color="var(--kupi-line)" />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>
      </div>

      <aside aria-label="Details" className="flex w-[360px] shrink-0 flex-col gap-3 overflow-hidden">
        <Segmented<Panel>
          label="Panel"
          value={panel === "live" && !live ? "checks" : panel}
          onChange={setPanel}
          options={
            live
              ? [
                  { value: "live", label: "Live" },
                  { value: "yaml", label: "YAML" },
                ]
              : [
                  { value: "object", label: selLink ? "Link" : "Block" },
                  { value: "checks", label: errors.length ? `Checks (${errors.length})` : "Checks" },
                  { value: "yaml", label: "YAML" },
                ]
          }
        />
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">
          {live && panel === "live" && <LivePanel model={model} states={liveStates} contextName={contextName} namespace={live} error={liveError} />}
          {!live && panel === "object" &&
            (sel ? (
              <BlockEditor
                block={sel}
                model={model}
                security={security.get(sel.id)}
                scans={scans}
                scanning={scanning}
                canScan={canScan}
                onScan={(images) => void scan(images)}
                onChange={(p) => updateBlock(sel.id, p)}
                onRemove={() => removeBlocks([sel.id])}
                onReset={() => resetBlock(sel.id)}
                onSelectLink={selectLink}
              />
            ) : selLink ? (
              <LinkEditor link={selLink} model={model} onChange={(f) => updateLink(selLink.id, f)} onRemove={() => removeLink(selLink.id)} />
            ) : (
              <p className="text-[13px] text-muted">Select a block or a link to change its settings.</p>
            ))}
          {!live && panel === "checks" && (
            <div className="flex flex-col gap-2">
              {allIssues.length === 0 && <Notice tone="good">No problems found. Apply previews the change against the cluster first: a server-side dry run, permissions and a diff.</Notice>}
              {allIssues.map((i, n) => (
                <button
                  key={n}
                  className="text-left"
                  disabled={!i.block && !i.link}
                  onClick={() => {
                    if (i.link) selectLink(i.link);
                    else if (i.block) selectBlock(i.block);
                  }}
                >
                  <Notice tone={i.level === "error" ? "bad" : "warn"} icon={i.level === "error" ? <CircleAlert className="size-4 text-bad" /> : <TriangleAlert className="size-4 text-warn" />}>
                    {i.text}
                  </Notice>
                </button>
              ))}
              <p className="text-[12px] text-muted">These checks only look at the canvas. Apply then asks the cluster: a server-side dry run, permissions and a diff, before anything changes.</p>
            </div>
          )}
          {panel === "yaml" && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <Button
                  icon={copied ? <Check className="size-4 text-good" /> : <Copy className="size-4" />}
                  disabled={!yaml}
                  onClick={() =>
                    void copyText(yaml).then(() => {
                      setCopied(true);
                      window.setTimeout(() => setCopied(false), 1500);
                    })
                  }
                >
                  {copied ? "Copied" : "Copy YAML"}
                </Button>
                <span className="text-[12px] text-muted">{hasImported ? "Only what changed." : "Updates as you build."}</span>
              </div>
              {yamlError && <Notice tone="bad">{yamlError}</Notice>}
              <pre className="overflow-x-auto rounded-lg border border-line bg-panel p-3 font-mono text-[11.5px] leading-[1.5]">{yaml || (hasImported ? "# Nothing changed yet" : "# Nothing yet")}</pre>
            </div>
          )}
        </div>
      </aside>

      <ReviewDialog
        plan={review}
        onClose={() => setReview(null)}
        onTakeOver={() => void prepare(true)}
        onCommitted={(result) => {
          setReview(null);
          setDone(`${result.message}. Audit entry #${result.auditSeq}.`);
          const ns = model.namespace.trim();
          // What was just applied is the new baseline for blocks opened from the cluster.
          setModel((m) => rebase(m, plan.byBlock));
          setLive(ns);
          setPanel("live");
          loadCapacity();
          // Show the namespace just built in, everywhere, so it can be opened here or on the Map.
          void reloadNamespaces().then(() => setNamespace(ns));
        }}
      />
    </div>
  );
}
