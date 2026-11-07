import { useCallback, useEffect, useMemo, useState } from "react";
import { Eye, Package, Plus, RefreshCw, RotateCcw, Trash2, Upload } from "lucide-react";
import { ChartPicker, type ChartChoice } from "../components/ChartPicker";
import { FormEnvContext, RefInput } from "../components/FormInputs";
import { ReviewDialog } from "../components/ReviewDialog";
import { ReasonDialog } from "../components/SecretFields";
import { YamlEditor } from "../components/YamlEditor";
import { Badge, Button, Field, Notice, PageHeader, inputClass } from "../components/ui";
import { HelmService, errorMessage, type HelmRelease, type PlanView, type ReleaseDetail } from "../lib/api";
import { age } from "../lib/format";
import { useClusters } from "../state/clusters";

export function statusTone(status: string): string {
  switch (status) {
    case "deployed":
      return "bg-good/15 text-good";
    case "failed":
      return "bg-bad/15 text-bad";
    case "superseded":
    case "uninstalled":
      return "bg-panel-2 text-muted";
  }
  return "bg-warn/15 text-warn";
}

/** Helm releases: what's installed, its history, values, rollbacks and upgrades. */
export function HelmPage() {
  const { active, namespace } = useClusters();
  const [releases, setReleases] = useState<HelmRelease[] | null>(null);
  const [selected, setSelected] = useState<{ namespace: string; name: string } | null>(null);
  const [installing, setInstalling] = useState(false);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const contextName = active?.name ?? "";

  const load = useCallback(() => {
    if (!contextName) return;
    setError(null);
    HelmService.Releases(contextName, namespace)
      .then((r) => setReleases(r ?? []))
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, namespace]);
  useEffect(load, [load, reload]);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return (releases ?? []).filter((r) => !q || r.name.includes(q) || r.chart.toLowerCase().includes(q));
  }, [releases, filter]);

  if (!active) return <p className="text-[13px] text-muted">Connect to a cluster first.</p>;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title="Helm"
        description="Releases installed with Helm. Install charts from Artifact Hub or a repository, change values or the chart version, roll back, or uninstall; every action shows the rendered difference first."
        actions={
          <Button
            variant="primary"
            icon={<Plus className="size-4" />}
            onClick={() => {
              setSelected(null);
              setInstalling(true);
            }}
          >
            Install a chart
          </Button>
        }
      />
      <div className="grid min-h-0 flex-1 grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)] gap-4">
        <section aria-label="Release list" className="flex min-h-0 flex-col rounded-lg border border-line bg-panel">
          <div className="flex gap-2 border-b border-line p-2">
            <input aria-label="Filter releases" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter by name or chart" className={`${inputClass} min-w-0 flex-1`} />
            <Button variant="ghost" className="px-2" aria-label="Reload" title="Reload" icon={<RefreshCw className="size-4" />} onClick={load} />
          </div>
          {error && (
            <div className="p-2">
              <Notice tone="bad">{error}</Notice>
            </div>
          )}
          <ul className="min-h-0 flex-1 overflow-y-auto p-1">
            {releases && shown.length === 0 && <li className="p-3 text-[13px] text-muted">No Helm releases{namespace ? ` in ${namespace}` : ""}.</li>}
            {shown.map((r) => {
              const isSel = selected?.name === r.name && selected.namespace === r.namespace;
              return (
                <li key={`${r.namespace}/${r.name}`}>
                  <button
                    aria-label={`${r.name} in ${r.namespace}`}
                    aria-current={isSel ? "true" : undefined}
                    onClick={() => {
                      setInstalling(false);
                      setSelected({ namespace: r.namespace, name: r.name });
                    }}
                    className={`flex w-full flex-col gap-0.5 rounded-md px-2.5 py-1.5 text-left ${isSel ? "bg-panel-2" : "hover:bg-panel-2"}`}
                  >
                    <span className="flex items-center gap-2">
                      <span className="truncate text-[13px] font-medium">{r.name}</span>
                      <Badge className={`ml-auto ${statusTone(r.status)}`}>{r.status}</Badge>
                    </span>
                    <span className="truncate text-[11px] text-muted">
                      {!namespace && `${r.namespace} · `}
                      {r.chart} {r.chartVersion} · rev {r.revision}
                      {r.updated && ` · ${age(r.updated)}`}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
        <section aria-label="Release details" className="min-h-0 overflow-y-auto rounded-lg border border-line bg-panel">
          {installing ? (
            <InstallPanel
              contextName={contextName}
              defaultNamespace={namespace || "default"}
              onCancel={() => setInstalling(false)}
              onInstalled={(ns, name) => {
                setInstalling(false);
                setSelected({ namespace: ns, name });
                setReload((n) => n + 1);
              }}
            />
          ) : selected ? (
            <ReleaseView
              key={`${selected.namespace}/${selected.name}/${reload}`}
              contextName={contextName}
              namespace={selected.namespace}
              name={selected.name}
              onChanged={(gone) => {
                if (gone) setSelected(null);
                setReload((n) => n + 1);
              }}
            />
          ) : (
            <p className="p-4 text-[13px] text-muted">Choose a release, or install a chart.</p>
          )}
        </section>
      </div>
    </div>
  );
}

type Tab = "history" | "values" | "version" | "manifest" | "resources" | "notes" | "defaults";

const NO_CHART: ChartChoice = { repo: "", chart: "", version: "", info: null };

/** Install a chart as a new release: pick it, name the release, adjust values, review. */
function InstallPanel({ contextName, defaultNamespace, onCancel, onInstalled }: { contextName: string; defaultNamespace: string; onCancel: () => void; onInstalled: (namespace: string, name: string) => void }) {
  const [choice, setChoice] = useState<ChartChoice>(NO_CHART);
  const [name, setName] = useState("");
  // The release name follows the chosen chart until the user types one.
  const [nameEdited, setNameEdited] = useState(false);
  const [ns, setNs] = useState(defaultNamespace);
  const [createNs, setCreateNs] = useState(false);
  const [values, setValues] = useState("");
  const [valuesFor, setValuesFor] = useState("");
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Start from the chosen version's own values.yaml, and the chart's name.
  const key = `${choice.repo}|${choice.chart}|${choice.info?.version ?? ""}`;
  useEffect(() => {
    if (!choice.info || valuesFor === key) return;
    setValues(choice.info.values);
    setValuesFor(key);
    if (!nameEdited) setName(choice.chart);
  }, [choice.info, choice.chart, key, valuesFor, nameEdited]);

  const preview = async () => {
    if (!choice.info) return;
    setBusy(true);
    setError(null);
    try {
      setPlan(
        await HelmService.PrepareInstall({
          context: contextName,
          namespace: ns,
          name: name.trim(),
          repo: choice.repo,
          chart: choice.chart,
          version: choice.version,
          // Unchanged defaults aren't stored as your values.
          values: values === choice.info.values ? "" : values,
          createNamespace: createNs,
        }),
      );
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex items-center gap-2">
        <h2 className="flex items-center gap-2 text-[15px] font-semibold">
          <Package className="size-4 text-muted" aria-hidden /> Install a chart
        </h2>
        <Button className="ml-auto" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
      <ChartPicker value={choice} onChange={setChoice} />
      {choice.info && (
        <>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Release name" hint="Lowercase letters, digits and dashes.">
              <input
                className={`${inputClass} font-mono`}
                value={name}
                maxLength={53}
                onChange={(e) => {
                  setName(e.target.value.toLowerCase());
                  setNameEdited(true);
                }}
                spellCheck={false}
              />
            </Field>
            <div className="flex flex-col gap-1">
              <span className="text-[12px] font-medium text-muted">Namespace</span>
              <FormEnvContext.Provider value={{ contextName, namespace: defaultNamespace }}>
                <RefInput label="Namespace" value={ns} onChange={(v) => setNs(v ?? "")} refSpec={{ type: "namespaces" }} root={{}} />
              </FormEnvContext.Provider>
              <label className="flex items-center gap-2 text-[12px] text-muted">
                <input type="checkbox" checked={createNs} onChange={(e) => setCreateNs(e.target.checked)} /> Create the namespace if it doesn't exist
              </label>
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-[12px] font-medium text-muted">Values (the chart's defaults; change what you need)</span>
            <div className="h-80 overflow-hidden rounded-md border border-line">
              <YamlEditor value={values} onChange={setValues} />
            </div>
          </div>
          {error && <Notice tone="bad">{error}</Notice>}
          <div>
            <Button variant="primary" busy={busy} icon={<Eye className="size-4" />} disabled={!name.trim() || !ns.trim()} onClick={() => void preview()}>
              Review install
            </Button>
          </div>
        </>
      )}
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={() => {
          setPlan(null);
          onInstalled(ns, name.trim());
        }}
      />
    </div>
  );
}

/** Moves a release to another version of its chart, keeping or editing its values. */
function VersionTab({ contextName, namespace, name, chart, currentVersion, values, onNeedValues, onPlan }: {
  contextName: string;
  namespace: string;
  name: string;
  chart: string;
  currentVersion: string;
  values: string | null;
  onNeedValues: () => void;
  onPlan: (p: PlanView) => void;
}) {
  const [choice, setChoice] = useState<ChartChoice>(NO_CHART);
  const [edited, setEdited] = useState(values ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setEdited(values ?? ""), [values]);

  const review = async () => {
    setBusy(true);
    setError(null);
    try {
      onPlan(await HelmService.PrepareUpgradeChart(contextName, namespace, name, choice.repo, choice.chart, choice.version, edited));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <p className="text-[12px] text-muted">
        Upgrade to another version of the chart (now {chart} {currentVersion}). Helm releases don't record where their chart came from, so find it below: the search starts with the chart's name.
      </p>
      <ChartPicker initialQuery={chart} value={choice} onChange={setChoice} />
      {choice.info &&
        (values === null ? (
          <div className="flex flex-col items-start gap-2">
            <p className="text-[13px] text-muted">The upgrade keeps this release's own values. They can hold passwords, so they stay hidden until you load them.</p>
            <Button icon={<Eye className="size-4" />} onClick={onNeedValues}>
              Load the current values
            </Button>
          </div>
        ) : (
          <>
            <span className="text-[12px] font-medium text-muted">Values for the new version (yours, not the chart's defaults)</span>
            <div className="h-64 overflow-hidden rounded-md border border-line">
              <YamlEditor value={edited} onChange={setEdited} />
            </div>
            {error && <Notice tone="bad">{error}</Notice>}
            <div>
              <Button variant="primary" busy={busy} icon={<Upload className="size-4" />} onClick={() => void review()}>
                Review upgrade to {choice.chart} {choice.version}
              </Button>
            </div>
          </>
        ))}
    </div>
  );
}

function ReleaseView({ contextName, namespace, name, onChanged }: { contextName: string; namespace: string; name: string; onChanged: (gone: boolean) => void }) {
  const [d, setD] = useState<ReleaseDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("history");
  const [values, setValues] = useState<string | null>(null);
  const [edited, setEdited] = useState("");
  const [askReason, setAskReason] = useState(false);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [planKind, setPlanKind] = useState<"upgrade" | "rollback" | "uninstall">("upgrade");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    HelmService.Release(contextName, namespace, name)
      .then(setD)
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, namespace, name]);

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (!d) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  const r = d.release;

  const reveal = async (reason: string) => {
    setAskReason(false);
    setError(null);
    try {
      const v = await HelmService.Values(contextName, namespace, name, reason);
      setValues(v);
      setEdited(v);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const prepare = async (kind: "upgrade" | "rollback" | "uninstall", revision = 0) => {
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      setPlanKind(kind);
      if (kind === "upgrade") setPlan(await HelmService.PrepareUpgrade(contextName, namespace, name, edited));
      else if (kind === "rollback") setPlan(await HelmService.PrepareRollback(contextName, namespace, name, revision));
      else setPlan(await HelmService.PrepareUninstall(contextName, namespace, name));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const tabs: { id: Tab; label: string }[] = [
    { id: "history", label: `History (${(d.history ?? []).length})` },
    { id: "values", label: "Values" },
    { id: "version", label: "Chart version" },
    { id: "manifest", label: "Manifest" },
    { id: "resources", label: `Resources (${(d.resources ?? []).length})` },
    ...(d.notes ? [{ id: "notes" as Tab, label: "Notes" }] : []),
    { id: "defaults", label: "Chart defaults" },
  ];

  return (
    <div className="flex flex-col">
      <div className="flex flex-wrap items-start gap-3 p-4 pb-2">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-[15px] font-semibold">
            <Package className="size-4 text-muted" aria-hidden /> {r.name}
          </h2>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[12px] text-muted">
            <span>{r.namespace}</span>
            <Badge className={statusTone(r.status)}>{r.status}</Badge>
            <span>
              {r.chart} {r.chartVersion}
              {r.appVersion && ` · app ${r.appVersion}`} · revision {r.revision}
              {r.updated && ` · ${age(r.updated)} ago`}
            </span>
          </div>
          {d.chartDescription && <p className="mt-1 text-[12px] text-muted">{d.chartDescription}</p>}
        </div>
        <Button className="ml-auto" variant="ghost" busy={busy && planKind === "uninstall"} icon={<Trash2 className="size-4" />} onClick={() => void prepare("uninstall")}>
          Uninstall
        </Button>
      </div>
      {r.description && r.status !== "deployed" && (
        <div className="px-4 pb-2">
          <Notice tone={r.status === "failed" ? "bad" : "warn"}>{r.description}</Notice>
        </div>
      )}
      {message && (
        <div className="px-4 pb-2">
          <Notice tone="good">{message}</Notice>
        </div>
      )}
      {error && (
        <div className="px-4 pb-2">
          <Notice tone="bad">{error}</Notice>
        </div>
      )}
      <div role="tablist" className="flex gap-1 border-b border-line px-3">
        {tabs.map((t) => (
          <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)} className={`border-b-2 px-2.5 py-2 text-[13px] ${tab === t.id ? "border-accent font-medium text-fg" : "border-transparent text-muted hover:text-fg"}`}>
            {t.label}
          </button>
        ))}
      </div>
      <div className="p-4">
        {tab === "history" && (
          <table className="w-full text-[13px]">
            <thead className="text-left text-[12px] text-muted">
              <tr>
                <th className="py-1 font-medium">Revision</th>
                <th className="font-medium">Updated</th>
                <th className="font-medium">Status</th>
                <th className="font-medium">Chart</th>
                <th className="font-medium">Description</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {(d.history ?? []).map((h) => (
                <tr key={h.revision} className="border-t border-line align-top">
                  <td className="py-1.5 font-mono">{h.revision}</td>
                  <td className="text-muted">{h.updated ? `${age(h.updated)} ago` : ""}</td>
                  <td>
                    <Badge className={statusTone(h.status)}>{h.status}</Badge>
                  </td>
                  <td className="text-muted">
                    {h.chartVersion}
                    {h.appVersion && ` (app ${h.appVersion})`}
                  </td>
                  <td className="max-w-64 truncate text-muted" title={h.description}>
                    {h.description}
                  </td>
                  <td className="text-right">
                    {h.revision < r.revision && (
                      <Button variant="ghost" className="h-7" icon={<RotateCcw className="size-3.5" />} onClick={() => void prepare("rollback", h.revision)}>
                        Roll back to this
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {tab === "values" &&
          (values === null ? (
            <div className="flex flex-col items-start gap-2">
              <p className="text-[13px] text-muted">
                {d.userValuesSupplied ? "Values often hold passwords and tokens, so they stay hidden until you ask." : "This release uses the chart's defaults only. Show the values to add your own."}
                {d.valuesNeedReason && " This cluster asks for a reason, saved in the audit log."}
              </p>
              <Button icon={<Eye className="size-4" />} onClick={() => (d.valuesNeedReason ? setAskReason(true) : void reveal(""))}>
                Show and edit values
              </Button>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <p className="text-[12px] text-muted">
                The values this release was deployed with. Edit them and review the upgrade; the chart stays at {r.chart} {r.chartVersion}
                {d.hasSchema ? " and its values schema is checked" : ""}.
              </p>
              <div className="h-80 overflow-hidden rounded-md border border-line">
                <YamlEditor value={edited} onChange={setEdited} />
              </div>
              <div className="flex gap-2">
                <Button variant="primary" busy={busy && planKind === "upgrade"} icon={<Upload className="size-4" />} disabled={edited === values} onClick={() => void prepare("upgrade")}>
                  Review upgrade
                </Button>
                <Button variant="ghost" disabled={edited === values} onClick={() => setEdited(values)}>
                  Undo edits
                </Button>
              </div>
            </div>
          ))}
        {tab === "version" && (
          <VersionTab
            contextName={contextName}
            namespace={namespace}
            name={name}
            chart={r.chart}
            currentVersion={r.chartVersion}
            values={values}
            onNeedValues={() => (d.valuesNeedReason ? setAskReason(true) : void reveal(""))}
            onPlan={(p) => {
              setPlanKind("upgrade");
              setPlan(p);
            }}
          />
        )}
        {tab === "manifest" && (
          <>
            <p className="mb-2 text-[12px] text-muted">What Helm rendered for revision {r.revision}. Secret values are hidden.</p>
            <pre className="max-h-[32rem] overflow-auto rounded-md border border-line bg-bg px-3 py-2 font-mono text-[12px] leading-5 whitespace-pre">{d.manifest}</pre>
          </>
        )}
        {tab === "resources" && (
          <ul className="flex flex-col divide-y divide-line rounded-md border border-line text-[13px]">
            {(d.resources ?? []).map((res) => (
              <li key={`${res.kind}/${res.namespace}/${res.name}`} className="flex gap-2 px-3 py-1.5">
                <span className="w-44 shrink-0 text-muted">{res.kind}</span>
                <span className="truncate">
                  {res.namespace ? `${res.namespace}/` : ""}
                  {res.name}
                </span>
              </li>
            ))}
          </ul>
        )}
        {tab === "notes" && <pre className="overflow-auto rounded-md border border-line bg-bg px-3 py-2 font-mono text-[12px] leading-5 whitespace-pre-wrap">{d.notes}</pre>}
        {tab === "defaults" && (
          <>
            <p className="mb-2 text-[12px] text-muted">The chart's own defaults, for reference. Your values override these.</p>
            <pre className="max-h-[32rem] overflow-auto rounded-md border border-line bg-bg px-3 py-2 font-mono text-[12px] leading-5 whitespace-pre">{d.defaults}</pre>
          </>
        )}
      </div>
      <ReasonDialog open={askReason} title={`Show the values of ${r.name}`} onCancel={() => setAskReason(false)} onSubmit={(reason) => void reveal(reason)} />
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={(res) => {
          setPlan(null);
          setMessage(res.message);
          onChanged(planKind === "uninstall");
        }}
      />
    </div>
  );
}
