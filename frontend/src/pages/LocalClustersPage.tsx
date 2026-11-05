import { useCallback, useEffect, useState } from "react";
import { Play, Plus, Square, Trash2 } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { Badge, Button, Field, Notice, PageHeader, inputClass } from "../components/ui";
import { LocalClusterService, errorMessage, type LocalCluster, type Tool } from "../lib/api";
import { useClusters } from "../state/clusters";

/** Create, start, stop and delete k3d clusters on this computer. */
export function LocalClustersPage() {
  const { refresh, connect, setActive } = useClusters();
  const [tools, setTools] = useState<Tool[] | null>(null);
  const [clusters, setClusters] = useState<LocalCluster[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<LocalCluster | null>(null);
  const hasK3d = (tools ?? []).some((t) => t.name === "k3d");

  const load = useCallback(async () => {
    try {
      const t = (await LocalClusterService.Tools()) ?? [];
      setTools(t);
      if (t.some((x) => x.name === "k3d")) setClusters((await LocalClusterService.List()) ?? []);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const act = async (name: string, fn: () => Promise<string>) => {
    setBusy(name);
    setError(null);
    try {
      await fn();
      await load();
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div>
      <PageHeader
        title="Local clusters"
        description="Kubernetes clusters running in Docker on this computer, made with k3d. The API listens on 127.0.0.1 only, and creating one never switches your kubectl context."
        actions={
          hasK3d && (
            <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
              New cluster
            </Button>
          )
        }
      />
      {tools && (
        <p className="mb-3 text-[12px] text-muted">
          Tools found: {tools.length > 0 ? tools.map((t) => t.name).join(", ") : "none"}
        </p>
      )}
      {tools && !hasK3d && (
        <Notice tone="warn">
          k3d isn't installed. Install it (on Windows: winget install k3d.k3d) and make sure Docker is running, then reload this page.
        </Notice>
      )}
      {error && <Notice tone="bad">{error}</Notice>}
      {clusters && clusters.length === 0 && <p className="text-[13px] text-muted">No k3d clusters yet.</p>}
      <ul className="flex flex-col gap-2">
        {(clusters ?? []).map((c) => (
          <li key={c.name} className="flex items-center gap-3 rounded-lg border border-line bg-panel px-4 py-3">
            <span className={`size-2 rounded-full ${c.running ? "bg-good" : "bg-line"}`} aria-hidden />
            <div className="min-w-0 flex-1">
              <div className="font-medium">{c.name}</div>
              <div className="text-[12px] text-muted">
                context {c.context} · {c.servers} server{c.servers === 1 ? "" : "s"}, {c.agents} agent{c.agents === 1 ? "" : "s"}
              </div>
            </div>
            <Badge className={c.running ? "bg-good/15 text-good" : "bg-panel-2 text-muted"}>{c.running ? "Running" : "Stopped"}</Badge>
            {c.running ? (
              <>
                <Button
                  variant="primary"
                  onClick={() =>
                    void connect(c.context)
                      .then(() => setActive(c.context))
                      .catch((err) => setError(errorMessage(err)))
                  }
                >
                  Connect
                </Button>
                <Button variant="ghost" busy={busy === c.name} icon={<Square className="size-4" />} onClick={() => void act(c.name, () => LocalClusterService.Stop(c.name))}>
                  Stop
                </Button>
              </>
            ) : (
              <Button busy={busy === c.name} icon={<Play className="size-4" />} onClick={() => void act(c.name, () => LocalClusterService.Start(c.name))}>
                Start
              </Button>
            )}
            <Button variant="ghost" aria-label={`Delete ${c.name}`} icon={<Trash2 className="size-4" />} onClick={() => setDeleting(c)} />
          </li>
        ))}
      </ul>
      {creating && (
        <CreateDialog
          onClose={() => setCreating(false)}
          onCreated={async () => {
            setCreating(false);
            await load();
            await refresh();
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          cluster={deleting}
          onClose={() => setDeleting(null)}
          onDeleted={async () => {
            setDeleting(null);
            await load();
            await refresh();
          }}
        />
      )}
    </div>
  );
}

function CreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => Promise<void> }) {
  const [name, setName] = useState("dev");
  const [agents, setAgents] = useState("0");
  const [image, setImage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !busy && onClose()}
      title="New local cluster"
      description="Takes about 30 seconds the first time; k3d downloads the k3s image once."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            variant="primary"
            busy={busy}
            onClick={() => {
              setBusy(true);
              setError(null);
              LocalClusterService.Create({ name: name.trim(), agents: Number(agents) || 0, image: image.trim() })
                .then(() => onCreated())
                .catch((err) => setError(errorMessage(err)))
                .finally(() => setBusy(false));
            }}
          >
            Create cluster
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label="Name" hint="Lowercase letters, digits and dashes. The kubectl context will be k3d-<name>.">
          <input aria-label="Cluster name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Worker nodes" hint="0 runs everything on one node, the lightest option.">
          <select aria-label="Worker nodes" className={inputClass} value={agents} onChange={(e) => setAgents(e.target.value)}>
            {[0, 1, 2, 3].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Kubernetes version (optional)" hint="A k3s image such as rancher/k3s:v1.35.5-k3s1. Empty uses k3d's default.">
          <input aria-label="k3s image" className={inputClass} value={image} onChange={(e) => setImage(e.target.value)} placeholder="rancher/k3s:v1.35.5-k3s1" />
        </Field>
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}

function DeleteDialog({ cluster, onClose, onDeleted }: { cluster: LocalCluster; onClose: () => void; onDeleted: () => Promise<void> }) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !busy && onClose()}
      title={`Delete ${cluster.name}?`}
      description="The cluster and everything in it are deleted, including volumes. This can't be undone."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            variant="danger"
            busy={busy}
            onClick={() => {
              if (typed !== cluster.name) {
                setError(`Type ${cluster.name} to confirm.`);
                return;
              }
              setBusy(true);
              LocalClusterService.Delete(cluster.name)
                .then(() => onDeleted())
                .catch((err) => setError(errorMessage(err)))
                .finally(() => setBusy(false));
            }}
          >
            Delete cluster
          </Button>
        </>
      }
    >
      <Field label={`Type ${cluster.name} to confirm`}>
        <input aria-label="Confirm cluster name" className={`${inputClass} font-mono`} value={typed} onChange={(e) => setTyped(e.target.value)} />
      </Field>
      {error && (
        <div className="mt-3">
          <Notice tone="bad">{error}</Notice>
        </div>
      )}
    </Dialog>
  );
}
