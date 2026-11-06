import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronUp, ClipboardCopy, Eraser, ExternalLink, Plus, Search, ShieldAlert, SquareTerminal, TriangleAlert, Wrench, X, Zap } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { TerminalTab, type TabSpec, type TerminalApi } from "../components/TerminalTab";
import { Badge, Button, Notice, inputClass } from "../components/ui";
import { AppService, TerminalService, errorMessage, type TerminalSession, type ToolsReport } from "../lib/api";
import { profileMeta } from "../lib/profiles";
import { BUILTIN_SNIPPETS, checkPaste, groupSnippets, matchesSnippet, type PasteCheck, type Snippet } from "../lib/snippets";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";
import { useSettings } from "../state/settings";

interface Tab extends TabSpec {
  session: TerminalSession | null;
  exited: boolean;
}

let nextKey = 1;

/**
 * The integrated terminal: the user's own shell, with kubectl pointed at
 * the cluster Kupi is connected to, plus paste checks, snippets and search.
 */
export function TerminalPage({ visible }: { visible: boolean }) {
  const { settings, enableTerminal, view } = useSettings();
  const { active, namespace } = useClusters();
  const { navigate } = useNav();
  const [tabs, setTabs] = useState<Tab[]>([]);
  const [current, setCurrent] = useState("");
  const [panel, setPanel] = useState<"snippets" | "tools" | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [notFound, setNotFound] = useState(false);
  const [paste, setPaste] = useState<{ text: string; check: PasteCheck; tab: string } | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const apis = useRef(new Map<string, TerminalApi>());

  const addTab = useCallback(
    (shellId = "") => {
      const key = `t${nextKey++}`;
      setTabs((list) => [...list, { key, shellId, context: settings.terminalFollowCluster ? (active?.name ?? "") : "", namespace: settings.terminalFollowCluster ? namespace : "", session: null, exited: false }]);
      setCurrent(key);
    },
    [active?.name, namespace, settings.terminalFollowCluster],
  );

  // Start with one tab the first time the page shows.
  useEffect(() => {
    if (visible && settings.terminalEnabled && tabs.length === 0) addTab();
  }, [visible, settings.terminalEnabled, tabs.length, addTab]);

  // Switching the terminal off ends every shell.
  useEffect(() => {
    if (!settings.terminalEnabled && view) setTabs([]);
  }, [settings.terminalEnabled, view]);

  const closeTab = (key: string) => {
    setTabs((list) => {
      const i = list.findIndex((t) => t.key === key);
      const next = list.filter((t) => t.key !== key);
      if (key === current) setCurrent(next[Math.max(0, i - 1)]?.key ?? "");
      return next;
    });
    apis.current.delete(key);
  };

  const update = (key: string, patch: Partial<Tab>) => setTabs((list) => list.map((t) => (t.key === key ? { ...t, ...patch } : t)));

  const tab = tabs.find((t) => t.key === current);
  const api = () => apis.current.get(current);
  const production = tab?.session?.profile === "production";

  // Ctrl+Shift+F searches, Ctrl+Shift+T opens a tab, Ctrl+Shift+W closes one.
  useEffect(() => {
    if (!visible) return;
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey && e.shiftKey)) return;
      if (e.code === "KeyF") {
        e.preventDefault();
        setSearchOpen(true);
      } else if (e.code === "KeyT") {
        e.preventDefault();
        addTab();
      } else if (e.code === "KeyW" && current) {
        e.preventDefault();
        closeTab(current);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const find = (dir: "next" | "previous") => {
    const a = api();
    if (!a || !query) return;
    const found = dir === "next" ? a.findNext(query) : a.findPrevious(query);
    setNotFound(!found);
  };

  const onPaste = (key: string, text: string) => {
    const t = tabs.find((x) => x.key === key);
    const check = checkPaste(text, t?.session?.profile === "production");
    if (settings.terminalPasteGuard && check.needsConfirm) setPaste({ text, check, tab: key });
    else apis.current.get(key)?.paste(text);
  };

  if (!settings.terminalEnabled) {
    return (
      <div className="mx-auto mt-10 flex max-w-xl flex-col items-center gap-4 text-center">
        <span className="grid size-14 place-items-center rounded-2xl bg-accent/12 text-accent">
          <SquareTerminal className="size-7" />
        </span>
        <h1 className="text-lg font-semibold">Terminal</h1>
        <p className="text-[13px] text-muted">
          Run kubectl, helm, docker or anything else in your own shell, inside Kupi. Each tab points kubectl at the cluster you're connected to without changing your kubeconfig, checks
          risky pastes, and keeps snippets and tool versions at hand.
        </p>
        <Notice tone="warn" icon={<ShieldAlert className="size-4 text-warn" />}>
          Commands in the terminal run directly on your computer and clusters. They don't go through Kupi's change preview, confirmation or audit log. The terminal is off until you turn it on; a system
          dialog asks to confirm.
        </Notice>
        {error && <Notice tone="bad">{error}</Notice>}
        <Button
          variant="primary"
          busy={asking}
          icon={<Zap className="size-4" />}
          onClick={async () => {
            setAsking(true);
            setError(null);
            try {
              if (!(await enableTerminal())) setError("The terminal stays off: the confirmation was cancelled.");
            } catch (err) {
              setError(errorMessage(err));
            } finally {
              setAsking(false);
            }
          }}
        >
          Turn on the terminal…
        </Button>
        <button className="text-[12px] text-muted underline hover:text-fg" onClick={() => navigate({ page: "settings" })}>
          Terminal settings
        </button>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-xl border border-line bg-[#0a0f1e] shadow-2xl shadow-black/20">
      {/* Tabs and tools */}
      <div className="flex items-center gap-1 border-b border-white/8 bg-[#0d1428] px-2 py-1.5">
        <div role="tablist" aria-label="Terminals" className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto">
          {tabs.map((t) => {
            const selected = t.key === current;
            const meta = t.session?.profile ? profileMeta(t.session.profile) : null;
            const label = t.session ? t.session.shell : "Starting…";
            const where = t.session?.context ? `${t.session.context}${t.session.namespace ? `/${t.session.namespace}` : ""}` : "";
            return (
              <div
                key={t.key}
                role="tab"
                aria-selected={selected}
                tabIndex={0}
                onClick={() => setCurrent(t.key)}
                onKeyDown={(e) => e.key === "Enter" && setCurrent(t.key)}
                onAuxClick={(e) => e.button === 1 && closeTab(t.key)}
                className={`group flex max-w-72 shrink-0 cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1 text-[12px] ${
                  selected ? "bg-white/10 text-white" : "text-white/60 hover:bg-white/5 hover:text-white/90"
                }`}
                title={where ? `${label} · kubectl uses ${where}` : label}
              >
                <span className={`size-1.5 shrink-0 rounded-full ${t.exited ? "bg-white/30" : meta ? meta.dot : "bg-cyan-400"}`} aria-hidden />
                <span className="truncate">{label}</span>
                {where && <span className="truncate font-mono text-[11px] text-white/45">{where}</span>}
                <button
                  aria-label={`Close ${label}`}
                  className="ml-0.5 rounded p-0.5 text-white/40 opacity-0 group-hover:opacity-100 hover:bg-white/10 hover:text-white focus:opacity-100"
                  onClick={(e) => {
                    e.stopPropagation();
                    closeTab(t.key);
                  }}
                >
                  <X className="size-3" />
                </button>
              </div>
            );
          })}
          <NewTabButton shells={view?.shells ?? []} onNew={addTab} />
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <ToolbarButton label="Find (Ctrl+Shift+F)" active={searchOpen} onClick={() => setSearchOpen((o) => !o)} icon={<Search className="size-4" />} />
          <ToolbarButton label="Snippets" active={panel === "snippets"} onClick={() => setPanel((p) => (p === "snippets" ? null : "snippets"))} icon={<Zap className="size-4" />} />
          <ToolbarButton label="Tools and versions" active={panel === "tools"} onClick={() => setPanel((p) => (p === "tools" ? null : "tools"))} icon={<Wrench className="size-4" />} />
          <ToolbarButton
            label="Copy all output"
            onClick={() => {
              const text = api()?.text() ?? "";
              void navigator.clipboard.writeText(text).catch(() => {});
            }}
            icon={<ClipboardCopy className="size-4" />}
          />
          <ToolbarButton label="Clear" onClick={() => api()?.clear()} icon={<Eraser className="size-4" />} />
        </div>
      </div>

      {/* Which cluster this tab's kubectl talks to */}
      {tab?.session && (
        <div className={`flex items-center gap-2 px-3 py-1 text-[11.5px] ${production ? "bg-red-500/15 text-red-200" : "bg-white/[0.03] text-white/55"}`}>
          {production && <TriangleAlert className="size-3.5 text-red-300" aria-hidden />}
          {tab.session.followsCluster ? (
            <span>
              kubectl and helm here use <span className="font-mono text-white/85">{tab.session.context}</span>
              {tab.session.namespace && (
                <>
                  {" "}
                  · namespace <span className="font-mono text-white/85">{tab.session.namespace}</span>
                </>
              )}
              {tab.session.profile && <Badge className={`ml-2 ${profileMeta(tab.session.profile).badge}`}>{profileMeta(tab.session.profile).label}</Badge>}
              {production && <span className="ml-2">Commands here skip Kupi's preview and audit log.</span>}
            </span>
          ) : (
            <span>kubectl uses your kubeconfig's current context.</span>
          )}
        </div>
      )}

      {searchOpen && (
        <div className="flex items-center gap-1.5 border-b border-white/8 bg-[#0d1428] px-3 py-1.5">
          <Search className="size-3.5 text-white/50" aria-hidden />
          <input
            autoFocus
            aria-label="Find in the terminal"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setNotFound(false);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") find(e.shiftKey ? "previous" : "next");
              if (e.key === "Escape") {
                setSearchOpen(false);
                api()?.clearSearch();
                api()?.focus();
              }
            }}
            placeholder="Find in output"
            className={`${inputClass} h-7 w-64 border-white/15 bg-black/30 text-white placeholder:text-white/40 ${notFound ? "border-red-400" : ""}`}
          />
          <ToolbarButton label="Previous match (Shift+Enter)" onClick={() => find("previous")} icon={<ChevronUp className="size-4" />} />
          <ToolbarButton label="Next match (Enter)" onClick={() => find("next")} icon={<ChevronDown className="size-4" />} />
          {notFound && <span className="text-[12px] text-red-300">No match</span>}
          <ToolbarButton
            label="Close search"
            onClick={() => {
              setSearchOpen(false);
              api()?.clearSearch();
            }}
            icon={<X className="size-4" />}
          />
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="relative min-h-0 min-w-0 flex-1 p-2">
          {tabs.length === 0 && (
            <div className="grid h-full place-items-center text-[13px] text-white/50">
              <Button onClick={() => addTab()} icon={<Plus className="size-4" />}>
                New terminal
              </Button>
            </div>
          )}
          {tabs.map((t) => (
            <TerminalTab
              key={t.key}
              spec={t}
              visible={visible && t.key === current}
              settings={settings}
              apiRef={(a: TerminalApi | null) => {
                if (a) apis.current.set(t.key, a);
              }}
              onSession={(info) => update(t.key, { session: info ?? t.session, exited: !info })}
              onExit={() => update(t.key, { exited: true })}
              onPaste={(text) => onPaste(t.key, text)}
              onLink={setLink}
              onError={(msg) => setError(msg)}
            />
          ))}
        </div>
        {panel === "snippets" && <SnippetsPanel saved={settings.snippets ?? []} onPick={(s) => api()?.insert(s.command)} onClose={() => setPanel(null)} onManage={() => navigate({ page: "settings" })} />}
        {panel === "tools" && <ToolsPanel contextName={tab?.session?.context ?? active?.name ?? ""} onClose={() => setPanel(null)} />}
      </div>

      {error && (
        <div className="border-t border-white/8 px-3 py-1.5 text-[12px] text-red-300">
          {error}{" "}
          <button className="underline" onClick={() => setError(null)}>
            Dismiss
          </button>
        </div>
      )}

      <Dialog
        open={!!paste}
        onOpenChange={(o) => !o && setPaste(null)}
        title={paste?.check.risky.length ? "Paste destructive commands?" : "Paste and run?"}
        description={
          paste?.check.runsImmediately
            ? `This paste has ${paste.check.lines.length} ${paste.check.lines.length === 1 ? "line" : "lines"} and a line break, so the shell may run it as soon as it's pasted.`
            : "Check the command before it reaches the shell."
        }
        footer={
          <>
            <Button variant="ghost" onClick={() => setPaste(null)}>
              Cancel
            </Button>
            {paste?.check.runsImmediately && (
              <Button
                onClick={() => {
                  if (paste) apis.current.get(paste.tab)?.paste(paste.check.lines.join(" ; ").replace(/\s+$/, ""));
                  setPaste(null);
                }}
                title="Joins the lines with ; and leaves them at the prompt for you to run"
              >
                Paste as one line, don't run
              </Button>
            )}
            <Button
              variant={paste?.check.risky.length ? "danger" : "primary"}
              onClick={() => {
                if (paste) apis.current.get(paste.tab)?.paste(paste.text);
                setPaste(null);
              }}
            >
              Paste
            </Button>
          </>
        }
      >
        {paste && (
          <div className="flex flex-col gap-2">
            {paste.check.risky.length > 0 && (
              <Notice tone="bad" icon={<ShieldAlert className="size-4 text-bad" />}>
                {tab?.session?.context ?? "This cluster"} is a production cluster, and this deletes, evicts or rolls back. It will run without Kupi's preview or audit log.
              </Notice>
            )}
            <pre className="max-h-64 overflow-auto rounded-md border border-line bg-bg p-2 font-mono text-[12px] leading-5 whitespace-pre-wrap">
              {paste.check.lines.map((l, i) => (
                <div key={i} className={paste.check.risky.includes(l) ? "text-bad" : ""}>
                  {l}
                </div>
              ))}
            </pre>
          </div>
        )}
      </Dialog>

      <Dialog
        open={!!link}
        onOpenChange={(o) => !o && setLink(null)}
        title="Open this link?"
        description="It opens in your web browser."
        footer={
          <>
            <Button variant="ghost" onClick={() => setLink(null)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              icon={<ExternalLink className="size-4" />}
              onClick={() => {
                if (link) AppService.OpenURL(link).catch((err) => setError(errorMessage(err)));
                setLink(null);
              }}
            >
              Open
            </Button>
          </>
        }
      >
        <code className="block rounded-md border border-line bg-bg p-2 text-[12px] break-all">{link}</code>
      </Dialog>
    </div>
  );
}

function ToolbarButton({ label, icon, onClick, active }: { label: string; icon: React.ReactNode; onClick: () => void; active?: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-pressed={active}
      title={label}
      onClick={onClick}
      className={`grid size-7 place-items-center rounded-md ${active ? "bg-white/12 text-white" : "text-white/55 hover:bg-white/8 hover:text-white"}`}
    >
      {icon}
    </button>
  );
}

function NewTabButton({ shells, onNew }: { shells: { id: string; name: string }[]; onNew: (shellId?: string) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative flex shrink-0 items-center">
      <button aria-label="New terminal (Ctrl+Shift+T)" title="New terminal (Ctrl+Shift+T)" onClick={() => onNew()} className="grid size-7 place-items-center rounded-md text-white/60 hover:bg-white/8 hover:text-white">
        <Plus className="size-4" />
      </button>
      {shells.length > 1 && (
        <button aria-label="New terminal with another shell" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="grid h-7 w-4 place-items-center rounded-md text-white/50 hover:bg-white/8 hover:text-white">
          <ChevronDown className="size-3" />
        </button>
      )}
      {open && (
        <ul role="menu" className="absolute top-8 left-0 z-30 min-w-48 rounded-md border border-line bg-panel py-1 shadow-xl" onMouseLeave={() => setOpen(false)}>
          {shells.map((s) => (
            <li key={s.id}>
              <button
                role="menuitem"
                className="w-full px-3 py-1.5 text-left text-[12px] hover:bg-panel-2"
                onClick={() => {
                  setOpen(false);
                  onNew(s.id);
                }}
              >
                {s.name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function SnippetsPanel({ saved, onPick, onClose, onManage }: { saved: { name: string; command: string }[]; onPick: (s: Snippet) => void; onClose: () => void; onManage: () => void }) {
  const [q, setQ] = useState("");
  const all = useMemo(() => [...saved.map((s) => ({ ...s, group: "My snippets" })), ...BUILTIN_SNIPPETS], [saved]);
  const groups = groupSnippets(all.filter((s) => matchesSnippet(s, q)));
  return (
    <aside className="flex w-80 shrink-0 flex-col border-l border-white/8 bg-[#0d1428] text-white/85">
      <div className="flex items-center gap-2 px-3 pt-2.5 pb-2">
        <h2 className="text-[13px] font-semibold text-white">Snippets</h2>
        <span className="text-[11px] text-white/45">typed at the prompt, you press Enter</span>
        <button aria-label="Close snippets" className="ml-auto rounded p-1 text-white/50 hover:bg-white/8 hover:text-white" onClick={onClose}>
          <X className="size-3.5" />
        </button>
      </div>
      <div className="px-3 pb-2">
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search snippets" aria-label="Search snippets" className={`${inputClass} h-7 w-full border-white/15 bg-black/30 text-white placeholder:text-white/40`} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {groups.map(([group, list]) => (
          <div key={group} className="mb-2">
            <div className="px-1.5 py-1 text-[10.5px] font-semibold tracking-wider text-white/40 uppercase">{group}</div>
            {list.map((s, i) => (
              <button key={`${group}-${i}`} onClick={() => onPick(s)} className="block w-full rounded-md px-2 py-1.5 text-left hover:bg-white/8" title={s.command}>
                <div className="text-[12.5px] text-white/90">{s.name}</div>
                <code className="block truncate text-[11px] text-cyan-300/80">{s.command}</code>
              </button>
            ))}
          </div>
        ))}
        {groups.length === 0 && <p className="px-2 text-[12px] text-white/45">No snippet matches.</p>}
      </div>
      <button className="border-t border-white/8 px-3 py-2 text-left text-[12px] text-white/55 hover:text-white" onClick={onManage}>
        Add your own in Settings →
      </button>
    </aside>
  );
}

function ToolsPanel({ contextName, onClose }: { contextName: string; onClose: () => void }) {
  const [report, setReport] = useState<ToolsReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    TerminalService.Tools(contextName)
      .then((r) => !cancelled && setReport(r))
      .catch((err) => !cancelled && setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [contextName]);
  return (
    <aside className="flex w-80 shrink-0 flex-col border-l border-white/8 bg-[#0d1428] text-white/85">
      <div className="flex items-center gap-2 px-3 pt-2.5 pb-2">
        <h2 className="text-[13px] font-semibold text-white">Tools on this computer</h2>
        <button aria-label="Close tools" className="ml-auto rounded p-1 text-white/50 hover:bg-white/8 hover:text-white" onClick={onClose}>
          <X className="size-3.5" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-3 text-[12px]">
        {error && <p className="text-red-300">{error}</p>}
        {!report && !error && <p className="text-white/45">Checking…</p>}
        {report?.serverVersion && (
          <p className="mb-2 text-white/55">
            Cluster version <span className="font-mono text-white/85">{report.serverVersion}</span>
          </p>
        )}
        {report?.skew && <p className="mb-2 rounded-md border border-amber-400/30 bg-amber-400/10 p-2 text-amber-200">{report.skew}</p>}
        <ul className="flex flex-col gap-1">
          {(report?.tools ?? []).map((t) => (
            <li key={t.name} className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-white/5" title={t.path}>
              <span className={`size-1.5 rounded-full ${t.found ? "bg-emerald-400" : "bg-white/20"}`} aria-hidden />
              <span className="w-20 font-medium text-white/90">{t.name}</span>
              <span className="truncate font-mono text-[11px] text-white/55">{t.found ? t.version || "installed" : "not installed"}</span>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  );
}
