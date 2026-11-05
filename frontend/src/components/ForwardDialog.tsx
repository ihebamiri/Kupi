import { useEffect, useState } from "react";
import { Dialog } from "./Dialog";
import { Button, Field, Notice, inputClass } from "./ui";
import { ForwardService, errorMessage, type Forward, type PortOption } from "../lib/api";

interface ForwardDialogProps {
  open: boolean;
  onClose: () => void;
  contextName: string;
  kind: string;
  namespace: string;
  name: string;
  onStarted: (f: Forward) => void;
}

export function ForwardDialog({ open, onClose, contextName, kind, namespace, name, onStarted }: ForwardDialogProps) {
  const [ports, setPorts] = useState<PortOption[]>([]);
  const [port, setPort] = useState("");
  const [local, setLocal] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    ForwardService.Ports(contextName, kind, namespace, name)
      .then((list) => {
        const all = list ?? [];
        setPorts(all);
        if (all[0]) setPort(String(all[0].port));
      })
      .catch((err) => setError(errorMessage(err)));
  }, [open, contextName, kind, namespace, name]);

  const start = async () => {
    const remote = Number(port);
    const localPort = local.trim() === "" ? 0 : Number(local);
    if (!Number.isInteger(remote) || remote < 1 || remote > 65535) {
      setError("Pick a port between 1 and 65535.");
      return;
    }
    if (!Number.isInteger(localPort) || localPort < 0 || localPort > 65535) {
      setError("The local port must be a number between 1 and 65535, or empty to pick one.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onStarted(await ForwardService.Start(contextName, { kind, namespace, name, port: remote }, localPort));
      onClose();
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
      title={`Forward a port to ${kind} ${name}`}
      description="Listens on 127.0.0.1 only, so other computers can't reach it."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" busy={busy} onClick={() => void start()}>
            Start forwarding
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label={kind === "Service" ? "Service port" : "Container port"}>
          {ports.length > 0 ? (
            <select aria-label="Remote port" className={inputClass} value={port} onChange={(e) => setPort(e.target.value)}>
              {ports.map((p) => (
                <option key={`${p.port}/${p.protocol}`} value={p.port}>
                  {p.port}
                  {p.name ? ` (${p.name})` : ""}
                  {p.protocol && p.protocol !== "TCP" ? ` ${p.protocol}` : ""}
                </option>
              ))}
            </select>
          ) : (
            <input aria-label="Remote port" className={inputClass} inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} placeholder="8080" />
          )}
        </Field>
        <Field label="Local port" hint="Leave empty to use the same number if it's free, otherwise any free port.">
          <input aria-label="Local port" className={inputClass} inputMode="numeric" value={local} onChange={(e) => setLocal(e.target.value)} placeholder="automatic" />
        </Field>
        {error && <Notice tone="bad">{error}</Notice>}
      </div>
    </Dialog>
  );
}
