export type DiffLineKind = "header" | "hunk" | "add" | "remove" | "context";

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
}

/** Splits a unified diff into typed lines for display. */
export function parseDiff(diff: string): DiffLine[] {
  if (!diff) return [];
  const lines = diff.endsWith("\n") ? diff.slice(0, -1).split("\n") : diff.split("\n");
  return lines.map((text) => {
    if (text.startsWith("+++ ") || text.startsWith("--- ")) return { kind: "header", text };
    if (text.startsWith("@@")) return { kind: "hunk", text };
    if (text.startsWith("+")) return { kind: "add", text };
    if (text.startsWith("-")) return { kind: "remove", text };
    return { kind: "context", text };
  });
}

/** Counts added and removed lines, ignoring the file headers. */
export function diffStats(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const line of lines) {
    if (line.kind === "add") added++;
    if (line.kind === "remove") removed++;
  }
  return { added, removed };
}
