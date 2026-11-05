/** Short age like kubectl: 45s, 12m, 5h, 3d, 2y. */
export function age(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  const d = Math.floor(h / 24);
  if (d < 730) return `${d}d`;
  return `${Math.floor(d / 365)}y`;
}

export type Tone = "good" | "warn" | "bad" | "muted";

const GOOD = new Set(["running", "active", "bound", "ready", "true", "available", "succeeded", "normal", "complete", "established"]);
const WARN = new Set([
  "pending", "containercreating", "podinitializing", "terminating", "unknown", "notready", "progressing",
  "init:0/1", "schedulingdisabled", "released",
]);
const BAD = new Set([
  "crashloopbackoff", "error", "failed", "imagepullbackoff", "errimagepull", "oomkilled", "evicted",
  "createcontainerconfigerror", "invalidimagename", "lost", "warning", "false", "backoff", "containercannotrun",
]);
const MUTED = new Set(["completed", "terminated"]);

/** Colour hint for a status-like cell, or null for ordinary text. */
export function statusTone(text: string): Tone | null {
  const key = text.trim().toLowerCase();
  if (!key) return null;
  if (BAD.has(key) || key.startsWith("init:error") || key.startsWith("init:crashloop")) return "bad";
  if (WARN.has(key) || key.startsWith("init:")) return "warn";
  if (GOOD.has(key)) return "good";
  if (MUTED.has(key)) return "muted";
  return null;
}

/** Compares table cells: numerically when both look like numbers, else as text. */
export function compareCells(a: string, b: string): number {
  const na = Number(a);
  const nb = Number(b);
  if (a !== "" && b !== "" && !Number.isNaN(na) && !Number.isNaN(nb)) return na - nb;
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
}
