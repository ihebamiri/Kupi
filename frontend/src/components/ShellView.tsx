import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { SquareTerminal } from "lucide-react";
import { KubectlButton } from "./CopyButton";
import { Button, Field, Notice, inputClass } from "./ui";
import { ResourceService, ShellService, errorMessage, type ContainerView, type ShellChunk } from "../lib/api";
import { base64ToBytes, textToBase64 } from "../lib/bytes";
import { KupiShell, bufferedListener } from "../lib/events";
import { kubectlExec, type KubeTarget } from "../lib/kubectl";
import { useClusters } from "../state/clusters";

interface ShellViewProps {
  contextName: string;
  namespace: string;
  pod: string;
  target: KubeTarget;
}

/**
 * Interactive shell in a container. Opening one goes through the Guard:
 * permissions are checked, production requires a reason, and the session
 * start is recorded in the audit log.
 */
export function ShellView({ contextName, namespace, pod, target }: ShellViewProps) {
  const { active } = useClusters();
  const needsReason = active?.profile === "production";
  const [containers, setContainers] = useState<ContainerView[]>([]);
  const [container, setContainer] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<string | null>(null);
  const [ended, setEnded] = useState<string | null>(null);
  const host = useRef<HTMLDivElement>(null);
  const cleanup = useRef<() => void>(() => {});

  useEffect(() => {
    ResourceService.Containers(contextName, namespace, pod)
      .then((list) => {
        const runnable = (list ?? []).filter((c) => !c.init);
        setContainers(runnable);
        if (runnable[0]) setContainer(runnable[0].name);
      })
      .catch(() => setContainers([]));
  }, [contextName, namespace, pod]);

  useEffect(() => () => cleanup.current(), []);

  const open = async () => {
    if (needsReason && reason.trim() === "") {
      setError("Add a reason. It's saved in the audit log.");
      return;
    }
    setBusy(true);
    setError(null);
    setEnded(null);
    try {
      const plan = await ShellService.Prepare(contextName, namespace, pod, container);
      if (!plan.allowed) {
        setError(plan.deniedReason);
        return;
      }
      const el = host.current;
      if (!el) return;
      cleanup.current();
      el.textContent = "";
      const term = new Terminal({
        cursorBlink: true,
        fontFamily: 'ui-monospace, "Cascadia Code", "JetBrains Mono", "Ubuntu Mono", Consolas, monospace',
        fontSize: 12.5,
        scrollback: 5000,
        theme: { background: "#0b0d12", foreground: "#e6e9ef", cursor: "#5b8def" },
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(el);
      fit.fit();
      const listener = bufferedListener<ShellChunk>(KupiShell, (chunk) => {
        if (chunk.data) term.write(base64ToBytes(chunk.data));
        if (chunk.closed) {
          setEnded(chunk.error || "");
          setSession(null);
        }
      });
      let id = "";
      try {
        id = await ShellService.Open(plan.id, reason.trim(), term.cols, term.rows);
      } catch (err) {
        listener.off();
        term.dispose();
        throw err;
      }
      listener.bind(id);
      setSession(id);
      const input = term.onData((data) => void ShellService.Input(id, textToBase64(data)).catch(() => {}));
      const resize = term.onResize(({ cols, rows }) => void ShellService.Resize(id, cols, rows));
      const observer = new ResizeObserver(() => fit.fit());
      observer.observe(el);
      term.focus();
      cleanup.current = () => {
        observer.disconnect();
        input.dispose();
        resize.dispose();
        listener.off();
        void ShellService.Close(id);
        term.dispose();
      };
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-end gap-2 border-b border-line px-3 py-2 text-[12px]">
        {containers.length > 1 && (
          <select aria-label="Container" className="rounded-md border border-line bg-panel-2 px-1.5 py-1" value={container} disabled={!!session} onChange={(e) => setContainer(e.target.value)}>
            {containers.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
        )}
        {needsReason && !session && (
          <div className="min-w-56 flex-1">
            <Field label="Reason (production)">
              <input className={inputClass} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} placeholder="Checking config files for incident 812" />
            </Field>
          </div>
        )}
        {session ? (
          <Button
            variant="ghost"
            onClick={() => {
              cleanup.current();
              cleanup.current = () => {};
              setSession(null);
              setEnded("");
            }}
          >
            End session
          </Button>
        ) : (
          <Button variant="primary" busy={busy} icon={<SquareTerminal className="size-4" />} onClick={() => void open()}>
            {ended !== null ? "Open a new shell" : "Open shell"}
          </Button>
        )}
        <span className="ml-auto">
          <KubectlButton command={kubectlExec(target, container || undefined)} label="kubectl" />
        </span>
      </div>
      {error && <div className="p-3"><Notice tone="bad">{error}</Notice></div>}
      {ended !== null && ended !== "" && <div className="p-3"><Notice tone="bad">The shell ended: {ended}</Notice></div>}
      <div ref={host} className="min-h-0 flex-1 bg-[#0b0d12] p-2" />
      {!session && ended === null && !error && (
        <p className="px-3 py-2 text-[12px] text-muted">Opens bash if the container has it, otherwise sh. The session start is recorded in the audit log.</p>
      )}
    </div>
  );
}
