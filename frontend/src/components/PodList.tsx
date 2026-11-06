import { Container, LoaderCircle } from "lucide-react";
import type { PodSummary } from "../lib/api";
import { age } from "../lib/format";

const DOT: Record<string, string> = { good: "bg-good", warn: "bg-warn", bad: "bg-bad" };

interface PodListProps {
  /** "Deployment web" */
  owner: string;
  pods: PodSummary[];
  note?: string;
  truncated?: boolean;
  loading: boolean;
  error: string | null;
  onOpen: (pod: PodSummary) => void;
}

/** The pods behind a workload or Service, worst first. Click one for its logs, shell and details. */
export function PodList({ owner, pods, note, truncated, loading, error, onOpen }: PodListProps) {
  const failing = pods.filter((p) => p.health === "bad").length;
  return (
    <section className="flex max-h-[42%] min-h-0 flex-col rounded-lg border border-line bg-panel">
      <header className="flex items-center gap-2 border-b border-line px-3 py-2">
        <Container className="size-4 text-muted" aria-hidden />
        <h2 className="text-[13px] font-semibold">Pods</h2>
        <span className="text-[12px] text-muted">
          of {owner}
          {!loading && ` · ${pods.length}${truncated ? "+" : ""}`}
          {failing > 0 && <span className="text-bad"> · {failing} failing</span>}
        </span>
        {loading && <LoaderCircle className="ml-auto size-3.5 animate-spin text-muted" aria-label="Loading pods" />}
      </header>
      {error && <p className="px-3 py-2 text-[12px] text-bad">{error}</p>}
      {!error && !loading && pods.length === 0 && <p className="px-3 py-2 text-[12px] text-muted">{note || "No pods right now."}</p>}
      {pods.length > 0 && (
        <div className="min-h-0 overflow-auto">
          <table className="w-full text-[12px]">
            <thead className="sticky top-0 bg-panel text-left text-[11px] text-muted">
              <tr>
                <th className="min-w-[150px] px-3 py-1.5 font-medium">Name</th>
                <th className="px-2 py-1.5 font-medium">Status</th>
                <th className="px-2 py-1.5 font-medium">Ready</th>
                <th className="px-2 py-1.5 text-right font-medium">Restarts</th>
                <th className="px-2 py-1.5 font-medium">Node</th>
                <th className="px-3 py-1.5 text-right font-medium">Age</th>
              </tr>
            </thead>
            <tbody>
              {pods.map((p) => (
                <tr
                  key={p.uid}
                  tabIndex={0}
                  onClick={() => onOpen(p)}
                  onKeyDown={(e) => e.key === "Enter" && onOpen(p)}
                  className="cursor-pointer border-t border-line/60 hover:bg-panel-2 focus:bg-panel-2 focus:outline-none"
                  title="Open logs, shell and details"
                >
                  <td className="max-w-0 px-3 py-1.5">
                    <span className="flex items-center gap-1.5">
                      <span className={`size-2 shrink-0 rounded-full ${DOT[p.health] ?? "bg-line"}`} aria-hidden />
                      <span className="truncate font-mono">{p.name}</span>
                    </span>
                  </td>
                  <td className={`px-2 py-1.5 whitespace-nowrap ${p.health === "bad" ? "text-bad" : p.health === "warn" ? "text-warn" : ""}`}>{p.status}</td>
                  <td className="px-2 py-1.5 tabular-nums">{p.ready}</td>
                  <td className={`px-2 py-1.5 text-right tabular-nums ${p.restarts > 0 ? "text-warn" : "text-muted"}`}>{p.restarts}</td>
                  <td className="max-w-[110px] truncate px-2 py-1.5 text-muted" title={p.node}>
                    {p.node || "—"}
                  </td>
                  <td className="px-3 py-1.5 text-right text-muted tabular-nums">{p.created ? age(p.created) : ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
