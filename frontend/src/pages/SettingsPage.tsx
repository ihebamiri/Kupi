import { useState, type ReactNode } from "react";
import { FileKey2, Info, Map as MapIcon, Palette, Pencil, Plus, RotateCcw, ScrollText, ShieldCheck, SquareTerminal, Trash2 } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { KupiLogo } from "../components/Logo";
import { Badge, Button, Field, Notice, PageHeader, Segmented, Switch, inputClass } from "../components/ui";
import { ClusterService, errorMessage, type SavedSnippet, type Settings } from "../lib/api";
import { useNav } from "../state/nav";
import { useSettings } from "../state/settings";

function Section({ icon, title, description, children }: { icon: ReactNode; title: string; description?: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-line bg-panel">
      <header className="flex items-start gap-3 border-b border-line px-5 py-3.5">
        <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-accent/12 text-accent">{icon}</span>
        <div>
          <h2 className="text-[14px] font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-[12px] text-muted">{description}</p>}
        </div>
      </header>
      <div className="divide-y divide-line/70">{children}</div>
    </section>
  );
}

function Row({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-2 px-5 py-3">
      <div className="min-w-56 flex-1">
        <div className="text-[13px] font-medium">{label}</div>
        {hint && <div className="mt-0.5 text-[12px] text-muted">{hint}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

function Select<T extends string | number>({ value, options, onChange, label }: { value: T; options: { value: T; label: string }[]; onChange: (v: T) => void; label: string }) {
  return (
    <select
      aria-label={label}
      className={`${inputClass} pr-7`}
      value={String(value)}
      onChange={(e) => {
        const o = options.find((x) => String(x.value) === e.target.value);
        if (o) onChange(o.value);
      }}
    >
      {options.map((o) => (
        <option key={String(o.value)} value={String(o.value)}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

const seconds = (list: number[]) => list.map((v) => ({ value: v, label: v < 60 ? `${v} seconds` : `${v / 60} minute${v === 60 ? "" : "s"}` }));
const minutes = (list: number[]) => list.map((v) => ({ value: v, label: v < 60 ? `${v} minutes` : `${v / 60} hour${v === 60 ? "" : "s"}` }));
const withCurrent = <T extends number>(opts: { value: T; label: string }[], current: T, fmt: (v: T) => string) =>
  opts.some((o) => o.value === current) ? opts : [...opts, { value: current, label: fmt(current) }].sort((a, b) => a.value - b.value);

/** Every preference in one place. Safety values stay inside safe limits. */
export function SettingsPage() {
  const { settings, view, error, save, enableTerminal, reset, reload } = useSettings();
  const { navigate } = useNav();
  const [problem, setProblem] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const [editing, setEditing] = useState<{ index: number; snippet: SavedSnippet } | null>(null);

  const set = async (patch: Partial<Settings>) => setProblem(await save(patch));

  const turnOn = async () => {
    setAsking(true);
    setProblem(null);
    try {
      const on = await enableTerminal();
      if (!on) setProblem("The terminal stays off: the confirmation was cancelled.");
    } catch (err) {
      setProblem(errorMessage(err));
    } finally {
      setAsking(false);
    }
  };

  const revoke = async (file: string) => {
    try {
      await ClusterService.RevokeTrust(file);
      reload();
    } catch (err) {
      setProblem(errorMessage(err));
    }
  };

  const shells = view?.shells ?? [];
  const snippets = settings.snippets ?? [];

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-5 pb-10">
      <PageHeader
        title="Settings"
        description="Preferences are saved on this computer only. Safety settings have limits: copied secrets always clear and the vault always locks on its own."
        actions={
          <Button variant="ghost" icon={<RotateCcw className="size-4" />} onClick={() => setConfirmReset(true)}>
            Reset all
          </Button>
        }
      />
      {error && <Notice tone="bad">Couldn't load settings: {error}</Notice>}
      {problem && <Notice tone="bad">{problem}</Notice>}

      <Section icon={<Palette className="size-4" />} title="Appearance">
        <Row label="Theme" hint="System follows your computer's light or dark mode.">
          <Segmented label="Theme" value={settings.theme} onChange={(v) => void set({ theme: v })} options={[{ value: "system", label: "System" }, { value: "dark", label: "Dark" }, { value: "light", label: "Light" }]} />
        </Row>
        <Row label="Interface size" hint="Makes everything in the window bigger or smaller.">
          <Segmented
            label="Interface size"
            value={settings.uiScale}
            onChange={(v) => void set({ uiScale: v })}
            options={[90, 100, 110, 125, 150].map((v) => ({ value: v, label: `${v}%` }))}
          />
        </Row>
      </Section>

      <Section icon={<ScrollText className="size-4" />} title="Logs" description="Defaults for every log view. You can still change them while reading.">
        <Row label="Lines to load first">
          <Select label="Lines to load first" value={settings.logTailLines} onChange={(v) => void set({ logTailLines: v })} options={withCurrent([100, 500, 1000, 5000, 10000].map((v) => ({ value: v, label: `${v} lines` })), settings.logTailLines, (v) => `${v} lines`)} />
        </Row>
        <Row label="Show timestamps">
          <Switch label="Show timestamps" checked={settings.logTimestamps} onChange={(v) => void set({ logTimestamps: v })} />
        </Row>
      </Section>

      <Section icon={<MapIcon className="size-4" />} title="Map">
        <Row label="Refresh every">
          <Select label="Map refresh" value={settings.mapRefreshSeconds} onChange={(v) => void set({ mapRefreshSeconds: v })} options={withCurrent(seconds([5, 10, 15, 30, 60, 120]), settings.mapRefreshSeconds, (v) => `${v} seconds`)} />
        </Row>
        <Row label="Show pods of the selected workload" hint="Selecting a Deployment, StatefulSet, Job or Service fans its pods out under it.">
          <Switch label="Show pods on the map" checked={settings.mapShowPods} onChange={(v) => void set({ mapShowPods: v })} />
        </Row>
      </Section>

      <Section icon={<ShieldCheck className="size-4" />} title="Security and privacy">
        <Row label="Clear copied secrets after" hint="Copied secret and vault values are removed from the clipboard after this time (5 minutes at most).">
          <Select label="Clear copied secrets after" value={settings.clipboardClearSeconds} onChange={(v) => void set({ clipboardClearSeconds: v })} options={withCurrent(seconds([10, 30, 60, 120, 300]), settings.clipboardClearSeconds, (v) => `${v} seconds`)} />
        </Row>
        <Row label="Lock the vault after" hint="Without use. The vault always locks when Kupi closes.">
          <Select label="Lock the vault after" value={settings.vaultAutoLockMinutes} onChange={(v) => void set({ vaultAutoLockMinutes: v })} options={withCurrent(minutes([5, 15, 30, 60, 120, 240]), settings.vaultAutoLockMinutes, (v) => `${v} minutes`)} />
        </Row>
        <Row label="Keep change history for" hint="What changed in your clusters, recorded while Kupi is connected. Older records are deleted.">
          <Select label="Keep change history for" value={settings.historyRetentionDays} onChange={(v) => void set({ historyRetentionDays: v })} options={withCurrent([1, 3, 7, 14, 30, 60, 90].map((v) => ({ value: v, label: v === 1 ? "1 day" : `${v} days` })), settings.historyRetentionDays, (v) => `${v} days`)} />
        </Row>
        <Row label="Audit log" hint="Every change, secret reveal and terminal start, in a tamper-evident log.">
          <Button onClick={() => navigate({ page: "audit" })}>Open the audit log</Button>
        </Row>
      </Section>

      <Section icon={<SquareTerminal className="size-4" />} title="Terminal" description="Your own shell inside Kupi, with kubectl pointed at the cluster you're connected to.">
        <Row
          label="Terminal"
          hint={
            settings.terminalEnabled
              ? "On. Commands typed there run directly, without Kupi's change preview or audit log."
              : "Off. Turning it on asks for confirmation in a system dialog."
          }
        >
          {settings.terminalEnabled ? (
            <>
              <Badge className="bg-good/15 text-good">On</Badge>
              <Button onClick={() => void set({ terminalEnabled: false })}>Turn off</Button>
            </>
          ) : (
            <Button variant="primary" busy={asking} onClick={() => void turnOn()}>
              Turn on…
            </Button>
          )}
        </Row>
        <Row label="Shell" hint={shells.length === 0 ? "No supported shell was found." : "Used for new terminal tabs."}>
          <Select
            label="Shell"
            value={settings.terminalShell}
            onChange={(v) => void set({ terminalShell: v })}
            options={[{ value: "", label: `Default${shells[0] ? ` (${shells[0].name})` : ""}` }, ...shells.map((s) => ({ value: s.id, label: s.name }))]}
          />
        </Row>
        <Row label="kubectl follows Kupi's cluster" hint="A new tab uses the connected cluster and namespace, without changing your kubeconfig's current context.">
          <Switch label="kubectl follows Kupi's cluster" checked={settings.terminalFollowCluster} onChange={(v) => void set({ terminalFollowCluster: v })} />
        </Row>
        <Row label="Check pastes" hint="Asks before a paste that would run straight away (it contains a line break), and before destructive commands on production.">
          <Switch label="Check pastes" checked={settings.terminalPasteGuard} onChange={(v) => void set({ terminalPasteGuard: v })} />
        </Row>
        <Row label="Copy on select">
          <Switch label="Copy on select" checked={settings.terminalCopyOnSelect} onChange={(v) => void set({ terminalCopyOnSelect: v })} />
        </Row>
        <Row label="Font size">
          <input
            type="range"
            aria-label="Terminal font size"
            className="kupi-range w-40"
            min={10}
            max={22}
            value={settings.terminalFontSize}
            onChange={(e) => void set({ terminalFontSize: Number(e.target.value) })}
          />
          <span className="w-10 text-right text-[12px] text-muted tabular-nums">{settings.terminalFontSize}px</span>
        </Row>
        <Row label="Scrollback">
          <Select label="Scrollback" value={settings.terminalScrollback} onChange={(v) => void set({ terminalScrollback: v })} options={withCurrent([1000, 5000, 10000, 50000, 100000].map((v) => ({ value: v, label: `${v.toLocaleString()} lines` })), settings.terminalScrollback, (v) => `${v} lines`)} />
        </Row>
        <div className="px-5 py-3">
          <div className="mb-2 flex items-center gap-2">
            <div className="flex-1">
              <div className="text-[13px] font-medium">My snippets</div>
              <div className="text-[12px] text-muted">Your own commands in the terminal's snippet list. Picking one types it; you press Enter.</div>
            </div>
            <Button icon={<Plus className="size-4" />} onClick={() => setEditing({ index: -1, snippet: { name: "", command: "" } })}>
              Add
            </Button>
          </div>
          {snippets.length === 0 ? (
            <p className="text-[12px] text-muted">None yet.</p>
          ) : (
            <ul className="divide-y divide-line/70 rounded-md border border-line">
              {snippets.map((s, i) => (
                <li key={i} className="flex items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <div className="text-[13px]">{s.name}</div>
                    <code className="block truncate text-[12px] text-muted">{s.command}</code>
                  </div>
                  <Button variant="ghost" aria-label={`Edit ${s.name}`} icon={<Pencil className="size-3.5" />} onClick={() => setEditing({ index: i, snippet: s })} />
                  <Button variant="ghost" aria-label={`Delete ${s.name}`} icon={<Trash2 className="size-3.5" />} onClick={() => void set({ snippets: snippets.filter((_, j) => j !== i) })} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </Section>

      <Section icon={<FileKey2 className="size-4" />} title="Clusters and credentials">
        <Row label="Kubeconfig files" hint="Read from KUBECONFIG, or ~/.kube/config. Kupi never copies the credentials in them.">
          <div className="flex max-w-md flex-col items-end gap-0.5">
            {(view?.kubeconfigFiles ?? []).map((f) => (
              <code key={f} className="truncate text-[12px] text-muted" title={f}>
                {f}
              </code>
            ))}
          </div>
        </Row>
        <Row label="Trusted credential plugins" hint="Kubeconfig files whose exec plugins you allowed to run. Kupi asks again when a file changes.">
          {(view?.trusted ?? []).length === 0 && <span className="text-[12px] text-muted">None</span>}
        </Row>
        {(view?.trusted ?? []).map((t) => (
          <div key={t.file} className="flex items-center gap-3 px-5 py-2 pl-10">
            <div className="min-w-0 flex-1">
              <code className="block truncate text-[12px]" title={t.file}>
                {t.file}
              </code>
              <span className="text-[11px] text-muted">runs {(t.commands ?? []).join(", ") || "a plugin"}</span>
            </div>
            <Button variant="ghost" onClick={() => void revoke(t.file)}>
              Revoke
            </Button>
          </div>
        ))}
        <Row label="Cluster profiles" hint="Local, dev, staging or production: set per cluster on the Clusters page.">
          <Button onClick={() => navigate({ page: "clusters" })}>Open Clusters</Button>
        </Row>
      </Section>

      <Section icon={<Info className="size-4" />} title="About">
        <div className="flex items-center gap-4 px-5 py-4">
          <KupiLogo className="size-12" />
          <div className="text-[13px]">
            <div className="font-semibold">Kupi · Kubernetes Pilot</div>
            <div className="text-muted">Free and open source (Apache 2.0). No telemetry, no accounts, no server.</div>
            <div className="mt-1 text-[12px] text-muted">
              Settings, audit log and vault: <code className="text-fg">{view?.configDir ?? "…"}</code>
            </div>
          </div>
        </div>
      </Section>

      <SnippetDialog
        editing={editing}
        onClose={() => setEditing(null)}
        onSave={async (s) => {
          const next = [...snippets];
          if (editing && editing.index >= 0) next[editing.index] = s;
          else next.push(s);
          const err = await save({ snippets: next });
          if (err) return err;
          setEditing(null);
          return null;
        }}
      />

      <Dialog
        open={confirmReset}
        onOpenChange={setConfirmReset}
        title="Reset all settings?"
        description="Every preference goes back to its default and the terminal is turned off. Your snippets are kept."
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmReset(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmReset(false);
                reset().catch((err) => setProblem(errorMessage(err)));
              }}
            >
              Reset
            </Button>
          </>
        }
      >
        <span />
      </Dialog>
    </div>
  );
}

function SnippetDialog({ editing, onClose, onSave }: { editing: { index: number; snippet: SavedSnippet } | null; onClose: () => void; onSave: (s: SavedSnippet) => Promise<string | null> }) {
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [key, setKey] = useState<unknown>(null);
  if (editing !== key) {
    setKey(editing);
    setName(editing?.snippet.name ?? "");
    setCommand(editing?.snippet.command ?? "");
    setError(null);
  }
  if (!editing) return null;
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={editing.index >= 0 ? "Edit snippet" : "New snippet"}
      description="It's typed into the terminal when you pick it. Nothing runs until you press Enter."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={async () => {
              const c = command.replace(/[\r\n]+$/g, "");
              if (!name.trim() || !c.trim()) {
                setError("Give it a name and a command.");
                return;
              }
              if (/[\r\n]/.test(c)) {
                setError("Keep it to one line, so it never runs by itself.");
                return;
              }
              setError(await onSave({ name: name.trim(), command: c }));
            }}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label="Name">
          <input className={inputClass} value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder="Restart the API" />
        </Field>
        <Field label="Command">
          <input className={`${inputClass} font-mono`} value={command} maxLength={2000} spellCheck={false} onChange={(e) => setCommand(e.target.value)} placeholder="kubectl rollout restart deployment/api" />
        </Field>
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}
