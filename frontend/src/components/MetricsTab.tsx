import { useEffect, useState } from "react";
import { MetricChart } from "./MetricChart";
import { Segmented, Notice } from "./ui";
import { MetricsService, errorMessage, type PodMetrics, type PrometheusStatus, type Sample } from "../lib/api";

const REFRESH_MS = 10_000;

type Range = 1 | 6 | 24 | 168;

/**
 * CPU and memory for one pod against its requests and limits. The last hour
 * comes from metrics-server; with Prometheus in the cluster, longer ranges
 * come from it.
 */
export function MetricsTab({ contextName, namespace, pod }: { contextName: string; namespace: string; pod: string }) {
  const [data, setData] = useState<PodMetrics | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [prom, setProm] = useState<PrometheusStatus | null>(null);
  const [range, setRange] = useState<Range>(1);
  const [history, setHistory] = useState<Sample[] | null>(null);
  const [historyError, setHistoryError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      MetricsService.Pod(contextName, namespace, pod)
        .then((d) => !cancelled && setData(d))
        .catch((err) => !cancelled && setError(errorMessage(err)));
    void load();
    const timer = window.setInterval(load, REFRESH_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [contextName, namespace, pod]);

  useEffect(() => {
    void MetricsService.Prometheus(contextName).then(setProm);
  }, [contextName]);

  useEffect(() => {
    setHistory(null);
    setHistoryError(null);
    if (range === 1) return;
    let cancelled = false;
    MetricsService.PodHistory(contextName, namespace, pod, range)
      .then((s) => !cancelled && setHistory(s ?? []))
      .catch((err) => !cancelled && setHistoryError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [contextName, namespace, pod, range]);

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (!data) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  const live = data.samples ?? [];
  const samples = range === 1 ? live : (history ?? []);
  const options: { value: Range; label: string }[] = [
    { value: 1, label: "Last hour" },
    ...(prom?.found
      ? ([
          { value: 6, label: "6 hours" },
          { value: 24, label: "24 hours" },
          { value: 168, label: "7 days" },
        ] as { value: Range; label: string }[])
      : []),
  ];

  return (
    <div className="flex h-full flex-col gap-5 overflow-y-auto px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented label="Time range" value={range} options={options} onChange={setRange} />
        <span className="text-[11px] text-muted">
          {range === 1 ? "From metrics-server, every 10 seconds" : `From Prometheus (${prom?.where})`}
          {range === 1 && !prom?.found && " · install Prometheus for longer history"}
        </span>
      </div>
      {range === 1 && !data.status.available && <Notice tone="warn">{data.status.reason}</Notice>}
      {range === 1 && data.status.available && live.length < 2 && <p className="text-[12px] text-muted">History builds up while Kupi is open: one point every 10 seconds, up to an hour.</p>}
      {range !== 1 && !history && !historyError && <p className="text-[12px] text-muted">Asking Prometheus…</p>}
      {historyError && <Notice tone="bad">{historyError}</Notice>}
      {range !== 1 && history && history.length === 0 && <p className="text-[12px] text-muted">Prometheus has no data for this pod in that range.</p>}
      {(range !== 1 || data.status.available) && (
        <>
          <MetricChart samples={samples} metric="cpu" request={data.requests.cpu} limit={data.limits.cpu} />
          <MetricChart samples={samples} metric="mem" request={data.requests.mem} limit={data.limits.mem} />
        </>
      )}
      {!data.limits.mem && <Notice tone="warn">This pod has no memory limit, so it can use all the memory on its node.</Notice>}
    </div>
  );
}
