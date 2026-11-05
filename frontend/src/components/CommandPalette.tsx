import { useEffect, useState } from "react";
import { Command } from "cmdk";
import { useClusters } from "../state/clusters";
import { useNav } from "../state/nav";

/** Ctrl+K (Cmd+K on Mac): jump to any page, resource type, namespace or cluster. */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const { navigate, types } = useNav();
  const { active, contexts, setActive, connect, namespaces, setNamespace } = useClusters();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const run = (fn: () => void) => {
    setOpen(false);
    fn();
  };
  const item = "flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 text-[13px] text-fg data-[selected=true]:bg-accent/20";
  const heading =
    "[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-muted [&_[cmdk-group-heading]]:uppercase";

  return (
    <Command.Dialog
      open={open}
      onOpenChange={setOpen}
      label="Command palette"
      overlayClassName="fixed inset-0 z-40 bg-black/50"
      contentClassName="fixed top-[15vh] left-1/2 z-50 w-[92vw] max-w-xl -translate-x-1/2 overflow-hidden rounded-lg border border-line bg-panel shadow-2xl"
    >
      <Command.Input
        placeholder="Jump to a resource type, namespace or cluster"
        className="h-11 w-full border-b border-line bg-transparent px-4 text-[14px] text-fg placeholder:text-muted focus:outline-none"
      />
      <Command.List className="max-h-[50vh] overflow-y-auto p-2">
        <Command.Empty className="px-3 py-6 text-center text-[13px] text-muted">Nothing matches.</Command.Empty>
        <Command.Group heading="Go to" className={heading}>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "clusters" }))}>Clusters</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "apply" }))}>Create or edit (form or YAML)</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "audit" }))}>Audit log</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "forwards" }))}>Port forwards</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "images" }))}>Images (local and Docker Hub)</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "local" }))}>Local clusters (k3d)</Command.Item>
          <Command.Item className={item} onSelect={() => run(() => navigate({ page: "vault" }))}>Vault (encrypted local secrets)</Command.Item>
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "overview" }))}>
              Overview (CPU and memory)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "canvas" }))}>
              Map (how everything connects)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "secrets" }))}>
              Secrets (and exposure scan)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "incident" }))}>
              Incident (what is broken and what changed)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "history" }))}>
              History (recorded changes and warnings)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "gitops" }))}>
              GitOps (Argo CD and Flux)
            </Command.Item>
          )}
          {active && (
            <Command.Item className={item} onSelect={() => run(() => navigate({ page: "helm" }))}>
              Helm releases
            </Command.Item>
          )}
        </Command.Group>
        {active && types.length > 0 && (
          <Command.Group heading="Resources" className={heading}>
            {types.map((t) => (
              <Command.Item
                key={`${t.group}/${t.resource}`}
                value={`${t.label} ${t.resource} ${t.group} ${(t.shortNames ?? []).join(" ")}`}
                className={item}
                onSelect={() => run(() => navigate({ page: "resources", group: t.group, resource: t.resource }))}
              >
                {t.label}
                <span className="ml-auto text-[11px] text-muted">
                  {(t.shortNames ?? []).join(", ") || t.group || "core"}
                </span>
              </Command.Item>
            ))}
          </Command.Group>
        )}
        {active && namespaces.length > 0 && (
          <Command.Group heading="Namespace" className={heading}>
            <Command.Item value="namespace all namespaces" className={item} onSelect={() => run(() => setNamespace(""))}>
              All namespaces
            </Command.Item>
            {namespaces.map((ns) => (
              <Command.Item key={ns} value={`namespace ${ns}`} className={item} onSelect={() => run(() => setNamespace(ns))}>
                ns: {ns}
              </Command.Item>
            ))}
          </Command.Group>
        )}
        <Command.Group heading="Clusters" className={heading}>
          {contexts.map((c) => (
            <Command.Item
              key={c.name}
              value={`cluster ${c.name}`}
              className={item}
              onSelect={() =>
                run(() => {
                  if (c.connected) setActive(c.name);
                  else if (!c.needsTrust || c.trusted) void connect(c.name).catch(() => navigate({ page: "clusters" }));
                  else navigate({ page: "clusters" });
                })
              }
            >
              {c.connected ? "Switch to" : "Connect to"} {c.name}
            </Command.Item>
          ))}
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  );
}
