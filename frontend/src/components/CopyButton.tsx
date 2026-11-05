import { useState } from "react";
import { Check, Terminal } from "lucide-react";
import { copyText } from "../lib/bytes";

/** Copies the equivalent kubectl command. Shows the command on hover. */
export function KubectlButton({ command, label = "Copy as kubectl" }: { command: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      title={command}
      onClick={() => {
        void copyText(command).then(() => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        });
      }}
      className="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-[12px] text-muted hover:bg-panel-2 hover:text-fg"
    >
      {copied ? <Check className="size-3.5 text-good" aria-hidden /> : <Terminal className="size-3.5" aria-hidden />}
      {copied ? "Copied" : label}
    </button>
  );
}
