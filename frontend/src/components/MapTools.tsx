import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowRight, CircleCheck, CircleX, Pause, Play, SkipBack, SkipForward, X } from "lucide-react";
import { Badge, Button, Notice, inputClass } from "./ui";
import { HistoryService, NetworkService, errorMessage, type HistoryChange, type TrafficCheck } from "../lib/api";
import type { GraphNode } from "../lib/canvas";
import { managerLabel } from "../lib/managers";

const SOURCE_KINDS = new Set(["Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job"]);
const DEST_KINDS = new Set([...SOURCE_KINDS, "Service"]);

/** "Can A talk to B?" from the cluster's NetworkPolicies. */
export function TrafficPanel({
  contextName,
  nodes,
  onClose,
  onResult,
}: {
  contextName: string;
  nodes: GraphNode[];
  onClose: () => void;
  onResult: (from: string, to: string, allowed: boolean | null) => void;
}) {
  const sources = useMemo(() => nodes.filter((n) => SOURCE_KINDS.has(n.kind) && !(n.flags ?? []).includes("missing")), [nodes]);
  const dests = useMemo(() => nodes.filter((n) => DEST_KINDS.has(n.kind) && !(n.flags ?? []).includes("missing")), [nodes]);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [port, setPort] = useState("80");
  const [protocol, setProtocol] = useState("TCP");
  const [result, setResult] = useState<TrafficCheck | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const label = (n: GraphNode) => `${n.kind} ${n.namespace ? `${n.namespace}/` : ""}${n.name}`;
  const find = (id: string) => nodes.find((n) => n.id === id);

  const check = async () => {
    const a = find(from);
    const b = find(to);
    if (!a || !b) return;
    setBusy(true);
    setError(null);
    try {
      const r = await NetworkService.CanReach(contextName, { kind: a.kind, namespace: a.namespace, name: a.name }, { kind: b.kind, namespace: b.namespace, name: b.name }, Number(port) || 0, protocol);
      setResult(r);
      onResult(from, to, r.allowed);
    } catch (err) {
      setError(errorMessage(err));
      setResult(null);
      onResult(from, to, null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="flex flex-col gap-3 rounded-lg border border-line bg-panel p-3">
      <header className="flex items-center gap-2">
        <h2 className="text-[13px] font-semibold">Can it talk?</h2>
        <span className="text-[11px] text-muted">from NetworkPolicies</span>
        <button aria-label="Close" className="ml-auto rounded p-1 text-muted hover:text-fg" onClick={onClose}>
          <X className="size-4" />
        </button>
      </header>
      <label className="flex flex-col gap-1 text-[12px] text-muted">
        From
        <select aria-label="From" className={inputClass} value={from} onChange={(e) => setFrom(e.target.value)}>
          <option value="">Choose a workload or pod…</option>
          {sources.map((n) => (
            <option key={n.id} value={n.id}>
              {label(n)}
            </option>
          ))}
        </select>
      </label>
      <label className="flex flex-col gap-1 text-[12px] text-muted">
        To
        <select aria-label="To" className={inputClass} value={to} onChange={(e) => setTo(e.target.value)}>
          <option value="">Choose a Service, workload or pod…</option>
          {dests.map((n) => (
            <option key={n.id} value={n.id}>
              {label(n)}
            </option>
          ))}
        </select>
      </label>
      <div className="flex items-end gap-2">
        <label className="flex flex-1 flex-col gap-1 text-[12px] text-muted">
          Port (0 for any)
          <input aria-label="Port" className={inputClass} inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, "").slice(0, 5))} />
        </label>
        <select aria-label="Protocol" className={`${inputClass} w-24`} value={protocol} onChange={(e) => setProtocol(e.target.value)}>
          <option>TCP</option>
          <option>UDP</option>
          <option>SCTP</option>
        </select>
        <Button variant="primary" busy={busy} disabled={!from || !to} onClick={() => void check()}>
          Check
        </Button>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {result && (
        <div className="flex flex-col gap-2">
          <div className={`flex items-center gap-2 rounded-md px-3 py-2 text-[13px] font-medium ${result.allowed ? "bg-good/12 text-good" : "bg-bad/12 text-bad"}`}>
            {result.allowed ? <CircleCheck className="size-4" /> : <CircleX className="size-4" />}
            {result.allowed ? "Allowed" : "Blocked"}
            <span className="ml-auto font-mono text-[11px] font-normal text-muted">
              {result.port ? `${result.port}/${result.protocol}` : "any port"}
            </span>
          </div>
          <div className="flex items-center gap-1.5 font-mono text-[11px] text-muted">
            {result.fromPod} <ArrowRight className="size-3" /> {result.toPod}
          </div>
          {(
            [
              ["Leaving the source", result.egress],
              ["Entering the destination", result.ingress],
            ] as const
          ).map(([title, side]) => (
            <div key={title} className="rounded-md border border-line px-3 py-2 text-[12px]">
              <div className="mb-0.5 flex items-center gap-2 font-medium">
                {side.allowed ? <CircleCheck className="size-3.5 text-good" /> : <CircleX className="size-3.5 text-bad" />}
                {title}
                {side.isolated && <Badge className="bg-panel-2 text-muted">{(side.policies ?? []).length} polic{(side.policies ?? []).length === 1 ? "y" : "ies"}</Badge>}
              </div>
              <p className="text-muted">{side.reason}</p>
            </div>
          ))}
          {(result.notes ?? []).map((n) => (
            <p key={n} className="text-[11px] text-muted">
              {n}
            </p>
          ))}
        </div>
      )}
    </section>
  );
}

/** Steps through the changes recorded in History, highlighting each object on the map. */
export function ReplayBar({
  contextName,
  namespace,
  onFocus,
  onClose,
}: {
  contextName: string;
  namespace: string;
  /** Called with the node id (Kind/namespace/name) of the change in view. */
  onFocus: (id: string | null) => void;
  onClose: () => void;
}) {
  const [changes, setChanges] = useState<HistoryChange[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [i, setI] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [hours, setHours] = useState(24);
  const onFocusRef = useRef(onFocus);
  onFocusRef.current = onFocus;

  useEffect(() => {
    setChanges(null);
    setError(null);
    HistoryService.Changes(contextName, { namespace, kind: "", name: "", hours, limit: 500 })
      .then((list) => {
        // Oldest first, for playing forward in time.
        const sorted = [...(list ?? [])].sort((a, b) => a.time.localeCompare(b.time));
        setChanges(sorted);
        setI(Math.max(0, sorted.length - 1));
      })
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, namespace, hours]);

  const current = changes?.[i];
  useEffect(() => {
    onFocusRef.current(current ? `${current.kind}/${current.namespace}/${current.name}` : null);
  }, [current]);
  useEffect(() => () => onFocusRef.current(null), []);

  useEffect(() => {
    if (!playing || !changes) return;
    if (i >= changes.length - 1) {
      setPlaying(false);
      return;
    }
    const t = window.setTimeout(() => setI((n) => n + 1), 1500);
    return () => window.clearTimeout(t);
  }, [playing, i, changes]);

  const n = changes?.length ?? 0;
  return (
    <div className="absolute inset-x-3 bottom-3 z-20 flex flex-col gap-2 rounded-lg border border-line bg-panel/95 px-3 py-2 shadow-xl backdrop-blur">
      <div className="flex items-center gap-2 text-[12px]">
        <span className="font-semibold">Replay</span>
        <select aria-label="Replay period" className={`${inputClass} h-7 text-[12px]`} value={hours} onChange={(e) => setHours(Number(e.target.value))}>
          <option value={1}>last hour</option>
          <option value={24}>last 24 hours</option>
          <option value={168}>last 7 days</option>
        </select>
        <span className="text-muted">{changes ? `${n} change${n === 1 ? "" : "s"} recorded` : "Loading…"}</span>
        <button aria-label="Close replay" className="ml-auto rounded p-1 text-muted hover:text-fg" onClick={onClose}>
          <X className="size-4" />
        </button>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {changes && n === 0 && <p className="text-[12px] text-muted">Nothing was recorded in this period. History records changes while Kupi is connected.</p>}
      {current && (
        <>
          <div className="flex items-center gap-2">
            <Button variant="ghost" className="px-1.5" aria-label="Previous change" disabled={i === 0} onClick={() => setI((x) => Math.max(0, x - 1))} icon={<SkipBack className="size-4" />} />
            <Button variant="ghost" className="px-1.5" aria-label={playing ? "Pause" : "Play"} onClick={() => setPlaying((p) => !p)} icon={playing ? <Pause className="size-4" /> : <Play className="size-4" />} />
            <Button variant="ghost" className="px-1.5" aria-label="Next change" disabled={i >= n - 1} onClick={() => setI((x) => Math.min(n - 1, x + 1))} icon={<SkipForward className="size-4" />} />
            <input type="range" aria-label="Change" className="kupi-range min-w-0 flex-1" min={0} max={n - 1} value={i} onChange={(e) => setI(Number(e.target.value))} />
            <span className="w-16 text-right text-[11px] text-muted tabular-nums">
              {i + 1} / {n}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-[12px]">
            <span className="text-muted tabular-nums">{new Date(current.time).toLocaleString()}</span>
            <Badge className={current.action === "deleted" ? "bg-bad/15 text-bad" : current.action === "created" ? "bg-good/15 text-good" : "bg-accent/15 text-accent"}>{current.action}</Badge>
            <span className="font-medium">
              {current.kind} {current.namespace ? `${current.namespace}/` : ""}
              {current.name}
            </span>
            {current.manager && <span className="text-muted">by {managerLabel(current.manager)}</span>}
            {current.note && <span className="text-muted">· {current.note}</span>}
          </div>
        </>
      )}
    </div>
  );
}
