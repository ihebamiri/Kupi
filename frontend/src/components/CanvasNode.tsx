import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Box, Container, FileText, Globe, HardDrive, KeyRound, Network, Timer, TrendingUp, TriangleAlert } from "lucide-react";
import type { GraphNode } from "../lib/canvas";
import type { PodSummary } from "../lib/api";

export interface CanvasNodeData extends Record<string, unknown> {
  node: GraphNode;
  /** Ring colour from the active overlay, or null. */
  ring: "good" | "warn" | "bad" | "accent" | null;
  dimmed: boolean;
  selected: boolean;
}

const ICONS: Record<string, typeof Box> = {
  Ingress: Globe,
  HTTPRoute: Globe,
  Service: Network,
  Deployment: Box,
  StatefulSet: Box,
  DaemonSet: Box,
  ReplicaSet: Box,
  Job: Timer,
  CronJob: Timer,
  Pod: Container,
  ConfigMap: FileText,
  Secret: KeyRound,
  PersistentVolumeClaim: HardDrive,
  HorizontalPodAutoscaler: TrendingUp,
};

const HEALTH_DOT: Record<string, string> = { good: "bg-good", warn: "bg-warn", bad: "bg-bad", unknown: "bg-line" };
const RING: Record<string, string> = {
  good: "border-good",
  warn: "border-warn",
  bad: "border-bad",
  accent: "border-accent",
};

/** One object on the canvas: its real kind, name, health and a one-line summary. */
export const CanvasNode = memo(function CanvasNode({ data }: NodeProps) {
  const { node, ring, dimmed, selected } = data as CanvasNodeData;
  const Icon = ICONS[node.kind] ?? Box;
  const missing = (node.flags ?? []).includes("missing");
  return (
    <div
      className={`flex h-[68px] w-[230px] items-center gap-2.5 rounded-lg border-2 bg-panel px-3 transition-opacity ${
        ring ? RING[ring] : "border-line"
      } ${dimmed ? "opacity-30" : ""} ${selected ? "shadow-[0_0_0_3px_var(--kupi-accent)]" : ""} ${missing ? "border-dashed" : ""}`}
      title={`${node.kind} ${node.namespace ? `${node.namespace}/` : ""}${node.name}`}
    >
      <Handle type="target" position={Position.Left} className="!size-1.5 !border-0 !bg-line" />
      <span className="grid size-8 shrink-0 place-items-center rounded-md bg-panel-2 text-muted">
        {missing ? <TriangleAlert className="size-4 text-bad" aria-hidden /> : <Icon className="size-4" aria-hidden />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className={`size-2 shrink-0 rounded-full ${HEALTH_DOT[node.health] ?? "bg-line"}`} aria-label={`health ${node.health}`} />
          <span className="truncate text-[13px] font-medium">{node.name}</span>
        </div>
        <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-muted">
          <span className="rounded bg-panel-2 px-1 font-mono">{node.kind}</span>
          <span className="truncate">{node.summary}</span>
        </div>
      </div>
      {node.pods > 0 && (
        <span className="shrink-0 rounded bg-panel-2 px-1.5 py-0.5 text-[11px]" title={`${node.ready} of ${node.pods} pods ready, ${node.restarts} restarts`}>
          ×{node.pods}
        </span>
      )}
      <Handle type="source" position={Position.Right} className="!size-1.5 !border-0 !bg-line" />
      <Handle id="pods" type="source" position={Position.Bottom} className="!size-1 !border-0 !bg-transparent" />
    </div>
  );
});

/** Frame around one namespace's objects. */
export const NamespaceFrame = memo(function NamespaceFrame({ data }: NodeProps) {
  return (
    <div className="h-full w-full rounded-xl border border-dashed border-line bg-panel/30">
      <span className="absolute top-2 left-3 text-[12px] font-medium text-muted">{String((data as { label: string }).label)}</span>
    </div>
  );
});

export interface PodChipData extends Record<string, unknown> {
  pod: PodSummary;
  selected: boolean;
  /** "+3 more" chip when the list is cut short. */
  more?: number;
}

export const POD_CHIP_W = 168;
export const POD_CHIP_H = 40;

/** One pod of the selected workload, fanned out under it on the canvas. */
export const PodChip = memo(function PodChip({ data }: NodeProps) {
  const { pod, selected, more } = data as PodChipData;
  if (more) {
    return (
      <div className="flex h-[40px] w-[168px] items-center justify-center rounded-md border border-dashed border-line bg-panel/80 text-[12px] text-muted">
        <Handle type="target" position={Position.Top} className="!size-1 !border-0 !bg-transparent" />+{more} more in the list
      </div>
    );
  }
  const bad = pod.health === "bad";
  return (
    <div
      className={`flex h-[40px] w-[168px] cursor-pointer items-center gap-2 rounded-md border bg-panel px-2 shadow-lg shadow-black/20 ${
        bad ? "border-bad/70" : pod.health === "warn" ? "border-warn/70" : "border-line"
      } ${selected ? "shadow-[0_0_0_2px_var(--kupi-accent)]" : ""}`}
      title={`Pod ${pod.name}: ${pod.status}, ${pod.ready} ready, ${pod.restarts} restarts${pod.node ? `, on ${pod.node}` : ""}`}
    >
      <Handle type="target" position={Position.Top} className="!size-1 !border-0 !bg-line" />
      <Container className="size-3.5 shrink-0 text-muted" aria-hidden />
      <div className="min-w-0 flex-1 leading-tight">
        <div className="flex items-center gap-1">
          <span className={`size-1.5 shrink-0 rounded-full ${HEALTH_DOT[pod.health] ?? "bg-line"}`} aria-hidden />
          <span className="truncate font-mono text-[11px]">{pod.name}</span>
        </div>
        <div className={`truncate text-[10px] ${bad ? "text-bad" : "text-muted"}`}>
          {pod.status} · {pod.ready}
          {pod.restarts > 0 ? ` · ${pod.restarts}↻` : ""}
        </div>
      </div>
    </div>
  );
});
