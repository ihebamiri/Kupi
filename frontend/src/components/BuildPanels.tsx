import { useState } from "react";
import { Bug, Check, CircleCheck, CircleX, Cloud, Plus, RotateCcw, ShieldAlert, ShieldCheck, Shield, Trash2 } from "lucide-react";
import { Button, Field, Notice, inputClass } from "./ui";
import type { LiveState } from "./BuildNode";
import { NetworkService, errorMessage, type ClusterCapacity, type ScanReport } from "../lib/api";
import { LINK_LABEL, SERVED, WORKLOADS, blockChanged, blockSpec, headlessName, parsePorts, parseServicePorts, type Block, type BuildModel, type Link, type Sidecar } from "../lib/build";
import type { PodSecurity } from "../lib/podsecurity";
import { formatBytes } from "../lib/units";

export type ScanResult = ScanReport | { error: string };

export function imagesOf(b: Block): string[] {
  return [b.fields.image ?? "", ...(b.sidecars ?? []).map((s) => s.image)].map((i) => i.trim()).filter(Boolean);
}

export function linkLabel(l: Link): string {
  if (l.kind === "routes") return `routes ${(l.fields?.path ?? "/").trim() || "/"} to`;
  if (l.kind === "env" && l.imported) return "uses";
  return LINK_LABEL[l.kind];
}

// --- Block --------------------------------------------------------------------

interface BlockEditorProps {
  block: Block;
  model: BuildModel;
  security?: PodSecurity;
  scans: Map<string, ScanResult>;
  scanning: boolean;
  canScan: boolean;
  onScan: (images: string[]) => void;
  onChange: (p: Partial<Block>) => void;
  onRemove: () => void;
  onReset: () => void;
  onSelectLink: (id: string) => void;
}

export function BlockEditor({ block, model, security, scans, scanning, canScan, onScan, onChange, onRemove, onReset, onSelectLink }: BlockEditorProps) {
  const spec = blockSpec(block.kind);
  const byId = new Map(model.blocks.map((b) => [b.id, b]));
  const out = model.links.filter((l) => l.from === block.id);
  const into = model.links.filter((l) => l.to === block.id);
  const sidecars = block.sidecars ?? [];
  const inCluster = !!block.source;
  const changed = inCluster && blockChanged(block, model.links);
  const locked = new Set(block.locked ?? []);
  const setSidecar = (i: number, patch: Partial<Sidecar>) => onChange({ sidecars: sidecars.map((s, n) => (n === i ? { ...s, ...patch } : s)) });
  const lockedHint = "Kupi can't show this exactly here. Edit it with Create or edit.";
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[12px]">{block.kind}</span>
        <span className="text-[12px] text-muted">{spec.description}</span>
      </div>
      {inCluster && (
        <Notice icon={<Cloud className="size-4 text-accent" />}>
          {changed ? "Opened from the cluster and changed here. Only your changes are written back, on top of the live object." : "Opened from the cluster. Nothing is written for it unless you change it."}
          {block.managedBy && <> It's managed by {block.managedBy}, which may undo changes made here.</>}
          {changed && (
            <div className="mt-1.5">
              <Button variant="ghost" icon={<RotateCcw className="size-4" />} onClick={onReset}>
                Undo changes
              </Button>
            </div>
          )}
        </Notice>
      )}
      <Field label="Name" hint={inCluster ? "An object in the cluster keeps its name." : undefined}>
        <input className={`${inputClass} font-mono`} value={block.name} spellCheck={false} disabled={inCluster} onChange={(e) => onChange({ name: e.target.value })} />
      </Field>
      {spec.fields.map((f) => {
        const disabled = locked.has(f.key);
        return (
          <Field key={f.key} label={f.label} hint={disabled ? lockedHint : f.hint}>
            {f.options ? (
              <select className={inputClass} disabled={disabled} value={block.fields[f.key] ?? ""} onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })}>
                {f.options.map((o) => (
                  <option key={o}>{o}</option>
                ))}
              </select>
            ) : f.multiline ? (
              <textarea
                className={`${inputClass} h-20 py-1.5 font-mono`}
                value={block.fields[f.key] ?? ""}
                spellCheck={false}
                disabled={disabled}
                onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })}
              />
            ) : (
              <input
                className={`${inputClass} font-mono`}
                value={block.fields[f.key] ?? ""}
                placeholder={f.placeholder}
                spellCheck={false}
                disabled={disabled}
                onChange={(e) => onChange({ fields: { [f.key]: e.target.value } })}
              />
            )}
          </Field>
        );
      })}
      {block.kind === "StatefulSet" && !inCluster && (
        <Notice>
          Kupi adds a headless Service, <span className="font-mono text-[12px]">{headlessName(block.name)}</span>, so each copy gets a stable address like{" "}
          <span className="font-mono text-[12px]">
            {block.name.trim()}-0.{headlessName(block.name)}
          </span>
          . A PersistentVolumeClaim connected here gives every copy its own claim.
        </Notice>
      )}
      {block.kind === "Ingress" && out.length > 0 && <p className="text-[12px] text-muted">Each link to a Service is one route. Click a link below, or on the canvas, to set its path and port.</p>}
      {block.kind === "Secret" && !inCluster && (
        <Notice>This points at a Secret that already exists in the namespace. Kupi never puts Secret values in a manifest: create or change it on the Secrets page.</Notice>
      )}
      {block.kind === "ConfigMap" && !locked.has("data") && <Notice tone="warn">Don't put passwords or tokens here: ConfigMaps aren't secret. Use a Secret block instead.</Notice>}

      {security && <SecuritySection security={security} />}

      {WORKLOADS.has(block.kind) && (
        <div className="flex flex-col gap-2">
          <div className="text-[12px] font-medium text-muted">Sidecar containers</div>
          {sidecars.length === 0 && <p className="text-[12px] text-muted">None. A sidecar runs next to the main container in every pod, like a log shipper or a proxy.</p>}
          {sidecars.map((s, i) => {
            const fromCluster = !!block.origSidecars?.some((o) => o.name === s.name);
            const disabled = locked.has("sidecars");
            return (
              <div key={i} className="flex flex-col gap-2 rounded-lg border border-line bg-panel p-2.5">
                <div className="grid grid-cols-2 gap-2">
                  <Field label="Name">
                    <input className={`${inputClass} font-mono`} value={s.name} spellCheck={false} disabled={fromCluster || disabled} onChange={(e) => setSidecar(i, { name: e.target.value })} />
                  </Field>
                  <Field label="Image">
                    <input className={`${inputClass} font-mono`} value={s.image} spellCheck={false} disabled={disabled} placeholder="registry/tool:1.0" onChange={(e) => setSidecar(i, { image: e.target.value })} />
                  </Field>
                </div>
                <Field label="Command" hint={disabled ? lockedHint : "Split on spaces, no shell. Leave empty to use the image's own."}>
                  <input className={`${inputClass} font-mono`} value={s.command} spellCheck={false} disabled={disabled} onChange={(e) => setSidecar(i, { command: e.target.value })} />
                </Field>
                {!disabled && (
                  <div>
                    <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={() => onChange({ sidecars: sidecars.filter((_, n) => n !== i) })}>
                      Remove sidecar
                    </Button>
                  </div>
                )}
              </div>
            );
          })}
          {!locked.has("sidecars") && (
            <div>
              <Button icon={<Plus className="size-4" />} onClick={() => onChange({ sidecars: [...sidecars, { name: freeSidecarName(block, sidecars), image: "", command: "" }] })}>
                Add a sidecar
              </Button>
            </div>
          )}
          {sidecars.length > 0 && !inCluster && <p className="text-[12px] text-muted">Sidecars get the same locked-down security settings and small resource limits. Ports, config and storage links apply to the main container.</p>}
        </div>
      )}

      {WORKLOADS.has(block.kind) && <ScanSection images={imagesOf(block)} scans={scans} scanning={scanning} canScan={canScan} onScan={onScan} />}

      {(out.length > 0 || into.length > 0) && (
        <div className="text-[12px] text-muted">
          <div className="mb-1 font-medium">Connections</div>
          {out.map((l) => (
            <button key={l.id} className="block text-left hover:text-fg hover:underline" onClick={() => onSelectLink(l.id)}>
              {linkLabel(l)} {byId.get(l.to)?.kind} {byId.get(l.to)?.name}
            </button>
          ))}
          {into.map((l) => (
            <button key={l.id} className="block text-left hover:text-fg hover:underline" onClick={() => onSelectLink(l.id)}>
              {byId.get(l.from)?.kind} {byId.get(l.from)?.name} {linkLabel(l)} this
            </button>
          ))}
        </div>
      )}
      <div className="flex flex-col gap-1">
        <div>
          <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={onRemove}>
            {inCluster ? "Take off the canvas" : "Remove block"}
          </Button>
        </div>
        {inCluster && <p className="text-[12px] text-muted">This doesn't delete it from the cluster.</p>}
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

function SecuritySection({ security }: { security: PodSecurity }) {
  const Icon = security.level === "restricted" ? ShieldCheck : security.level === "baseline" ? Shield : ShieldAlert;
  const tone = security.level === "restricted" ? "text-good" : security.level === "baseline" ? "text-warn" : "text-bad";
  const title =
    security.level === "restricted"
      ? "Meets the restricted Pod Security Standard, the strictest level."
      : security.level === "baseline"
        ? "Meets baseline, but not restricted, because it:"
        : "Doesn't meet baseline, the minimum most clusters enforce, because it:";
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-line bg-panel p-2.5 text-[12px]">
      <div className={`flex items-center gap-1.5 font-medium ${tone}`}>
        <Icon className="size-4" aria-hidden /> Pod security: {security.level}
      </div>
      <p className="text-muted">{title}</p>
      {security.problems.length > 0 && (
        <ul className="list-disc pl-5 text-muted">
          {security.problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ScanSection({ images, scans, scanning, canScan, onScan }: { images: string[]; scans: Map<string, ScanResult>; scanning: boolean; canScan: boolean; onScan: (images: string[]) => void }) {
  if (!images.length) return null;
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-line bg-panel p-2.5 text-[12px]">
      <div className="flex items-center gap-1.5 font-medium text-muted">
        <Bug className="size-4" aria-hidden /> Image vulnerabilities
      </div>
      {images.map((img) => {
        const r = scans.get(img);
        return (
          <div key={img} className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate font-mono text-[11.5px]" title={img}>
              {img}
            </span>
            {!r ? (
              <span className="text-muted">not scanned</span>
            ) : "error" in r ? (
              <span className="text-bad" title={r.error}>
                scan failed
              </span>
            ) : (
              <span className={(r.counts?.CRITICAL ?? 0) ? "text-bad" : (r.counts?.HIGH ?? 0) ? "text-warn" : "text-good"}>
                {r.counts?.CRITICAL ?? 0} critical · {r.counts?.HIGH ?? 0} high
              </span>
            )}
          </div>
        );
      })}
      {canScan ? (
        <div>
          <Button busy={scanning} icon={<Bug className="size-4" />} onClick={() => onScan(images)}>
            Scan with Trivy
          </Button>
        </div>
      ) : (
        <p className="text-muted">Install Trivy to scan images here. The Images page explains how.</p>
      )}
    </div>
  );
}

// --- Link ---------------------------------------------------------------------

export function LinkEditor({ link, model, onChange, onRemove }: { link: Link; model: BuildModel; onChange: (f: Record<string, string>) => void; onRemove: () => void }) {
  const from = model.blocks.find((b) => b.id === link.from);
  const to = model.blocks.find((b) => b.id === link.to);
  if (!from || !to) return null;
  const servicePorts = to.kind === "Service" ? parseServicePorts(to.fields.ports).ports : [];
  const calleePorts = SERVED.has(to.kind) ? parsePorts(to.fields.ports).ports : [];
  const claim = to.name.trim();
  const text: Record<Link["kind"], string> = {
    selects: `Service ${from.name} sends traffic to the pods of ${to.kind} ${to.name}, and each Service port goes to the container port it names.`,
    routes: `Ingress ${from.name} sends requests whose path starts with this prefix to Service ${to.name}.`,
    env: link.imported
      ? `${from.kind} ${from.name} uses ${to.kind} ${to.name}, as environment variables or a mounted volume.`
      : `Every key in ${to.kind} ${to.name} becomes an environment variable in ${from.kind} ${from.name}'s main container (envFrom).`,
    mounts:
      from.kind === "StatefulSet"
        ? `Each copy of StatefulSet ${from.name} gets its own claim with ${to.name}'s size and class, mounted at /data/${claim}. The claims are named ${claim}-${from.name.trim()}-0, -1 and so on, and stay when the StatefulSet is deleted.`
        : `${from.kind} ${from.name} mounts the claim ${to.name}${link.imported ? "" : ` at /data/${claim} in its main container`}.`,
    scales: `Autoscaler ${from.name} changes how many copies of ${to.kind} ${to.name} run, between its fewest and most copies, from CPU use.`,
    talks: `${from.kind} ${from.name} may open connections to ${to.kind} ${to.name}. With Lock down traffic on, a NetworkPolicy lets these in and blocks callers you haven't drawn.`,
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
      {link.kind === "talks" && (
        <Field label="Port" hint="Which of its ports the caller may use.">
          <select className={inputClass} value={port} onChange={(e) => onChange({ port: e.target.value })}>
            <option value="">Any port</option>
            {calleePorts.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name} ({p.port})
              </option>
            ))}
            {port && !calleePorts.some((p) => p.name === port) && <option value={port}>{port} (not a container port)</option>}
          </select>
        </Field>
      )}
      {link.imported ? (
        <p className="text-[12px] text-muted">This connection exists in the cluster. To remove it, edit the object with Create or edit.</p>
      ) : (
        <div>
          <Button variant="ghost" icon={<Trash2 className="size-4" />} onClick={onRemove}>
            Remove link
          </Button>
        </div>
      )}
    </div>
  );
}

// --- Fit ----------------------------------------------------------------------

export interface Need {
  cpu: number;
  mem: number;
  /** The biggest single pod. */
  podCpu: number;
  podMem: number;
}

function cpuText(milli: number): string {
  return milli >= 1000 ? `${(milli / 1000).toFixed(milli % 1000 ? 1 : 0)} CPU` : `${milli}m CPU`;
}

/** "Does it fit?": what new blocks request against what the cluster has free. */
export function CapacityMeter({ need, cap }: { need: Need; cap: ClusterCapacity | null }) {
  if (!cap || cap.nodes === 0 || (need.cpu === 0 && need.mem === 0)) return null;
  const freeCpu = Math.max(0, cap.cpuAllocatable - cap.cpuRequested);
  const freeMem = Math.max(0, cap.memAllocatable - cap.memRequested);
  const over = need.cpu > freeCpu || need.mem > freeMem;
  const pct = Math.min(100, Math.round(Math.max(freeCpu ? need.cpu / freeCpu : 1, freeMem ? need.mem / freeMem : 1) * 100));
  return (
    <div
      className="flex items-center gap-2 text-[11.5px] text-muted"
      title={`New objects request ${cpuText(need.cpu)} and ${formatBytes(need.mem)}. The cluster's ${cap.nodes} ${cap.nodes === 1 ? "node has" : "nodes have"} ${cpuText(freeCpu)} and ${formatBytes(freeMem)} not yet requested by other pods.`}
    >
      <span className={over ? "text-bad" : ""}>
        Needs {cpuText(need.cpu)} · {formatBytes(need.mem)}
      </span>
      <span className="h-1.5 w-16 overflow-hidden rounded-full bg-panel-2" aria-hidden>
        <span className={`block h-full ${over ? "bg-bad" : pct > 75 ? "bg-warn" : "bg-good"}`} style={{ width: `${pct}%` }} />
      </span>
      <span>
        {cpuText(freeCpu)} · {formatBytes(freeMem)} free
      </span>
    </div>
  );
}

// --- Live ---------------------------------------------------------------------

interface Verdict {
  from: string;
  to: string;
  port: number;
  expected: boolean;
  allowed: boolean | null;
  error?: string;
}

export function LivePanel({ model, states, contextName, namespace, error }: { model: BuildModel; states: Map<string, LiveState | null>; contextName: string; namespace: string; error: string | null }) {
  const [verdicts, setVerdicts] = useState<Verdict[] | null>(null);
  const [checking, setChecking] = useState(false);
  const shown = model.blocks.filter((b) => b.kind !== "Secret");
  const ready = shown.filter((b) => states.get(b.id)?.health === "good").length;
  const served = model.blocks.filter((b) => SERVED.has(b.kind));
  const nsOf = (b: Block) => (((b.source?.metadata ?? {}) as Record<string, unknown>).namespace as string | undefined) ?? namespace;

  const checkTraffic = async () => {
    setChecking(true);
    const pairs: Verdict[] = [];
    for (const from of served)
      for (const to of served) {
        if (from.id === to.id || pairs.length >= 30) continue;
        const link = model.links.find((l) => l.kind === "talks" && l.from === from.id && l.to === to.id);
        const ports = parsePorts(to.fields.ports).ports;
        const named = link?.fields?.port ? ports.find((p) => p.name === link.fields?.port)?.port : undefined;
        pairs.push({ from: from.name, to: to.name, port: named ?? ports[0]?.port ?? 0, expected: !model.lockdown || !!link, allowed: null });
      }
    const out: Verdict[] = [];
    for (const p of pairs) {
      const from = served.find((b) => b.name === p.from)!;
      const to = served.find((b) => b.name === p.to)!;
      try {
        const r = await NetworkService.CanReach(contextName, { kind: from.kind, namespace: nsOf(from), name: from.name.trim() }, { kind: to.kind, namespace: nsOf(to), name: to.name.trim() }, p.port, "TCP");
        out.push({ ...p, allowed: !!r?.allowed });
      } catch (err) {
        out.push({ ...p, error: errorMessage(err) });
      }
      setVerdicts([...out]);
    }
    setChecking(false);
  };

  return (
    <div className="flex flex-col gap-3">
      <Notice tone={ready === shown.length && shown.length > 0 ? "good" : "info"}>
        {ready} of {shown.length} objects healthy in {namespace}. This view refreshes every few seconds.
      </Notice>
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="flex flex-col gap-1 text-[12px]">
        {shown.map((b) => {
          const s = states.get(b.id);
          return (
            <div key={b.id} className="flex items-center gap-2">
              <span className={`size-2 shrink-0 rounded-full ${!s ? "animate-pulse bg-line" : s.health === "good" ? "bg-good" : s.health === "warn" ? "bg-warn" : s.health === "bad" ? "bg-bad" : "bg-line"}`} />
              <span className="font-mono text-[11.5px]">{b.kind}</span>
              <span className="font-medium">{b.name}</span>
              <span className="ml-auto truncate text-muted" title={s?.problem ?? s?.text}>
                {s ? (s.problem ?? s.text) : "waiting…"}
              </span>
            </div>
          );
        })}
      </div>
      {served.length > 1 && (
        <div className="flex flex-col gap-2 rounded-lg border border-line bg-panel p-2.5 text-[12px]">
          <div className="font-medium text-muted">Who can reach whom</div>
          <p className="text-muted">
            Kupi reads the NetworkPolicies now in the cluster and checks every pair of workloads. {model.lockdown ? "With lockdown, only the pairs you drew should be allowed." : "Without lockdown, every pair should be allowed."}
          </p>
          <div>
            <Button busy={checking} icon={<Check className="size-4" />} onClick={() => void checkTraffic()}>
              Check traffic
            </Button>
          </div>
          {verdicts?.map((v) => {
            const ok = v.allowed !== null && v.allowed === v.expected;
            return (
              <div key={`${v.from}>${v.to}`} className="flex items-center gap-1.5">
                {v.error ? <CircleX className="size-4 shrink-0 text-muted" /> : ok ? <CircleCheck className="size-4 shrink-0 text-good" /> : <CircleX className="size-4 shrink-0 text-bad" />}
                <span className="font-mono text-[11.5px]">
                  {v.from} → {v.to}
                  {v.port ? `:${v.port}` : ""}
                </span>
                <span className={`ml-auto ${v.error ? "text-muted" : v.allowed ? "text-good" : "text-bad"}`} title={v.error}>
                  {v.error ? "couldn't check" : v.allowed ? "allowed" : "blocked"}
                  {!v.error && !ok ? ` (expected ${v.expected ? "allowed" : "blocked"})` : ""}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
