import { FileCode, ScrollText, Server } from "lucide-react";
import type { ReactNode } from "react";

export type Page = "clusters" | "apply" | "audit";

const ITEMS: { id: Page; label: string; icon: ReactNode }[] = [
  { id: "clusters", label: "Clusters", icon: <Server className="size-4" aria-hidden /> },
  { id: "apply", label: "Apply YAML", icon: <FileCode className="size-4" aria-hidden /> },
  { id: "audit", label: "Audit log", icon: <ScrollText className="size-4" aria-hidden /> },
];

export function Sidebar({ page, onNavigate, version }: { page: Page; onNavigate: (page: Page) => void; version: string }) {
  return (
    <nav aria-label="Main" className="flex w-52 shrink-0 flex-col border-r border-line bg-panel">
      <div className="flex h-12 items-center gap-2 border-b border-line px-4">
        <span className="grid size-6 place-items-center rounded-md bg-accent text-[12px] font-bold text-accent-fg">K</span>
        <span className="text-[15px] font-semibold tracking-tight">Kupi</span>
      </div>
      <ul className="flex flex-1 flex-col gap-0.5 p-2">
        {ITEMS.map((item) => (
          <li key={item.id}>
            <button
              onClick={() => onNavigate(item.id)}
              aria-current={page === item.id ? "page" : undefined}
              className={`flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[13px] ${
                page === item.id ? "bg-panel-2 font-medium text-fg" : "text-muted hover:bg-panel-2 hover:text-fg"
              }`}
            >
              {item.icon}
              {item.label}
            </button>
          </li>
        ))}
      </ul>
      <div className="border-t border-line px-4 py-2 text-[11px] text-muted">Kupi {version}</div>
    </nav>
  );
}
