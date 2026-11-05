import { useCallback, useEffect, useMemo, useState } from "react";
import { CircleCheck, RefreshCw, ScrollText, Siren } from "lucide-react";
import { Timeline, mergeTimeline } from "../components/Timeline";
import { Badge, Button, Notice, PageHeader, inputClass } from "../components/ui";
import { IncidentService, errorMessage, type IncidentReport, type Problem } from "../lib/api";
import { age } from "../lib/format";
import { KIND_TYPES } from "../lib/managers";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";

const WINDOWS = [
  { minutes: 15, label: "Last 15 minutes" },
  { minutes: 30, label: "Last 30 minutes" },
  { minutes: 60, label: "Last hour" },
  { minutes: 180, label: "Last 3 hours" },
  { minutes: 1440, label: "Last 24 hours" },
];

/** One screen for "what is broken, and what changed right before?" */
export function IncidentPage() {
  const { active, namespace } = useClusters();
  const [minutes, setMinutes] = useState(30);
  const [report, setReport] = useState<IncidentReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const contextName = active?.name ?? "";

  const load = useCallback(async () => {
    if (!contextName) return;
    setBusy(true);
    setError(null);
    try {
      setReport(await IncidentService.Report(contextName, namespace, minutes));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }, [contextName, namespace, minutes]);

  useEffect(() => {
    void load();
  }, [load]);

  const timeline = useMemo(() => mergeTimeline(report?.changes ?? [], report?.events ?? []), [report]);
  if (!active) return <p className="text-[13px] text-muted">Connect to a cluster first.</p>;

  const problems = report?.problems ?? [];
  const critical = problems.filter((p) => p.severity === "critical").length;
  const scope = namespace ? `namespace ${namespace}` : "all namespaces";

  return (
    <div className="max-w-5xl">
      <PageHeader
        title="Incident"
        description="What is failing right now, and what changed right before it: Kupi's recorded changes, warning events and error lines from failing containers, in one place."
        actions={
          <>
            <select aria-label="Time window" value={minutes} onChange={(e) => setMinutes(Number(e.target.value))} className={inputClass}>
              {WINDOWS.map((w) => (
                <option key={w.minutes} value={w.minutes}>
                  {w.label}
                </option>
              ))}
            </select>
            <Button busy={busy} icon={<RefreshCw className="size-4" />} onClick={() => void load()}>
              Refresh
            </Button>
          </>
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {report && (
        <div className="flex flex-col gap-6">
          <div className="flex flex-wrap items-center gap-3 text-[13px]">
            {problems.length === 0 ? (
              <span className="flex items-center gap-2 font-medium text-good">
                <CircleCheck className="size-5" aria-hidden /> Nothing is failing in {scope}.
              </span>
            ) : (
              <span className="flex items-center gap-2 font-medium">
                <Siren className="size-5 text-bad" aria-hidden />
                {critical > 0 && <span className="text-bad">{critical} critical</span>}
                {problems.length - critical > 0 && <span className="text-warn">{problems.length - critical} warning{problems.length - critical === 1 ? "" : "s"}</span>}
                <span className="font-normal text-muted">in {scope}</span>
              </span>
            )}
            <span className="text-muted">
              · {report.changes?.length ?? 0} change{report.changes?.length === 1 ? "" : "s"} and {report.events?.length ?? 0} warning{report.events?.length === 1 ? "" : "s"} in the window
            </span>
          </div>
          {(report.skipped ?? []).length > 0 && <p className="text-[12px] text-muted">Couldn't check: {(report.skipped ?? []).join(", ")}.</p>}

          {problems.length > 0 && (
            <section aria-label="Failing now">
              <h2 className="mb-2 text-[14px] font-semibold">Failing now</h2>
              <ul className="flex flex-col gap-1.5">
                {problems.map((p, i) => (
                  <ProblemRow key={i} p={p} />
                ))}
              </ul>
            </section>
          )}

          {(report.logs ?? []).length > 0 && (
            <section aria-label="Error lines">
              <h2 className="mb-2 text-[14px] font-semibold">From the logs</h2>
              <div className="flex flex-col gap-3">
                {(report.logs ?? []).map((l) => (
                  <div key={`${l.namespace}/${l.pod}/${l.container}`} className="rounded-md border border-line">
                    <div className="flex items-center gap-2 border-b border-line bg-panel-2 px-3 py-1.5 text-[12px]">
                      <ScrollText className="size-3.5 text-muted" aria-hidden />
                      <span className="font-medium">
                        {l.namespace}/{l.pod}
                      </span>
                      <span className="text-muted">
                        container {l.container}
                        {l.previous && " · previous run"}
                        {!l.matched && !l.error && " · no error lines; last lines shown"}
                      </span>
                    </div>
                    {l.error ? (
                      <p className="px-3 py-2 text-[12px] text-muted">Couldn't read the log: {l.error}</p>
                    ) : (
                      <pre className="max-h-56 overflow-auto bg-bg px-3 py-2 font-mono text-[12px] leading-5 break-all whitespace-pre-wrap">{(l.lines ?? []).join("\n")}</pre>
                    )}
                  </div>
                ))}
              </div>
            </section>
          )}

          <section aria-label="What changed">
            <h2 className="mb-1 text-[14px] font-semibold">What changed</h2>
            <p className="mb-2 text-[12px] text-muted">
              {report.historySince
                ? `Changes Kupi recorded since it connected ${age(report.historySince)} ago, and warning events. Earlier changes, or changes while Kupi was closed, aren't known.`
                : "Kupi isn't recording this cluster, so only the cluster's own warning events are shown."}
            </p>
            {timeline.length === 0 ? <p className="text-[13px] text-muted">Nothing changed and no warnings in this window.</p> : <Timeline items={timeline} />}
          </section>
        </div>
      )}
    </div>
  );
}

function ProblemRow({ p }: { p: Problem }) {
  const { navigate } = useNav();
  const target = KIND_TYPES[p.kind];
  return (
    <li className={`flex items-start gap-3 rounded-md border px-3 py-2 text-[13px] ${p.severity === "critical" ? "border-bad/40 bg-bad/5" : "border-warn/40 bg-warn/5"}`}>
      <Badge className={p.severity === "critical" ? "bg-bad/15 text-bad" : "bg-warn/15 text-warn"}>{p.severity === "critical" ? "Critical" : "Warning"}</Badge>
      <div className="min-w-0 flex-1">
        <div>
          <span className="text-muted">{p.kind} </span>
          <span className="font-medium">
            {p.namespace ? `${p.namespace}/` : ""}
            {p.name}
          </span>
          <span> · {p.problem}</span>
        </div>
        {p.detail && <p className="mt-0.5 break-words text-[12px] text-muted">{p.detail}</p>}
      </div>
      {p.since && <span className="shrink-0 text-[12px] text-muted">{age(p.since)} ago</span>}
      {target && (
        <Button variant="ghost" className="h-7 shrink-0" onClick={() => navigate({ page: "resources", group: target.group, resource: target.resource, select: { namespace: p.namespace, name: p.name } })}>
          Open
        </Button>
      )}
    </li>
  );
}
