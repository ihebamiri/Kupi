import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { Sample } from "../lib/api";
import { formatBytes, formatCPU } from "../lib/units";

interface MetricChartProps {
  samples: Sample[];
  metric: "cpu" | "mem";
  /** Horizontal guide lines, e.g. the request and the limit. */
  request?: number;
  limit?: number;
  height?: number;
}

const COLORS = { line: "#5b8def", request: "#3ccf91", limit: "#f06a6a", grid: "rgba(150,160,180,0.15)", axis: "#9aa3b2" };

/** Canvas time-series chart (uPlot), cheap enough to update every few seconds. */
export function MetricChart({ samples, metric, request, limit, height = 150 }: MetricChartProps) {
  const host = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);
  const format = metric === "cpu" ? formatCPU : formatBytes;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const opts: uPlot.Options = {
      width: el.clientWidth || 400,
      height,
      legend: { show: false },
      cursor: { drag: { x: false, y: false } },
      scales: { x: { time: true }, y: { range: (_u, _min, max) => [0, Math.max(max, request ?? 0, limit ?? 0) * 1.15 || 1] } },
      axes: [
        { stroke: COLORS.axis, grid: { stroke: COLORS.grid }, ticks: { stroke: COLORS.grid } },
        { stroke: COLORS.axis, grid: { stroke: COLORS.grid }, ticks: { stroke: COLORS.grid }, size: 64, values: (_u, vals) => vals.map((v) => format(v)) },
      ],
      series: [
        {},
        { stroke: COLORS.line, width: 1.5, fill: "rgba(91,141,239,0.12)", label: metric === "cpu" ? "CPU" : "Memory" },
        { stroke: COLORS.request, width: 1, dash: [4, 4], label: "Request" },
        { stroke: COLORS.limit, width: 1, dash: [4, 4], label: "Limit" },
      ],
    };
    plot.current = new uPlot(opts, [[], [], [], []], el);
    const observer = new ResizeObserver(() => plot.current?.setSize({ width: el.clientWidth, height }));
    observer.observe(el);
    return () => {
      observer.disconnect();
      plot.current?.destroy();
      plot.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [metric, height, request, limit]);

  useEffect(() => {
    const xs = samples.map((s) => s.t);
    const ys = samples.map((s) => (metric === "cpu" ? s.cpu : s.mem));
    const req = samples.map(() => (request ? request : null));
    const lim = samples.map(() => (limit ? limit : null));
    plot.current?.setData([xs, ys, req, lim] as uPlot.AlignedData);
  }, [samples, metric, request, limit]);

  const last = samples.length > 0 ? samples[samples.length - 1] : null;
  return (
    <div>
      <div className="mb-1 flex items-baseline gap-3 text-[12px] text-muted">
        <span className="font-medium text-fg">{metric === "cpu" ? "CPU" : "Memory"}</span>
        {last && <span className="text-fg">{format(metric === "cpu" ? last.cpu : last.mem)} now</span>}
        {request ? <span className="text-good">request {format(request)}</span> : null}
        {limit ? <span className="text-bad">limit {format(limit)}</span> : null}
      </div>
      <div ref={host} role="img" aria-label={`${metric === "cpu" ? "CPU" : "Memory"} over the last hour`} />
    </div>
  );
}
