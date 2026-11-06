import { useEffect, useState } from "react";
import { Ban, CircleCheck, ShieldAlert, TriangleAlert } from "lucide-react";
import { Dialog } from "./Dialog";
import { DiffView } from "./DiffView";
import { PlanItems } from "./PlanItems";
import { Badge, Button, Field, Notice, inputClass } from "./ui";
import { GuardService, errorMessage, type CommitResult, type PlanView } from "../lib/api";
import { profileMeta } from "../lib/profiles";

interface ReviewDialogProps {
  plan: PlanView | null;
  onClose: () => void;
  onCommitted: (result: CommitResult) => void;
  /** Re-prepares the change taking over conflicting fields. Shown only when there are conflicts. */
  onTakeOver?: () => void;
}

const ACTION_LABEL: Record<string, string> = {
  apply: "Apply change",
  delete: "Delete",
  scale: "Scale",
  restart: "Restart",
  "helm-upgrade": "Upgrade",
  "helm-rollback": "Roll back",
  "helm-uninstall": "Uninstall",
};

/**
 * Shows a prepared change from the Guard: what will happen, the diff, any
 * warnings, and the reason and confirmation the cluster's profile requires.
 */
export function ReviewDialog({ plan, onClose, onCommitted, onTakeOver }: ReviewDialogProps) {
  const [reason, setReason] = useState("");
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setReason("");
    setTyped("");
    setError(null);
  }, [plan?.id]);

  if (!plan) return null;
  const meta = profileMeta(plan.profile);
  const summary = plan.summary ?? [];
  const warnings = plan.warnings ?? [];
  const conflicts = plan.conflicts ?? [];
  const destructive = plan.action === "delete" || plan.action === "helm-uninstall";

  const close = () => {
    void GuardService.Cancel(plan.id);
    onClose();
  };

  const commit = async () => {
    if (plan.needsReason && reason.trim() === "") {
      setError("Add a reason. It's saved in the audit log.");
      return;
    }
    if (plan.confirmName && typed !== plan.confirmName) {
      setError(`Type ${plan.confirmName} to confirm.`);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const result = await GuardService.Commit(plan.id, reason.trim(), typed);
      onCommitted(result);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const resource = plan.namespace ? `${plan.namespace}/${plan.name}` : plan.name;
  const items = plan.items ?? [];
  const multi = items.length > 0;

  return (
    <Dialog
      open
      wide
      onOpenChange={(open) => !open && close()}
      title={multi ? `Review: ${items.length} objects` : `Review: ${plan.kind} ${resource}`}
      description={`On ${plan.context}. Nothing has changed yet.`}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            Cancel
          </Button>
          {plan.allowed && (
            <Button variant={destructive ? "danger" : "primary"} busy={busy} onClick={() => void commit()}>
              {ACTION_LABEL[plan.action] ?? "Apply"}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge className={meta.badge}>{meta.label}</Badge>
          {!multi && <Badge className="bg-panel-2 text-muted">{plan.apiVersion}</Badge>}
          <Badge className="bg-panel-2 text-muted">{plan.action}</Badge>
        </div>

        {!plan.allowed && (
          <Notice tone="bad" icon={<Ban className="size-4 text-bad" />}>
            {plan.deniedReason}
          </Notice>
        )}

        {summary.length > 0 && (
          <section>
            <h3 className="mb-1.5 text-[12px] font-semibold tracking-wide text-muted uppercase">What changes</h3>
            <ul className="flex flex-col gap-1 text-[13px]">
              {summary.map((line, i) => (
                <li key={i} className="flex gap-2">
                  <CircleCheck className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
                  <span className="font-mono text-[12px] break-all">{line}</span>
                </li>
              ))}
            </ul>
          </section>
        )}

        {warnings.map((w, i) => (
          <Notice key={i} tone="warn" icon={<TriangleAlert className="size-4 text-warn" />}>
            {w}
          </Notice>
        ))}

        {conflicts.length > 0 && (
          <Notice tone="warn" icon={<ShieldAlert className="size-4 text-warn" />}>
            <p className="font-medium">Fields managed by other tools</p>
            <ul className="mt-1 list-disc pl-4 font-mono text-[12px]">
              {conflicts.map((c, i) => (
                <li key={i}>{c}</li>
              ))}
            </ul>
            {onTakeOver && (
              <Button className="mt-2" onClick={onTakeOver}>
                Take over these fields and preview again
              </Button>
            )}
          </Notice>
        )}

        {multi ? (
          <section>
            <h3 className="mb-1.5 text-[12px] font-semibold tracking-wide text-muted uppercase">Objects, in the order they're applied</h3>
            <PlanItems items={items} />
          </section>
        ) : (
          <section>
            <h3 className="mb-1.5 text-[12px] font-semibold tracking-wide text-muted uppercase">Diff</h3>
            <DiffView diff={plan.diff} />
          </section>
        )}

        {plan.allowed && plan.needsReason && (
          <Field label="Reason" hint="Required on this cluster. Saved in the audit log.">
            <textarea
              className={`${inputClass} h-16 py-1.5`}
              value={reason}
              maxLength={500}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Rolling back the config change from ticket 1234"
            />
          </Field>
        )}

        {plan.allowed && plan.confirmName && (
          <Field label={`Type ${plan.confirmName} to confirm`}>
            <input
              className={`${inputClass} font-mono`}
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
        )}

        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}
