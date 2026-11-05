import { useState } from "react";
import { Eye, EyeOff, FileUp, Plus, Trash2, Undo2 } from "lucide-react";
import { Dialog } from "./Dialog";
import { Button, Notice, inputClass } from "./ui";
import { parseEnv } from "../lib/dotenv";
import { mergeValues, newRow, type KVRow } from "../lib/kvrows";

/** Props that keep typed secrets out of spell checkers and autofill. */
export const secretInputProps = {
  autoComplete: "off",
  autoCorrect: "off",
  autoCapitalize: "off",
  spellCheck: false,
  "data-1p-ignore": true,
} as const;

/**
 * A value field that hides its text until shown. It is a textarea so
 * certificates and multi-line values fit; the mask is the browser's own
 * text-security style, so the value never appears on screen by accident.
 */
export function MaskedValue({ value, onChange, show, label, disabled, placeholder }: { value: string; onChange: (v: string) => void; show: boolean; label: string; disabled?: boolean; placeholder?: string }) {
  const lines = Math.min(8, Math.max(1, value.split("\n").length));
  return (
    <textarea
      {...secretInputProps}
      aria-label={label}
      value={value}
      disabled={disabled}
      placeholder={placeholder}
      rows={lines}
      onChange={(e) => onChange(e.target.value)}
      className={`min-h-8 w-full resize-y rounded-md border border-line bg-bg px-2.5 py-1.5 font-mono text-[12px] leading-5 text-fg placeholder:font-sans placeholder:text-muted focus:border-accent focus:outline-none disabled:opacity-50 ${show ? "" : "[-webkit-text-security:disc]"}`}
    />
  );
}

/** Edits keys and values. Existing keys can be kept, replaced or removed. */
export function KeyValueEditor({ rows, onChange }: { rows: KVRow[]; onChange: (rows: KVRow[]) => void }) {
  const [importing, setImporting] = useState(false);
  const update = (id: number, patch: Partial<KVRow>) => onChange(rows.map((r) => (r.id === id ? { ...r, ...patch } : r)));

  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col gap-2">
        {rows.map((r) => {
          const existing = r.state !== "new";
          return (
            <li key={r.id} className={`grid grid-cols-[minmax(0,12rem)_minmax(0,1fr)_auto] items-start gap-2 ${r.state === "remove" ? "opacity-60" : ""}`}>
              <input
                {...secretInputProps}
                aria-label="Key"
                value={r.key}
                disabled={existing}
                placeholder="KEY_NAME"
                onChange={(e) => update(r.id, { key: e.target.value })}
                className={`${inputClass} font-mono text-[12px] disabled:opacity-80 ${r.state === "remove" ? "line-through" : ""}`}
              />
              {r.state === "keep" ? (
                <div className="flex h-8 items-center rounded-md border border-dashed border-line px-2.5 text-[12px] text-muted">Current value kept</div>
              ) : r.state === "remove" ? (
                <div className="flex h-8 items-center rounded-md border border-dashed border-bad/50 px-2.5 text-[12px] text-bad">Will be removed</div>
              ) : (
                <MaskedValue label={`Value of ${r.key || "new key"}`} value={r.value} show={r.show} onChange={(value) => update(r.id, { value })} placeholder={r.state === "replace" ? "New value" : "Value"} />
              )}
              <div className="flex items-center gap-0.5">
                {(r.state === "new" || r.state === "replace") && (
                  <Button variant="ghost" className="px-2" aria-label={r.show ? "Hide value" : "Show value"} title={r.show ? "Hide value" : "Show value"} icon={r.show ? <EyeOff className="size-4" /> : <Eye className="size-4" />} onClick={() => update(r.id, { show: !r.show })} />
                )}
                {r.state === "keep" && (
                  <Button variant="ghost" onClick={() => update(r.id, { state: "replace", value: "" })}>
                    Replace
                  </Button>
                )}
                {(r.state === "replace" || r.state === "remove") && (
                  <Button variant="ghost" className="px-2" aria-label="Keep the current value" title="Keep the current value" icon={<Undo2 className="size-4" />} onClick={() => update(r.id, { state: "keep", value: "", show: false })} />
                )}
                {r.state !== "remove" && (
                  <Button
                    variant="ghost"
                    className="px-2"
                    aria-label={existing ? `Remove ${r.key}` : "Remove row"}
                    title={existing ? "Remove this key" : "Remove row"}
                    icon={<Trash2 className="size-4" />}
                    onClick={() => (existing ? update(r.id, { state: "remove", value: "" }) : onChange(rows.filter((x) => x.id !== r.id)))}
                  />
                )}
              </div>
            </li>
          );
        })}
      </ul>
      <div className="flex gap-2">
        <Button variant="ghost" icon={<Plus className="size-4" />} onClick={() => onChange([...rows, newRow()])}>
          Add key
        </Button>
        <Button variant="ghost" icon={<FileUp className="size-4" />} onClick={() => setImporting(true)}>
          Import .env
        </Button>
      </div>
      <EnvImportDialog open={importing} onClose={() => setImporting(false)} onImport={(values) => onChange(mergeValues(rows, values))} />
    </div>
  );
}

function EnvImportDialog({ open, onClose, onImport }: { open: boolean; onClose: () => void; onImport: (values: Record<string, string>) => void }) {
  const [text, setText] = useState("");
  const [errors, setErrors] = useState<string[]>([]);
  const close = () => {
    setText("");
    setErrors([]);
    onClose();
  };
  const readFile = async (file: File | undefined) => {
    if (!file) return;
    if (file.size > 1 << 20) {
      setErrors(["The file is larger than 1 MiB."]);
      return;
    }
    setText(await file.text());
  };
  const submit = () => {
    const parsed = parseEnv(text);
    if (Object.keys(parsed.values).length === 0) {
      setErrors(parsed.errors.length ? parsed.errors : ["No KEY=value lines found."]);
      return;
    }
    onImport(parsed.values);
    close();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title="Import a .env file"
      description="Paste KEY=value lines or choose a file. The text stays on this computer until you save."
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit}>
            Import
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <input type="file" aria-label="Choose a .env file" accept=".env,text/plain" onChange={(e) => void readFile(e.target.files?.[0])} className="text-[13px] text-muted file:mr-3 file:rounded-md file:border file:border-line file:bg-panel-2 file:px-3 file:py-1 file:text-fg" />
        <MaskedValue label=".env text" value={text} onChange={setText} show={false} placeholder="DB_PASSWORD=..." />
        {errors.length > 0 && (
          <Notice tone="warn">
            {errors.slice(0, 5).map((e) => (
              <div key={e}>{e}</div>
            ))}
          </Notice>
        )}
      </div>
    </Dialog>
  );
}

/** Asks for the reason the cluster's profile requires before revealing data. */
export function ReasonDialog({ open, title, onCancel, onSubmit }: { open: boolean; title: string; onCancel: () => void; onSubmit: (reason: string) => void }) {
  const [reason, setReason] = useState("");
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onCancel()}
      title={title}
      description="This cluster asks for a reason before Secret values are shown or copied. It's saved in the audit log."
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" disabled={reason.trim() === ""} onClick={() => onSubmit(reason.trim())}>
            Continue
          </Button>
        </>
      }
    >
      <input aria-label="Reason" autoFocus value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} onKeyDown={(e) => e.key === "Enter" && reason.trim() && onSubmit(reason.trim())} placeholder="For example: debugging checkout login failures" className={`${inputClass} w-full`} />
    </Dialog>
  );
}
