import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { Button, Field, Notice, inputClass } from "./ui";
import { LocalClusterService, errorMessage, type LaptopStatus, type LocalCluster, type LocalUsage } from "../lib/api";
import { formatBytes } from "../lib/units";

const IDLE_CHOICES = [0, 15, 30, 60, 120, 240];

function Bar({ used, total, label }: { used: number; total: number; label: string }) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0;
  return (
    <div>
      <div className="mb-1 flex justify-between text-[12px]">
        <span className="text-muted">{label}</span>
        <span>{pct.toFixed(pct < 10 ? 1 : 0)}%</span>
      </div>
      <div className="h-2 overflow-hidden rounded bg-panel-2" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
        <div className={`h-full ${pct > 75 ? "bg-bad" : pct > 50 ? "bg-warn" : "bg-accent"}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

/** How much of this computer a k3d cluster takes, its caps, and its idle timer. */
export function LaptopPanel({ cluster }: { cluster: LocalCluster }) {
  const [usage, setUsage] = useState<LocalUsage | null>(null);
  const [status, setStatus] = useState<LaptopStatus | null>(null);
  const [memory, setMemory] = useState("");
  const [cpus, setCpus] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [u, s] = await Promise.all([LocalClusterService.Usage(cluster.name), LocalClusterService.LaptopStatus(cluster.name)]);
      setUsage(u);
      setStatus(s);
      const n = (u.nodes ?? [])[0];
      if (n) {
        setMemory(n.memCap > 0 && n.memCap < u.hostMem ? String(Math.round(n.memCap / 2 ** 20)) : "");
        setCpus(n.cpuCap > 0 && n.cpuCap < u.hostCpus ? String(n.cpuCap) : "");
      }
    } catch (err) {
      setError(errorMessage(err));
    }
  }, [cluster.name]);

  useEffect(() => {
    void load();
  }, [load]);

  const applyLimits = async () => {
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await LocalClusterService.SetLimits(cluster.name, Number(memory) || 0, Number(cpus) || 0);
      setMessage(memory || cpus ? "Caps applied to every node. They stay when the cluster restarts." : "Caps lifted.");
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const setIdle = async (minutes: number) => {
    setError(null);
    try {
      await LocalClusterService.SetIdleStop(cluster.name, minutes);
      setStatus(await LocalClusterService.LaptopStatus(cluster.name));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const nodes = usage?.nodes ?? [];
  return (
    <div className="mt-3 flex flex-col gap-4 border-t border-line pt-3">
      {error && <Notice tone="bad">{error}</Notice>}
      {message && <Notice tone="good">{message}</Notice>}
      {usage && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 text-[13px]">
            <span>
              Uses <span className="font-medium">{formatBytes(usage.memUsage)}</span> of {formatBytes(usage.hostMem)} and{" "}
              <span className="font-medium">{usage.cpuCores.toFixed(2)}</span> of {usage.hostCpus} CPUs
            </span>
            <Button variant="ghost" className="ml-auto h-7" icon={<RefreshCw className="size-3.5" />} onClick={() => void load()}>
              Measure again
            </Button>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Bar label="Memory" used={usage.memUsage} total={usage.hostMem} />
            <Bar label="CPU" used={usage.cpuCores} total={usage.hostCpus} />
          </div>
          <p className="text-[12px] text-muted">
            {nodes.map((n) => `${n.name} (${n.role}): ${n.running ? `${formatBytes(n.memUsage)}, ${n.cpuCores.toFixed(2)} CPUs` : "stopped"}`).join(" · ")}. With Docker Desktop, the machine total is what Docker's VM is allowed to use.
          </p>
        </div>
      )}
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Memory cap per node (MB)" hint="Empty for no cap. At least 768.">
          <input aria-label="Memory cap per node in MB" inputMode="numeric" value={memory} onChange={(e) => setMemory(e.target.value.replace(/[^0-9]/g, ""))} placeholder="No cap" className={`${inputClass} w-40`} />
        </Field>
        <Field label="CPU cap per node" hint="Empty for no cap. At least 0.5.">
          <input aria-label="CPU cap per node" inputMode="decimal" value={cpus} onChange={(e) => setCpus(e.target.value.replace(/[^0-9.]/g, ""))} placeholder="No cap" className={`${inputClass} w-32`} />
        </Field>
        <Button busy={busy} onClick={() => void applyLimits()}>
          Apply caps
        </Button>
      </div>
      {status && (
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Stop when idle" hint="Kupi stops the cluster after this long without using it (open port-forwards count as use), and starts it again when you connect. Tools outside Kupi don't count.">
            <select aria-label="Stop when idle" value={status.idleStopMinutes} onChange={(e) => void setIdle(Number(e.target.value))} className={`${inputClass} w-48`}>
              {IDLE_CHOICES.map((m) => (
                <option key={m} value={m}>
                  {m === 0 ? "Never" : m < 60 ? `After ${m} minutes` : `After ${m / 60} hour${m === 60 ? "" : "s"}`}
                </option>
              ))}
            </select>
          </Field>
          {status.stoppedIdle && <Notice tone="info">Stopped because it was idle. Connecting starts it again.</Notice>}
        </div>
      )}
    </div>
  );
}
