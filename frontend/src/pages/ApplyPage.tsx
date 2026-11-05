import { useState } from "react";
import { Eye } from "lucide-react";
import { ReviewDialog } from "../components/ReviewDialog";
import { Button, Notice, PageHeader } from "../components/ui";
import { useClusters } from "../state/clusters";
import { GuardService, errorMessage, type PlanView } from "../lib/api";

const EXAMPLE = `apiVersion: v1
kind: ConfigMap
metadata:
  name: hello-kupi
data:
  greeting: hello
`;

export function ApplyPage({ onConnect, initialManifest }: { onConnect: () => void; initialManifest?: string }) {
  const { active } = useClusters();
  const [manifest, setManifest] = useState(initialManifest ?? EXAMPLE);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  if (!active) {
    return (
      <div>
        <PageHeader title="Apply YAML" />
        <Notice>
          Connect to a cluster first.{" "}
          <button className="text-accent underline" onClick={onConnect}>
            Go to clusters
          </button>
        </Notice>
      </div>
    );
  }

  const preview = async (force: boolean) => {
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      if (plan) void GuardService.Cancel(plan.id);
      setPlan(await GuardService.PrepareApply(active.name, manifest, force));
    } catch (err) {
      setPlan(null);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title="Apply YAML"
        description={`Paste one object. Kupi previews it on ${active.name} with a server-side dry run before anything changes.`}
        actions={
          <Button variant="primary" icon={<Eye className="size-4" />} busy={busy} onClick={() => void preview(false)}>
            Preview change
          </Button>
        }
      />
      {done && <Notice tone="good">{done}</Notice>}
      {error && <Notice tone="bad">{error}</Notice>}
      <textarea
        aria-label="Manifest"
        className="mt-3 min-h-80 flex-1 resize-none rounded-lg border border-line bg-panel p-3 font-mono text-[12.5px] leading-5 text-fg focus:border-accent focus:outline-none"
        value={manifest}
        spellCheck={false}
        onChange={(e) => setManifest(e.target.value)}
      />
      <ReviewDialog
        plan={plan}
        onClose={() => setPlan(null)}
        onTakeOver={() => void preview(true)}
        onCommitted={(result) => {
          setPlan(null);
          setDone(`${result.message}. Audit entry #${result.auditSeq}.`);
        }}
      />
    </div>
  );
}
