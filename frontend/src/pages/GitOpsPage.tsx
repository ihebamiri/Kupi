import { useCallback, useEffect, useState } from "react";
import { Browser } from "@wailsio/runtime";
import { ExternalLink, GitBranch, Pause, Play, RefreshCw, RotateCw } from "lucide-react";
import { ReviewDialog } from "../components/ReviewDialog";
import { Badge, Button, Notice, PageHeader } from "../components/ui";
import { GitOpsService, errorMessage, type GitOpsApp, type GitOpsOverview, type PlanView } from "../lib/api";
import { age } from "../lib/format";
import { useClusters } from "../state/clusters";

export function syncTone(s: string): string {
  switch (s) {
    case "Synced":
    case "Ready":
    case "Healthy":
    case "Succeeded":
      return "bg-good/15 text-good";
    case "OutOfSync":
    case "Progressing":
    case "Running":
      return "bg-warn/15 text-warn";
    case "NotReady":
    case "Degraded":
    case "Missing":
    case "Failed":
    case "Error":
      return "bg-bad/15 text-bad";
  }
  return "bg-panel-2 text-muted";
}

/** Argo CD and Flux apps: are they in sync with Git, and what do they deploy? */
export function GitOpsPage() {
  const { active, namespace } = useClusters();
  const [ov, setOv] = useState<GitOpsOverview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const contextName = active?.name ?? "";

  const load = useCallback(() => {
    if (!contextName) return;
    GitOpsService.Overview(contextName)
      .then(setOv)
      .catch((err) => setError(errorMessage(err)));
  }, [contextName]);
  useEffect(load, [load]);

  if (!active) return <p className="text-[13px] text-muted">Connect to a cluster first.</p>;

  const act = async (a: GitOpsApp, action: string) => {
    setError(null);
    setMessage(null);
    try {
      setPlan(await GitOpsService.PrepareAction(contextName, a.kind, a.namespace, a.name, action));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const apps = (ov?.apps ?? []).filter((a) => !namespace || a.namespace === namespace || a.destination.endsWith(`/ ${namespace}`) || a.destination === namespace);
  return (
    <div className="max-w-5xl">
      <PageHeader
        title="GitOps"
        description="Argo CD and Flux apply what's in Git and revert live edits. Here you see whether each app matches Git, and can ask the tool to sync or reconcile. Change what they deploy in the repository."
        actions={
          <Button icon={<RefreshCw className="size-4" />} onClick={load}>
            Refresh
          </Button>
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {message && <Notice tone="good">{message}</Notice>}
      {ov && (ov.installed ?? []).length === 0 && <p className="text-[13px] text-muted">Neither Argo CD nor Flux is installed on this cluster.</p>}
      {ov && (ov.skipped ?? []).length > 0 && <p className="mb-2 text-[12px] text-muted">Couldn't read: {(ov.skipped ?? []).join(", ")}.</p>}
      {ov && (ov.installed ?? []).length > 0 && apps.length === 0 && <p className="text-[13px] text-muted">No apps{namespace ? ` for ${namespace}` : ""}.</p>}
      <ul className="mt-2 flex flex-col gap-2">
        {apps.map((a) => (
          <li key={`${a.kind}/${a.namespace}/${a.name}`} className="rounded-lg border border-line bg-panel px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <GitBranch className="size-4 text-muted" aria-hidden />
              <span className="text-[14px] font-medium">{a.name}</span>
              <span className="text-[12px] text-muted">
                {a.tool} {a.kind} · {a.namespace}
              </span>
              {a.sync && <Badge className={syncTone(a.sync)}>{a.sync === "OutOfSync" ? "Out of sync" : a.sync === "NotReady" ? "Not ready" : a.sync}</Badge>}
              {a.health && a.tool === "Argo CD" && <Badge className={syncTone(a.health)}>{a.health}</Badge>}
              {a.operation && a.operation !== "Succeeded" && <Badge className={syncTone(a.operation)}>Last operation {a.operation.toLowerCase()}</Badge>}
              {a.suspended && <Badge className="bg-warn/15 text-warn">Suspended</Badge>}
              <div className="ml-auto flex gap-1">
                {a.repoUrl && (
                  <Button variant="ghost" icon={<ExternalLink className="size-4" />} title={a.repoUrl} onClick={() => void Browser.OpenURL(a.repoUrl)}>
                    Repository
                  </Button>
                )}
                {a.tool === "Argo CD" ? (
                  <>
                    <Button variant="ghost" icon={<RefreshCw className="size-4" />} onClick={() => void act(a, "refresh")}>
                      Refresh
                    </Button>
                    <Button variant="ghost" icon={<RotateCw className="size-4" />} onClick={() => void act(a, "sync")}>
                      Sync
                    </Button>
                  </>
                ) : (
                  <>
                    <Button variant="ghost" icon={<RotateCw className="size-4" />} disabled={a.suspended} onClick={() => void act(a, "reconcile")}>
                      Reconcile
                    </Button>
                    {a.suspended ? (
                      <Button variant="ghost" icon={<Play className="size-4" />} onClick={() => void act(a, "resume")}>
                        Resume
                      </Button>
                    ) : (
                      <Button variant="ghost" icon={<Pause className="size-4" />} onClick={() => void act(a, "suspend")}>
                        Suspend
                      </Button>
                    )}
                  </>
                )}
              </div>
            </div>
            <dl className="mt-2 grid grid-cols-[8rem_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-[12px]">
              {a.source && (
                <>
                  <dt className="text-muted">Source</dt>
                  <dd className="break-all">{a.source}</dd>
                </>
              )}
              {(a.target || a.revision) && (
                <>
                  <dt className="text-muted">Revision</dt>
                  <dd className="break-all">
                    {a.target && a.target !== a.revision ? `${a.target} → ` : ""}
                    {a.revision || "not applied yet"}
                  </dd>
                </>
              )}
              {a.destination && (
                <>
                  <dt className="text-muted">Deploys to</dt>
                  <dd className="break-all">{a.destination}</dd>
                </>
              )}
              {a.resources > 0 && (
                <>
                  <dt className="text-muted">Resources</dt>
                  <dd>
                    {a.resources}
                    {a.outOfSync > 0 && <span className="text-warn"> · {a.outOfSync} out of sync</span>}
                  </dd>
                </>
              )}
              {a.lastSync && (
                <>
                  <dt className="text-muted">Last sync</dt>
                  <dd>{age(a.lastSync)} ago{a.autoSync ? " · syncs automatically" : " · manual sync"}</dd>
                </>
              )}
            </dl>
            {a.message && <p className="mt-1.5 text-[12px] break-words text-muted">{a.message}</p>}
          </li>
        ))}
      </ul>
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={(res) => {
          setPlan(null);
          setMessage(res.message);
          window.setTimeout(load, 1500);
        }}
      />
    </div>
  );
}
