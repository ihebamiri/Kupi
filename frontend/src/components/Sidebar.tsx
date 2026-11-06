import { useMemo, useState } from "react";
import { Cable, ChevronDown, ChevronRight, FileCode, Gauge, GitBranch, History, KeyRound, Laptop, Layers, Package, Search, ScrollText, Server, Settings as SettingsIcon, Siren, SquareTerminal, Vault, Workflow } from "lucide-react";
import type { ReactNode } from "react";
import { KupiLogo } from "./Logo";
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
const TERMINAL_KEY = "Ctrl `";
const icon = "size-4 shrink-0";

function FooterButton({ label, active, onClick, icon }: { label: string; active: boolean; onClick: () => void; icon: ReactNode }) {
  return (
    <button
      aria-label={label}
      title={label}
      aria-current={active ? "page" : undefined}
      onClick={onClick}
      className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-[12px] ${active ? "bg-accent/12 text-fg" : "text-muted hover:bg-panel-2 hover:text-fg"}`}
    >
      {icon} {label}
    </button>
  );
}

function NavButton({ active, onClick, icon, children, title, label }: { active: boolean; onClick: () => void; icon?: ReactNode; children: ReactNode; title?: string; label: string }) {
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={label}
      aria-current={active ? "page" : undefined}
      className={`relative flex w-full items-center gap-2 rounded-md px-2.5 py-[5px] text-left text-[13px] transition-colors ${
        active ? "bg-accent/12 font-medium text-fg before:absolute before:inset-y-1.5 before:left-0 before:w-[3px] before:rounded-full before:bg-accent" : "text-muted hover:bg-panel-2 hover:text-fg"
      }`}
    >
      {icon}
      <span className="flex min-w-0 flex-1 items-center truncate">{children}</span>
    </button>
  );
}

function SectionLabel({ children }: { children: ReactNode }) {
  return <div className="px-2.5 pt-3 pb-1 text-[10.5px] font-semibold tracking-[0.08em] text-muted/80 uppercase">{children}</div>;
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
    <nav aria-label="Main" className="flex w-60 shrink-0 flex-col border-r border-line bg-panel">
      <div className="flex h-12 shrink-0 items-center gap-2.5 border-b border-line px-4">
        <KupiLogo className="size-7" />
        <span className="text-[14px] font-semibold tracking-[0.2em]">KUPI</span>
        <kbd className="ml-auto rounded border border-line bg-bg px-1.5 py-0.5 font-sans text-[10px] text-muted" title="Command palette">
          Ctrl K
        </kbd>
      </div>
      <div className="flex shrink-0 flex-col gap-0.5 px-2 pb-2">
        <SectionLabel>Clusters</SectionLabel>
        <NavButton label="Clusters" active={is({ page: "clusters" })} onClick={() => navigate({ page: "clusters" })} icon={<Server className={icon} aria-hidden />}>
          Clusters
        </NavButton>
        {active && (
          <>
            <NavButton label="Overview" active={is({ page: "overview" })} onClick={() => navigate({ page: "overview" })} icon={<Gauge className={icon} aria-hidden />}>
              Overview
            </NavButton>
            <NavButton label="Map" active={is({ page: "canvas" })} onClick={() => navigate({ page: "canvas" })} icon={<Workflow className={icon} aria-hidden />}>
              Map
            </NavButton>
            <NavButton label="Incident" active={is({ page: "incident" })} onClick={() => navigate({ page: "incident" })} icon={<Siren className={icon} aria-hidden />}>
              Incident
            </NavButton>
            <NavButton label="History" active={is({ page: "history" })} onClick={() => navigate({ page: "history" })} icon={<History className={icon} aria-hidden />}>
              History
            </NavButton>
            <NavButton label="Helm" active={is({ page: "helm" })} onClick={() => navigate({ page: "helm" })} icon={<Package className={icon} aria-hidden />}>
              Helm
            </NavButton>
            <NavButton label="GitOps" active={is({ page: "gitops" })} onClick={() => navigate({ page: "gitops" })} icon={<GitBranch className={icon} aria-hidden />}>
              GitOps
            </NavButton>
            <NavButton label="Secrets" active={route.page === "secrets"} onClick={() => navigate({ page: "secrets" })} icon={<KeyRound className={icon} aria-hidden />}>
              Secrets
            </NavButton>
          </>
        )}
        <SectionLabel>Workspace</SectionLabel>
        <NavButton label="Create or edit" active={route.page === "apply"} onClick={() => navigate({ page: "apply" })} icon={<FileCode className={icon} aria-hidden />}>
          Create or edit
        </NavButton>
        <NavButton label="Terminal" active={is({ page: "terminal" })} onClick={() => navigate({ page: "terminal" })} icon={<SquareTerminal className={icon} aria-hidden />}>
          Terminal
          <kbd className="ml-auto font-sans text-[10px] text-muted/70">{TERMINAL_KEY}</kbd>
        </NavButton>
        <NavButton label="Port forwards" active={is({ page: "forwards" })} onClick={() => navigate({ page: "forwards" })} icon={<Cable className={icon} aria-hidden />}>
          Port forwards
          {forwards.length > 0 && <span className="ml-auto rounded bg-accent/20 px-1.5 text-[11px] text-accent">{forwards.length}</span>}
        </NavButton>
        <NavButton label="Images" active={is({ page: "images" })} onClick={() => navigate({ page: "images" })} icon={<Layers className={icon} aria-hidden />}>
          Images
        </NavButton>
        <NavButton label="Local clusters" active={is({ page: "local" })} onClick={() => navigate({ page: "local" })} icon={<Laptop className={icon} aria-hidden />}>
          Local clusters
        </NavButton>
        <NavButton label="Vault" active={is({ page: "vault" })} onClick={() => navigate({ page: "vault" })} icon={<Vault className={icon} aria-hidden />}>
          Vault
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
      <div className="flex items-center gap-1 border-t border-line px-2 py-1.5">
        <FooterButton label="Audit log" active={is({ page: "audit" })} onClick={() => navigate({ page: "audit" })} icon={<ScrollText className="size-3.5" aria-hidden />} />
        <FooterButton label="Settings" active={is({ page: "settings" })} onClick={() => navigate({ page: "settings" })} icon={<SettingsIcon className="size-3.5" aria-hidden />} />
        <span className="ml-auto pr-1 text-[10.5px] text-muted/70">{version ? `v${version.replace(/^v/, "")}` : ""}</span>
      </div>
    </nav>
  );
}
