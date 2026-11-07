import { useCallback, useEffect, useMemo, useState } from "react";
import { Events } from "@wailsio/runtime";
import { Download, PackagePlus, Rocket, Search, ShieldAlert, Star, Trash2, Upload, X } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { ImageSecurity, RegistryLogins } from "../components/ImageSecurity";
import { Badge, Button, Field, Notice, PageHeader, inputClass } from "../components/ui";
import {
  ImageService,
  KupiPull,
  errorMessage,
  type DiskUsage,
  type EngineInfo,
  type ImageDetailView,
  type ImageView,
  type PullEvent,
  type SearchView,
  type TagView,
} from "../lib/api";
import { age } from "../lib/format";
import { isDockerHub } from "../lib/imageref";
import { mergePull } from "../lib/pulls";
import { formatBytes } from "../lib/units";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";

type Tab = "local" | "find" | "disk" | "logins";
/** A local image with its tag list normalised to an array. */
type Img = ImageView & { tags: string[] };

export function ImagesPage() {
  const [tab, setTab] = useState<Tab>("local");
  const [engine, setEngine] = useState<EngineInfo | null>(null);
  const [pulls, setPulls] = useState<Record<string, PullEvent>>({});
  const [reload, setReload] = useState(0);

  useEffect(() => {
    void ImageService.Engine().then(setEngine);
  }, []);

  useEffect(() => {
    const off = Events.On(KupiPull, (event: { data: unknown }) => {
      const p = event.data as PullEvent;
      setPulls((cur) => mergePull(cur, p));
      if (p.done) setReload((n) => n + 1);
    });
    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  return (
    <div>
      <PageHeader
        title="Images"
        description="Container images on this computer, and images you can download. Local images can be loaded straight into a local cluster, no registry needed."
      />
      {engine && !engine.available && <Notice tone="warn">{engine.reason}. Start Docker Desktop, Rancher Desktop or Podman.</Notice>}
      {engine?.available && (
        <p className="mb-3 text-[12px] text-muted">
          {engine.kind} {engine.version} · {engine.endpoint}
        </p>
      )}
      <div role="tablist" className="mb-4 flex gap-1 border-b border-line">
        {(
          [
            ["local", "On this computer"],
            ["find", "Find images"],
            ["disk", "Disk space"],
            ["logins", "Registry logins"],
          ] as [Tab, string][]
        ).map(([t, label]) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`border-b-2 px-3 py-2 text-[13px] ${tab === t ? "border-accent font-medium" : "border-transparent text-muted hover:text-fg"}`}
          >
            {label}
          </button>
        ))}
      </div>
      <PullStatus
        pulls={pulls}
        onDismiss={(id) =>
          setPulls((cur) => {
            const next = { ...cur };
            delete next[id];
            return next;
          })
        }
      />
      {engine?.available && tab === "local" && <LocalImages reload={reload} />}
      {engine?.available && tab === "find" && <FindImages />}
      {engine?.available && tab === "disk" && <DiskSpace />}
      {tab === "logins" && <RegistryLogins />}
    </div>
  );
}

function PullStatus({ pulls, onDismiss }: { pulls: Record<string, PullEvent>; onDismiss: (id: string) => void }) {
  const list = Object.values(pulls);
  if (list.length === 0) return null;
  const dismiss = (id: string) => (
    <button aria-label="Dismiss" className="ml-auto shrink-0 rounded p-0.5 text-muted hover:text-fg" onClick={() => onDismiss(id)}>
      <X className="size-3.5" />
    </button>
  );
  return (
    <div className="mb-3 flex flex-col gap-2">
      {list.map((p) =>
        p.done ? (
          <Notice key={p.id} tone={p.error ? "bad" : "good"}>
            <div className="flex items-start gap-2">
              <span className="min-w-0 break-words">{p.error ? `Pulling ${p.ref} failed: ${p.error}` : `Pulled ${p.ref}.`}</span>
              {dismiss(p.id)}
            </div>
          </Notice>
        ) : (
          <div key={p.id} className="rounded-md border border-line bg-panel px-3 py-2 text-[13px]">
            <div className="flex justify-between">
              <span>Pulling {p.ref}</span>
              <span className="text-muted">{p.percent}%</span>
            </div>
            <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-panel-2">
              <div className="h-full bg-accent transition-all" style={{ width: `${p.percent}%` }} />
            </div>
          </div>
        ),
      )}
    </div>
  );
}

function LocalImages({ reload }: { reload: number }) {
  const { active } = useClusters();
  const { navigate } = useNav();
  const [images, setImages] = useState<Img[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: "good" | "bad"; text: string } | null>(null);
  const [details, setDetails] = useState<string | null>(null);
  const [removing, setRemoving] = useState<Img | null>(null);
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const canLoad = !!active && active.profile === "local" && /^(k3d-|kind-|minikube)/.test(active.name);

  const load = useCallback(() => {
    ImageService.Images()
      .then((l) => setImages((l ?? []).map((i) => ({ ...i, tags: i.tags ?? [] }))))
      .catch((err) => setError(errorMessage(err)));
  }, []);
  useEffect(load, [load, reload]);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return (images ?? []).filter((i) => !q || i.tags.some((t) => t.toLowerCase().includes(q)) || i.id.includes(q));
  }, [images, filter]);

  const loadIntoCluster = async (ref: string) => {
    if (!active) return;
    setBusy(ref);
    setNotice(null);
    try {
      await ImageService.LoadIntoCluster(active.name, ref);
      setNotice({ tone: "good", text: `Loaded ${ref} into ${active.name}. Pods there can now use it without a registry; set imagePullPolicy to IfNotPresent.` });
    } catch (err) {
      setNotice({ tone: "bad", text: errorMessage(err) });
    } finally {
      setBusy(null);
    }
  };

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!images) return <p className="text-[13px] text-muted">Loading…</p>;
  return (
    <div className="flex flex-col gap-3">
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      <input aria-label="Filter images" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter by name" className={`${inputClass} w-64`} />
      <div className="overflow-hidden rounded-lg border border-line">
        <table className="w-full table-fixed text-left text-[13px]">
          <thead className="bg-panel-2 text-[12px] text-muted">
            <tr>
              <th className="px-3 py-2 font-medium">Image</th>
              <th className="w-24 px-3 py-2 font-medium">Size</th>
              <th className="w-24 px-3 py-2 font-medium">Created</th>
              <th className="w-[340px] px-3 py-2 font-medium" />
            </tr>
          </thead>
          <tbody>
            {shown.map((img) => {
              const ref = img.tags[0] ?? img.id;
              return (
                <tr key={img.id} className="border-t border-line">
                  <td className="truncate px-3 py-2" title={img.tags.join(", ") || img.id}>
                    {img.dangling ? <span className="text-muted">untagged {img.id.slice(7, 19)}</span> : img.tags.join(", ")}
                    {img.containers > 0 && <Badge className="ml-2 bg-panel-2 text-muted">in use</Badge>}
                  </td>
                  <td className="px-3 py-2">{formatBytes(img.size)}</td>
                  <td className="px-3 py-2 text-muted">{age(img.created)}</td>
                  <td className="px-3 py-1.5">
                    <div className="flex justify-end gap-1">
                      <Button variant="ghost" aria-label={`Details of ${ref}`} onClick={() => setDetails(ref)}>
                        Details
                      </Button>
                      {canLoad && !img.dangling && (
                        <Button
                          variant="ghost"
                          aria-label={`Load into cluster: ${ref}`}
                          busy={busy === ref}
                          icon={<Upload className="size-4" />}
                          onClick={() => void loadIntoCluster(ref)}
                          title={`Copy into ${active?.name}`}
                        >
                          Load into cluster
                        </Button>
                      )}
                      {!img.dangling && active && (
                        <Button variant="ghost" aria-label={`Deploy ${ref}`} icon={<Rocket className="size-4" />} onClick={() => navigate({ page: "apply", fromImage: ref })}>
                          Deploy
                        </Button>
                      )}
                      <Button variant="ghost" aria-label={`Remove ${ref}`} icon={<Trash2 className="size-4" />} onClick={() => setRemoving(img)} />
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {details && <ImageDetails refName={details} onClose={() => setDetails(null)} />}
      {removing && (
        <RemoveDialog
          image={removing}
          onClose={() => setRemoving(null)}
          onRemoved={() => {
            setRemoving(null);
            load();
          }}
        />
      )}
    </div>
  );
}

function ImageDetails({ refName, onClose }: { refName: string; onClose: () => void }) {
  const [d, setD] = useState<ImageDetailView | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    ImageService.Inspect(refName)
      .then(setD)
      .catch((err) => setError(errorMessage(err)));
  }, [refName]);
  const biggest = Math.max(1, ...(d?.layers ?? []).map((l) => l.size));
  return (
    <Dialog open wide onOpenChange={(o) => !o && onClose()} title={refName} description={d ? `${d.platform} · ${formatBytes(d.size)}` : undefined}>
      {error && <Notice tone="bad">{error}</Notice>}
      {d && (
        <div className="flex flex-col gap-4 text-[13px]">
          {d.runsAsRoot && (
            <Notice tone="warn" icon={<ShieldAlert className="size-4 text-warn" />}>
              This image runs as root by default. Set runAsNonRoot or a non-zero runAsUser when you deploy it, if the app allows.
            </Notice>
          )}
          <dl className="grid grid-cols-[130px_1fr] gap-x-3 gap-y-1.5">
            <dt className="text-muted">User</dt>
            <dd>{d.user || "root (not set)"}</dd>
            <dt className="text-muted">Entrypoint</dt>
            <dd className="font-mono text-[12px] break-all">{[...(d.entrypoint ?? []), ...(d.cmd ?? [])].join(" ") || "—"}</dd>
            <dt className="text-muted">Ports</dt>
            <dd>{(d.ports ?? []).join(", ") || "none declared"}</dd>
            <dt className="text-muted">Environment</dt>
            <dd className="font-mono text-[12px] break-all" title="Names only; values can hold secrets baked into the image">
              {(d.envNames ?? []).join(", ") || "none"}
            </dd>
            <dt className="text-muted">Digest</dt>
            <dd className="font-mono text-[12px] break-all">{(d.digests ?? [])[0] ?? "not pushed or pulled by digest"}</dd>
          </dl>
          <section>
            <h3 className="mb-1.5 text-[12px] font-semibold text-muted">Layers, newest first</h3>
            <ul className="flex flex-col gap-1">
              {(d.layers ?? []).map((l, i) => (
                <li key={i} className="grid grid-cols-[80px_1fr] items-center gap-2">
                  <div className="h-2 overflow-hidden rounded-full bg-panel-2" title={formatBytes(l.size)}>
                    <div className="h-full bg-accent" style={{ width: `${Math.max(2, (l.size / biggest) * 100)}%`, opacity: l.size === 0 ? 0.2 : 1 }} />
                  </div>
                  <div className="flex min-w-0 gap-2 font-mono text-[12px]">
                    <span className="w-14 shrink-0 text-right text-muted">{formatBytes(l.size)}</span>
                    <span className="truncate" title={l.createdBy}>
                      {l.createdBy || "(no command)"}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          </section>
          <ImageSecurity refName={refName} />
        </div>
      )}
    </Dialog>
  );
}

function RemoveDialog({ image, onClose, onRemoved }: { image: Img; onClose: () => void; onRemoved: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = image.tags[0] ?? image.id;
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Remove ${ref}?`}
      description="It's only removed from this computer; registries and clusters keep their copies."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="danger"
            busy={busy}
            onClick={() => {
              setBusy(true);
              ImageService.Remove(ref, false)
                .then(onRemoved)
                .catch((err) => setError(errorMessage(err)))
                .finally(() => setBusy(false));
            }}
          >
            Remove
          </Button>
        </>
      }
    >
      {image.containers > 0 && <Notice tone="warn">A container uses this image; the engine will refuse until it's removed.</Notice>}
      {error && <Notice tone="bad">{error}</Notice>}
    </Dialog>
  );
}

function FindImages() {
  const [term, setTerm] = useState("");
  const [results, setResults] = useState<SearchView[] | null>(null);
  const [repo, setRepo] = useState<string | null>(null);
  const [tags, setTags] = useState<TagView[] | null>(null);
  const [manual, setManual] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const search = async () => {
    setBusy(true);
    setError(null);
    setRepo(null);
    try {
      setResults((await ImageService.Search(term)) ?? []);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  const openTags = async (name: string) => {
    setRepo(name);
    setTags(null);
    try {
      setTags((await ImageService.Tags(name)) ?? []);
    } catch (err) {
      setError(errorMessage(err));
    }
  };
  const pull = (ref: string) => void ImageService.Pull(ref).catch((err) => setError(errorMessage(err)));

  return (
    <div className="flex flex-col gap-4">
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void search();
        }}
      >
        <input aria-label="Search Docker Hub" value={term} onChange={(e) => setTerm(e.target.value)} placeholder="Search Docker Hub, e.g. postgres" className={`${inputClass} w-80`} />
        <Button type="submit" variant="primary" busy={busy} icon={<Search className="size-4" />}>
          Search
        </Button>
      </form>
      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (manual.trim()) pull(manual.trim());
        }}
      >
        <Field label="Or pull any reference" hint="Any registry, e.g. ghcr.io/org/app:1.2. Pin a version or digest rather than latest.">
          <input aria-label="Image reference" value={manual} onChange={(e) => setManual(e.target.value)} placeholder="ghcr.io/org/app:1.2" className={`${inputClass} w-80`} />
        </Field>
        <Button type="submit" icon={<Download className="size-4" />}>
          Pull
        </Button>
      </form>
      {error && <Notice tone="bad">{error}</Notice>}
      {results && results.length === 0 && <p className="text-[13px] text-muted">No results.</p>}
      {results && results.length > 0 && (
        <ul className="flex flex-col gap-2">
          {results.map((r) => (
            <li key={r.name} className="rounded-lg border border-line bg-panel px-4 py-2.5">
              <div className="flex items-center gap-2">
                <span className="font-medium">{r.name}</span>
                {r.official && <Badge className="bg-good/15 text-good">Official</Badge>}
                <span className="flex items-center gap-0.5 text-[12px] text-muted">
                  <Star className="size-3" aria-hidden /> {r.stars}
                </span>
                {isDockerHub(r.name) && (
                  <Button variant="ghost" className="ml-auto" onClick={() => void openTags(r.name)}>
                    Versions
                  </Button>
                )}
              </div>
              {r.description && <p className="mt-0.5 text-[12px] text-muted">{r.description}</p>}
              {r.looksLike && (
                <p className="mt-1 text-[12px] text-warn">
                  Looks similar to the official image "{r.looksLike}". Check it's the one you want before pulling.
                </p>
              )}
              {repo === r.name && (
                <div className="mt-2 border-t border-line pt-2">
                  {!tags && <p className="text-[12px] text-muted">Loading versions…</p>}
                  {tags && (
                    <div className="flex flex-wrap gap-1.5">
                      {tags.map((t) => (
                        <button
                          key={t.name}
                          onClick={() => pull(`${r.name}:${t.name}`)}
                          className="flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[12px] hover:border-accent"
                          title={`Pull ${r.name}:${t.name}${t.size ? ` (${formatBytes(t.size)})` : ""}, updated ${age(t.updated)} ago`}
                        >
                          <PackagePlus className="size-3.5" aria-hidden /> {t.name}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function DiskSpace() {
  const [usage, setUsage] = useState<DiskUsage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const [busy, setBusy] = useState(false);
  const load = useCallback(() => {
    ImageService.DiskUsage()
      .then(setUsage)
      .catch((err) => setError(errorMessage(err)));
  }, []);
  useEffect(load, [load]);

  const run = async (fn: () => Promise<number>, what: string) => {
    setBusy(true);
    setNotice(null);
    try {
      const freed = await fn();
      setNotice(`Freed ${formatBytes(freed)} of ${what}.`);
      load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!usage) return <p className="text-[13px] text-muted">Loading…</p>;
  const cards: [string, number][] = [
    ["Images", usage.images],
    ["Unused images", usage.imagesReclaimable],
    ["Build cache", usage.buildCache],
    ["Volumes", usage.volumes],
    ["Containers", usage.containers],
  ];
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        {cards.map(([label, value]) => (
          <div key={label} className="rounded-lg bg-panel-2 px-4 py-3">
            <div className="text-[12px] text-muted">{label}</div>
            <div className="text-lg font-semibold">{formatBytes(value)}</div>
          </div>
        ))}
      </div>
      {notice && <Notice tone="good">{notice}</Notice>}
      <div className="flex flex-wrap gap-2">
        <Button busy={busy} onClick={() => void run(() => ImageService.Prune(false), "untagged images")}>
          Remove untagged images
        </Button>
        <Button busy={busy} onClick={() => void run(() => ImageService.PruneBuildCache(), "build cache")}>
          Clear build cache
        </Button>
        <Button variant="danger" busy={busy} onClick={() => setConfirmAll(true)}>
          Remove all unused images
        </Button>
      </div>
      <Dialog
        open={confirmAll}
        onOpenChange={setConfirmAll}
        title="Remove every image no container uses?"
        description="You'll need to pull or build them again later. Volumes are never touched."
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmAll(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmAll(false);
                void run(() => ImageService.Prune(true), "unused images");
              }}
            >
              Remove unused images
            </Button>
          </>
        }
      >
        <p className="text-[13px]">Up to {formatBytes(usage.imagesReclaimable)} can be freed.</p>
      </Dialog>
    </div>
  );
}
