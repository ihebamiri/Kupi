// Typed access to the Go services. Everything the UI does goes through these
// generated bindings and events; there is no other channel to the backend.
export {
  AppService,
  AuditService,
  CanvasService,
  ClusterService,
  ForwardService,
  FormService,
  GitOpsService,
  GuardService,
  HelmService,
  HistoryService,
  ImageService,
  IncidentService,
  LocalClusterService,
  LogService,
  MetricsService,
  ResourceService,
  SecretService,
  SettingsService,
  ShellService,
  TerminalService,
  VaultService,
} from "../../bindings/github.com/ihebamiri/kupi/internal/bridge";

export type {
  EngineInfo,
  ImageDetailView,
  InstallRequest,
  RegistryLogin,
  ImageView,
  LayerView,
  EnvCopy,
  HistoryQuery,
  NodeMetrics,
  OwnerView,
  ReleaseDetail,
  PodMetrics,
  PortOption,
  PullEvent,
  RevealedValue,
  SecretView,
  SecretWriteRequest,
  SettingsView,
  TerminalSession,
  ToolInfo,
  ToolsReport,
  Resources,
  SearchView,
  TagView,
} from "../../bindings/github.com/ihebamiri/kupi/internal/bridge";
export type { DiskUsage } from "../../bindings/github.com/ihebamiri/kupi/internal/engine";
export type { PodSummary, PodsResult } from "../../bindings/github.com/ihebamiri/kupi/internal/graph";
export type { Finding as ScanFinding, Report as ScanReport, SignatureReport } from "../../bindings/github.com/ihebamiri/kupi/internal/imagescan";
export type { Info as ChartInfo, Package as ChartPackage, Version as ChartVersion } from "../../bindings/github.com/ihebamiri/kupi/internal/charts";
export type { Settings, Snippet as SavedSnippet } from "../../bindings/github.com/ihebamiri/kupi/internal/settings";
export type { Chunk as TerminalChunk, Shell } from "../../bindings/github.com/ihebamiri/kupi/internal/terminal";
export type { Entry as TrustEntry } from "../../bindings/github.com/ihebamiri/kupi/internal/trust";
export type { Cluster as LocalCluster, CreateOptions, Tool, Usage as LocalUsage, NodeUsage as LocalNodeUsage } from "../../bindings/github.com/ihebamiri/kupi/internal/localk8s";
export type { Status as LaptopStatus } from "../../bindings/github.com/ihebamiri/kupi/internal/laptop";
export type { NodeUsage, Overview, PodUsage, Sample, Status as MetricsStatus } from "../../bindings/github.com/ihebamiri/kupi/internal/metrics";
export type { Details as SecretDetails, Finding, Key as SecretKey, ScanResult, TLSInfo, Usage as SecretUsage } from "../../bindings/github.com/ihebamiri/kupi/internal/secrets";
export type { Change as HistoryChange, Event as HistoryEvent, Status as HistoryStatus } from "../../bindings/github.com/ihebamiri/kupi/internal/history";
export type { LogExcerpt, Problem, Report as IncidentReport } from "../../bindings/github.com/ihebamiri/kupi/internal/incident";
export type { App as GitOpsApp, Overview as GitOpsOverview, Owner as GitOpsOwner } from "../../bindings/github.com/ihebamiri/kupi/internal/gitops";
export type { Release as HelmRelease, Resource as HelmResource } from "../../bindings/github.com/ihebamiri/kupi/internal/helmrel";
export type { ItemInfo as VaultItem, Status as VaultStatus } from "../../bindings/github.com/ihebamiri/kupi/internal/vault";
export type { Changed as ForwardsChanged, Forward, Target as ForwardTarget } from "../../bindings/github.com/ihebamiri/kupi/internal/forward";

export type {
  AppInfo,
  AuditEntry,
  AuditVerification,
  CommitResult,
  Connection,
  ContainerView,
  ContextInfo,
  EventView,
  LogRequest,
  PlanItemView,
  PlanView,
  ProposalView,
  GitHubStatus,
  GitOpsOwner as GitOpsOwnerInfo,
  ResourceRef,
  ResourceTypeView,
  TableColumn,
  TableRow,
  TableView,
  TypeRef,
} from "../../bindings/github.com/ihebamiri/kupi/internal/bridge";

export type { Changed, LogBatch, LogLine, ShellChunk } from "../../bindings/github.com/ihebamiri/kupi/internal/live";

export { KupiChanged, KupiForwards, KupiLogs, KupiPull, KupiShell, KupiTerminal } from "../../bindings/events";

/** Turns a rejected backend call into a readable sentence. */
export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  if (err && typeof err === "object" && "message" in err) {
    const message = (err as { message: unknown }).message;
    if (typeof message === "string") return message;
  }
  return "Something went wrong.";
}
