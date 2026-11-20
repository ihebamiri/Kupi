import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Box, Bug, Cloud, Database, FileText, Globe, HardDrive, KeyRound, Network, Server, Shield, ShieldAlert, ShieldCheck, Timer, TrendingUp } from "lucide-react";
import { blockSummary, type Block, type BlockKind } from "../lib/build";
import type { PodSummary } from "../lib/api";
import type { PodSecurity } from "../lib/podsecurity";

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

/** What the cluster says about a block once it's applied. */
export interface LiveState {
  health: "good" | "warn" | "bad" | "unknown";
  /** Short status, like "2/3 ready" or "Bound · 1Gi". */
  text: string;
  /** Pods of a workload, worst first. */
  pods?: PodSummary[];
  /** The worst pod's reason, like ImagePullBackOff. */
  problem?: string;
}

export interface ScanBadge {
  critical: number;
  high: number;
}

export interface BuildNodeData extends Record<string, unknown> {
  block: Block;
  problem: "error" | "warn" | null;
  security?: PodSecurity;
  scan?: ScanBadge | null;
  /** Opened from the cluster; and whether it's been changed since. */
  inCluster?: boolean;
  changed?: boolean;
  /** Set in live mode. */
  live?: LiveState | null;
  liveMode?: boolean;
}

const handle = "!size-3 !border-2 !border-panel !bg-muted hover:!bg-accent";
const DOT: Record<string, string> = { good: "bg-good", warn: "bg-warn", bad: "bg-bad", unknown: "bg-line" };
const RING: Record<string, string> = { good: "border-good", warn: "border-warn", bad: "border-bad", unknown: "border-line" };
const PODS_SHOWN = 10;

function SecurityIcon({ s }: { s: PodSecurity }) {
  const title = s.level === "restricted" ? "Meets the restricted Pod Security Standard" : `Pod security: ${s.level}. ${s.problems.slice(0, 4).join("; ")}${s.problems.length > 4 ? "…" : ""}`;
  if (s.level === "restricted") return <ShieldCheck className="size-3.5 shrink-0 text-good" aria-label={title}><title>{title}</title></ShieldCheck>;
  if (s.level === "baseline") return <Shield className="size-3.5 shrink-0 text-warn" aria-label={title}><title>{title}</title></Shield>;
  return <ShieldAlert className="size-3.5 shrink-0 text-bad" aria-label={title}><title>{title}</title></ShieldAlert>;
}

/** One block on the Build canvas. Drag from either dot to connect it. */
export const BuildNode = memo(function BuildNode({ data, selected }: NodeProps) {
  const { block, problem, security, scan, inCluster, changed, live, liveMode } = data as BuildNodeData;
  const Icon = BLOCK_ICONS[block.kind];
  const ring = liveMode ? (live ? RING[live.health] : "border-line border-dashed") : problem === "error" ? "border-bad" : problem === "warn" ? "border-warn/70" : "border-line";
  const pods = live?.pods ?? [];
  return (
    <div
      className={`flex min-h-[64px] w-[220px] flex-col justify-center gap-1 rounded-lg border-2 bg-panel px-3 py-2 transition-colors duration-500 ${ring} ${selected ? "shadow-[0_0_0_3px_var(--kupi-accent)]" : ""} ${
        block.kind === "Secret" && !liveMode ? "border-dashed" : ""
      } ${inCluster && !changed && !liveMode ? "opacity-80" : ""}`}
      title={`${block.kind} ${block.name}`}
    >
      <Handle id="l" type="source" position={Position.Left} className={handle} isConnectable={!liveMode} />
      <div className="flex items-center gap-2.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-md bg-panel-2 text-muted">
          <Icon className="size-4" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1">
            {liveMode && <span className={`size-2 shrink-0 rounded-full ${live ? DOT[live.health] : "animate-pulse bg-line"}`} aria-label={live ? `health ${live.health}` : "waiting"} />}
            <span className="truncate text-[13px] font-medium">{block.name || "(no name)"}</span>
            {inCluster && !liveMode && (
              <span title={changed ? "In the cluster, changed here" : "In the cluster"} className="ml-auto flex shrink-0 items-center gap-0.5 text-[10px] text-muted">
                <Cloud className="size-3" aria-hidden />
                {changed && <span className="size-1.5 rounded-full bg-accent" aria-label="changed" />}
              </span>
            )}
          </div>
          <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-muted">
            <span className="shrink-0 rounded bg-panel-2 px-1 font-mono">{block.kind === "PersistentVolumeClaim" ? "PVC" : block.kind === "HorizontalPodAutoscaler" ? "HPA" : block.kind}</span>
            <span className="truncate">{liveMode && live ? live.text : blockSummary(block)}</span>
          </div>
        </div>
      </div>
      {!liveMode && (security || scan) && (
        <div className="flex items-center gap-1.5 pl-[42px] text-[10.5px]">
          {security && <SecurityIcon s={security} />}
          {scan && (
            <span className={`flex items-center gap-0.5 ${scan.critical ? "text-bad" : scan.high ? "text-warn" : "text-good"}`} title={`Image scan: ${scan.critical} critical, ${scan.high} high`}>
              <Bug className="size-3" aria-hidden />
              {scan.critical || scan.high ? `${scan.critical} crit · ${scan.high} high` : "no high or critical"}
            </span>
          )}
        </div>
      )}
      {liveMode && pods.length > 0 && (
        <div className="flex flex-wrap items-center gap-1 pl-[42px]" aria-label={`${pods.length} pods`}>
          {pods.slice(0, PODS_SHOWN).map((p) => (
            <span key={p.uid} className={`size-2.5 rounded-sm transition-colors duration-500 ${DOT[p.health] ?? "bg-line"}`} title={`${p.name}: ${p.status}, ${p.ready} ready`} />
          ))}
          {pods.length > PODS_SHOWN && <span className="text-[10px] text-muted">+{pods.length - PODS_SHOWN}</span>}
        </div>
      )}
      {liveMode && live?.problem && <div className="truncate pl-[42px] text-[10.5px] text-bad">{live.problem}</div>}
      <Handle id="r" type="source" position={Position.Right} className={handle} isConnectable={!liveMode} />
    </div>
  );
});
