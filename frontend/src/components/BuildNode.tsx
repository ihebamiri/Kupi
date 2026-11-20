import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Box, Database, FileText, Globe, HardDrive, KeyRound, Network, Server, Timer, TrendingUp } from "lucide-react";
import { blockSummary, type Block, type BlockKind } from "../lib/build";

export const BLOCK_ICONS: Record<BlockKind, typeof Box> = {
  Deployment: Box,
  StatefulSet: Database,
  DaemonSet: Server,
  Job: Timer,
  CronJob: Timer,
  Service: Network,
  Ingress: Globe,
  ConfigMap: FileText,
  Secret: KeyRound,
  PersistentVolumeClaim: HardDrive,
  HorizontalPodAutoscaler: TrendingUp,
};

export interface BuildNodeData extends Record<string, unknown> {
  block: Block;
  problem: "error" | "warn" | null;
}

const handle = "!size-3 !border-2 !border-panel !bg-muted hover:!bg-accent";

/** One block on the Build canvas. Drag from either dot to connect it. */
export const BuildNode = memo(function BuildNode({ data, selected }: NodeProps) {
  const { block, problem } = data as BuildNodeData;
  const Icon = BLOCK_ICONS[block.kind];
  return (
    <div
      className={`flex h-[64px] w-[220px] items-center gap-2.5 rounded-lg border-2 bg-panel px-3 ${
        problem === "error" ? "border-bad" : problem === "warn" ? "border-warn/70" : "border-line"
      } ${selected ? "shadow-[0_0_0_3px_var(--kupi-accent)]" : ""} ${block.kind === "Secret" ? "border-dashed" : ""}`}
      title={`${block.kind} ${block.name}`}
    >
      <Handle id="l" type="source" position={Position.Left} className={handle} />
      <span className="grid size-8 shrink-0 place-items-center rounded-md bg-panel-2 text-muted">
        <Icon className="size-4" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] font-medium">{block.name || "(no name)"}</div>
        <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-muted">
          <span className="shrink-0 rounded bg-panel-2 px-1 font-mono">{block.kind === "PersistentVolumeClaim" ? "PVC" : block.kind === "HorizontalPodAutoscaler" ? "HPA" : block.kind}</span>
          <span className="truncate">{blockSummary(block)}</span>
        </div>
      </div>
      <Handle id="r" type="source" position={Position.Right} className={handle} />
    </div>
  );
});
