import type { PullEvent } from "./api";

/**
 * Adds a pull event to the ones on screen. A new pull of an image replaces
 * what earlier pulls of the same image said, so an old failure doesn't stay
 * next to a later success.
 */
export function mergePull(cur: Record<string, PullEvent>, p: PullEvent): Record<string, PullEvent> {
  const next: Record<string, PullEvent> = {};
  for (const [id, q] of Object.entries(cur)) {
    if (id === p.id || q.ref !== p.ref) next[id] = q;
  }
  next[p.id] = p;
  return next;
}
