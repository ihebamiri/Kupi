import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";
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
import { Check, CircleAlert, Copy, FileCode, Plus, Trash2, TriangleAlert } from "lucide-react";
import { BLOCK_ICONS, BuildNode, type BuildNodeData } from "../components/BuildNode";
import { Button, Field, Notice, Segmented, Switch, inputClass } from "../components/ui";
import { FormService, errorMessage } from "../lib/api";
import { copyText } from "../lib/bytes";
import {
  BLOCKS,
  LINK_LABEL,
  WORKLOADS,
  blockSpec,
  buildObjects,
  check,
  headlessName,
  linkFor,
  newBlock,
  parseServicePorts,
  type Block,
  type BlockKind,
  type BuildModel,
  type Link,
  type Sidecar,
} from "../lib/build";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";
import { effectiveTheme, useSettings } from "../state/settings";

const DRAG_TYPE = "application/x-kupi-block";
const nodeTypes = { block: BuildNode };

interface Draft {
  model: BuildModel;
  positions: Record<string, { x: number; y: number }>;
}

// The draft lives for the app session, so leaving the page and coming back
// keeps the work. It's never written to disk.
let saved: Draft | null = null;
let nextId = 1;
const id = () => `b${nextId++}`;

export function BuildPage() {
  return (
    <ReactFlowProvider>
      <Builder />
    </ReactFlowProvider>
  );
}

type Panel = "object" | "checks" | "yaml";

/** Drag blocks together and get a manifest, reviewed in the editor before anything is applied. */
function Builder() {
  const { active, namespace } = useClusters();
  const { navigate } = useNav();
  const { settings } = useSettings();
  const { screenToFlowPosition } = useReactFlow();
  const wrapper = useRef<HTMLDivElement>(null);

  const [model, setModel] = useState<BuildModel>(() => saved?.model ?? { namespace: namespace || "default", createNamespace: false, blocks: [], links: [] });
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

  useEffect(() => {
    saved = { model, positions };
  }, [model, positions]);

  useEffect(() => {
    if (!refusal) return;
    const t = window.setTimeout(() => setRefusal(null), 6000);
    return () => window.clearTimeout(t);
  }, [refusal]);

  const issues = useMemo(() => check(model), [model]);
  const errors = issues.filter((i) => i.level === "error");
  const problemOf = useMemo(() => {
    const m = new Map<string, "error" | "warn">();
    for (const i of issues) if (i.block && m.get(i.block) !== "error") m.set(i.block, i.level);
    return m;
  }, [issues]);
  const badLinks = useMemo(() => new Set(errors.flatMap((i) => (i.link ? [i.link] : []))), [errors]);

  // The manifest, converted to YAML in Go like the editor does.
  const objects = useMemo(() => buildObjects(model), [model]);
  useEffect(() => {
    let cancelled = false;
    const t = window.setTimeout(() => {
      Promise.all(objects.map((o) => FormService.ToYAML(JSON.stringify(o))))
        .then((parts) => {
          if (cancelled) return;
          setYaml(parts.join("---\n"));
          setYamlError(null);
        })
        .catch((err) => !cancelled && setYamlError(errorMessage(err)));
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(t);
    };
  }, [objects]);

  const addBlock = useCallback((kind: BlockKind, at?: { x: number; y: number }) => {
    const bid = id();
    setModel((m) => ({ ...m, blocks: [...m.blocks, newBlock(kind, bid, m.blocks)] }));
    setPositions((p) => {
      const n = Object.keys(p).length;
      return { ...p, [bid]: at ?? { x: 40 + (n % 3) * 270, y: 40 + Math.floor(n / 3) * 120 } };
    });
    setSelected(bid);
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

  const updateLink = (lid: string, fields: Record<string, string>) =>
    setModel((m) => ({ ...m, links: m.links.map((l) => (l.id === lid ? { ...l, fields: { ...l.fields, ...fields } } : l)) }));

  const removeLink = (lid: string) => {
    setModel((m) => ({ ...m, links: m.links.filter((l) => l.id !== lid) }));
    setSelectedLink(null);
  };

  const selectBlock = (bid: string | null) => {
    setSelected(bid);
    setSelectedLink(null);
    if (bid) setPanel("object");
  };

  const selectLink = (lid: string) => {
    setSelectedLink(lid);
    setSelected(null);
    setPanel("object");
  };

  const nodes: Node[] = useMemo(
    () =>
      model.blocks.map((b) => {
        const data: BuildNodeData = { block: b, problem: problemOf.get(b.id) ?? null };
        return { id: b.id, type: "block", position: positions[b.id] ?? { x: 0, y: 0 }, data, selected: b.id === selected, measured: sizes[b.id] };
      }),
    [model.blocks, positions, problemOf, selected, sizes],
  );

  const edges: Edge[] = useMemo(
    () =>
      model.links.map((l) => {
        // Leave from the side that faces the other block, wherever they've been moved.
        const leftToRight = (positions[l.from]?.x ?? 0) <= (positions[l.to]?.x ?? 0);
        return {
        id: l.id,
        source: l.from,
        target: l.to,
        sourceHandle: leftToRight ? "r" : "l",
        targetHandle: leftToRight ? "l" : "r",
        // A route shows its path, so several routes from one Ingress can be told apart.
        label: l.kind === "routes" ? `${(l.fields?.path ?? "/").trim() || "/"}${l.fields?.port ? ` :${l.fields.port}` : ""}` : LINK_LABEL[l.kind],
        labelStyle: { fill: badLinks.has(l.id) ? "var(--kupi-bad)" : "var(--kupi-muted)", fontSize: l.kind === "routes" ? 11 : 10, fontFamily: l.kind === "routes" ? "var(--font-mono, monospace)" : undefined },
        labelBgStyle: { fill: "var(--kupi-bg)" },
        markerEnd: { type: MarkerType.ArrowClosed, color: "var(--kupi-muted)" },
        selected: l.id === selectedLink,
        style: {
          stroke: badLinks.has(l.id) ? "var(--kupi-bad)" : l.kind === "routes" || l.kind === "selects" ? "var(--kupi-accent)" : "var(--kupi-muted)",
          strokeDasharray: l.kind === "env" || l.kind === "mounts" ? "5 4" : undefined,
          strokeWidth: l.id === selectedLink ? 3 : 1.6,
        },
        animated: l.kind === "routes" || l.kind === "selects",
        } as Edge;
      }),
    [model.links, positions, badLinks, selectedLink],
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
    if (removed.size) {
      setModel((m) => ({ ...m, links: m.links.filter((l) => !removed.has(l.id)) }));
      setSelectedLink((s) => (s && removed.has(s) ? null : s));
    }
  }, []);

  const onConnect = useCallback(
    (c: Connection) => {
      const a = model.blocks.find((b) => b.id === c.source);
      const b = model.blocks.find((x) => x.id === c.target);
      if (!a || !b) return;
      const r = linkFor(a, b, model.links);
      if (!r.ok) {
        setRefusal(r.reason);
        return;
      }
      setRefusal(null);
      const link: Link = { id: `l${nextId++}`, ...r.link };
      setModel((m) => ({ ...m, links: [...m.links, link] }));
      // A route has settings of its own: show them right away.
      if (link.kind === "routes") selectLink(link.id);
    },
    [model.blocks, model.links],
  );

  const onDrop = (e: DragEvent) => {
    const kind = e.dataTransfer.getData(DRAG_TYPE) as BlockKind;
    if (!BLOCKS.some((b) => b.kind === kind)) return;
    e.preventDefault();
    const p = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    addBlock(kind, { x: p.x - 110, y: p.y - 32 });
  };

  const clear = () => {
    if (model.blocks.length && !window.confirm("Remove every block from the canvas?")) return;
    setModel((m) => ({ ...m, blocks: [], links: [] }));
    setPositions({});
    setSelected(null);
    setSelectedLink(null);
  };

  const openInEditor = async () => {
    // Converted again here so the editor gets exactly what the checks saw.
    const parts = await Promise.all(buildObjects(model).map((o) => FormService.ToYAML(JSON.stringify(o))));
    navigate({ page: "apply", manifest: parts.join("---\n") });
  };

  const sel = model.blocks.find((b) => b.id === selected);
  const selLink = model.links.find((l) => l.id === selectedLink);
  const canApply = errors.length === 0 && objects.length > 0;

  return (
    <div className="flex h-full min-h-0 gap-4">
      <aside aria-label="Blocks" className="flex w-52 shrink-0 flex-col gap-1 overflow-y-auto">
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
        <p className="mt-2 px-1 text-[11.5px] leading-5 text-muted">Drag blocks onto the canvas, then drag from a dot on one block to another to connect them. Click a link to see what it does (and an Ingress route's path). Select a link or block and press Delete to remove it.</p>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">Build</h1>
          <span className="text-[12px] text-muted">
            {model.blocks.length} {model.blocks.length === 1 ? "block" : "blocks"} · {objects.length} {objects.length === 1 ? "object" : "objects"}
          </span>
          <div className="ml-auto flex items-center gap-2">
            <label className="flex items-center gap-1.5 text-[12px] text-muted">
              Namespace
              <input
                aria-label="Namespace"
                value={model.namespace}
                onChange={(e) => setModel((m) => ({ ...m, namespace: e.target.value }))}
                className={`${inputClass} w-36 font-mono`}
                spellCheck={false}
              />
            </label>
            <label className="flex items-center gap-1.5 text-[12px] text-muted">
              <Switch label="Create the namespace too" checked={model.createNamespace} onChange={(v) => setModel((m) => ({ ...m, createNamespace: v }))} />
              Create it
            </label>
            <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={clear} disabled={model.blocks.length === 0}>
              Clear
            </Button>
            <Button
              variant="primary"
              icon={<FileCode className="size-4" />}
              disabled={!canApply || !active}
              title={!active ? "Connect to a cluster to review and apply" : errors.length ? "Fix the problems under Checks first" : "Open the manifest in the editor, then preview the change"}
              onClick={() => void openInEditor()}
            >
              Review in editor
            </Button>
          </div>
        </div>
        {refusal && (
          <div className="mb-2">
            <Notice tone="warn">{refusal}</Notice>
          </div>
        )}
        <div ref={wrapper} className="relative min-h-0 flex-1 overflow-hidden rounded-lg border border-line bg-bg" onDragOver={(e) => e.preventDefault()} onDrop={onDrop}>
          {model.blocks.length === 0 && (
            <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center">
              <p className="max-w-sm text-center text-[13px] text-muted">Drag a Deployment here to start. Add a Service and connect them, and Kupi fills in the selector, labels and ports.</p>
            </div>
          )}
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            connectionMode={ConnectionMode.Loose}
            deleteKeyCode={["Delete", "Backspace"]}
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
          value={panel}
          onChange={setPanel}
          options={[
            { value: "object", label: selLink ? "Link" : "Block" },
            { value: "checks", label: errors.length ? `Checks (${errors.length})` : "Checks" },
            { value: "yaml", label: "YAML" },
          ]}
        />
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">
          {panel === "object" &&
            (sel ? (
              <BlockEditor block={sel} model={model} onChange={(p) => updateBlock(sel.id, p)} onRemove={() => removeBlocks([sel.id])} onSelectLink={selectLink} />
            ) : selLink ? (
              <LinkEditor link={selLink} model={model} onChange={(f) => updateLink(selLink.id, f)} onRemove={() => removeLink(selLink.id)} />
            ) : (
              <p className="text-[13px] text-muted">Select a block or a link to change its settings.</p>
            ))}
          {panel === "checks" && (
            <div className="flex flex-col gap-2">
              {issues.length === 0 && <Notice tone="good">No problems found. Review in editor to preview the change against the cluster.</Notice>}
              {issues.map((i, n) => (
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
              <p className="text-[12px] text-muted">These checks only look at the canvas. The editor's preview then asks the cluster: a server-side dry run, permissions and a diff.</p>
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
                <span className="text-[12px] text-muted">Updates as you build.</span>
              </div>
              {yamlError && <Notice tone="bad">{yamlError}</Notice>}
              <pre className="overflow-x-auto rounded-lg border border-line bg-panel p-3 font-mono text-[11.5px] leading-[1.5]">{yaml || "# Nothing yet"}</pre>
            </div>
          )}
        </div>
      </aside>
    </div>
  );
}

function BlockEditor({
  block,
  model,
  onChange,
  onRemove,
  onSelectLink,
}: {
  block: Block;
  model: BuildModel;
  onChange: (p: Partial<Block>) => void;
  onRemove: () => void;
  onSelectLink: (id: string) => void;
}) {
  const spec = blockSpec(block.kind);
  const byId = new Map(model.blocks.map((b) => [b.id, b]));
  const out = model.links.filter((l) => l.from === block.id);
  const into = model.links.filter((l) => l.to === block.id);
  const sidecars = block.sidecars ?? [];
  const setSidecar = (i: number, patch: Partial<Sidecar>) => onChange({ sidecars: sidecars.map((s, n) => (n === i ? { ...s, ...patch } : s)) });
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[12px]">{block.kind}</span>
        <span className="text-[12px] text-muted">{spec.description}</span>
      </div>
      <Field label="Name">
        <input className={`${inputClass} font-mono`} value={block.name} spellCheck={false} onChange={(e) => onChange({ name: e.target.value })} />
      </Field>
      {spec.fields.map((f) => (
        <Field key={f.key} label={f.label} hint={f.hint}>
          {f.options ? (
            <select className={inputClass} value={block.fields[f.key] ?? ""} onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })}>
              {f.options.map((o) => (
                <option key={o}>{o}</option>
              ))}
            </select>
          ) : f.multiline ? (
            <textarea
              className={`${inputClass} h-20 py-1.5 font-mono`}
              value={block.fields[f.key] ?? ""}
              spellCheck={false}
              onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })}
            />
          ) : (
            <input className={`${inputClass} font-mono`} value={block.fields[f.key] ?? ""} placeholder={f.placeholder} spellCheck={false} onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })} />
          )}
        </Field>
      ))}
      {block.kind === "StatefulSet" && (
        <Notice>
          Kupi adds a headless Service, <span className="font-mono text-[12px]">{headlessName(block.name)}</span>, so each copy gets a stable address like{" "}
          <span className="font-mono text-[12px]">
            {block.name.trim()}-0.{headlessName(block.name)}
          </span>
          . A PersistentVolumeClaim connected here gives every copy its own claim.
        </Notice>
      )}
      {block.kind === "Ingress" && out.length > 0 && <p className="text-[12px] text-muted">Each link to a Service is one route. Click a link below, or on the canvas, to set its path and port.</p>}
      {block.kind === "Secret" && (
        <Notice>
          This points at a Secret that already exists in the namespace. Kupi never puts Secret values in a manifest: create or change it on the Secrets page.
        </Notice>
      )}
      {block.kind === "ConfigMap" && <Notice tone="warn">Don't put passwords or tokens here: ConfigMaps aren't secret. Use a Secret block instead.</Notice>}
      {WORKLOADS.has(block.kind) && (
        <div className="flex flex-col gap-2">
          <div className="text-[12px] font-medium text-muted">Sidecar containers</div>
          {sidecars.length === 0 && <p className="text-[12px] text-muted">None. A sidecar runs next to the main container in every pod, like a log shipper or a proxy.</p>}
          {sidecars.map((s, i) => (
            <div key={i} className="flex flex-col gap-2 rounded-lg border border-line bg-panel p-2.5">
              <div className="grid grid-cols-2 gap-2">
                <Field label="Name">
                  <input className={`${inputClass} font-mono`} value={s.name} spellCheck={false} onChange={(e) => setSidecar(i, { name: e.target.value })} />
                </Field>
                <Field label="Image">
                  <input className={`${inputClass} font-mono`} value={s.image} spellCheck={false} placeholder="registry/tool:1.0" onChange={(e) => setSidecar(i, { image: e.target.value })} />
                </Field>
              </div>
              <Field label="Command" hint="Split on spaces, no shell. Leave empty to use the image's own.">
                <input className={`${inputClass} font-mono`} value={s.command} spellCheck={false} onChange={(e) => setSidecar(i, { command: e.target.value })} />
              </Field>
              <div>
                <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={() => onChange({ sidecars: sidecars.filter((_, n) => n !== i) })}>
                  Remove sidecar
                </Button>
              </div>
            </div>
          ))}
          <div>
            <Button icon={<Plus className="size-4" />} onClick={() => onChange({ sidecars: [...sidecars, { name: freeSidecarName(block, sidecars), image: "", command: "" }] })}>
              Add a sidecar
            </Button>
          </div>
          {sidecars.length > 0 && <p className="text-[12px] text-muted">Sidecars get the same locked-down security settings and small resource limits. Ports, config and storage links apply to the main container.</p>}
        </div>
      )}
      {(out.length > 0 || into.length > 0) && (
        <div className="text-[12px] text-muted">
          <div className="mb-1 font-medium">Connections</div>
          {out.map((l) => (
            <button key={l.id} className="block text-left hover:text-fg hover:underline" onClick={() => onSelectLink(l.id)}>
              {l.kind === "routes" ? `routes ${(l.fields?.path ?? "/").trim() || "/"} to` : LINK_LABEL[l.kind]} {byId.get(l.to)?.kind} {byId.get(l.to)?.name}
            </button>
          ))}
          {into.map((l) => (
            <button key={l.id} className="block text-left hover:text-fg hover:underline" onClick={() => onSelectLink(l.id)}>
              {byId.get(l.from)?.kind} {byId.get(l.from)?.name} {LINK_LABEL[l.kind]} this
            </button>
          ))}
        </div>
      )}
      <div>
        <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={onRemove}>
          Remove block
        </Button>
      </div>
    </div>
  );
}

function freeSidecarName(block: Block, sidecars: Sidecar[]): string {
  const taken = new Set([block.name.trim(), ...sidecars.map((s) => s.name.trim())]);
  for (let i = 1; ; i++) {
    const name = i === 1 ? "sidecar" : `sidecar-${i}`;
    if (!taken.has(name)) return name;
  }
}

/** What a link does, in plain words, plus an Ingress route's path and port. */
function LinkEditor({ link, model, onChange, onRemove }: { link: Link; model: BuildModel; onChange: (f: Record<string, string>) => void; onRemove: () => void }) {
  const from = model.blocks.find((b) => b.id === link.from);
  const to = model.blocks.find((b) => b.id === link.to);
  if (!from || !to) return null;
  const servicePorts = to.kind === "Service" ? parseServicePorts(to.fields.ports).ports : [];
  const claim = to.name.trim();
  const text: Record<Link["kind"], string> = {
    selects: `Service ${from.name} sends traffic to the pods of ${to.kind} ${to.name}: its selector is app=${to.name}, and each Service port goes to the container port it names.`,
    routes: `Ingress ${from.name} sends requests whose path starts with this prefix to Service ${to.name}.`,
    env: `Every key in ${to.kind} ${to.name} becomes an environment variable in ${from.kind} ${from.name}'s main container (envFrom).`,
    mounts:
      from.kind === "StatefulSet"
        ? `Each copy of StatefulSet ${from.name} gets its own claim with ${to.name}'s size and class, mounted at /data/${claim}. The claims are named ${claim}-${from.name.trim()}-0, -1 and so on, and stay when the StatefulSet is deleted.`
        : `${from.kind} ${from.name} mounts the claim ${to.name} at /data/${claim} in its main container.`,
    scales: `Autoscaler ${from.name} changes how many copies of ${to.kind} ${to.name} run, between its fewest and most copies, from CPU use.`,
  };
  const port = link.fields?.port ?? "";
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-1.5 text-[12px]">
        <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono">{from.kind}</span>
        <span className="font-medium">{from.name}</span>
        <span className="text-muted">→</span>
        <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono">{to.kind}</span>
        <span className="font-medium">{to.name}</span>
      </div>
      <p className="text-[13px] leading-5 text-muted">{text[link.kind]}</p>
      {link.kind === "routes" && (
        <>
          <Field label="Path" hint="A prefix: /api also matches /api/users.">
            <input className={`${inputClass} font-mono`} value={link.fields?.path ?? "/"} spellCheck={false} onChange={(e) => onChange({ path: e.target.value })} />
          </Field>
          <Field label="Service port">
            <select className={inputClass} value={port} onChange={(e) => onChange({ port: e.target.value })}>
              <option value="">{servicePorts.length ? `First port (${servicePorts[0].port})` : "First port"}</option>
              {servicePorts.map((p) => (
                <option key={p.port} value={String(p.port)}>
                  {p.port} → {String(p.target)}
                </option>
              ))}
              {port && !servicePorts.some((p) => String(p.port) === port) && <option value={port}>{port} (not on the Service)</option>}
            </select>
          </Field>
        </>
      )}
      <div>
        <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={onRemove}>
          Remove link
        </Button>
      </div>
    </div>
  );
}
