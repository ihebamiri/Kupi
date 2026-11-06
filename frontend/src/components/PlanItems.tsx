import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { DiffView } from "./DiffView";
import { Badge } from "./ui";
import type { PlanItemView } from "../lib/api";

const STATE: Record<string, { label: string; className: string }> = {
  create: { label: "Create", className: "bg-good/15 text-good" },
  update: { label: "Update", className: "bg-accent/15 text-accent" },
  unchanged: { label: "Unchanged", className: "bg-panel-2 text-muted" },
  deferred: { label: "Checked when applied", className: "bg-warn/15 text-warn" },
  denied: { label: "Blocked", className: "bg-bad/15 text-bad" },
};

/** The objects of a multi-object apply, in the order they'll be applied. */
export function PlanItems({ items }: { items: PlanItemView[] }) {
  // Open the blocked ones first: that's what needs attention.
  const [open, setOpen] = useState<Set<number>>(() => new Set(items.flatMap((it, i) => (it.state === "denied" ? [i] : []))));
  const toggle = (i: number) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });

  return (
    <ol className="flex flex-col divide-y divide-line overflow-hidden rounded-md border border-line">
      {items.map((it, i) => {
        const st = STATE[it.state] ?? { label: it.state, className: "bg-panel-2 text-muted" };
        const isOpen = open.has(i);
        const where = it.namespace ? `${it.namespace}/${it.name}` : it.name;
        return (
          <li key={i}>
            <button
              type="button"
              aria-expanded={isOpen}
              onClick={() => toggle(i)}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-[13px] hover:bg-panel-2"
            >
              {isOpen ? <ChevronDown className="size-4 shrink-0 text-muted" /> : <ChevronRight className="size-4 shrink-0 text-muted" />}
              <span className="w-5 shrink-0 text-right text-[11px] text-muted tabular-nums">{i + 1}</span>
              <span className="font-medium">{it.kind}</span>
              <span className="min-w-0 truncate font-mono text-[12px] text-muted">{where}</span>
              <Badge className={`ml-auto shrink-0 ${st.className}`}>{st.label}</Badge>
            </button>
            {isOpen && (
              <div className="flex flex-col gap-2 border-t border-line bg-bg/40 px-3 py-3">
                {it.note && <p className={`text-[13px] ${it.state === "denied" ? "text-bad" : "text-muted"}`}>{it.note}</p>}
                {(it.warnings ?? []).map((w, j) => (
                  <p key={j} className="text-[13px] text-warn">
                    {w}
                  </p>
                ))}
                {(it.conflicts ?? []).length > 0 && (
                  <ul className="list-disc pl-4 font-mono text-[12px] text-warn">
                    {(it.conflicts ?? []).map((c, j) => (
                      <li key={j}>{c}</li>
                    ))}
                  </ul>
                )}
                {(it.summary ?? []).length > 0 && (
                  <ul className="flex flex-col gap-0.5 font-mono text-[12px] text-muted">
                    {(it.summary ?? []).map((s, j) => (
                      <li key={j} className="break-all">
                        {s}
                      </li>
                    ))}
                  </ul>
                )}
                {it.diff ? <DiffView diff={it.diff} /> : it.state === "unchanged" && <p className="text-[13px] text-muted">Already matches the cluster.</p>}
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
