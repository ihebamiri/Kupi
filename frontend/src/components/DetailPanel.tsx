import { Suspense, lazy, useEffect, useState } from "react";
import { RotateCcw, Scaling, Trash2, X } from "lucide-react";
import { KubectlButton } from "./CopyButton";
import { LogsView } from "./LogsView";
import { ReviewDialog } from "./ReviewDialog";

// The terminal library is large; load it only when a shell is opened.
const ShellView = lazy(() => import("./ShellView").then((m) => ({ default: m.ShellView })));
import { Badge, Button, Notice, inputClass } from "./ui";
import {
  GuardService,
  ResourceService,
  errorMessage,
  type ContainerView,
  type EventView,
  type PlanView,
  type ResourceRef,
  type ResourceTypeView,
  type TableRow,
} from "../lib/api";
import { age } from "../lib/format";
import { kubectlDelete, kubectlRestart, kubectlScale, kubectlYaml, type KubeTarget } from "../lib/kubectl";
import { useNav } from "../state/nav";

type Tab = "overview" | "yaml" | "events" | "logs" | "shell";

const SCALABLE = new Set(["deployments", "statefulsets", "replicasets"]);
const RESTARTABLE = new Set(["deployments", "statefulsets", "daemonsets"]);
const HAS_PODS = new Set(["deployments", "statefulsets", "daemonsets", "replicasets", "jobs"]);

interface DetailPanelProps {
  contextName: string;
  type: ResourceTypeView;
  row: TableRow;
  onClose: () => void;
}

export function DetailPanel({ contextName, type, row, onClose }: DetailPanelProps) {
  const isPod = type.group === "" && type.resource === "pods";
  const isWorkload = type.group === "apps" || type.group === "batch" ? HAS_PODS.has(type.resource) : false;
  const tabs: Tab[] = ["overview", "yaml", "events"];
  if (isPod || isWorkload) tabs.push("logs");
  if (isPod) tabs.push("shell");
  const [tab, setTab] = useState<Tab>("overview");
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [notice, setNotice] = useState<{ tone: "good" | "bad"; text: string } | null>(null);
  const [replicas, setReplicas] = useState("");
  const [busy, setBusy] = useState(false);

  const ref: ResourceRef = { group: type.group, version: type.version, resource: type.resource, namespace: row.namespace, name: row.name };
  const target: KubeTarget = { context: contextName, group: type.group, resource: type.resource, namespace: row.namespace || undefined, name: row.name };
  const canScale = type.group === "apps" && SCALABLE.has(type.resource);
  const canRestart = type.group === "apps" && RESTARTABLE.has(type.resource);
  const canDelete = (type.verbs ?? []).includes("delete");

  const prepare = async (fn: () => Promise<PlanView>) => {
    setBusy(true);
    setNotice(null);
    try {
      setPlan(await fn());
    } catch (err) {
      setNotice({ tone: "bad", text: errorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <aside className="flex w-[46%] max-w-[760px] min-w-[420px] flex-col rounded-lg border border-line bg-panel" aria-label={`${type.kind} ${row.name}`}>
      <div className="flex items-start gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="text-[11px] font-medium tracking-wide text-muted uppercase">{type.kind}</div>
          <h2 className="truncate text-[15px] font-semibold" title={row.name}>
            {row.name}
          </h2>
          {row.namespace && <div className="text-[12px] text-muted">namespace {row.namespace}</div>}
        </div>
        <button onClick={onClose} className="rounded p-1 text-muted hover:bg-panel-2 hover:text-fg" aria-label="Close details">
          <X className="size-4" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-1.5 border-b border-line px-4 py-2">
        {canScale && (
          <form
            className="flex items-center gap-1"
            onSubmit={(e) => {
              e.preventDefault();
              const n = Number(replicas);
              if (!Number.isInteger(n) || n < 0) {
                setNotice({ tone: "bad", text: "Enter a whole number of copies, 0 or more." });
                return;
              }
              void prepare(() => GuardService.PrepareScale(contextName, ref, n));
            }}
          >
            <input
              aria-label="Copies"
              className={`${inputClass} w-16`}
              inputMode="numeric"
              placeholder="copies"
              value={replicas}
              onChange={(e) => setReplicas(e.target.value)}
            />
            <Button type="submit" busy={busy} icon={<Scaling className="size-4" />} title={kubectlScale(target, Number(replicas) || 0)}>
              Scale
            </Button>
          </form>
        )}
        {canRestart && (
          <Button busy={busy} icon={<RotateCcw className="size-4" />} title={kubectlRestart(target)} onClick={() => void prepare(() => GuardService.PrepareRestart(contextName, ref))}>
            Restart
          </Button>
        )}
        {canDelete && (
          <Button variant="ghost" busy={busy} icon={<Trash2 className="size-4" />} title={kubectlDelete(target)} onClick={() => void prepare(() => GuardService.PrepareDelete(contextName, ref))}>
            Delete
          </Button>
        )}
        <span className="ml-auto">
          <KubectlButton command={kubectlYaml(target)} label="kubectl" />
        </span>
      </div>

      {notice && (
        <div className="px-4 pt-3">
          <Notice tone={notice.tone}>{notice.text}</Notice>
        </div>
      )}

      <div role="tablist" className="flex gap-1 border-b border-line px-3">
        {tabs.map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`border-b-2 px-2.5 py-2 text-[13px] capitalize ${tab === t ? "border-accent font-medium text-fg" : "border-transparent text-muted hover:text-fg"}`}
          >
            {t === "yaml" ? "YAML" : t}
          </button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-hidden" role="tabpanel">
        {tab === "overview" && <Overview contextName={contextName} row={row} isPod={isPod} />}
        {tab === "yaml" && <YamlTab contextName={contextName} type={type} ref_={ref} />}
        {tab === "events" && <EventsTab contextName={contextName} row={row} />}
        {tab === "logs" && (
          <LogsView
            contextName={contextName}
            namespace={row.namespace}
            pod={isPod ? row.name : undefined}
            workload={isWorkload ? ref : undefined}
            target={target}
          />
        )}
        {tab === "shell" && isPod && (
          <Suspense fallback={<p className="p-4 text-[13px] text-muted">Loading terminal…</p>}>
            <ShellView contextName={contextName} namespace={row.namespace} pod={row.name} target={target} />
          </Suspense>
        )}
      </div>

      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={(result) => {
          setPlan(null);
          setNotice({ tone: "good", text: `${result.message}. Audit entry #${result.auditSeq}.` });
        }}
      />
    </aside>
  );
}

function Overview({ contextName, row, isPod }: { contextName: string; row: TableRow; isPod: boolean }) {
  const [containers, setContainers] = useState<ContainerView[] | null>(null);
  useEffect(() => {
    if (!isPod) return;
    ResourceService.Containers(contextName, row.namespace, row.name)
      .then((c) => setContainers(c ?? []))
      .catch(() => setContainers([]));
  }, [contextName, row.namespace, row.name, isPod]);

  const labels = Object.entries(row.labels ?? {});
  return (
    <div className="h-full overflow-y-auto px-4 py-3 text-[13px]">
      <dl className="grid grid-cols-[120px_1fr] gap-x-3 gap-y-1.5">
        <dt className="text-muted">Created</dt>
        <dd>{row.created ? `${age(row.created)} ago · ${new Date(row.created).toLocaleString()}` : "—"}</dd>
        {row.ownerKind && (
          <>
            <dt className="text-muted">Controlled by</dt>
            <dd>
              {row.ownerKind} {row.ownerName}
            </dd>
          </>
        )}
        <dt className="text-muted">UID</dt>
        <dd className="font-mono text-[12px] break-all">{row.uid || "—"}</dd>
      </dl>
      {labels.length > 0 && (
        <section className="mt-4">
          <h3 className="mb-1.5 text-[12px] font-semibold text-muted">Labels</h3>
          <div className="flex flex-wrap gap-1">
            {labels.map(([k, v]) => (
              <Badge key={k} className="bg-panel-2 font-mono text-fg">
                {k}={v}
              </Badge>
            ))}
          </div>
        </section>
      )}
      {containers && containers.length > 0 && (
        <section className="mt-4">
          <h3 className="mb-1.5 text-[12px] font-semibold text-muted">Containers</h3>
          <ul className="flex flex-col gap-1.5">
            {containers.map((c) => (
              <li key={c.name} className="rounded-md border border-line px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{c.name}</span>
                  {c.init && <Badge className="bg-panel-2 text-muted">init</Badge>}
                  <Badge className={c.ready ? "bg-good/15 text-good" : "bg-warn/15 text-warn"}>{c.ready ? "ready" : c.state || "not ready"}</Badge>
                  {c.restartCount > 0 && <Badge className="bg-bad/15 text-bad">{c.restartCount} restarts</Badge>}
                </div>
                <div className="mt-0.5 truncate font-mono text-[12px] text-muted" title={c.image}>
                  {c.image}
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function YamlTab({ contextName, type, ref_ }: { contextName: string; type: ResourceTypeView; ref_: ResourceRef }) {
  const { navigate } = useNav();
  const [yaml, setYaml] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const isSecret = type.group === "" && type.resource === "secrets";
  useEffect(() => {
    ResourceService.GetYAML(contextName, ref_)
      .then(setYaml)
      .catch((err) => setError(errorMessage(err)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contextName, ref_.group, ref_.resource, ref_.namespace, ref_.name]);

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (yaml === null) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-line px-4 py-2 text-[12px] text-muted">
        {isSecret ? (
          <span>Secret values are hidden. Editing Secrets comes with the secrets screen.</span>
        ) : (
          <>
            <span>Read-only view.</span>
            <Button className="ml-auto" onClick={() => navigate({ page: "apply", edit: ref_ })}>
              Edit with form or YAML
            </Button>
          </>
        )}
      </div>
      <pre className="min-h-0 flex-1 overflow-auto bg-bg px-4 py-3 font-mono text-[12px] leading-5 whitespace-pre">{yaml}</pre>
    </div>
  );
}

function EventsTab({ contextName, row }: { contextName: string; row: TableRow }) {
  const [events, setEvents] = useState<EventView[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!row.uid) {
      setEvents([]);
      return;
    }
    ResourceService.Events(contextName, row.namespace, row.uid)
      .then((e) => setEvents(e ?? []))
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, row.namespace, row.uid]);

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (events === null) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  if (events.length === 0) return <p className="p-4 text-[13px] text-muted">No recent events. Kubernetes keeps events for about an hour.</p>;
  return (
    <ul className="h-full overflow-y-auto px-4 py-3 text-[13px]">
      {events.map((e, i) => (
        <li key={i} className="border-b border-line/60 py-2">
          <div className="flex items-center gap-2">
            <Badge className={e.type === "Warning" ? "bg-bad/15 text-bad" : "bg-panel-2 text-muted"}>{e.type}</Badge>
            <span className="font-medium">{e.reason}</span>
            {e.count > 1 && <span className="text-[12px] text-muted">×{e.count}</span>}
            <span className="ml-auto text-[12px] text-muted">{age(e.lastSeen)} ago</span>
          </div>
          <p className="mt-1 break-words text-fg/90">{e.message}</p>
          {e.source && <p className="mt-0.5 text-[11px] text-muted">from {e.source}</p>}
        </li>
      ))}
    </ul>
  );
}
