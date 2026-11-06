// Ready-made commands for the terminal, and the paste check. Snippets are
// typed into the terminal for the user to finish and run; Kupi never presses
// Enter for them.

export interface Snippet {
  group: string;
  name: string;
  command: string;
}

export const BUILTIN_SNIPPETS: Snippet[] = [
  { group: "Kubernetes", name: "Pods here, with nodes and IPs", command: "kubectl get pods -o wide" },
  { group: "Kubernetes", name: "Watch pods change", command: "kubectl get pods -w" },
  { group: "Kubernetes", name: "Pods that aren't running, everywhere", command: "kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded" },
  { group: "Kubernetes", name: "Recent events, newest last", command: "kubectl get events --sort-by=.lastTimestamp" },
  { group: "Kubernetes", name: "Warnings only", command: "kubectl get events --field-selector type=Warning --sort-by=.lastTimestamp" },
  { group: "Kubernetes", name: "Top pods by memory", command: "kubectl top pods --sort-by=memory" },
  { group: "Kubernetes", name: "Top nodes", command: "kubectl top nodes" },
  { group: "Kubernetes", name: "What may my account do here?", command: "kubectl auth can-i --list" },
  { group: "Kubernetes", name: "Follow a Deployment's logs", command: "kubectl logs -f deployment/NAME --all-containers --since=10m" },
  { group: "Kubernetes", name: "Logs of the previous (crashed) container", command: "kubectl logs POD --previous" },
  { group: "Kubernetes", name: "Rollout status", command: "kubectl rollout status deployment/NAME" },
  { group: "Kubernetes", name: "Rollout history", command: "kubectl rollout history deployment/NAME" },
  { group: "Kubernetes", name: "Throwaway debug pod", command: "kubectl run debug-$RANDOM --rm -it --image=busybox:1.36 --restart=Never -- sh" },
  { group: "Kubernetes", name: "Explain a field", command: "kubectl explain deployment.spec.strategy" },
  { group: "Kubernetes", name: "Every resource type", command: "kubectl api-resources" },
  { group: "Kubernetes", name: "Contexts (the one marked * is in use here)", command: "kubectl config get-contexts" },
  { group: "Helm", name: "Every release", command: "helm list -A" },
  { group: "Helm", name: "A release's history", command: "helm history RELEASE" },
  { group: "Helm", name: "Values a release uses", command: "helm get values RELEASE" },
  { group: "Helm", name: "Preview an upgrade as a diff", command: "helm template RELEASE CHART | kubectl diff -f -" },
  { group: "Docker", name: "Running containers", command: 'docker ps --format "table {{.Names}}\\t{{.Image}}\\t{{.Status}}"' },
  { group: "Docker", name: "Disk usage", command: "docker system df" },
  { group: "Docker", name: "CPU and memory per container", command: "docker stats --no-stream" },
  { group: "Docker", name: "Images with their size", command: 'docker images --format "table {{.Repository}}:{{.Tag}}\\t{{.Size}}"' },
  { group: "Local clusters", name: "k3d clusters", command: "k3d cluster list" },
  { group: "Local clusters", name: "Load a local image into a k3d cluster", command: "k3d image import IMAGE -c CLUSTER" },
];

// Commands that delete, evict or roll back. Pasted while connected to
// production, they get a second look.
const RISKY: RegExp[] = [
  /\bkubectl\b.*\b(delete|drain|cordon|taint|replace\s+--force)\b/i,
  /\bkubectl\b.*\bapply\b.*--force\b/i,
  /\bkubectl\b.*\bscale\b.*--replicas[= ]0\b/i,
  /\bkubectl\b.*\brollout\s+undo\b/i,
  /\bhelm\s+(uninstall|delete|rollback)\b/i,
  /\bdocker\s+(rm|rmi|kill)\b/i,
  /\bdocker\s+(system|volume|image|container)\s+prune\b/i,
  /\bk3d\s+cluster\s+delete\b/i,
  /\brm\s+-[a-z]*r[a-z]*\b/i,
  /\bRemove-Item\b.*-Recurse\b/i,
];

export interface PasteCheck {
  /** Lines with something on them. */
  lines: string[];
  /** A newline in a paste runs the command straight away in most shells. */
  runsImmediately: boolean;
  risky: string[];
  needsConfirm: boolean;
}

export function checkPaste(text: string, production: boolean): PasteCheck {
  const normal = text.replace(/\r\n?/g, "\n");
  const lines = normal.split("\n").filter((l) => l.trim() !== "");
  const runsImmediately = normal.includes("\n");
  const risky = lines.filter((l) => RISKY.some((re) => re.test(l)));
  return { lines, runsImmediately, risky, needsConfirm: runsImmediately || (production && risky.length > 0) };
}

/** Groups snippets in their first-seen order. */
export function groupSnippets(list: Snippet[]): [string, Snippet[]][] {
  const map = new Map<string, Snippet[]>();
  for (const s of list) map.set(s.group, [...(map.get(s.group) ?? []), s]);
  return [...map.entries()];
}

export function matchesSnippet(s: Snippet, query: string): boolean {
  const q = query.trim().toLowerCase();
  return !q || s.name.toLowerCase().includes(q) || s.command.toLowerCase().includes(q) || s.group.toLowerCase().includes(q);
}
