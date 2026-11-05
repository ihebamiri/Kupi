import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ClipboardCopy, Eye, EyeOff, FileKey2, KeyRound, Lock, Pencil, Plus, RefreshCw, RotateCcw, ScanSearch, ShieldAlert, TriangleAlert } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { ReviewDialog } from "../components/ReviewDialog";
import { KeyValueEditor, MaskedValue, ReasonDialog, secretInputProps } from "../components/SecretFields";
import { Badge, Button, Field, Notice, PageHeader, inputClass } from "../components/ui";
import {
  GuardService,
  ResourceService,
  SecretService,
  errorMessage,
  type CommitResult,
  type Finding,
  type PlanView,
  type ScanResult,
  type SecretUsage,
  type SecretView,
  type TableView,
  type TLSInfo,
} from "../lib/api";
import { age } from "../lib/format";
import { collect, hasChanges, newRow, rowsFromKeys, type KVRow } from "../lib/kvrows";
import { useClusters } from "../state/clusters";

const SECRET_TYPE = { group: "", version: "v1", resource: "secrets", namespaced: true };

const TYPES: { value: string; label: string }[] = [
  { value: "Opaque", label: "Opaque (any keys and values)" },
  { value: "kubernetes.io/tls", label: "TLS certificate and key" },
  { value: "kubernetes.io/dockerconfigjson", label: "Registry login for image pulls" },
  { value: "kubernetes.io/basic-auth", label: "Basic auth (username and password)" },
  { value: "kubernetes.io/ssh-auth", label: "SSH private key" },
];

const TYPE_SHORT: Record<string, string> = {
  Opaque: "Opaque",
  "kubernetes.io/tls": "TLS",
  "kubernetes.io/dockerconfigjson": "Registry login",
  "kubernetes.io/dockercfg": "Registry login (old format)",
  "kubernetes.io/basic-auth": "Basic auth",
  "kubernetes.io/ssh-auth": "SSH key",
  "kubernetes.io/service-account-token": "Service account token",
  "helm.sh/release.v1": "Helm release",
};

interface Selected {
  namespace: string;
  name: string;
}

/** Secrets: keys and who uses them, values only on request, edits through the Guard. */
export function SecretsPage({ initial }: { initial?: Selected }) {
  const { active, namespace } = useClusters();
  const [tab, setTab] = useState<"secrets" | "scan">("secrets");
  const [table, setTable] = useState<TableView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Selected | null>(initial?.name ? { namespace: initial.namespace ?? "", name: initial.name } : null);
  const [creating, setCreating] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const contextName = active?.name ?? "";

  const load = useCallback(() => {
    if (!contextName) return;
    setError(null);
    ResourceService.List(contextName, SECRET_TYPE, namespace, "")
      .then(setTable)
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, namespace]);

  useEffect(load, [load]);

  const rows = useMemo(() => {
    if (!table) return [];
    const col = (name: string) => (table.columns ?? []).findIndex((c) => c.name.toLowerCase() === name);
    const typeCol = col("type");
    const dataCol = col("data");
    const q = filter.trim().toLowerCase();
    return (table.rows ?? [])
      .map((r) => ({ name: r.name, namespace: r.namespace, type: (r.cells ?? [])[typeCol] ?? "", keys: (r.cells ?? [])[dataCol] ?? "", created: r.created }))
      .filter((r) => !q || r.name.toLowerCase().includes(q) || r.type.toLowerCase().includes(q));
  }, [table, filter]);

  if (!active) return <p className="text-[13px] text-muted">Connect to a cluster to see its Secrets.</p>;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title="Secrets"
        description="Values stay hidden. Reveal or copy one key at a time; on production each reveal needs a reason and is written to the audit log. Copied values are cleared from the clipboard after 30 seconds."
        actions={
          <>
            <div role="tablist" aria-label="Secrets view" className="flex rounded-md border border-line p-0.5">
              {(["secrets", "scan"] as const).map((t) => (
                <button key={t} role="tab" aria-selected={tab === t} onClick={() => setTab(t)} className={`rounded px-3 py-1 text-[13px] ${tab === t ? "bg-panel-2 font-medium text-fg" : "text-muted hover:text-fg"}`}>
                  {t === "secrets" ? "Secrets" : "Exposure scan"}
                </button>
              ))}
            </div>
            {tab === "secrets" && (
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
                New Secret
              </Button>
            )}
          </>
        }
      />
      {tab === "scan" ? (
        <ExposureScan contextName={contextName} namespace={namespace} />
      ) : (
        <div className="grid min-h-0 flex-1 grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)] gap-4">
          <section aria-label="Secret list" className="flex min-h-0 flex-col rounded-lg border border-line bg-panel">
            <div className="flex gap-2 border-b border-line p-2">
              <input aria-label="Filter Secrets" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter by name or type" className={`${inputClass} min-w-0 flex-1`} />
              <Button variant="ghost" className="px-2" aria-label="Reload" title="Reload" icon={<RefreshCw className="size-4" />} onClick={load} />
            </div>
            {error && (
              <div className="p-2">
                <Notice tone="bad">{error}</Notice>
              </div>
            )}
            <ul className="min-h-0 flex-1 overflow-y-auto p-1">
              {table && rows.length === 0 && <li className="p-3 text-[13px] text-muted">No Secrets{namespace ? ` in ${namespace}` : ""}.</li>}
              {rows.map((r) => {
                const isSel = selected?.name === r.name && selected.namespace === r.namespace;
                return (
                  <li key={`${r.namespace}/${r.name}`}>
                    <button
                      onClick={() => setSelected({ namespace: r.namespace, name: r.name })}
                      aria-label={`${r.name} in ${r.namespace}`}
                      aria-current={isSel ? "true" : undefined}
                      className={`flex w-full flex-col gap-0.5 rounded-md px-2.5 py-1.5 text-left ${isSel ? "bg-panel-2" : "hover:bg-panel-2"}`}
                    >
                      <span className="truncate text-[13px] font-medium">{r.name}</span>
                      <span className="truncate text-[11px] text-muted">
                        {!namespace && `${r.namespace} · `}
                        {TYPE_SHORT[r.type] ?? r.type} · {r.keys} {r.keys === "1" ? "key" : "keys"}
                        {r.created && ` · ${age(r.created)}`}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
          <section aria-label="Secret details" className="min-h-0 overflow-y-auto rounded-lg border border-line bg-panel">
            {notice && (
              <div className="p-4 pb-0">
                <Notice tone="good">{notice}</Notice>
              </div>
            )}
            {selected ? (
              <SecretDetail key={`${selected.namespace}/${selected.name}/${reloadKey}`} contextName={contextName} target={selected} onSaved={(msg) => { setNotice(msg); setReloadKey((n) => n + 1); load(); }} />
            ) : (
              <p className="p-4 text-[13px] text-muted">Choose a Secret to see its keys and who uses it.</p>
            )}
          </section>
        </div>
      )}
      <SecretEditor
        open={creating}
        contextName={contextName}
        defaultNamespace={namespace || active.namespace || "default"}
        onClose={() => setCreating(false)}
        onSaved={(msg, target) => {
          setCreating(false);
          setNotice(msg);
          setSelected(target);
          load();
        }}
      />
    </div>
  );
}

type Pending = { kind: "reveal" | "copy"; key: string } | { kind: "env" };

function SecretDetail({ contextName, target, onSaved }: { contextName: string; target: Selected; onSaved: (message: string) => void }) {
  const [view, setView] = useState<SecretView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<Record<string, { value: string; encoding: string }>>({});
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<Pending | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const timers = useRef<Record<string, number>>({});

  useEffect(() => {
    SecretService.Describe(contextName, target.namespace, target.name)
      .then(setView)
      .catch((err) => setError(errorMessage(err)));
  }, [contextName, target.namespace, target.name]);

  // Forget revealed values when leaving this Secret.
  useEffect(() => {
    const t = timers.current;
    return () => Object.values(t).forEach((id) => window.clearTimeout(id));
  }, []);

  if (error) return <div className="p-4"><Notice tone="bad">{error}</Notice></div>;
  if (!view) return <p className="p-4 text-[13px] text-muted">Loading…</p>;
  const d = view.details;
  const clearAfter = view.clearAfterSeconds;

  const hide = (key: string) => {
    window.clearTimeout(timers.current[key]);
    setRevealed((r) => {
      const next = { ...r };
      delete next[key];
      return next;
    });
  };

  const run = async (action: Pending, givenReason?: string) => {
    const why = givenReason ?? reason;
    if (view.revealNeedsReason && !why) {
      setPending(action);
      return;
    }
    setPending(null);
    setActionError(null);
    setMessage(null);
    try {
      if (action.kind === "reveal") {
        const v = await SecretService.Reveal(contextName, d.namespace, d.name, action.key, why);
        setRevealed((r) => ({ ...r, [action.key]: v }));
        window.clearTimeout(timers.current[action.key]);
        timers.current[action.key] = window.setTimeout(() => hide(action.key), clearAfter * 1000);
      } else if (action.kind === "copy") {
        await SecretService.CopyValue(contextName, d.namespace, d.name, action.key, why);
        setMessage(`Copied ${action.key}. The clipboard is cleared in ${clearAfter} seconds.`);
      } else {
        const res = await SecretService.CopyEnv(contextName, d.namespace, d.name, why);
        const skipped = res.skipped ?? [];
        setMessage(`Copied ${res.keys} ${res.keys === 1 ? "key" : "keys"} as .env. The clipboard is cleared in ${clearAfter} seconds.${skipped.length ? ` Left out (not valid variable names or binary): ${skipped.join(", ")}.` : ""}`);
      }
      if (givenReason) setReason(givenReason);
    } catch (err) {
      setActionError(errorMessage(err));
    }
  };

  const restart = async (u: SecretUsage) => {
    setActionError(null);
    try {
      setPlan(await GuardService.PrepareRestart(contextName, { group: "apps", version: "v1", resource: `${u.kind.toLowerCase()}s`, namespace: u.namespace, name: u.name }));
    } catch (err) {
      setActionError(errorMessage(err));
    }
  };

  const managed = d.managedBy !== "";
  const editable = !d.immutable && d.type !== "kubernetes.io/service-account-token" && d.type !== "helm.sh/release.v1";
  const usedBy = d.usedBy ?? [];
  const restartable = usedBy.filter((u) => u.needsRestart && u.restartable);

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-[15px] font-semibold">
            <KeyRound className="size-4 text-muted" aria-hidden />
            <span className="truncate">{d.name}</span>
          </h2>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[12px] text-muted">
            <span>{d.namespace}</span>
            <Badge className="bg-panel-2 text-fg" title={d.type}>{TYPE_SHORT[d.type] ?? d.type}</Badge>
            {d.immutable && <Badge className="bg-panel-2 text-muted"><Lock className="size-3" aria-hidden />Immutable</Badge>}
            {d.created && <span>created {age(d.created)} ago</span>}
          </div>
        </div>
        <div className="ml-auto flex gap-2">
          <Button icon={<ClipboardCopy className="size-4" />} disabled={(d.keys ?? []).length === 0} onClick={() => void run({ kind: "env" })}>
            Copy as .env
          </Button>
          <Button variant="primary" icon={<Pencil className="size-4" />} disabled={!editable} title={editable ? undefined : "This Secret can't be edited"} onClick={() => setEditing(true)}>
            Edit
          </Button>
        </div>
      </div>

      {managed && (
        <Notice tone="warn" icon={<TriangleAlert className="size-4 text-warn" />}>
          <strong className="font-medium">Managed by {d.managedBy}.</strong> {d.managedNote}
        </Notice>
      )}
      {d.valuesInAnnotation && (
        <Notice tone="bad" icon={<ShieldAlert className="size-4 text-bad" />}>
          kubectl apply copied this Secret's values into the <code className="font-mono">kubectl.kubernetes.io/last-applied-configuration</code> annotation, where tools that only read metadata can see them. Saving here with Kupi doesn't add it again; remove the annotation to clean it up.
        </Notice>
      )}
      {message && <Notice tone="good">{message}</Notice>}
      {actionError && <Notice tone="bad">{actionError}</Notice>}

      {d.tls && <TLSCard info={d.tls} />}
      {(d.registries ?? []).length > 0 && (
        <div className="text-[13px]">
          <span className="text-muted">Registries: </span>
          {(d.registries ?? []).join(", ")}
        </div>
      )}

      <div>
        <h3 className="mb-2 text-[13px] font-semibold">Keys</h3>
        {(d.keys ?? []).length === 0 ? (
          <p className="text-[13px] text-muted">This Secret has no keys.</p>
        ) : (
          <ul className="divide-y divide-line rounded-md border border-line">
            {(d.keys ?? []).map((k) => {
              const shown = revealed[k.name];
              return (
                <li key={k.name} className="px-3 py-2">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-[12px] font-medium">{k.name}</span>
                    <span className="text-[11px] text-muted">
                      {k.size} {k.size === 1 ? "byte" : "bytes"}
                      {k.binary && " · binary"}
                    </span>
                    <div className="ml-auto flex gap-1">
                      {shown ? (
                        <Button variant="ghost" icon={<EyeOff className="size-4" />} onClick={() => hide(k.name)}>
                          Hide
                        </Button>
                      ) : (
                        <Button variant="ghost" icon={<Eye className="size-4" />} onClick={() => void run({ kind: "reveal", key: k.name })}>
                          Reveal
                        </Button>
                      )}
                      <Button variant="ghost" icon={<ClipboardCopy className="size-4" />} disabled={k.binary} title={k.binary ? "Binary values can only be revealed, as base64" : undefined} onClick={() => void run({ kind: "copy", key: k.name })}>
                        Copy
                      </Button>
                    </div>
                  </div>
                  {shown && (
                    <div className="mt-2">
                      <pre className="max-h-48 overflow-auto rounded-md border border-line bg-bg px-2.5 py-1.5 font-mono text-[12px] break-all whitespace-pre-wrap select-text">{shown.value}</pre>
                      <p className="mt-1 text-[11px] text-muted">
                        {shown.encoding === "base64" ? "Binary value shown as base64. " : ""}Hidden again after {clearAfter} seconds.
                      </p>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <div>
        <h3 className="mb-2 text-[13px] font-semibold">Used by</h3>
        {(d.usageSkipped ?? []).length > 0 && (
          <p className="mb-2 text-[12px] text-muted">Couldn't check: {(d.usageSkipped ?? []).join(", ")}.</p>
        )}
        {usedBy.length === 0 ? (
          <p className="text-[13px] text-muted">Nothing in {d.namespace} uses this Secret.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {usedBy.map((u) => (
              <li key={`${u.kind}/${u.name}`} className="rounded-md border border-line px-3 py-2">
                <div className="flex items-center gap-2 text-[13px]">
                  <span className="text-muted">{u.kind}</span>
                  <span className="font-medium">{u.name}</span>
                  {u.needsRestart && <Badge className="bg-warn/15 text-warn" title="Environment variables are read when a container starts">Restart picks up changes</Badge>}
                  {u.restartable && (
                    <Button variant="ghost" className="ml-auto" icon={<RotateCcw className="size-4" />} onClick={() => void restart(u)}>
                      Restart
                    </Button>
                  )}
                </div>
                <ul className="mt-1 list-disc pl-5 text-[12px] text-muted">
                  {(u.how ?? []).map((h) => (
                    <li key={h}>{h}</li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )}
        {restartable.length > 0 && (
          <p className="mt-2 text-[12px] text-muted">Mounted files update on their own within a minute or so. Environment variables only change when pods restart.</p>
        )}
      </div>

      <ReasonDialog
        open={pending !== null}
        title={pending?.kind === "env" ? "Copy every value" : pending?.kind === "copy" ? `Copy ${pending.key}` : `Reveal ${pending?.kind === "reveal" ? pending.key : ""}`}
        onCancel={() => setPending(null)}
        onSubmit={(r) => pending && void run(pending, r)}
      />
      <SecretEditor open={editing} contextName={contextName} existing={view} onClose={() => setEditing(false)} onSaved={(msg) => { setEditing(false); onSaved(msg); }} />
      <ReviewDialog plan={plan} onClose={() => setPlan(null)} onCommitted={(res: CommitResult) => { setPlan(null); setMessage(res.message); }} />
    </div>
  );
}

function TLSCard({ info }: { info: TLSInfo }) {
  const problems = info.problems ?? [];
  const tone = problems.length === 0 ? "good" : info.daysLeft < 0 || info.keyStatus === "mismatch" ? "bad" : "warn";
  return (
    <Notice tone={tone} icon={<FileKey2 className="size-4" />}>
      <div className="flex flex-col gap-0.5">
        <div>
          <span className="text-muted">Certificate for </span>
          {(info.dnsNames ?? []).join(", ") || info.subject || "unknown names"}
        </div>
        {info.notAfter && (
          <div>
            <span className="text-muted">Valid until </span>
            {new Date(info.notAfter).toLocaleDateString()} ({info.daysLeft} days left)
            {info.selfSigned && <span className="text-muted"> · self-signed</span>}
            {info.chainLength > 1 && <span className="text-muted"> · chain of {info.chainLength}</span>}
          </div>
        )}
        {info.issuer && !info.selfSigned && (
          <div>
            <span className="text-muted">Issued by </span>
            {info.issuer}
          </div>
        )}
        <div>
          <span className="text-muted">Private key: </span>
          {{ matches: "matches the certificate", mismatch: "doesn't match", missing: "missing", unreadable: "can't be read" }[info.keyStatus] ?? info.keyStatus}
        </div>
        {problems.map((p) => (
          <div key={p} className="font-medium">
            {p}
          </div>
        ))}
      </div>
    </Notice>
  );
}

const KEY_HINTS: Record<string, string[]> = {
  "kubernetes.io/basic-auth": ["username", "password"],
  "kubernetes.io/ssh-auth": ["ssh-privatekey"],
};

/** Creates a Secret, or edits one when existing is set. */
function SecretEditor({
  open,
  contextName,
  existing,
  defaultNamespace = "default",
  onClose,
  onSaved,
}: {
  open: boolean;
  contextName: string;
  existing?: SecretView;
  defaultNamespace?: string;
  onClose: () => void;
  onSaved: (message: string, target: Selected) => void;
}) {
  const { namespaces } = useClusters();
  const d = existing?.details;
  const [ns, setNs] = useState(d?.namespace ?? defaultNamespace);
  const [name, setName] = useState(d?.name ?? "");
  const [type, setType] = useState(d?.type ?? "Opaque");
  const [rows, setRows] = useState<KVRow[]>([]);
  const [cert, setCert] = useState("");
  const [key, setKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [certInfo, setCertInfo] = useState<TLSInfo | null>(null);
  const [registry, setRegistry] = useState({ server: "", username: "", password: "", email: "" });
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Start fresh each time the editor opens.
  useEffect(() => {
    if (!open) return;
    setNs(d?.namespace ?? defaultNamespace);
    setName(d?.name ?? "");
    setType(d?.type ?? "Opaque");
    setRows(d ? rowsFromKeys((d.keys ?? []).map((k) => k.name)) : [newRow()]);
    setCert("");
    setKey("");
    setShowKey(false);
    setCertInfo(null);
    setRegistry({ server: "", username: "", password: "", email: "" });
    setError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const chooseType = (t: string) => {
    setType(t);
    if (!d && KEY_HINTS[t]) setRows(KEY_HINTS[t].map((k) => newRow(k)));
  };

  const readInto = async (file: File | undefined, set: (v: string) => void) => {
    if (!file) return;
    if (file.size > 1 << 20) {
      setError("The file is larger than 1 MiB.");
      return;
    }
    set(await file.text());
  };

  const checkCert = async () => {
    try {
      setCertInfo(await SecretService.CheckCertificate(cert, key));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const submit = async () => {
    setError(null);
    setBusy(true);
    try {
      if (type === "kubernetes.io/tls") {
        setPlan(await SecretService.PrepareTLS(contextName, ns, name.trim(), cert, key));
      } else if (type === "kubernetes.io/dockerconfigjson") {
        setPlan(await SecretService.PrepareRegistry(contextName, ns, name.trim(), registry.server.trim(), registry.username, registry.password, registry.email.trim()));
      } else {
        const c = collect(rows);
        if (c.error) throw new Error(c.error);
        if (!hasChanges(rows)) throw new Error(d ? "Nothing changed. Replace, add or remove a key first." : "Add at least one key.");
        setPlan(await SecretService.PrepareWrite(contextName, { namespace: ns, name: name.trim(), type: d ? "" : type, set: c.set, delete: c.del }));
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const isTLS = type === "kubernetes.io/tls";
  const isRegistry = type === "kubernetes.io/dockerconfigjson";

  return (
    <>
      <Dialog
        open={open && plan === null}
        onOpenChange={(o) => !o && onClose()}
        wide
        title={d ? `Edit Secret ${d.name}` : "New Secret"}
        description={d ? "Only the keys you replace, add or remove are sent. Current values are never loaded into this form." : "Values go from this form to the cluster through the change review. Nothing is written to disk."}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" busy={busy} onClick={() => void submit()}>
              Review change
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          {d?.managedBy && (
            <Notice tone="warn" icon={<TriangleAlert className="size-4 text-warn" />}>
              Managed by {d.managedBy}. {d.managedNote}
            </Notice>
          )}
          {!d && (
            <div className="grid grid-cols-3 gap-3">
              <Field label="Namespace">
                <select aria-label="Namespace" value={ns} onChange={(e) => setNs(e.target.value)} className={inputClass}>
                  {[...new Set([ns, ...namespaces])].filter(Boolean).map((n) => (
                    <option key={n}>{n}</option>
                  ))}
                </select>
              </Field>
              <Field label="Name">
                <input {...secretInputProps} aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} placeholder="db-credentials" className={inputClass} />
              </Field>
              <Field label="Type">
                <select aria-label="Type" value={type} onChange={(e) => chooseType(e.target.value)} className={inputClass}>
                  {TYPES.map((t) => (
                    <option key={t.value} value={t.value}>
                      {t.label}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
          )}

          {isTLS && (
            <div className="flex flex-col gap-3">
              {d && <p className="text-[13px] text-muted">Saving replaces both the certificate and the key.</p>}
              <Field label="Certificate (PEM, leaf first, then any intermediates)">
                <textarea {...secretInputProps} aria-label="Certificate" rows={5} value={cert} onChange={(e) => { setCert(e.target.value); setCertInfo(null); }} placeholder="-----BEGIN CERTIFICATE-----" className="rounded-md border border-line bg-bg px-2.5 py-1.5 font-mono text-[12px] focus:border-accent focus:outline-none" />
              </Field>
              <input type="file" aria-label="Choose a certificate file" accept=".crt,.pem,.cer" onChange={(e) => void readInto(e.target.files?.[0], (v) => { setCert(v); setCertInfo(null); })} className="text-[12px] text-muted" />
              <Field label="Private key (PEM)">
                <div className="flex items-start gap-1">
                  <MaskedValue label="Private key" value={key} show={showKey} onChange={(v) => { setKey(v); setCertInfo(null); }} placeholder="-----BEGIN PRIVATE KEY-----" />
                  <Button variant="ghost" className="px-2" aria-label={showKey ? "Hide key" : "Show key"} icon={showKey ? <EyeOff className="size-4" /> : <Eye className="size-4" />} onClick={() => setShowKey(!showKey)} />
                </div>
              </Field>
              <input type="file" aria-label="Choose a key file" accept=".key,.pem" onChange={(e) => void readInto(e.target.files?.[0], (v) => { setKey(v); setCertInfo(null); })} className="text-[12px] text-muted" />
              <div>
                <Button disabled={!cert} onClick={() => void checkCert()}>
                  Check certificate
                </Button>
              </div>
              {certInfo && <TLSCard info={certInfo} />}
            </div>
          )}

          {isRegistry && (
            <div className="grid grid-cols-2 gap-3">
              {d && <p className="col-span-2 text-[13px] text-muted">Saving replaces the whole registry login.</p>}
              <Field label="Registry" hint="Leave empty for Docker Hub. For example ghcr.io or registry.example.com:5000.">
                <input {...secretInputProps} aria-label="Registry" value={registry.server} onChange={(e) => setRegistry({ ...registry, server: e.target.value })} placeholder="ghcr.io" className={inputClass} />
              </Field>
              <Field label="User name">
                <input {...secretInputProps} aria-label="User name" value={registry.username} onChange={(e) => setRegistry({ ...registry, username: e.target.value })} className={inputClass} />
              </Field>
              <Field label="Password or access token">
                <input {...secretInputProps} type="password" aria-label="Password or access token" value={registry.password} onChange={(e) => setRegistry({ ...registry, password: e.target.value })} className={inputClass} />
              </Field>
              <Field label="Email (optional)">
                <input {...secretInputProps} aria-label="Email" value={registry.email} onChange={(e) => setRegistry({ ...registry, email: e.target.value })} className={inputClass} />
              </Field>
            </div>
          )}

          {!isTLS && !isRegistry && <KeyValueEditor rows={rows} onChange={setRows} />}
          {error && <Notice tone="bad">{error}</Notice>}
        </div>
      </Dialog>
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={(res) => {
          setPlan(null);
          onSaved(res.message, { namespace: ns, name: name.trim() });
        }}
      />
    </>
  );
}

function ExposureScan({ contextName, namespace }: { contextName: string; namespace: string }) {
  const [result, setResult] = useState<ScanResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const scan = async () => {
    setBusy(true);
    setError(null);
    try {
      setResult(await SecretService.Scan(contextName, namespace));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const findings = result?.findings ?? [];
  return (
    <div className="flex flex-col gap-4 overflow-y-auto">
      <div className="flex items-center gap-3">
        <Button variant="primary" busy={busy} icon={<ScanSearch className="size-4" />} onClick={() => void scan()}>
          Scan {namespace ? namespace : "all namespaces"}
        </Button>
        <p className="text-[13px] text-muted">Looks for passwords, tokens and keys in ConfigMaps, plain environment variables and command-line flags, and for Secret values copied into annotations. Values are never shown.</p>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {result && (
        <>
          <p className="text-[13px] text-muted">
            Checked {result.scanned} objects. {findings.length === 0 ? "Nothing found." : `${findings.length} ${findings.length === 1 ? "finding" : "findings"}.`}
            {(result.skipped ?? []).length > 0 && ` Couldn't check: ${(result.skipped ?? []).join(", ")}.`}
          </p>
          <ul className="flex flex-col gap-2">
            {findings.map((f, i) => (
              <FindingRow key={i} f={f} />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

function FindingRow({ f }: { f: Finding }) {
  return (
    <li className="rounded-lg border border-line bg-panel px-4 py-3">
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <Badge className={f.severity === "high" ? "bg-bad/15 text-bad" : "bg-warn/15 text-warn"}>{f.severity === "high" ? "High" : "Medium"}</Badge>
        <span className="text-muted">{f.kind}</span>
        <span className="font-medium">
          {f.namespace}/{f.name}
        </span>
        <span className="text-muted">· {f.where}</span>
      </div>
      <p className="mt-1 text-[13px]">{f.problem}</p>
      <p className="mt-0.5 text-[12px] text-muted">{f.fix}</p>
    </li>
  );
}
