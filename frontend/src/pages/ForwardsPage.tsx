import { Browser } from "@wailsio/runtime";
import { Copy, ExternalLink, Square } from "lucide-react";
import { Badge, Button, PageHeader } from "../components/ui";
import { ForwardService, type Forward } from "../lib/api";
import { copyText } from "../lib/bytes";
import { age } from "../lib/format";
import { useForwards } from "../state/forwards";

const STATUS: Record<string, { label: string; className: string }> = {
  active: { label: "Active", className: "bg-good/15 text-good" },
  starting: { label: "Starting", className: "bg-panel-2 text-muted" },
  reconnecting: { label: "Reconnecting", className: "bg-warn/15 text-warn" },
};

/** Every port-forward in one place. Forwards reconnect on their own when pods restart. */
export function ForwardsPage() {
  const forwards = useForwards();
  return (
    <div>
      <PageHeader
        title="Port forwards"
        description="Reach services in the cluster from this computer. Forwards listen on 127.0.0.1 only and reconnect on the same port when a pod restarts. Start one from a Pod, Service or Deployment."
      />
      {forwards.length === 0 && <p className="text-[13px] text-muted">No port forwards running.</p>}
      <ul className="flex flex-col gap-2">
        {forwards.map((f) => (
          <ForwardRow key={f.id} f={f} />
        ))}
      </ul>
    </div>
  );
}

function ForwardRow({ f }: { f: Forward }) {
  const url = `http://127.0.0.1:${f.localPort}`;
  const status = STATUS[f.status] ?? STATUS.starting;
  return (
    <li className="rounded-lg border border-line bg-panel px-4 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-[13px] font-medium">127.0.0.1:{f.localPort}</span>
        <span className="text-[13px] text-muted">→</span>
        <span className="text-[13px]">
          {f.target.kind} {f.target.namespace}/{f.target.name}:{f.target.port}
        </span>
        <Badge className={status.className}>{status.label}</Badge>
        {f.reconnects > 0 && <Badge className="bg-panel-2 text-muted">reconnected {f.reconnects}×</Badge>}
        <div className="ml-auto flex items-center gap-1">
          <Button variant="ghost" icon={<ExternalLink className="size-4" />} onClick={() => void Browser.OpenURL(url)}>
            Open
          </Button>
          <Button variant="ghost" icon={<Copy className="size-4" />} onClick={() => void copyText(url)}>
            Copy
          </Button>
          <Button variant="ghost" icon={<Square className="size-4" />} onClick={() => void ForwardService.Stop(f.id)}>
            Stop
          </Button>
        </div>
      </div>
      <div className="mt-1 text-[12px] text-muted">
        {f.context} · {f.pod ? `pod ${f.pod}:${f.podPort}` : "finding a pod…"} · started {age(f.since)} ago
      </div>
      {f.error && <div className="mt-1 text-[12px] text-warn">{f.error}</div>}
    </li>
  );
}
