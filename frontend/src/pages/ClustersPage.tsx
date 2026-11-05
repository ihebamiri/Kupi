import { useState } from "react";
import { KeyRound, Plug, RefreshCw, ShieldAlert, ShieldCheck, TriangleAlert, Unplug } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { Badge, Button, Notice, PageHeader } from "../components/ui";
import { useClusters } from "../state/clusters";
import { errorMessage, type ContextInfo } from "../lib/api";
import { PROFILE_KINDS, profileMeta } from "../lib/profiles";

export function ClustersPage({ onOpened }: { onOpened: () => void }) {
  const { contexts, loading, error, refresh } = useClusters();
  const [trusting, setTrusting] = useState<ContextInfo | null>(null);

  return (
    <div>
      <PageHeader
        title="Clusters"
        description="Contexts from your kubeconfig. Kupi reads it the same way kubectl does and never edits it."
        actions={
          <Button icon={<RefreshCw className="size-4" />} onClick={() => void refresh()}>
            Reload
          </Button>
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {!loading && contexts.length === 0 && !error && (
        <Notice>
          No contexts found. Kupi looks in the KUBECONFIG environment variable, then in ~/.kube/config.
        </Notice>
      )}
      <ul className="flex flex-col gap-2">
        {contexts.map((c) => (
          <ContextRow key={c.name} ctx={c} onTrust={() => setTrusting(c)} onOpened={onOpened} />
        ))}
      </ul>
      <TrustDialog ctx={trusting} onClose={() => setTrusting(null)} onTrusted={onOpened} />
    </div>
  );
}

function ContextRow({ ctx, onTrust, onOpened }: { ctx: ContextInfo; onTrust: () => void; onOpened: () => void }) {
  const { connect, disconnect, setProfile, setActive } = useClusters();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const meta = profileMeta(ctx.profile);
  const blocked = ctx.needsTrust && !ctx.trusted;

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <li className="rounded-lg border border-line bg-panel px-4 py-3">
      <div className="flex items-center gap-3">
        <span className={`size-2 shrink-0 rounded-full ${ctx.connected ? "bg-good" : "bg-line"}`} aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium">{ctx.name}</span>
            {ctx.current && <Badge className="bg-panel-2 text-muted">kubectl current</Badge>}
            {ctx.connected && <Badge className="bg-good/15 text-good">Connected · {ctx.serverVersion}</Badge>}
          </div>
          <div className="mt-0.5 truncate font-mono text-[12px] text-muted">{ctx.server || "no server address"}</div>
        </div>

        <label className="flex items-center gap-1.5 text-[12px] text-muted">
          <span className="sr-only">Profile for {ctx.name}</span>
          <select
            className={`rounded-md border border-line bg-panel-2 px-1.5 py-1 text-[12px] font-medium ${meta.badge.split(" ")[1]}`}
            value={ctx.profileOverridden ? ctx.profile : ""}
            title={meta.description}
            disabled={busy}
            onChange={(e) => void run(() => setProfile(ctx.name, e.target.value))}
          >
            <option value="">Auto: {profileMeta(ctx.detectedProfile).label}</option>
            {PROFILE_KINDS.map((k) => (
              <option key={k} value={k}>
                {profileMeta(k).label}
              </option>
            ))}
          </select>
        </label>

        {ctx.connected ? (
          <>
            <Button
              variant="primary"
              onClick={() => {
                setActive(ctx.name);
                onOpened();
              }}
            >
              Open
            </Button>
            <Button variant="ghost" icon={<Unplug className="size-4" />} busy={busy} onClick={() => void run(() => disconnect(ctx.name))}>
              Disconnect
            </Button>
          </>
        ) : blocked ? (
          <Button icon={<KeyRound className="size-4" />} onClick={onTrust}>
            Review and connect
          </Button>
        ) : (
          <Button
            variant="primary"
            icon={<Plug className="size-4" />}
            busy={busy}
            onClick={() =>
              void run(async () => {
                await connect(ctx.name);
                onOpened();
              })
            }
          >
            Connect
          </Button>
        )}
      </div>

      {(ctx.needsTrust || ctx.insecureSkipTLSVerify) && (
        <div className="mt-2 flex flex-wrap gap-2 pl-5">
          {ctx.needsTrust &&
            (ctx.trusted ? (
              <Badge className="bg-panel-2 text-muted" title={ctx.file}>
                <ShieldCheck className="size-3" aria-hidden /> Credential plugin trusted: {ctx.execCommand}
              </Badge>
            ) : (
              <Badge className="bg-warn/15 text-warn" title={ctx.file}>
                <ShieldAlert className="size-3" aria-hidden /> Runs {ctx.execCommand} to log in; needs your review
              </Badge>
            ))}
          {ctx.insecureSkipTLSVerify && (
            <Badge className="bg-bad/15 text-bad">
              <TriangleAlert className="size-3" aria-hidden /> TLS certificate not verified
            </Badge>
          )}
        </div>
      )}
      {error && (
        <div className="mt-2 pl-5">
          <Notice tone="bad">{error}</Notice>
        </div>
      )}
    </li>
  );
}

function TrustDialog({ ctx, onClose, onTrusted }: { ctx: ContextInfo | null; onClose: () => void; onTrusted: () => void }) {
  const { trust, connect } = useClusters();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!ctx) return null;

  const accept = async () => {
    setBusy(true);
    setError(null);
    try {
      await trust(ctx.file);
      await connect(ctx.name);
      onClose();
      onTrusted();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => !open && onClose()}
      title="This kubeconfig runs a program to log in"
      description="Only trust kubeconfig files from a source you trust. A credential plugin can run any command on this computer."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" busy={busy} onClick={() => void accept()}>
            Trust this file and connect
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-[13px]">
        <div>
          <div className="mb-1 text-[12px] font-medium text-muted">Command</div>
          <pre className="overflow-x-auto rounded-md border border-line bg-bg px-3 py-2 font-mono text-[12px] whitespace-pre-wrap break-all">
            {[ctx.execCommand, ...(ctx.execArgs ?? [])].join(" ")}
          </pre>
        </div>
        {ctx.execEnvNames && ctx.execEnvNames.length > 0 && (
          <div>
            <div className="mb-1 text-[12px] font-medium text-muted">Sets environment variables</div>
            <div className="font-mono text-[12px]">{ctx.execEnvNames.join(", ")}</div>
          </div>
        )}
        <div>
          <div className="mb-1 text-[12px] font-medium text-muted">From file</div>
          <div className="font-mono text-[12px] break-all">{ctx.file}</div>
        </div>
        <Notice>Kupi will ask again if this file changes.</Notice>
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}
