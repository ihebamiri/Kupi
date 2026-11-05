import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Box, Container, FileText, Globe, HardDrive, KeyRound, Network, Timer, TrendingUp, TriangleAlert } from "lucide-react";
import type { GraphNode } from "../lib/canvas";

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
