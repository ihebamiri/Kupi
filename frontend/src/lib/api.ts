// Typed access to the Go services. Everything the UI does goes through these
// generated bindings; there is no other channel to the backend.
export {
  AppService,
  AuditService,
  ClusterService,
  GuardService,
} from "../../bindings/github.com/iheb-amiri/kupi/internal/bridge";

export type {
  AppInfo,
  AuditEntry,
  AuditVerification,
  CommitResult,
  Connection,
  ContextInfo,
  PlanView,
  ResourceRef,
} from "../../bindings/github.com/iheb-amiri/kupi/internal/bridge";

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
