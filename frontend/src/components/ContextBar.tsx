import { useState } from "react";
import { Lock, LockOpen } from "lucide-react";
import { Dialog } from "./Dialog";
import { Badge, Button, Field, Notice, inputClass } from "./ui";
import { useClusters } from "../state/clusters";
import { errorMessage } from "../lib/api";
import { profileMeta } from "../lib/profiles";

/** Shows the active cluster, its profile and whether editing is on. */
export function ContextBar() {
  const { active, contexts, setActive, setEditing } = useClusters();
  const [askReason, setAskReason] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const connected = contexts.filter((c) => c.connected);
  if (!active) {
    return (
      <div className="flex h-12 items-center border-b border-line px-5 text-[13px] text-muted">
        Not connected. Pick a cluster to start.
      </div>
    );
  }
  const meta = profileMeta(active.profile);
  const needsReason = active.profile === "production";

  const toggle = async (enabled: boolean, why: string) => {
    setBusy(true);
    setError(null);
    try {
      await setEditing(active.name, enabled, why);
      setAskReason(false);
      setReason("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={`flex h-12 items-center gap-3 border-t-2 border-b border-b-line px-5 ${meta.border}`}>
      <select
        aria-label="Active cluster"
        className="max-w-72 truncate rounded-md border border-line bg-panel-2 px-2 py-1 text-[13px] font-medium text-fg"
        value={active.name}
        onChange={(e) => setActive(e.target.value)}
      >
        {connected.map((c) => (
          <option key={c.name} value={c.name}>
            {c.name}
          </option>
        ))}
      </select>
      <Badge className={meta.badge} title={meta.description}>
        {meta.label}
      </Badge>
      <span className="text-[12px] text-muted">{active.serverVersion}</span>
      <div className="ml-auto flex items-center gap-2">
        {active.editingEnabled ? (
          <>
            <Badge className="bg-good/15 text-good">
              <LockOpen className="size-3" aria-hidden /> Editing on
            </Badge>
            <Button variant="ghost" busy={busy} onClick={() => void toggle(false, "")}>
              Switch to read-only
            </Button>
          </>
        ) : (
          <>
            <Badge className="bg-panel-2 text-muted">
              <Lock className="size-3" aria-hidden /> Read-only
            </Badge>
            <Button busy={busy} onClick={() => (needsReason ? setAskReason(true) : void toggle(true, ""))}>
              Switch editing on
            </Button>
          </>
        )}
      </div>
      {error && !askReason && <span className="text-[12px] text-bad">{error}</span>}

      <Dialog
        open={askReason}
        onOpenChange={setAskReason}
        title="Switch editing on"
        description={`Changes to ${active.name} will be allowed until you disconnect or switch back to read-only.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAskReason(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              busy={busy}
              onClick={() => {
                if (reason.trim() === "") {
                  setError("Add a reason. It's saved in the audit log.");
                  return;
                }
                void toggle(true, reason.trim());
              }}
            >
              Switch editing on
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          <Notice tone="warn">This is a production cluster. Every change still needs review and a reason.</Notice>
          <Field label="Reason" hint="Saved in the audit log.">
            <textarea
              className={`${inputClass} h-16 py-1.5`}
              value={reason}
              maxLength={500}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Incident 4521: restarting the payments API"
            />
          </Field>
          {error && <Notice tone="bad">{error}</Notice>}
        </div>
      </Dialog>
    </div>
  );
}
