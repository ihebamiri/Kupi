import { useMemo, useState } from "react";
import { Cable, ChevronDown, ChevronRight, FileCode, Gauge, Search, ScrollText, Server } from "lucide-react";
import type { ReactNode } from "react";
import { useClusters } from "../state/clusters";
import { useForwards } from "../state/forwards";
import { useNav, type Route } from "../state/nav";
import type { ResourceTypeView } from "../lib/api";

const CATEGORY_LABEL: Record<string, string> = {
  workloads: "Workloads",
  network: "Network",
  config: "Config",
  storage: "Storage",
  access: "Access control",
  cluster: "Cluster",
  custom: "Custom resources",
};

const DEFAULT_OPEN = new Set(["workloads", "network", "config"]);

function NavButton({ active, onClick, icon, children, title, label }: { active: boolean; onClick: () => void; icon?: ReactNode; children: ReactNode; title?: string; label: string }) {
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={label}
      aria-current={active ? "page" : undefined}
      className={`flex w-full items-center gap-2 rounded-md px-2.5 py-1 text-left text-[13px] ${
        active ? "bg-panel-2 font-medium text-fg" : "text-muted hover:bg-panel-2 hover:text-fg"
      }`}
    >
      {icon}
      <span className="truncate">{children}</span>
    </button>
  );
}

export function Sidebar({ version }: { version: string }) {
  const { route, navigate, types, typesError } = useNav();
  const { active } = useClusters();
  const forwards = useForwards();
  const [filter, setFilter] = useState("");
  const [open, setOpen] = useState<Set<string>>(DEFAULT_OPEN);

  const groups = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const match = (t: ResourceTypeView) =>
      !q ||
      t.label.toLowerCase().includes(q) ||
      t.resource.includes(q) ||
      t.group.includes(q) ||
      (t.shortNames ?? []).some((s) => s === q);
    const map = new Map<string, ResourceTypeView[]>();
    for (const t of types) {
      if (!match(t)) continue;
      const list = map.get(t.category) ?? [];
      list.push(t);
      map.set(t.category, list);
    }
    return [...map.entries()];
  }, [types, filter]);

  const is = (r: Route) => JSON.stringify(r) === JSON.stringify(route);
  const isType = (t: ResourceTypeView) => route.page === "resources" && route.group === t.group && route.resource === t.resource;
  const toggle = (cat: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(cat)) next.delete(cat);
      else next.add(cat);
      return next;
    });

  return (
    <nav aria-label="Main" className="flex w-56 shrink-0 flex-col border-r border-line bg-panel">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-line px-4">
        <span className="grid size-6 place-items-center rounded-md bg-accent text-[12px] font-bold text-accent-fg">K</span>
        <span className="text-[15px] font-semibold tracking-tight">Kupi</span>
        <kbd className="ml-auto rounded border border-line px-1 text-[10px] text-muted" title="Command palette">
          Ctrl K
        </kbd>
      </div>
      <div className="flex flex-col gap-0.5 p-2">
        <NavButton label="Clusters" active={is({ page: "clusters" })} onClick={() => navigate({ page: "clusters" })} icon={<Server className="size-4" aria-hidden />}>
          Clusters
        </NavButton>
        {active && (
          <NavButton label="Overview" active={is({ page: "overview" })} onClick={() => navigate({ page: "overview" })} icon={<Gauge className="size-4" aria-hidden />}>
            Overview
          </NavButton>
        )}
        <NavButton label="Port forwards" active={is({ page: "forwards" })} onClick={() => navigate({ page: "forwards" })} icon={<Cable className="size-4" aria-hidden />}>
          Port forwards
          {forwards.length > 0 && <span className="ml-1 rounded bg-accent/20 px-1 text-[11px] text-accent">{forwards.length}</span>}
        </NavButton>
        <NavButton label="Create or edit" active={route.page === "apply"} onClick={() => navigate({ page: "apply" })} icon={<FileCode className="size-4" aria-hidden />}>
          Create or edit
        </NavButton>
        <NavButton label="Audit log" active={is({ page: "audit" })} onClick={() => navigate({ page: "audit" })} icon={<ScrollText className="size-4" aria-hidden />}>
          Audit log
        </NavButton>
      </div>

      {active && (
        <div className="flex min-h-0 flex-1 flex-col border-t border-line">
          <div className="relative px-2 pt-2">
            <Search className="pointer-events-none absolute top-1/2 left-4 size-3.5 -translate-y-1/4 text-muted" aria-hidden />
            <input
              aria-label="Filter resource types"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter types"
              className="h-7 w-full rounded-md border border-line bg-bg pr-2 pl-7 text-[12px] text-fg placeholder:text-muted focus:border-accent focus:outline-none"
            />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {typesError && <p className="px-2 text-[12px] text-bad">{typesError}</p>}
            {groups.map(([cat, list]) => {
              const expanded = open.has(cat) || filter.trim() !== "";
              return (
                <div key={cat} className="mb-1">
                  <button
                    onClick={() => toggle(cat)}
                    aria-expanded={expanded}
                    className="flex w-full items-center gap-1 px-1.5 py-1 text-[11px] font-semibold tracking-wide text-muted uppercase hover:text-fg"
                  >
                    {expanded ? <ChevronDown className="size-3" aria-hidden /> : <ChevronRight className="size-3" aria-hidden />}
                    {CATEGORY_LABEL[cat] ?? cat}
                    <span className="ml-auto font-normal normal-case">{list.length}</span>
                  </button>
                  {expanded && (
                    <div className="flex flex-col gap-0.5 pl-2">
                      {list.map((t) => (
                        <NavButton
                          key={`${t.group}/${t.resource}`}
                          label={t.group ? `${t.label} (${t.group})` : t.label}
                          active={isType(t)}
                          title={`${t.kind} (${t.group ? `${t.group}/` : ""}${t.version})`}
                          onClick={() => navigate({ page: "resources", group: t.group, resource: t.resource })}
                        >
                          {t.label}
                          {cat === "custom" && <span className="ml-1 text-[11px] text-muted">{t.group}</span>}
                        </NavButton>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}
      {!active && <div className="flex-1" />}
      <div className="border-t border-line px-4 py-2 text-[11px] text-muted">Kupi {version}</div>
    </nav>
  );
}
