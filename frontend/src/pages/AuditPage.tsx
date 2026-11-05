import { useCallback, useEffect, useState } from "react";
import { RefreshCw, ShieldCheck, ShieldX } from "lucide-react";
import { Badge, Button, Notice, PageHeader } from "../components/ui";
import { AuditService, errorMessage, type AuditEntry, type AuditVerification } from "../lib/api";
import { profileMeta } from "../lib/profiles";

export function AuditPage() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [check, setCheck] = useState<AuditVerification | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      const [list, verification] = await Promise.all([AuditService.Recent(200), AuditService.Verify()]);
      setEntries(list ?? []);
      setCheck(verification);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div>
      <PageHeader
        title="Audit log"
        description="Every change Kupi makes, newest first. Each entry is chained to the one before it, so edits to the file are detected."
        actions={
          <Button icon={<RefreshCw className="size-4" />} busy={busy} onClick={() => void load()}>
            Reload
          </Button>
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {check &&
        (check.ok ? (
          <Notice tone="good" icon={<ShieldCheck className="size-4 text-good" />}>
            Chain verified: {check.entries} {check.entries === 1 ? "entry" : "entries"}, none modified or removed.
          </Notice>
        ) : (
          <Notice tone="bad" icon={<ShieldX className="size-4 text-bad" />}>
            The audit log was tampered with at entry #{check.brokenAt}: {check.problem}.
          </Notice>
        ))}
      {entries.length === 0 && !error && <p className="mt-4 text-[13px] text-muted">No changes recorded yet.</p>}
      {entries.length > 0 && (
        <div className="mt-4 overflow-hidden rounded-lg border border-line">
          <table className="w-full table-fixed text-left text-[13px]">
            <thead className="bg-panel-2 text-[12px] text-muted">
              <tr>
                <th className="w-12 px-3 py-2 font-medium">#</th>
                <th className="w-40 px-3 py-2 font-medium">Time</th>
                <th className="w-44 px-3 py-2 font-medium">Cluster</th>
                <th className="w-32 px-3 py-2 font-medium">Action</th>
                <th className="px-3 py-2 font-medium">Resource and reason</th>
                <th className="w-24 px-3 py-2 font-medium">Outcome</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.seq} className="border-t border-line align-top">
                  <td className="px-3 py-2 text-muted">{e.seq}</td>
                  <td className="px-3 py-2 text-muted">{new Date(e.time).toLocaleString()}</td>
                  <td className="truncate px-3 py-2">
                    {e.context || "—"}
                    {e.profile && <Badge className={`ml-1 ${profileMeta(e.profile).badge}`}>{profileMeta(e.profile).label}</Badge>}
                  </td>
                  <td className="px-3 py-2 font-mono text-[12px]">{e.action}</td>
                  <td className="px-3 py-2">
                    <div className="truncate font-mono text-[12px]" title={e.resource}>
                      {e.resource}
                    </div>
                    {e.reason && <div className="mt-0.5 text-[12px] text-muted">{e.reason}</div>}
                    {e.error && <div className="mt-0.5 text-[12px] text-bad">{e.error}</div>}
                  </td>
                  <td className="px-3 py-2">
                    <Badge
                      className={
                        e.outcome === "applied"
                          ? "bg-good/15 text-good"
                          : e.outcome === "failed"
                            ? "bg-bad/15 text-bad"
                            : "bg-panel-2 text-muted"
                      }
                    >
                      {e.outcome}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
