import { useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp } from "lucide-react";
import type { TableColumn, TableRow } from "../lib/api";
import { age, statusTone, type Tone } from "../lib/format";

export interface Column {
  key: string;
  label: string;
  /** Index into row.cells, or -1 for the namespace column Kupi adds. */
  cell: number;
  type: string;
  width: string;
}

const TONE_CLASS: Record<Tone, string> = {
  good: "text-good",
  warn: "text-warn",
  bad: "text-bad",
  muted: "text-muted",
};

/** Picks the visible columns from the server's column definitions. */
export function visibleColumns(defs: TableColumn[], wide: boolean, showNamespace: boolean): Column[] {
  const cols: Column[] = [];
  defs.forEach((d, i) => {
    if (!wide && d.priority > 0) return;
    const lower = d.name.toLowerCase();
    const width = lower === "name" ? "minmax(180px,2.2fr)" : lower === "message" ? "minmax(240px,4fr)" : "minmax(80px,1fr)";
    cols.push({ key: `c${i}`, label: d.name, cell: i, type: d.type, width });
    if (showNamespace && lower === "name") {
      cols.push({ key: "namespace", label: "Namespace", cell: -1, type: "string", width: "minmax(110px,1fr)" });
    }
  });
  if (showNamespace && !cols.some((c) => c.cell === -1)) {
    cols.unshift({ key: "namespace", label: "Namespace", cell: -1, type: "string", width: "minmax(110px,1fr)" });
  }
  return cols;
}

export function cellText(row: TableRow, col: Column): string {
  if (col.cell === -1) return row.namespace;
  const raw = row.cells?.[col.cell] ?? "";
  return col.type === "date" && raw ? age(raw) : raw;
}

interface ResourceTableProps {
  columns: Column[];
  rows: TableRow[];
  selectedUid: string | null;
  onSelect: (row: TableRow) => void;
  sort: { key: string; dir: 1 | -1 } | null;
  onSort: (key: string) => void;
}

/** Virtualized table: renders only visible rows, so 10,000 objects scroll smoothly. */
export function ResourceTable({ columns, rows, selectedUid, onSelect, sort, onSort }: ResourceTableProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const template = useMemo(() => columns.map((c) => c.width).join(" "), [columns]);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 32,
    overscan: 12,
  });

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line" role="table" aria-rowcount={rows.length}>
      <div className="grid shrink-0 border-b border-line bg-panel-2 text-[12px] text-muted" style={{ gridTemplateColumns: template }} role="row">
        {columns.map((c) => (
          <button
            key={c.key}
            role="columnheader"
            aria-sort={sort?.key === c.key ? (sort.dir === 1 ? "ascending" : "descending") : "none"}
            onClick={() => onSort(c.key)}
            className="flex items-center gap-1 truncate px-3 py-2 text-left font-medium hover:text-fg"
          >
            {c.label}
            {sort?.key === c.key && (sort.dir === 1 ? <ArrowUp className="size-3" /> : <ArrowDown className="size-3" />)}
          </button>
        ))}
      </div>
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((item) => {
            const row = rows[item.index];
            const selected = row.uid !== "" && row.uid === selectedUid;
            return (
              <div
                key={row.uid || `${row.namespace}/${row.name}/${item.index}`}
                role="row"
                aria-selected={selected}
                tabIndex={0}
                onClick={() => onSelect(row)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelect(row);
                  }
                }}
                className={`absolute left-0 grid w-full cursor-pointer items-center border-b border-line/60 text-[13px] ${
                  selected ? "bg-accent/15" : "hover:bg-panel-2"
                }`}
                style={{ gridTemplateColumns: template, height: item.size, transform: `translateY(${item.start}px)` }}
              >
                {columns.map((c, i) => {
                  const text = cellText(row, c);
                  const tone = i > 0 ? statusTone(text) : null;
                  return (
                    <div
                      key={c.key}
                      role="cell"
                      title={text}
                      className={`truncate px-3 ${i === 0 ? "font-medium" : ""} ${tone ? TONE_CLASS[tone] : ""}`}
                    >
                      {text}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
