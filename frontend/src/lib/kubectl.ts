// Builds the kubectl command equivalent to what Kupi does, so every action is
// transparent and users learn the CLI as they go.

export interface KubeTarget {
  context: string;
  group: string;
  resource: string;
  namespace?: string;
  name?: string;
}

/** Quotes an argument only when the shell would need it. */
export function shellArg(value: string): string {
  return /^[A-Za-z0-9._\-/:=@]+$/.test(value) ? value : `'${value.replace(/'/g, `'\\''`)}'`;
}

/** "deployments.apps" for named groups, "pods" for the core group. */
export function qualifiedResource(group: string, resource: string): string {
  return group ? `${resource}.${group}` : resource;
}

function base(t: KubeTarget): string[] {
  const parts = ["kubectl", "--context", shellArg(t.context)];
  if (t.namespace) parts.push("-n", shellArg(t.namespace));
  return parts;
}

export function kubectlGet(t: KubeTarget, allNamespaces = false): string {
  const parts = ["kubectl", "--context", shellArg(t.context), "get", qualifiedResource(t.group, t.resource)];
  if (t.name) parts.push(shellArg(t.name));
  if (allNamespaces) parts.push("--all-namespaces");
  else if (t.namespace) parts.push("-n", shellArg(t.namespace));
  return parts.join(" ");
}

export function kubectlYaml(t: KubeTarget): string {
  return [...base(t), "get", qualifiedResource(t.group, t.resource), shellArg(t.name ?? ""), "-o", "yaml"].join(" ");
}

export function kubectlDelete(t: KubeTarget): string {
  return [...base(t), "delete", qualifiedResource(t.group, t.resource), shellArg(t.name ?? "")].join(" ");
}

export function kubectlScale(t: KubeTarget, replicas: number): string {
  return [...base(t), "scale", qualifiedResource(t.group, t.resource), shellArg(t.name ?? ""), `--replicas=${replicas}`].join(" ");
}

export function kubectlRestart(t: KubeTarget): string {
  return [...base(t), "rollout", "restart", `${qualifiedResource(t.group, t.resource)}/${t.name ?? ""}`].join(" ");
}

export interface LogsOptions {
  container?: string;
  follow?: boolean;
  previous?: boolean;
  tail?: number;
  timestamps?: boolean;
  workload?: boolean;
}

export function kubectlLogs(t: KubeTarget, o: LogsOptions = {}): string {
  const target = o.workload ? `${qualifiedResource(t.group, t.resource)}/${t.name ?? ""}` : shellArg(t.name ?? "");
  const parts = [...base(t), "logs", target];
  if (o.workload) parts.push("--all-pods=true", "--prefix");
  if (o.container) parts.push("-c", shellArg(o.container));
  if (o.follow) parts.push("-f");
  if (o.previous) parts.push("--previous");
  if (o.timestamps) parts.push("--timestamps");
  if (o.tail) parts.push(`--tail=${o.tail}`);
  return parts.join(" ");
}

export function kubectlExec(t: KubeTarget, container?: string): string {
  const parts = [...base(t), "exec", "-it", shellArg(t.name ?? "")];
  if (container) parts.push("-c", shellArg(container));
  parts.push("--", "sh");
  return parts.join(" ");
}
