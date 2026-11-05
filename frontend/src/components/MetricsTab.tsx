import { useEffect, useState } from "react";
import { MetricChart } from "./MetricChart";
import { Notice } from "./ui";
import { MetricsService, errorMessage, type PodMetrics } from "../lib/api";

const REFRESH_MS = 10_000;

/** CPU and memory for one pod over the last hour, against its requests and limits. */
export function MetricsTab({ contextName, namespace, pod }: { contextName: string; namespace: string; pod: string }) {
  const [data, setData] = useState<PodMetrics | null>(null);
  const [error, setError] = useState<string | null>(null);

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

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (!data) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  if (!data.status.available) return <div className="p-4"><Notice tone="warn">{data.status.reason}</Notice></div>;
  const samples = data.samples ?? [];
  return (
    <div className="flex h-full flex-col gap-5 overflow-y-auto px-4 py-3">
      {samples.length < 2 && <p className="text-[12px] text-muted">History builds up while Kupi is open: one point every 10 seconds, up to an hour.</p>}
      <MetricChart samples={samples} metric="cpu" request={data.requests.cpu} limit={data.limits.cpu} />
      <MetricChart samples={samples} metric="mem" request={data.requests.mem} limit={data.limits.mem} />
      {!data.limits.mem && <Notice tone="warn">This pod has no memory limit, so it can use all the memory on its node.</Notice>}
    </div>
  );
}
