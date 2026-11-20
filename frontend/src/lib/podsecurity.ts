// Pod Security Standards: which level a pod spec meets. Kubernetes enforces
// these with Pod Security Admission; Kupi checks them before anything is
// applied, so the Build canvas can show a badge on each workload.
// https://kubernetes.io/docs/concepts/security/pod-security-standards/

import { parseBytes, parseCPU } from "./quantity";

type Obj = Record<string, unknown>;

export type PssLevel = "restricted" | "baseline" | "privileged";

export interface PodSecurity {
  /** The strictest level the pod meets. */
  level: PssLevel;
  /** What keeps it from the next level up, in plain words. */
  problems: string[];
}

const BASELINE_CAPS = new Set(["AUDIT_WRITE", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "KILL", "MKNOD", "NET_BIND_SERVICE", "SETFCAP", "SETGID", "SETPCAP", "SETUID", "SYS_CHROOT"]);
const RESTRICTED_VOLUMES = new Set(["configMap", "csi", "downwardAPI", "emptyDir", "ephemeral", "persistentVolumeClaim", "projected", "secret"]);

function obj(v: unknown): Obj {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : {};
}

function list(v: unknown): Obj[] {
  return Array.isArray(v) ? (v.filter((x) => x && typeof x === "object") as Obj[]) : [];
}

/** The pod spec inside a workload, a CronJob or a bare pod. */
export function podSpecOf(o: Obj): Obj | null {
  const spec = obj(o.spec);
  switch (o.kind) {
    case "Pod":
      return spec;
    case "CronJob":
      return obj(obj(obj(obj(spec.jobTemplate).spec).template).spec);
    case "Deployment":
    case "StatefulSet":
    case "DaemonSet":
    case "ReplicaSet":
    case "Job":
      return obj(obj(spec.template).spec);
    default:
      return null;
  }
}

export function evaluatePod(spec: Obj): PodSecurity {
  const baseline: string[] = [];
  const restricted: string[] = [];
  const podCtx = obj(spec.securityContext);
  const containers = [...list(spec.initContainers), ...list(spec.containers), ...list(spec.ephemeralContainers)];

  // Baseline: no host access or privileged containers.
const HOST_FIELDS = { hostNetwork: "network", hostPID: "process list", hostIPC: "IPC" } as const;
  for (const [f, what] of Object.entries(HOST_FIELDS)) if (spec[f] === true) baseline.push(`uses the node's ${what} (${f})`);
  for (const v of list(spec.volumes)) {
    if (v.hostPath) baseline.push(`mounts a path from the node (volume ${String(v.name)})`);
    const type = Object.keys(v).find((k) => k !== "name");
    if (type && !RESTRICTED_VOLUMES.has(type) && !v.hostPath) restricted.push(`uses a ${type} volume (${String(v.name)})`);
  }
  if (obj(podCtx.seccompProfile).type === "Unconfined") baseline.push("turns seccomp off for the pod");

  let allNonRoot = true;
  let allSeccomp = true;
  const podNonRoot = podCtx.runAsNonRoot === true;
  const podSeccomp = ["RuntimeDefault", "Localhost"].includes(String(obj(podCtx.seccompProfile).type));
  if (podCtx.runAsUser === 0) restricted.push("runs as user 0 (root)");

  for (const c of containers) {
    const name = String(c.name ?? "");
    const ctx = obj(c.securityContext);
    if (ctx.privileged === true) baseline.push(`container ${name} is privileged`);
    for (const p of list(c.ports)) if (typeof p.hostPort === "number" && p.hostPort !== 0) baseline.push(`container ${name} opens port ${p.hostPort} on the node`);
    const caps = obj(ctx.capabilities);
    const add = (Array.isArray(caps.add) ? caps.add : []).map(String);
    const extra = add.filter((c2) => !BASELINE_CAPS.has(c2));
    if (extra.length) baseline.push(`container ${name} adds ${extra.join(", ")}`);
    if (ctx.procMount === "Unmasked") baseline.push(`container ${name} unmasks /proc`);
    if (obj(ctx.seccompProfile).type === "Unconfined") baseline.push(`container ${name} turns seccomp off`);

    if (ctx.allowPrivilegeEscalation !== false) restricted.push(`container ${name} may gain more privileges (allowPrivilegeEscalation isn't false)`);
    const drop = (Array.isArray(caps.drop) ? caps.drop : []).map(String);
    if (!drop.includes("ALL")) restricted.push(`container ${name} doesn't drop all capabilities`);
    const addRestricted = add.filter((c2) => c2 !== "NET_BIND_SERVICE");
    if (addRestricted.length && !extra.length) restricted.push(`container ${name} adds ${addRestricted.join(", ")}`);
    if (ctx.runAsUser === 0) restricted.push(`container ${name} runs as user 0 (root)`);
    const nonRoot = ctx.runAsNonRoot === true || (ctx.runAsNonRoot === undefined && podNonRoot);
    if (!nonRoot) allNonRoot = false;
    const seccomp = ["RuntimeDefault", "Localhost"].includes(String(obj(ctx.seccompProfile).type)) || (obj(ctx.seccompProfile).type === undefined && podSeccomp);
    if (!seccomp) allSeccomp = false;
  }
  if (!allNonRoot) restricted.push("may run as root (runAsNonRoot isn't set)");
  if (!allSeccomp) restricted.push("has no seccomp profile (RuntimeDefault)");

  if (baseline.length) return { level: "privileged", problems: baseline };
  if (restricted.length) return { level: "baseline", problems: restricted };
  return { level: "restricted", problems: [] };
}

/** CPU (millicores) and memory (bytes) the scheduler reserves for one pod. */
export function podRequests(spec: Obj): { cpu: number; mem: number } {
  const sum = (items: Obj[]) =>
    items.reduce<{ cpu: number; mem: number; maxCpu: number; maxMem: number }>(
      (acc, c) => {
        const req = obj(obj(c.resources).requests);
        const cpu = parseCPU(req.cpu) ?? 0;
        const mem = parseBytes(req.memory) ?? 0;
        return { cpu: acc.cpu + cpu, mem: acc.mem + mem, maxCpu: Math.max(acc.maxCpu, cpu), maxMem: Math.max(acc.maxMem, mem) };
      },
      { cpu: 0, mem: 0, maxCpu: 0, maxMem: 0 },
    );
  const main = sum(list(spec.containers));
  const init = sum(list(spec.initContainers));
  return { cpu: Math.round(Math.max(main.cpu, init.maxCpu)), mem: Math.max(main.mem, init.maxMem) };
}
