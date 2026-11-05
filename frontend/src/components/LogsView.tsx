import { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Copy, Pause, Play } from "lucide-react";
import { KubectlButton } from "./CopyButton";
import { Notice } from "./ui";
import { LogService, ResourceService, errorMessage, type ContainerView, type LogBatch, type LogLine, type ResourceRef } from "../lib/api";
import { copyText } from "../lib/bytes";
import { KupiLogs, bufferedListener } from "../lib/events";
import { kubectlLogs, type KubeTarget } from "../lib/kubectl";

/** Kept in memory per view; older lines are dropped. */
const MAX_LINES = 50_000;
const POD_COLORS = ["text-dev", "text-staging", "text-local", "text-accent", "text-production"];

interface LogsViewProps {
  contextName: string;
  namespace: string;
  pod?: string;
  workload?: ResourceRef;
  target: KubeTarget;
}

export function LogsView({ contextName, namespace, pod, workload, target }: LogsViewProps) {
  const [containers, setContainers] = useState<ContainerView[]>([]);
  const [container, setContainer] = useState("");
  const [follow, setFollow] = useState(true);
  const [previous, setPrevious] = useState(false);
  const [timestamps, setTimestamps] = useState(false);
  const [tail, setTail] = useState(500);
  const [filter, setFilter] = useState("");
  const [paused, setPaused] = useState(false);
  const [lines, setLines] = useState<LogLine[]>([]);
  const [ended, setEnded] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const buffer = useRef<LogLine[]>([]);
  const frame = useRef(0);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  useEffect(() => {
    if (!pod) return;
    ResourceService.Containers(contextName, namespace, pod)
      .then((list) => {
        const all = list ?? [];
        setContainers(all);
        const first = all.find((c) => !c.init) ?? all[0];
        if (first) setContainer((cur) => cur || first.name);
      })
      .catch(() => setContainers([]));
  }, [contextName, namespace, pod]);

  const workloadKey = workload ? `${workload.group}/${workload.resource}/${workload.name}` : "";

  useEffect(() => {
    if (pod && containers.length > 0 && !container) return;
    buffer.current = [];
    setLines([]);
    setEnded(null);
    setError(null);
    let id = "";
    let cancelled = false;
    const flush = () => {
      frame.current = 0;
      if (!pausedRef.current) setLines(buffer.current);
    };
    const listener = bufferedListener<LogBatch>(KupiLogs, (batch) => {
      if (batch.lines && batch.lines.length > 0) {
        const next = buffer.current.concat(batch.lines);
        buffer.current = next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
        if (!frame.current) frame.current = requestAnimationFrame(flush);
      }
      if (batch.done) setEnded(batch.error || "");
    });
    LogService.Start(contextName, {
      namespace,
      pod: pod ?? "",
      workload: workload ?? null,
      container,
      follow,
      previous,
      timestamps,
      tailLines: tail,
    })
      .then((sid) => {
        if (cancelled) {
          void LogService.Stop(sid);
          return;
        }
        id = sid;
        listener.bind(sid);
      })
      .catch((err) => {
        listener.off();
        setError(errorMessage(err));
      });
    return () => {
      cancelled = true;
      listener.off();
      if (frame.current) cancelAnimationFrame(frame.current);
      frame.current = 0;
      if (id) void LogService.Stop(id);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contextName, namespace, pod, workloadKey, container, follow, previous, timestamps, tail, containers.length]);

  useEffect(() => {
    if (!paused) setLines(buffer.current);
  }, [paused]);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? lines.filter((l) => l.text.toLowerCase().includes(q)) : lines;
  }, [lines, filter]);
  const multiPod = useMemo(() => new Set(lines.map((l) => l.pod)).size > 1 || !!workload, [lines, workload]);
  const podColor = useMemo(() => {
    const map = new Map<string, string>();
    for (const l of lines) if (!map.has(l.pod)) map.set(l.pod, POD_COLORS[map.size % POD_COLORS.length]);
    return map;
  }, [lines]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const virtualizer = useVirtualizer({ count: shown.length, getScrollElement: () => scrollRef.current, estimateSize: () => 18, overscan: 30 });
  useEffect(() => {
    if (follow && atBottom.current && shown.length > 0) virtualizer.scrollToIndex(shown.length - 1, { align: "end" });
  }, [shown.length, follow, virtualizer]);

  const command = kubectlLogs(target, { container: container || undefined, follow, previous, tail, timestamps, workload: !!workload });

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2 text-[12px]">
        {pod && containers.length > 1 && (
          <select aria-label="Container" className="rounded-md border border-line bg-panel-2 px-1.5 py-1" value={container} onChange={(e) => setContainer(e.target.value)}>
            {containers.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
                {c.init ? " (init)" : ""}
              </option>
            ))}
          </select>
        )}
        <select aria-label="Lines" className="rounded-md border border-line bg-panel-2 px-1.5 py-1" value={tail} onChange={(e) => setTail(Number(e.target.value))}>
          {[100, 500, 2000, 10000].map((n) => (
            <option key={n} value={n}>
              last {n} lines
            </option>
          ))}
        </select>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
        </label>
        <label className="flex items-center gap-1" title="Logs of the previous container instance, useful after a crash">
          <input type="checkbox" checked={previous} onChange={(e) => setPrevious(e.target.checked)} /> Previous
        </label>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={timestamps} onChange={(e) => setTimestamps(e.target.checked)} /> Timestamps
        </label>
        <input
          aria-label="Filter log lines"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter"
          className="h-7 w-32 rounded-md border border-line bg-bg px-2 text-fg placeholder:text-muted focus:border-accent focus:outline-none"
        />
        <button className="rounded p-1 text-muted hover:text-fg" onClick={() => setPaused((p) => !p)} aria-label={paused ? "Resume" : "Pause"} title={paused ? "Resume" : "Pause the view"}>
          {paused ? <Play className="size-4" /> : <Pause className="size-4" />}
        </button>
        <button className="rounded p-1 text-muted hover:text-fg" onClick={() => void copyText(shown.map((l) => l.text).join("\n"))} aria-label="Copy lines" title="Copy shown lines">
          <Copy className="size-4" />
        </button>
        <span className="ml-auto">
          <KubectlButton command={command} label="kubectl" />
        </span>
      </div>
      {error && <div className="p-3"><Notice tone="bad">{error}</Notice></div>}
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-auto bg-bg font-mono text-[12px] leading-[18px]"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
      >
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((item) => {
            const l = shown[item.index];
            return (
              <div key={item.key} className="absolute left-0 w-full px-3 whitespace-pre" style={{ transform: `translateY(${item.start}px)`, height: item.size }}>
                {multiPod && <span className={`mr-2 ${podColor.get(l.pod) ?? "text-muted"}`}>{l.pod}</span>}
                {l.text}
              </div>
            );
          })}
        </div>
      </div>
      <div className="border-t border-line px-3 py-1 text-[11px] text-muted">
        {shown.length} lines{paused ? " · paused" : ""}
        {ended !== null && (ended ? <span className="text-bad"> · stream ended: {ended}</span> : " · stream ended")}
      </div>
    </div>
  );
}
