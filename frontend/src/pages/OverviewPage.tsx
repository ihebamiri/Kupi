import { useEffect, useState } from "react";
import { Notice, PageHeader } from "../components/ui";
import { MetricsService, errorMessage, type Overview, type PodUsage } from "../lib/api";
import { formatBytes, formatCPU, percent } from "../lib/units";
import { useClusters } from "../state/clusters";

const REFRESH_MS = 10_000;

function Bar({ value, label }: { value: number | null; label: string }) {
  const pct = value ?? 0;
  const tone = pct >= 90 ? "bg-bad" : pct >= 70 ? "bg-warn" : "bg-good";
  return (
    <div className="flex items-center gap-2" aria-label={`${label} ${value === null ? "unknown" : `${value}%`}`}>
      <div className="h-2 flex-1 overflow-hidden rounded-full bg-panel-2">
        <div className={`h-full ${tone}`} style={{ width: `${Math.min(100, pct)}%` }} />
      </div>
      <span className="w-10 text-right text-[12px] text-muted">{value === null ? "—" : `${value}%`}</span>
    </div>
  );
}

function TopList({ title, pods, metric }: { title: string; pods: PodUsage[]; metric: "cpu" | "mem" }) {
  return (
    <section className="rounded-lg border border-line bg-panel">
      <h2 className="border-b border-line px-4 py-2 text-[13px] font-semibold">{title}</h2>
      {pods.length === 0 ? (
        <p className="px-4 py-3 text-[13px] text-muted">No pod data yet.</p>
      ) : (
        <ol className="text-[13px]">
          {pods.map((p) => (
            <li key={`${p.namespace}/${p.name}`} className="flex items-center gap-2 border-b border-line/60 px-4 py-1.5 last:border-0">
              <span className="truncate" title={`${p.namespace}/${p.name}`}>
                {p.name}
              </span>
              <span className="text-[12px] text-muted">{p.namespace}</span>
              <span className="ml-auto font-mono text-[12px]">{metric === "cpu" ? formatCPU(p.cpu) : formatBytes(p.mem)}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

/** Live CPU and memory for the cluster: nodes, and the busiest pods. */
export function OverviewPage() {
  const { active, namespace } = useClusters();
  const [data, setData] = useState<Overview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const contextName = active?.name ?? "";

  useEffect(() => {
    if (!contextName) return;
    let cancelled = false;
    const load = () =>
      MetricsService.Overview(contextName, namespace)
        .then((o) => {
          if (!cancelled) {
            setData(o);
            setError(null);
          }
        })
        .catch((err) => !cancelled && setError(errorMessage(err)));
    void load();
    const timer = window.setInterval(load, REFRESH_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [contextName, namespace]);

  if (!active) return null;
  const status = data?.status;
  return (
    <div>
      <PageHeader
        title="Overview"
        description={`Live CPU and memory on ${active.name}${namespace ? `, pods in ${namespace}` : ""}. Updates every 10 seconds while this page is open.`}
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {status && !status.available && <Notice tone="warn">{status.reason}</Notice>}
      {data && status?.available && (
        <div className="flex flex-col gap-4">
          <section className="rounded-lg border border-line bg-panel">
            <h2 className="border-b border-line px-4 py-2 text-[13px] font-semibold">Nodes</h2>
            <div className="grid grid-cols-[minmax(140px,1fr)_2fr_2fr] gap-x-4 gap-y-2 px-4 py-3 text-[13px]">
              <span className="text-[12px] text-muted">Node</span>
              <span className="text-[12px] text-muted">CPU</span>
              <span className="text-[12px] text-muted">Memory</span>
              {(data.nodes ?? []).map((n) => (
                <div key={n.name} className="contents">
                  <span className="truncate font-medium">{n.name}</span>
                  <div>
                    <Bar value={percent(n.cpu, n.cpuCapacity)} label="CPU" />
                    <span className="text-[11px] text-muted">
                      {formatCPU(n.cpu)} of {formatCPU(n.cpuCapacity)}
                    </span>
                  </div>
                  <div>
                    <Bar value={percent(n.mem, n.memCapacity)} label="Memory" />
                    <span className="text-[11px] text-muted">
                      {formatBytes(n.mem)} of {formatBytes(n.memCapacity)}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </section>
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <TopList title="Most CPU" pods={data.topCpu ?? []} metric="cpu" />
            <TopList title="Most memory" pods={data.topMem ?? []} metric="mem" />
          </div>
        </div>
      )}
      {!data && !error && <p className="text-[13px] text-muted">Collecting the first sample…</p>}
    </div>
  );
}
