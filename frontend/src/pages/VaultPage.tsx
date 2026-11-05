import { useCallback, useEffect, useRef, useState } from "react";
import { ClipboardCopy, Eye, EyeOff, KeyRound, Lock, LockOpen, Pencil, Plus, Send, Trash2, Vault } from "lucide-react";
import { Dialog } from "../components/Dialog";
import { ReviewDialog } from "../components/ReviewDialog";
import { KeyValueEditor, secretInputProps } from "../components/SecretFields";
import { Button, Field, Notice, PageHeader, inputClass } from "../components/ui";
import { VaultService, errorMessage, type PlanView, type VaultItem, type VaultStatus } from "../lib/api";
import { age } from "../lib/format";
import { collect, hasChanges, newRow, rowsFromKeys, type KVRow } from "../lib/kvrows";
import { useClusters } from "../state/clusters";

const RESET_CONFIRM = "delete my vault";
const REVEAL_SECONDS = 30;

/** The local vault: encrypted secret values kept on this computer. */
export function VaultPage() {
  const [status, setStatus] = useState<VaultStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(() => {
    VaultService.Status()
      .then(setStatus)
      .catch((err) => setError(errorMessage(err)));
  }, []);
  useEffect(refresh, [refresh]);

  return (
    <div className="max-w-4xl">
      <PageHeader
        title="Vault"
        description="Keep credentials for your clusters on this computer, encrypted, instead of in YAML files. Turn any item into a Kubernetes Secret through the usual change review."
        actions={
          status?.unlocked && (
            <Button icon={<Lock className="size-4" />} onClick={() => void VaultService.Lock().then(refresh)}>
              Lock now
            </Button>
          )
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {status && !status.exists && <CreateVault status={status} onCreated={refresh} />}
      {status && status.exists && !status.unlocked && <UnlockVault status={status} onUnlocked={refresh} />}
      {status?.unlocked && <VaultItems status={status} onLocked={refresh} />}
    </div>
  );
}

function CreateVault({ status, onCreated }: { status: VaultStatus; onCreated: () => void }) {
  const [mode, setMode] = useState(status.keyringAvailable ? "keyring" : "passphrase");
  const [pass, setPass] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = async () => {
    setError(null);
    if (mode === "passphrase" && pass !== confirm) {
      setError("The passphrases don't match.");
      return;
    }
    setBusy(true);
    try {
      await VaultService.Create(mode, mode === "passphrase" ? pass : "");
      onCreated();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-line bg-panel p-5">
      <h2 className="flex items-center gap-2 text-[15px] font-semibold">
        <Vault className="size-4" aria-hidden /> Set up the vault
      </h2>
      <p className="text-[13px] text-muted">The vault file is encrypted with age. Choose where its key lives:</p>
      <fieldset className="flex flex-col gap-2">
        <legend className="sr-only">Where the vault key lives</legend>
        <label className={`flex items-start gap-2 rounded-md border px-3 py-2 ${mode === "keyring" ? "border-accent" : "border-line"} ${status.keyringAvailable ? "" : "opacity-50"}`}>
          <input type="radio" name="mode" value="keyring" checked={mode === "keyring"} disabled={!status.keyringAvailable} onChange={() => setMode("keyring")} className="mt-1" />
          <span className="text-[13px]">
            <span className="font-medium">In {status.keyringName || "the system password store"}</span>
            <span className="block text-muted">{status.keyringAvailable ? "Unlocks with your login. Nothing to remember." : "No password store was found on this system."}</span>
          </span>
        </label>
        <label className={`flex items-start gap-2 rounded-md border px-3 py-2 ${mode === "passphrase" ? "border-accent" : "border-line"}`}>
          <input type="radio" name="mode" value="passphrase" checked={mode === "passphrase"} onChange={() => setMode("passphrase")} className="mt-1" />
          <span className="text-[13px]">
            <span className="font-medium">Behind a passphrase</span>
            <span className="block text-muted">Asked each time the vault unlocks. If you forget it, the vault can't be recovered.</span>
          </span>
        </label>
      </fieldset>
      {mode === "passphrase" && (
        <div className="grid grid-cols-2 gap-3">
          <Field label="Passphrase" hint="At least 12 characters. A few random words work well.">
            <input {...secretInputProps} type="password" aria-label="Passphrase" value={pass} onChange={(e) => setPass(e.target.value)} className={inputClass} />
          </Field>
          <Field label="Repeat it">
            <input {...secretInputProps} type="password" aria-label="Repeat the passphrase" value={confirm} onChange={(e) => setConfirm(e.target.value)} className={inputClass} />
          </Field>
        </div>
      )}
      {error && <Notice tone="bad">{error}</Notice>}
      <div>
        <Button variant="primary" busy={busy} onClick={() => void create()}>
          Create vault
        </Button>
      </div>
    </div>
  );
}

function UnlockVault({ status, onUnlocked }: { status: VaultStatus; onUnlocked: () => void }) {
  const [pass, setPass] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [resetting, setResetting] = useState(false);
  const [typed, setTyped] = useState("");

  const unlock = async () => {
    setBusy(true);
    setError(null);
    try {
      await VaultService.Unlock(pass);
      setPass("");
      onUnlocked();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    try {
      await VaultService.Reset(typed);
      setResetting(false);
      onUnlocked();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-line bg-panel p-5">
      <h2 className="flex items-center gap-2 text-[15px] font-semibold">
        <Lock className="size-4" aria-hidden /> The vault is locked
      </h2>
      <p className="text-[13px] text-muted">
        {status.mode === "keyring" ? `Its key is in ${status.keyringName || "the system password store"}.` : "Enter its passphrase."} It locks again after {status.autoLockMinutes} minutes without use.
      </p>
      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void unlock();
        }}
      >
        {status.mode === "passphrase" && (
          <Field label="Passphrase">
            <input {...secretInputProps} autoFocus type="password" aria-label="Passphrase" value={pass} onChange={(e) => setPass(e.target.value)} className={`${inputClass} w-72`} />
          </Field>
        )}
        <Button type="submit" variant="primary" busy={busy} icon={<LockOpen className="size-4" />}>
          Unlock
        </Button>
      </form>
      {error && <Notice tone="bad">{error}</Notice>}
      <div>
        <Button variant="ghost" className="text-bad" onClick={() => setResetting(true)}>
          Delete the vault…
        </Button>
      </div>
      <Dialog
        open={resetting}
        onOpenChange={setResetting}
        title="Delete the vault"
        description="Every item in it is lost for good. Secrets already created in clusters are not affected."
        footer={
          <>
            <Button variant="ghost" onClick={() => setResetting(false)}>
              Cancel
            </Button>
            <Button variant="danger" disabled={typed !== RESET_CONFIRM} onClick={() => void reset()}>
              Delete vault
            </Button>
          </>
        }
      >
        <Field label={`Type "${RESET_CONFIRM}" to confirm`}>
          <input aria-label="Confirmation" value={typed} onChange={(e) => setTyped(e.target.value)} className={inputClass} />
        </Field>
      </Dialog>
    </div>
  );
}

function VaultItems({ status, onLocked }: { status: VaultStatus; onLocked: () => void }) {
  const [items, setItems] = useState<VaultItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<VaultItem | "new" | null>(null);
  const [sending, setSending] = useState<VaultItem | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const load = useCallback(() => {
    VaultService.Items()
      .then((list) => setItems(list ?? []))
      .catch((err) => {
        setError(errorMessage(err));
        onLocked();
      });
  }, [onLocked]);
  useEffect(load, [load]);

  const remove = async (item: VaultItem) => {
    if (!window.confirm(`Delete ${item.name} from the vault?`)) return;
    try {
      await VaultService.DeleteItem(item.name);
      load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
          New item
        </Button>
        <p className="text-[12px] text-muted">
          Unlocked · key in {status.mode === "keyring" ? status.keyringName : "a passphrase-protected file"} · locks after {status.autoLockMinutes} minutes without use
        </p>
      </div>
      {error && <Notice tone="bad">{error}</Notice>}
      {message && <Notice tone="good">{message}</Notice>}
      {items && items.length === 0 && <p className="text-[13px] text-muted">The vault is empty. Add an item, such as the database login for a dev cluster, or import a .env file.</p>}
      <ul className="flex flex-col gap-3">
        {(items ?? []).map((item) => (
          <VaultItemCard key={item.name} item={item} onEdit={() => setEditing(item)} onDelete={() => void remove(item)} onSend={() => setSending(item)} onError={setError} />
        ))}
      </ul>
      <ItemEditor
        open={editing !== null}
        item={editing === "new" ? null : editing}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          load();
        }}
      />
      <SendToCluster item={sending} onClose={() => setSending(null)} onDone={(msg) => { setSending(null); setMessage(msg); }} />
    </div>
  );
}

function VaultItemCard({ item, onEdit, onDelete, onSend, onError }: { item: VaultItem; onEdit: () => void; onDelete: () => void; onSend: () => void; onError: (e: string | null) => void }) {
  const [shown, setShown] = useState<Record<string, string>>({});
  const [copied, setCopied] = useState<string | null>(null);
  const timers = useRef<Record<string, number>>({});
  useEffect(() => {
    const t = timers.current;
    return () => Object.values(t).forEach((id) => window.clearTimeout(id));
  }, []);

  const hide = (key: string) =>
    setShown((s) => {
      const next = { ...s };
      delete next[key];
      return next;
    });

  const reveal = async (key: string) => {
    try {
      const value = await VaultService.Reveal(item.name, key);
      setShown((s) => ({ ...s, [key]: value }));
      window.clearTimeout(timers.current[key]);
      timers.current[key] = window.setTimeout(() => hide(key), REVEAL_SECONDS * 1000);
    } catch (err) {
      onError(errorMessage(err));
    }
  };

  const copy = async (key: string) => {
    try {
      await VaultService.CopyValue(item.name, key);
      setCopied(key);
    } catch (err) {
      onError(errorMessage(err));
    }
  };

  return (
    <li className="rounded-lg border border-line bg-panel px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <KeyRound className="size-4 text-muted" aria-hidden />
        <span className="text-[14px] font-medium">{item.name}</span>
        {item.description && <span className="text-[13px] text-muted">{item.description}</span>}
        <span className="text-[12px] text-muted">· changed {age(item.updated)} ago</span>
        <div className="ml-auto flex gap-1">
          <Button variant="ghost" icon={<Send className="size-4" />} onClick={onSend}>
            Create Secret
          </Button>
          <Button variant="ghost" icon={<Pencil className="size-4" />} onClick={onEdit}>
            Edit
          </Button>
          <Button variant="ghost" className="px-2" aria-label={`Delete ${item.name}`} title="Delete" icon={<Trash2 className="size-4" />} onClick={onDelete} />
        </div>
      </div>
      <ul className="mt-2 divide-y divide-line rounded-md border border-line">
        {(item.keys ?? []).map((k) => (
          <li key={k} className="px-3 py-1.5">
            <div className="flex items-center gap-2">
              <span className="font-mono text-[12px]">{k}</span>
              {copied === k && <span className="text-[11px] text-good">Copied · cleared in 30 seconds</span>}
              <div className="ml-auto flex gap-1">
                {shown[k] !== undefined ? (
                  <Button variant="ghost" icon={<EyeOff className="size-4" />} onClick={() => hide(k)}>
                    Hide
                  </Button>
                ) : (
                  <Button variant="ghost" icon={<Eye className="size-4" />} onClick={() => void reveal(k)}>
                    Reveal
                  </Button>
                )}
                <Button variant="ghost" icon={<ClipboardCopy className="size-4" />} onClick={() => void copy(k)}>
                  Copy
                </Button>
              </div>
            </div>
            {shown[k] !== undefined && <pre className="mt-1 max-h-40 overflow-auto rounded-md border border-line bg-bg px-2.5 py-1.5 font-mono text-[12px] break-all whitespace-pre-wrap">{shown[k]}</pre>}
          </li>
        ))}
      </ul>
    </li>
  );
}

function ItemEditor({ open, item, onClose, onSaved }: { open: boolean; item: VaultItem | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [rows, setRows] = useState<KVRow[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(item?.name ?? "");
    setDescription(item?.description ?? "");
    setRows(item ? rowsFromKeys(item.keys ?? []) : [newRow()]);
    setError(null);
  }, [open, item]);

  const save = async () => {
    const c = collect(rows);
    if (c.error) {
      setError(c.error);
      return;
    }
    if (!item && !hasChanges(rows)) {
      setError("Add at least one key.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await VaultService.SaveItem(name.trim(), description.trim(), c.set, c.del);
      onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      wide
      title={item ? `Edit ${item.name}` : "New vault item"}
      description="Values are encrypted on this computer as soon as you save."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" busy={busy} onClick={() => void save()}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name" hint="Lowercase letters, digits, '.', '-' and '_'.">
            <input {...secretInputProps} aria-label="Item name" disabled={item !== null} value={name} onChange={(e) => setName(e.target.value)} placeholder="shop-db-dev" className={inputClass} />
          </Field>
          <Field label="Description (optional)">
            <input aria-label="Description" value={description} maxLength={200} onChange={(e) => setDescription(e.target.value)} placeholder="Postgres login for the dev cluster" className={inputClass} />
          </Field>
        </div>
        <KeyValueEditor rows={rows} onChange={setRows} />
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}

function SendToCluster({ item, onClose, onDone }: { item: VaultItem | null; onClose: () => void; onDone: (message: string) => void }) {
  const { active, namespace, namespaces } = useClusters();
  const [ns, setNs] = useState("");
  const [name, setName] = useState("");
  const [type, setType] = useState("Opaque");
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!item) return;
    setNs(namespace || active?.namespace || "default");
    setName(item.name.replace(/[._]/g, "-"));
    setType("Opaque");
    setError(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item]);

  const prepare = async () => {
    if (!active || !item) return;
    setBusy(true);
    setError(null);
    try {
      setPlan(await VaultService.PrepareSecret(active.name, ns, name.trim(), item.name, type));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Dialog
        open={item !== null && plan === null}
        onOpenChange={(o) => !o && onClose()}
        title={`Create a Secret from ${item?.name ?? ""}`}
        description="Every key of the item goes into the Secret. If the Secret already exists, these keys are added or replaced and its other keys are kept."
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" busy={busy} disabled={!active} onClick={() => void prepare()}>
              Review change
            </Button>
          </>
        }
      >
        {!active ? (
          <Notice tone="warn">Connect to a cluster first.</Notice>
        ) : (
          <div className="flex flex-col gap-3">
            <p className="text-[13px] text-muted">
              Cluster: <span className="text-fg">{active.name}</span> ({active.profile})
            </p>
            <Field label="Namespace">
              <select aria-label="Namespace" value={ns} onChange={(e) => setNs(e.target.value)} className={inputClass}>
                {[...new Set([ns, ...namespaces])].filter(Boolean).map((n) => (
                  <option key={n}>{n}</option>
                ))}
              </select>
            </Field>
            <Field label="Secret name">
              <input {...secretInputProps} aria-label="Secret name" value={name} onChange={(e) => setName(e.target.value)} className={inputClass} />
            </Field>
            <Field label="Type">
              <select aria-label="Type" value={type} onChange={(e) => setType(e.target.value)} className={inputClass}>
                <option value="Opaque">Opaque</option>
                <option value="kubernetes.io/basic-auth">Basic auth (needs username and password keys)</option>
                <option value="kubernetes.io/ssh-auth">SSH key (needs an ssh-privatekey key)</option>
              </select>
            </Field>
            {error && <Notice tone="bad">{error}</Notice>}
          </div>
        )}
      </Dialog>
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onCommitted={(res) => {
          setPlan(null);
          onDone(res.message);
        }}
      />
    </>
  );
}

