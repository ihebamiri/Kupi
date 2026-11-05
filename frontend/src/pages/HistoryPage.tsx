import { useCallback, useEffect, useState } from "react";
import { History, RefreshCw } from "lucide-react";
import { ChangeRow, EventRow } from "../components/Timeline";
import { Button, Notice, PageHeader, inputClass } from "../components/ui";
import { HistoryService, errorMessage, type HistoryChange, type HistoryEvent, type HistoryStatus } from "../lib/api";
import { age } from "../lib/format";
import { RECORDED_KINDS } from "../lib/managers";
import { useClusters } from "../state/clusters";

const PERIODS = [
  { hours: 1, label: "Last hour" },
  { hours: 6, label: "Last 6 hours" },
  { hours: 24, label: "Last 24 hours" },
  { hours: 72, label: "Last 3 days" },
  { hours: 336, label: "Last 14 days" },
];

/** Every change and warning Kupi recorded for the connected cluster. */
export function HistoryPage() {
  const { active, namespace } = useClusters();
  const [tab, setTab] = useState<"changes" | "events">("changes");
  const [kind, setKind] = useState("");
  const [name, setName] = useState("");
  const [hours, setHours] = useState(24);
  const [changes, setChanges] = useState<HistoryChange[] | null>(null);
  const [events, setEvents] = useState<HistoryEvent[] | null>(null);
  const [status, setStatus] = useState<HistoryStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const contextName = active?.name ?? "";

  const load = useCallback(() => {
    if (!contextName) return;
    setError(null);
    const q = { namespace, kind: tab === "changes" ? kind : "", name: name.trim(), hours, limit: 500 };
    HistoryService.Status(contextName).then(setStatus).catch(() => setStatus(null));
    const req = tab === "changes" ? HistoryService.Changes(contextName, q).then((c) => setChanges(c ?? [])) : HistoryService.Events(contextName, q).then((e) => setEvents(e ?? []));
    req.catch((err) => setError(errorMessage(err)));
  }, [contextName, namespace, kind, name, hours, tab]);

  useEffect(load, [load]);
  if (!active) return <p className="text-[13px] text-muted">Connect to a cluster first.</p>;

  const list = tab === "changes" ? changes : events;
  return (
    <div className="max-w-5xl">
      <PageHeader
        title="History"
        description="Changes to workloads, services, config and storage that Kupi saw while connected, with the tool that made each one and what changed. Warning events are kept too, since Kubernetes forgets them after about an hour. Records are kept on this computer for 14 days."
        actions={
          <div role="tablist" aria-label="History view" className="flex rounded-md border border-line p-0.5">
            {(["changes", "events"] as const).map((t) => (
              <button key={t} role="tab" aria-selected={tab === t} onClick={() => setTab(t)} className={`rounded px-3 py-1 text-[13px] ${tab === t ? "bg-panel-2 font-medium text-fg" : "text-muted hover:text-fg"}`}>
                {t === "changes" ? "Changes" : "Warnings"}
              </button>
            ))}
          </div>
        }
      />
      {status && (
        <p className="mb-3 flex items-center gap-2 text-[12px] text-muted">
          <History className="size-3.5" aria-hidden />
          {status.recording
            ? `Recording ${status.namespace ? `namespace ${status.namespace}` : "all namespaces"} since ${age(status.since)} ago.`
            : "Not recording this cluster right now."}
          {(status.skipped ?? []).length > 0 && ` Not recorded: ${(status.skipped ?? []).join(", ")}.`}
        </p>
      )}
      <div className="mb-4 flex flex-wrap gap-2">
        {tab === "changes" && (
          <select aria-label="Kind" value={kind} onChange={(e) => setKind(e.target.value)} className={inputClass}>
            <option value="">All kinds</option>
            {RECORDED_KINDS.map((k) => (
              <option key={k}>{k}</option>
            ))}
          </select>
        )}
        <input aria-label="Exact name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Exact name" className={`${inputClass} w-56`} />
        <select aria-label="Period" value={hours} onChange={(e) => setHours(Number(e.target.value))} className={inputClass}>
          {PERIODS.map((p) => (
            <option key={p.hours} value={p.hours}>
              {p.label}
            </option>
          ))}
        </select>
        <Button variant="ghost" icon={<RefreshCw className="size-4" />} onClick={load}>
          Refresh
        </Button>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {list && list.length === 0 && <p className="text-[13px] text-muted">Nothing recorded{namespace ? ` in ${namespace}` : ""} for this period.</p>}
      <ul className="flex flex-col gap-1.5">
        {tab === "changes" ? (changes ?? []).map((c, i) => <ChangeRow key={i} change={c} />) : (events ?? []).map((e) => <EventRow key={e.uid} event={e} />)}
      </ul>
      {list && list.length >= 500 && <p className="mt-2 text-[12px] text-muted">Showing the newest 500. Narrow the filters to see older ones.</p>}
    </div>
  );
}

/** A resource's own history, for the detail panel. */
export function ObjectHistory({ contextName, kind, namespace, name }: { contextName: string; kind: string; namespace: string; name: string }) {
  const [changes, setChanges] = useState<HistoryChange[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    HistoryService.Changes(contextName, { namespace, kind, name, hours: 336, limit: 200 })
      .then((c) => setChanges(c ?? []))
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, kind, namespace, name]);
  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (!changes) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  return (
    <div className="p-4">
      {changes.length === 0 ? (
        <p className="text-[13px] text-muted">No changes recorded. Kupi records changes while it's connected, for 14 days.</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {changes.map((c, i) => (
            <ChangeRow key={i} change={c} showName={false} />
          ))}
        </ul>
      )}
    </div>
  );
}
