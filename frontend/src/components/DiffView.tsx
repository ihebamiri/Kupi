import { useMemo } from "react";
import { diffStats, parseDiff, type DiffLineKind } from "../lib/diff";

const LINE_CLASS: Record<DiffLineKind, string> = {
  header: "text-muted",
  hunk: "text-accent",
  add: "bg-good/10 text-good",
  remove: "bg-bad/10 text-bad",
  context: "text-fg/80",
};

/** Renders a unified diff as plain text lines. Never interprets content as HTML. */
export function DiffView({ diff }: { diff: string }) {
  const lines = useMemo(() => parseDiff(diff), [diff]);
  const stats = useMemo(() => diffStats(lines), [lines]);
  if (lines.length === 0) {
    return <p className="text-[13px] text-muted">No differences.</p>;
  }
  return (
    <div className="overflow-hidden rounded-md border border-line">
      <div className="flex gap-3 border-b border-line bg-panel-2 px-3 py-1.5 text-[12px] text-muted">
        <span className="text-good">+{stats.added}</span>
        <span className="text-bad">−{stats.removed}</span>
        <span>lines</span>
      </div>
      <pre className="max-h-80 overflow-auto bg-bg py-2 font-mono text-[12px] leading-5">
        {lines.map((line, i) => (
          <div key={i} className={`px-3 whitespace-pre ${LINE_CLASS[line.kind]}`}>
            {line.text || " "}
          </div>
        ))}
      </pre>
    </div>
  );
}
