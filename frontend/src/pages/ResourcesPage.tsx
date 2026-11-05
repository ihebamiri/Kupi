import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RefreshCw, Search } from "lucide-react";
import { DetailPanel } from "../components/DetailPanel";
import { KubectlButton } from "../components/CopyButton";
import { ResourceTable, cellText, visibleColumns } from "../components/ResourceTable";
import { Button, Notice } from "../components/ui";
import { ResourceService, errorMessage, type TableRow, type TableView, type TypeRef } from "../lib/api";
import { onChanged } from "../lib/events";
import { compareCells } from "../lib/format";
import { kubectlGet } from "../lib/kubectl";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";

export function ResourcesPage({ group, resource }: { group: string; resource: string }) {
  const { active, namespace } = useClusters();
  const { findType, types } = useNav();
  const type = findType(group, resource);
  const [table, setTable] = useState<TableView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [wide, setWide] = useState(false);
  const [warningsOnly, setWarningsOnly] = useState(false);
  const [selectedUid, setSelectedUid] = useState<string | null>(null);
  const [sort, setSort] = useState<{ key: string; dir: 1 | -1 } | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const contextName = active?.name ?? "";
  const ns = type?.namespaced ? namespace : "";
  const typeRef: TypeRef | null = useMemo(
    () => (type ? { group: type.group, version: type.version, resource: type.resource, namespaced: type.namespaced } : null),
    [type],
  );
  const loadSeq = useRef(0);

  const load = useCallback(async () => {
    if (!typeRef || !contextName) return null;
    const seq = ++loadSeq.current;
    try {
      const t = await ResourceService.List(contextName, typeRef, ns, "");
      if (seq === loadSeq.current) {
        setTable(t);
        setError(null);
      }
      return t;
    } catch (err) {
      if (seq === loadSeq.current) setError(errorMessage(err));
      return null;
    }
  }, [contextName, typeRef, ns]);

  // Load, then watch for changes and reload when the cluster changes.
  useEffect(() => {
    setTable(null);
    setSelectedUid(null);
    setSort(null);
    setError(null);
    if (!typeRef || !contextName) return;
    let watchId = "";
    let off = () => {};
    let cancelled = false;
    void (async () => {
      const first = await load();
      if (cancelled || !first) return;
      try {
        const id = await ResourceService.Watch(contextName, typeRef, ns, first.resourceVersion);
        if (cancelled) {
          void ResourceService.Unwatch(id);
          return;
        }
        watchId = id;
        off = onChanged(id, () => void load());
      } catch {
        // Without a watch the list still works; it just won't update live.
      }
    })();
    return () => {
      cancelled = true;
      off();
      if (watchId) void ResourceService.Unwatch(watchId);
    };
  }, [contextName, typeRef, ns, load]);

  const isEvents = type?.resource === "events" && type.group === "";
  const columns = useMemo(
    () => (table ? visibleColumns(table.columns ?? [], wide, !!type?.namespaced && ns === "") : []),
    [table, wide, type, ns],
  );
  const rows = useMemo(() => {
    let list = table?.rows ?? [];
    const q = filter.trim().toLowerCase();
    if (q) {
      list = list.filter(
        (r) =>
          r.name.toLowerCase().includes(q) ||
          r.namespace.toLowerCase().includes(q) ||
          (r.cells ?? []).some((c) => c.toLowerCase().includes(q)) ||
          Object.entries(r.labels ?? {}).some(([k, v]) => `${k}=${v}`.toLowerCase().includes(q)),
      );
    }
    if (isEvents && warningsOnly) list = list.filter((r) => (r.cells ?? []).includes("Warning"));
    if (sort) {
      const col = columns.find((c) => c.key === sort.key);
      if (col) list = [...list].sort((a, b) => sort.dir * compareCells(cellText(a, col), cellText(b, col)));
    }
    return list;
  }, [table, filter, isEvents, warningsOnly, sort, columns]);

  if (!active) return null;
  if (!type) {
    return types.length === 0 ? <p className="text-[13px] text-muted">Loading resource types…</p> : <Notice tone="bad">This resource type isn't available on {active.name}.</Notice>;
  }

  const selected = rows.find((r) => r.uid === selectedUid) ?? table?.rows?.find((r) => r.uid === selectedUid) ?? null;
  const command = kubectlGet({ context: active.name, group: type.group, resource: type.resource, namespace: ns || undefined }, type.namespaced && ns === "");

  return (
    <div className="flex h-full min-h-0 gap-4">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">{type.label}</h1>
          <span className="text-[12px] text-muted">
            {type.group ? `${type.group}/` : ""}
            {type.version} · {type.namespaced ? (ns ? `namespace ${ns}` : "all namespaces") : "cluster-wide"} ·{" "}
            {table ? `${rows.length}${rows.length !== (table.rows ?? []).length ? ` of ${(table.rows ?? []).length}` : ""}` : "…"}
          </span>
          <div className="ml-auto flex items-center gap-1">
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
              <input
                aria-label="Filter rows"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter by name, label, status"
                className="h-8 w-60 rounded-md border border-line bg-bg pr-2 pl-7 text-[13px] text-fg placeholder:text-muted focus:border-accent focus:outline-none"
              />
            </div>
            {isEvents && (
              <Button variant={warningsOnly ? "primary" : "secondary"} onClick={() => setWarningsOnly((w) => !w)}>
                Warnings only
              </Button>
            )}
            <Button variant="ghost" onClick={() => setWide((w) => !w)}>
              {wide ? "Fewer columns" : "All columns"}
            </Button>
            <Button
              variant="ghost"
              busy={refreshing}
              icon={<RefreshCw className="size-4" />}
              aria-label="Reload"
              onClick={() => {
                setRefreshing(true);
                void load().finally(() => setRefreshing(false));
              }}
            />
            <KubectlButton command={command} label="kubectl" />
          </div>
        </div>
        {error && <Notice tone="bad">{error}</Notice>}
        {table?.truncated && <Notice tone="warn">Showing the first {(table.rows ?? []).length} objects. Pick a namespace or filter to narrow the list.</Notice>}
        {table && (table.rows ?? []).length === 0 && !error && (
          <p className="mt-2 text-[13px] text-muted">No {type.label.toLowerCase()} {ns ? `in ${ns}` : ""}.</p>
        )}
        {table && (table.rows ?? []).length > 0 && (
          <ResourceTable
            columns={columns}
            rows={rows}
            selectedUid={selectedUid}
            onSelect={(r: TableRow) => setSelectedUid(r.uid)}
            sort={sort}
            onSort={(key) => setSort((s) => (s?.key === key ? (s.dir === 1 ? { key, dir: -1 } : null) : { key, dir: 1 }))}
          />
        )}
      </div>
      {selected && (
        <DetailPanel key={selected.uid} contextName={active.name} type={type} row={selected} onClose={() => setSelectedUid(null)} />
      )}
    </div>
  );
}
