import { useState } from "react";
import { ChevronDown, ChevronRight, FilePen, FilePlus2, FileX2, TriangleAlert } from "lucide-react";
import { DiffView } from "./DiffView";
import { Badge } from "./ui";
import type { HistoryChange, HistoryEvent } from "../lib/api";
import { age } from "../lib/format";
import { managerLabel } from "../lib/managers";

export type TimelineItem = { type: "change"; at: string; change: HistoryChange } | { type: "event"; at: string; event: HistoryEvent };

/** Merges changes and warnings into one list, newest first. */
export function mergeTimeline(changes: HistoryChange[], events: HistoryEvent[]): TimelineItem[] {
  const items: TimelineItem[] = [
    ...changes.map((c) => ({ type: "change" as const, at: c.time, change: c })),
    ...events.map((e) => ({ type: "event" as const, at: e.time, event: e })),
  ];
  return items.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

const ACTION_ICON = {
  created: <FilePlus2 className="size-4 text-good" aria-hidden />,
  changed: <FilePen className="size-4 text-accent" aria-hidden />,
  deleted: <FileX2 className="size-4 text-bad" aria-hidden />,
};

function clock(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function ChangeRow({ change, showName = true }: { change: HistoryChange; showName?: boolean }) {
  const [open, setOpen] = useState(false);
  const hasDiff = change.diff !== "";
  return (
    <li className="rounded-md border border-line bg-panel">
      <button
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-[13px] disabled:cursor-default"
        disabled={!hasDiff}
        aria-expanded={hasDiff ? open : undefined}
        onClick={() => setOpen(!open)}
      >
        {hasDiff ? open ? <ChevronDown className="size-3.5 text-muted" aria-hidden /> : <ChevronRight className="size-3.5 text-muted" aria-hidden /> : <span className="w-3.5" />}
        {ACTION_ICON[change.action as keyof typeof ACTION_ICON] ?? ACTION_ICON.changed}
        <span className="min-w-0 truncate">
          {showName && (
            <>
              <span className="text-muted">{change.kind} </span>
              <span className="font-medium">
                {change.namespace ? `${change.namespace}/` : ""}
                {change.name}
              </span>{" "}
            </>
          )}
          {change.action}
          {change.manager && <span className="text-muted"> by {managerLabel(change.manager)}</span>}
        </span>
        <span className="ml-auto shrink-0 text-[12px] text-muted" title={change.time}>
          {clock(change.time)} · {age(change.time)} ago
        </span>
      </button>
      {change.note && <p className="px-9 pb-2 text-[12px] text-muted">{change.note}</p>}
      {open && hasDiff && (
        <div className="px-3 pb-3">
          <DiffView diff={change.diff} />
        </div>
      )}
    </li>
  );
}

export function EventRow({ event }: { event: HistoryEvent }) {
  return (
    <li className="flex items-start gap-2 rounded-md border border-warn/30 bg-warn/5 px-3 py-2 text-[13px]">
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warn" aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2">
          <span className="font-medium">{event.reason}</span>
          <span className="text-muted">
            {event.kind} {event.namespace ? `${event.namespace}/` : ""}
            {event.name}
          </span>
          {event.count > 1 && <Badge className="bg-panel-2 text-muted">×{event.count}</Badge>}
        </div>
        <p className="mt-0.5 break-words text-fg/90">{event.message}</p>
      </div>
      <span className="shrink-0 text-[12px] text-muted" title={event.time}>
        {clock(event.time)} · {age(event.time)} ago
      </span>
    </li>
  );
}

export function Timeline({ items }: { items: TimelineItem[] }) {
  return (
    <ul className="flex flex-col gap-1.5">
      {items.map((it, i) => (it.type === "change" ? <ChangeRow key={`c${i}`} change={it.change} /> : <EventRow key={`e${it.event.uid}`} event={it.event} />))}
    </ul>
  );
}
