// The Build canvas model: blocks the user places, the links between them, and
// the manifest they turn into. Everything here is pure, so it's tested without
// a cluster. Nothing is applied from here: the manifest goes to the editor and
// through the Guard review like any other.

import { KINDS } from "./templates";

export type BlockKind =
  | "Deployment"
  | "StatefulSet"
  | "DaemonSet"
  | "Job"
  | "CronJob"
  | "Service"
  | "Ingress"
  | "ConfigMap"
  | "Secret"
  | "PersistentVolumeClaim"
  | "HorizontalPodAutoscaler";

/** An extra container in a workload's pods, next to the main one. */
export interface Sidecar {
  name: string;
  image: string;
  /** Split on spaces, no shell. Empty runs the image's own command. */
  command: string;
}

export interface Block {
  id: string;
  kind: BlockKind;
  name: string;
  /** Kind-specific settings, all kept as the text the user typed. */
  fields: Record<string, string>;
  sidecars?: Sidecar[];
}

export type LinkKind = "selects" | "routes" | "env" | "mounts" | "scales";

export interface Link {
  id: string;
  /** Always the side that refers to the other (Service → Deployment, Deployment → ConfigMap). */
  from: string;
  to: string;
  kind: LinkKind;
  /** Settings of the link itself: an Ingress route's path and Service port. */
  fields?: Record<string, string>;
}

export interface BuildModel {
  namespace: string;
  createNamespace: boolean;
  blocks: Block[];
  links: Link[];
}

export interface Issue {
  level: "error" | "warn";
  /** The block it's about, if any. */
  block?: string;
  /** The link it's about, if any. */
  link?: string;
  text: string;
}

type Obj = Record<string, unknown>;

export interface FieldSpec {
  key: string;
  label: string;
  hint?: string;
  placeholder?: string;
  /** A multi-line text area instead of one line. */
  multiline?: boolean;
  options?: string[];
}

export interface BlockSpec {
  kind: BlockKind;
  label: string;
  description: string;
  defaultName: string;
  defaults: Record<string, string>;
  fields: FieldSpec[];
}

/** Everything that runs pods. */
export const WORKLOADS = new Set<BlockKind>(["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"]);
/** Long-running workloads: they have ports and a Service can send them traffic. */
export const SERVED = new Set<BlockKind>(["Deployment", "StatefulSet", "DaemonSet"]);
const SCALABLE = new Set<BlockKind>(["Deployment", "StatefulSet"]);

const IMAGE = "nginxinc/nginx-unprivileged:1.27";
const imageField: FieldSpec = { key: "image", label: "Image", hint: "Pin a version or digest.", placeholder: "registry/app:1.0" };
const portsField: FieldSpec = {
  key: "ports",
  label: "Container ports",
  hint: "One name=port per line, like http=8080. Leave empty if the app doesn't listen.",
  multiline: true,
};
const commandField: FieldSpec = { key: "command", label: "Command", hint: "Split on spaces, no shell. Leave empty to use the image's own." };
const replicasField: FieldSpec = { key: "replicas", label: "Copies running", placeholder: "2" };

export const BLOCKS: BlockSpec[] = [
  {
    kind: "Deployment",
    label: "Deployment",
    description: "An app that keeps running",
    defaultName: "web",
    defaults: { image: IMAGE, ports: "http=8080", replicas: "2", command: "" },
    fields: [imageField, portsField, replicasField, commandField],
  },
  {
    kind: "StatefulSet",
    label: "StatefulSet",
    description: "Copies with their own name and storage",
    defaultName: "db",
    defaults: { image: IMAGE, ports: "http=8080", replicas: "3", command: "" },
    fields: [imageField, portsField, replicasField, commandField],
  },
  {
    kind: "DaemonSet",
    label: "DaemonSet",
    description: "One copy on every node",
    defaultName: "agent",
    defaults: { image: IMAGE, ports: "", command: "" },
    fields: [imageField, portsField, commandField],
  },
  {
    kind: "Job",
    label: "Job",
    description: "Runs once to completion",
    defaultName: "once",
    defaults: { image: "busybox:1.36", command: "echo done" },
    fields: [{ key: "image", label: "Image" }, { ...commandField, hint: "Split on spaces, no shell.", placeholder: "echo done" }],
  },
  {
    kind: "CronJob",
    label: "CronJob",
    description: "Runs on a schedule",
    defaultName: "nightly",
    defaults: { image: "busybox:1.36", command: "date", schedule: "0 3 * * *" },
    fields: [
      { key: "schedule", label: "Schedule", hint: "Cron format: minute hour day month weekday.", placeholder: "0 3 * * *" },
      { key: "image", label: "Image" },
      { ...commandField, hint: "Split on spaces, no shell." },
    ],
  },
  {
    kind: "Service",
    label: "Service",
    description: "A stable address for an app",
    defaultName: "web",
    defaults: { ports: "80:http", type: "ClusterIP" },
    fields: [
      {
        key: "ports",
        label: "Ports",
        hint: "One port:target per line. The target is a container port's name or number, like 80:http or 9090:9090.",
        multiline: true,
      },
      { key: "type", label: "Type", options: ["ClusterIP", "NodePort", "LoadBalancer"], hint: "ClusterIP is reachable inside the cluster only." },
    ],
  },
  {
    kind: "Ingress",
    label: "Ingress",
    description: "Routes outside traffic to Services",
    defaultName: "web",
    defaults: { host: "", className: "" },
    fields: [
      { key: "host", label: "Host name", hint: "Leave empty to match any host.", placeholder: "app.example.com" },
      { key: "className", label: "Ingress class", hint: "Leave empty for the cluster's default.", placeholder: "traefik" },
    ],
  },
  {
    kind: "ConfigMap",
    label: "ConfigMap",
    description: "Settings as environment variables",
    defaultName: "settings",
    defaults: { data: "LOG_LEVEL=info" },
    fields: [{ key: "data", label: "Keys and values", hint: "One KEY=value per line.", multiline: true }],
  },
  {
    kind: "Secret",
    label: "Secret (existing)",
    description: "Use a Secret that's already there",
    defaultName: "app-secret",
    defaults: {},
    fields: [],
  },
  {
    kind: "PersistentVolumeClaim",
    label: "PersistentVolumeClaim",
    description: "Storage that outlives pods",
    defaultName: "data",
    defaults: { size: "1Gi", storageClass: "" },
    fields: [
      { key: "size", label: "Size", placeholder: "1Gi" },
      { key: "storageClass", label: "Storage class", hint: "Leave empty for the cluster's default." },
    ],
  },
  {
    kind: "HorizontalPodAutoscaler",
    label: "HorizontalPodAutoscaler",
    description: "Adds copies under load",
    defaultName: "web",
    defaults: { min: "2", max: "5", cpu: "70" },
    fields: [
      { key: "min", label: "Fewest copies" },
      { key: "max", label: "Most copies" },
      { key: "cpu", label: "Target CPU use (%)", hint: "Of each container's CPU request." },
    ],
  },
];

export function blockSpec(kind: BlockKind): BlockSpec {
  return BLOCKS.find((b) => b.kind === kind)!;
}

/** A name not yet used by a block of this kind: web, web-2, web-3… */
export function freeName(kind: BlockKind, base: string, blocks: Block[]): string {
  const taken = new Set(blocks.filter((b) => b.kind === kind).map((b) => b.name));
  if (!taken.has(base)) return base;
  for (let i = 2; ; i++) if (!taken.has(`${base}-${i}`)) return `${base}-${i}`;
}

export function newBlock(kind: BlockKind, id: string, blocks: Block[]): Block {
  const spec = blockSpec(kind);
  const block: Block = { id, kind, name: freeName(kind, spec.defaultName, blocks), fields: { ...spec.defaults } };
  if (WORKLOADS.has(kind)) block.sidecars = [];
  return block;
}

/** The name of the headless Service Kupi adds for a StatefulSet. */
export function headlessName(statefulSet: string): string {
  return `${statefulSet.trim()}-headless`;
}

// --- Ports -----------------------------------------------------------------

export interface ContainerPort {
  name: string;
  port: number;
}

export interface ServicePort {
  port: number;
  /** A container port's name, or a number. */
  target: string | number;
}

const PORT_NAME = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

function validPortName(name: string): boolean {
  return name.length <= 15 && PORT_NAME.test(name) && /[a-z]/.test(name);
}

function validPort(n: number | null): n is number {
  return n !== null && n >= 1 && n <= 65535;
}

function lines(text: string | undefined): string[] {
  return (text ?? "")
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith("#"));
}

/** Container ports: "http=8080" per line, or a bare number (named port-8080). */
export function parsePorts(text: string | undefined): { ports: ContainerPort[]; bad: string[] } {
  const ports: ContainerPort[] = [];
  const bad: string[] = [];
  for (const line of lines(text)) {
    const i = line.indexOf("=");
    const name = i < 0 ? `port-${line}` : line.slice(0, i).trim();
    const port = int(i < 0 ? line : line.slice(i + 1));
    if (!validPort(port) || !validPortName(name)) bad.push(line);
    else ports.push({ name, port });
  }
  return { ports, bad };
}

/** Service ports: "80:http" or "80:8080" per line, or a bare number (same target). */
export function parseServicePorts(text: string | undefined): { ports: ServicePort[]; bad: string[] } {
  const ports: ServicePort[] = [];
  const bad: string[] = [];
  for (const line of lines(text)) {
    const i = line.indexOf(":");
    const port = int(i < 0 ? line : line.slice(0, i));
    const rawTarget = i < 0 ? line : line.slice(i + 1).trim();
    const targetNum = int(rawTarget);
    if (!validPort(port)) bad.push(line);
    else if (targetNum !== null) (validPort(targetNum) ? ports.push({ port, target: targetNum }) : bad.push(line));
    else if (validPortName(rawTarget)) ports.push({ port, target: rawTarget });
    else bad.push(line);
  }
  return { ports, bad };
}

// --- Links ----------------------------------------------------------------

const RULES: { from: BlockKind[]; to: BlockKind[]; kind: LinkKind }[] = [
  { from: ["Service"], to: ["Deployment", "StatefulSet", "DaemonSet"], kind: "selects" },
  { from: ["Ingress"], to: ["Service"], kind: "routes" },
  { from: ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"], to: ["ConfigMap", "Secret"], kind: "env" },
  { from: ["Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"], to: ["PersistentVolumeClaim"], kind: "mounts" },
  { from: ["HorizontalPodAutoscaler"], to: ["Deployment", "StatefulSet"], kind: "scales" },
];

/** Kinds that point at exactly one thing. */
const SINGLE_TARGET: Partial<Record<BlockKind, string>> = {
  Service: "A Service here sends traffic to one workload. Remove its current link first.",
  HorizontalPodAutoscaler: "An autoscaler scales one workload. Remove its current link first.",
};

export type LinkResult = { ok: true; link: Omit<Link, "id"> } | { ok: false; reason: string };

/**
 * What connecting two blocks means, whichever way the user dragged. Refuses
 * links that don't mean anything in Kubernetes, with a reason.
 */
export function linkFor(a: Block, b: Block, links: Link[]): LinkResult {
  if (a.id === b.id) return { ok: false, reason: "A block can't connect to itself." };
  for (const [x, y] of [
    [a, b],
    [b, a],
  ]) {
    const rule = RULES.find((r) => r.from.includes(x.kind) && r.to.includes(y.kind));
    if (!rule) continue;
    if (links.some((l) => l.from === x.id && l.to === y.id)) return { ok: false, reason: `${x.kind} ${x.name} already connects to ${y.kind} ${y.name}.` };
    const single = SINGLE_TARGET[x.kind];
    if (single && links.some((l) => l.from === x.id)) return { ok: false, reason: single };
    const link: Omit<Link, "id"> = { from: x.id, to: y.id, kind: rule.kind };
    // The first route gets "/", later ones a path named after their Service.
    if (rule.kind === "routes") link.fields = { path: links.some((l) => l.from === x.id) ? `/${y.name.trim()}` : "/", port: "" };
    return { ok: true, link };
  }
  return { ok: false, reason: `${a.kind} and ${b.kind} don't connect directly. ${hintFor(a.kind, b.kind)}`.trim() };
}

function hintFor(a: BlockKind, b: BlockKind): string {
  const pair = new Set([a, b]);
  const other = (k: BlockKind) => (a === k ? b : a);
  if (pair.has("Ingress") && WORKLOADS.has(other("Ingress"))) return "Put a Service between them.";
  if (pair.has("Service") && (pair.has("Job") || pair.has("CronJob"))) return "Services send traffic to long-running apps, not Jobs.";
  if (pair.has("Service") && (pair.has("ConfigMap") || pair.has("Secret") || pair.has("PersistentVolumeClaim"))) return "Connect the config or storage to the workload instead.";
  if (pair.has("HorizontalPodAutoscaler") && pair.has("DaemonSet")) return "A DaemonSet runs one copy per node, so it can't be autoscaled.";
  if (pair.has("HorizontalPodAutoscaler") && (pair.has("Job") || pair.has("CronJob"))) return "Autoscalers scale Deployments and StatefulSets.";
  return "";
}

export const LINK_LABEL: Record<LinkKind, string> = {
  selects: "sends traffic to",
  routes: "routes to",
  env: "env from",
  mounts: "mounts",
  scales: "scales",
};

// --- Checks -----------------------------------------------------------------

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const DNS_1035 = /^[a-z]([-a-z0-9]*[a-z0-9])?$/;
const DNS_SUBDOMAIN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/;
const ENV_KEY = /^[-._a-zA-Z0-9]+$/;
const QUANTITY = /^[0-9]+(\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$/;

function int(text: string | undefined): number | null {
  const t = (text ?? "").trim();
  return /^[0-9]+$/.test(t) ? Number(t) : null;
}

/** Parses ConfigMap lines. Blank lines and lines starting with # are skipped. */
export function parseData(text: string): { data: Record<string, string>; bad: string[] } {
  const data: Record<string, string> = {};
  const bad: string[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const i = line.indexOf("=");
    const key = i < 0 ? line : line.slice(0, i).trim();
    if (i < 0 || !ENV_KEY.test(key) || key.length > 253) {
      bad.push(short(line));
      continue;
    }
    data[key] = line.slice(i + 1);
  }
  return { data, bad };
}

function short(line: string): string {
  return line.length > 40 ? `${line.slice(0, 40)}…` : line;
}

function words(text: string | undefined): string[] {
  return (text ?? "").trim().split(/\s+/).filter(Boolean);
}

function unpinned(image: string): boolean {
  return /:latest$/.test(image) || !/[:@]/.test(image.split("/").pop() ?? "");
}

export function check(model: BuildModel): Issue[] {
  const issues: Issue[] = [];
  const add = (level: Issue["level"], text: string, block?: Block, link?: Link) => issues.push({ level, text, block: block?.id, link: link?.id });
  const byId = new Map(model.blocks.map((b) => [b.id, b]));
  const linksFrom = (b: Block) => model.links.filter((l) => l.from === b.id);
  const ns = model.namespace.trim();
  if (!ns) add("error", "Pick a namespace for these objects.");
  else if (!DNS_LABEL.test(ns) || ns.length > 63) add("error", `"${ns}" isn't a valid namespace name: lowercase letters, digits and dashes, up to 63 characters.`);
  if (model.blocks.length === 0) add("warn", "The canvas is empty. Drag a block from the left to start.");

  const seen = new Map<string, Block>();
  const workloadNames = new Map<string, Block>();
  for (const b of model.blocks) {
    const label = `${b.kind} ${b.name || "(no name)"}`;
    const name = b.name.trim();
    const rule = b.kind === "Service" ? DNS_1035 : b.kind === "ConfigMap" || b.kind === "Secret" || b.kind === "PersistentVolumeClaim" ? DNS_SUBDOMAIN : DNS_LABEL;
    const max = b.kind === "CronJob" || b.kind === "StatefulSet" ? 52 : 63;
    if (!name) add("error", `A ${b.kind} has no name.`, b);
    else if (!rule.test(name) || name.length > max)
      add("error", `${label}: names use lowercase letters, digits and dashes${b.kind === "Service" ? ", start with a letter" : ""}, up to ${max} characters.`, b);
    const key = `${b.kind}/${name}`;
    if (name && seen.has(key)) add("error", `Two ${b.kind} blocks are called ${name}.`, b);
    seen.set(key, b);

    const f = b.fields;
    if (WORKLOADS.has(b.kind)) {
      // Workloads label their pods app=<name>, so two with one name would share pods' label.
      const twin = workloadNames.get(name);
      if (name && twin && twin.kind !== b.kind)
        add("error", `${twin.kind} ${name} and ${b.kind} ${name} would both label their pods app=${name}, so a Service would send traffic to both. Rename one.`, b);
      workloadNames.set(name, b);
      checkContainer(add, label, b, f.image ?? "");
      const containerNames = new Set([name]);
      for (const [i, s] of (b.sidecars ?? []).entries()) {
        const sl = `${label}, sidecar ${s.name.trim() || i + 1}`;
        if (!DNS_LABEL.test(s.name.trim()) || s.name.trim().length > 63) add("error", `${sl}: container names use lowercase letters, digits and dashes.`, b);
        else if (containerNames.has(s.name.trim())) add("error", `${label}: two containers are called ${s.name.trim()}.`, b);
        containerNames.add(s.name.trim());
        checkContainer(add, sl, b, s.image);
      }
    }
    if (SERVED.has(b.kind)) {
      const { ports, bad } = parsePorts(f.ports);
      if (bad.length) add("error", `${label}: these container ports aren't name=port (names up to 15 lowercase letters, digits and dashes): ${bad.map(short).join(", ")}.`, b);
      const names = ports.map((p) => p.name);
      const nums = ports.map((p) => p.port);
      if (new Set(names).size !== names.length || new Set(nums).size !== nums.length) add("error", `${label}: each container port needs its own name and number.`, b);
    }
    if (SCALABLE.has(b.kind)) {
      const r = int(f.replicas);
      if (r === null || r > 1000) add("error", `${label}: copies running must be a whole number up to 1000.`, b);
    }
    if (b.kind === "StatefulSet" && model.blocks.some((x) => x.kind === "Service" && x.name.trim() === headlessName(name)))
      add("error", `${label} gets a headless Service called ${headlessName(name)}, and a Service block already has that name.`, b);
    if (b.kind === "CronJob" && words(f.schedule).length !== 5 && !(f.schedule ?? "").trim().startsWith("@"))
      add("error", `${label}: the schedule needs five fields, like 0 3 * * *.`, b);
    if (b.kind === "Service") checkService(add, label, b, model);
    if (b.kind === "Ingress") {
      const routes = linksFrom(b);
      if (routes.length === 0) add("error", `${label} needs a Service to route to. Connect it to one.`, b);
      const host = (f.host ?? "").trim();
      if (host && (!DNS_SUBDOMAIN.test(host.replace(/^\*\./, "")) || host.length > 253)) add("error", `${label}: "${host}" isn't a valid host name.`, b);
      const paths = new Set<string>();
      for (const l of routes) {
        const svc = byId.get(l.to);
        const path = (l.fields?.path ?? "/").trim();
        if (!path.startsWith("/")) add("error", `${label}: the path to Service ${svc?.name} must start with /.`, b, l);
        else if (paths.has(path)) add("error", `${label}: two routes use the path ${path}. Click a link to change its path.`, b, l);
        paths.add(path);
        const port = (l.fields?.port ?? "").trim();
        if (port && svc && !parseServicePorts(svc.fields.ports).ports.some((p) => p.port === int(port)))
          add("error", `${label}: Service ${svc.name} has no port ${port}.`, b, l);
      }
    }
    if (b.kind === "ConfigMap") {
      const { data, bad } = parseData(f.data ?? "");
      if (bad.length) add("error", `${label}: these lines aren't KEY=value: ${bad.join(", ")}.`, b);
      else if (Object.keys(data).length === 0) add("warn", `${label} has no keys.`, b);
    }
    if (b.kind === "Secret") add("warn", `Secret ${b.name} isn't created here: it must already exist in ${ns || "the namespace"}. Create it on the Secrets page so its values never pass through a manifest.`, b);
    if (b.kind === "PersistentVolumeClaim") {
      if (!QUANTITY.test((f.size ?? "").trim())) add("error", `${label}: the size must look like 1Gi or 500Mi.`, b);
      const users = model.links.filter((l) => l.to === b.id).map((l) => byId.get(l.from)).filter(Boolean) as Block[];
      const sts = users.find((u) => u.kind === "StatefulSet");
      if (sts) {
        if (users.length > 1) add("error", `${label} gives each copy of StatefulSet ${sts.name} its own claim, so it can't also be used by other workloads. Add a second PersistentVolumeClaim block for them.`, b);
        if (!DNS_LABEL.test(name)) add("error", `${label}: a StatefulSet's claim name can't contain dots.`, b);
      } else if (users.length > 1 || users.some((u) => u.kind === "DaemonSet" || (u.kind === "Deployment" && (int(u.fields.replicas) ?? 1) > 1)))
        add("warn", `${label} is ReadWriteOnce: it attaches to one node at a time, so several copies may not all start. Use one copy, or a StatefulSet so each copy gets its own.`, b);
    }
    if (b.kind === "HorizontalPodAutoscaler") {
      const min = int(f.min);
      const max = int(f.max);
      const cpu = int(f.cpu);
      if (min === null || min < 1 || max === null || max < min || max > 1000) add("error", `${label}: fewest copies must be at least 1 and no more than most copies (up to 1000).`, b);
      if (cpu === null || cpu < 1 || cpu > 1000) add("error", `${label}: target CPU use must be a percentage from 1 to 1000.`, b);
      if (linksFrom(b).length === 0) add("error", `${label} needs a Deployment or StatefulSet to scale. Connect it to one.`, b);
    }
  }
  return issues;
}

type Add = (level: Issue["level"], text: string, block?: Block, link?: Link) => void;

function checkContainer(add: Add, label: string, b: Block, image: string) {
  const img = image.trim();
  if (!img) add("error", `${label} needs an image.`, b);
  else if (/\s/.test(img)) add("error", `${label}: the image can't contain spaces.`, b);
  else if (unpinned(img)) add("warn", `${label} uses an image without a fixed version. Pin a tag or digest so every copy runs the same code.`, b);
}

function checkService(add: Add, label: string, b: Block, model: BuildModel) {
  const { ports, bad } = parseServicePorts(b.fields.ports);
  if (bad.length) add("error", `${label}: these ports aren't port:target: ${bad.map(short).join(", ")}.`, b);
  else if (ports.length === 0) add("error", `${label} needs at least one port.`, b);
  if (new Set(ports.map((p) => p.port)).size !== ports.length) add("error", `${label}: each port number can appear once.`, b);
  const link = model.links.find((l) => l.from === b.id && l.kind === "selects");
  if (!link) add("warn", `${label} doesn't send traffic anywhere yet. Connect it to a Deployment, StatefulSet or DaemonSet.`, b);
  else {
    const w = model.blocks.find((x) => x.id === link.to);
    if (w) {
      const cports = parsePorts(w.fields.ports).ports;
      for (const p of ports) {
        if (typeof p.target === "string" && !cports.some((c) => c.name === p.target))
          add("error", `${label} sends port ${p.port} to "${p.target}", but ${w.kind} ${w.name} has no container port with that name.`, b);
        else if (typeof p.target === "number" && cports.length > 0 && !cports.some((c) => c.port === p.target))
          add("warn", `${label} sends port ${p.port} to ${p.target}, which isn't one of ${w.kind} ${w.name}'s container ports.`, b);
      }
    }
  }
  if (b.fields.type === "LoadBalancer") add("warn", `${label} is a LoadBalancer: on a cloud cluster this makes it reachable from the internet and may cost money.`, b);
}

// --- Manifest ----------------------------------------------------------------

function deploymentTemplate(ns: string): Obj {
  return structuredClone(KINDS.find((k) => k.kind === "Deployment")!.template!(ns)) as Obj;
}

function template(kind: BlockKind, ns: string): Obj {
  if (kind === "StatefulSet" || kind === "DaemonSet") {
    const obj = deploymentTemplate(ns);
    obj.kind = kind;
    return obj;
  }
  return structuredClone(KINDS.find((k) => k.kind === kind)!.template!(ns)) as Obj;
}

function podSpecPath(kind: BlockKind): string[] {
  return kind === "CronJob" ? ["spec", "jobTemplate", "spec", "template", "spec"] : ["spec", "template", "spec"];
}

function at(obj: Obj, path: string[]): Obj {
  let cur = obj;
  for (const p of path) cur = cur[p] as Obj;
  return cur;
}

/** Volume names are DNS labels of at most 63 characters. */
function volumeName(pvc: string): string {
  return `pvc-${pvc}`.slice(0, 63).replace(/[^a-z0-9-]/g, "-").replace(/-+$/, "");
}

function claimSpec(pvc: Block): Obj {
  const spec: Obj = { accessModes: ["ReadWriteOnce"], resources: { requests: { storage: (pvc.fields.size ?? "").trim() || "1Gi" } } };
  if ((pvc.fields.storageClass ?? "").trim()) spec.storageClassName = pvc.fields.storageClass.trim();
  return spec;
}

const ORDER: BlockKind[] = ["ConfigMap", "PersistentVolumeClaim", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "Service", "Ingress", "HorizontalPodAutoscaler"];

/**
 * The objects the canvas describes, in apply order. Secret blocks are
 * references only and produce nothing; a claim used by a StatefulSet becomes
 * its per-copy template. Call check() first: this assumes the fields are
 * valid and fills gaps with safe defaults.
 */
export function buildObjects(model: BuildModel): Obj[] {
  const ns = model.namespace.trim() || "default";
  const byId = new Map(model.blocks.map((b) => [b.id, b]));
  const out: Obj[] = [];
  if (model.createNamespace) out.push({ apiVersion: "v1", kind: "Namespace", metadata: { name: ns } });
  const usedByStatefulSet = new Set(model.links.filter((l) => l.kind === "mounts" && byId.get(l.from)?.kind === "StatefulSet").map((l) => l.to));

  const blocks = [...model.blocks].sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind));
  for (const b of blocks) {
    const f = b.fields;
    const name = b.name.trim();
    const linked = (kind: LinkKind) => model.links.filter((l) => l.from === b.id && l.kind === kind);
    const targets = (kind: LinkKind) => linked(kind).map((l) => byId.get(l.to)).filter(Boolean) as Block[];

    if (WORKLOADS.has(b.kind)) {
      out.push(...workloadObjects(b, ns, targets("env"), targets("mounts")));
      continue;
    }
    switch (b.kind) {
      case "Service": {
        const w = targets("selects")[0];
        const names = new Set<string>();
        const ports = parseServicePorts(f.ports).ports.map((p) => {
          let pname = typeof p.target === "string" ? p.target : `port-${p.port}`;
          if (names.has(pname)) pname = `port-${p.port}`;
          names.add(pname);
          return { name: pname, port: p.port, targetPort: p.target };
        });
        const spec: Obj = { type: f.type || "ClusterIP", ports };
        if (w) spec.selector = { app: w.name.trim() };
        out.push({ apiVersion: "v1", kind: "Service", metadata: { name, namespace: ns }, spec });
        break;
      }
      case "Ingress": {
        const paths = linked("routes").map((l) => {
          const svc = byId.get(l.to);
          const first = parseServicePorts(svc?.fields.ports).ports[0]?.port ?? 80;
          return {
            path: (l.fields?.path ?? "").trim() || "/",
            pathType: "Prefix",
            backend: { service: { name: svc?.name.trim() ?? "", port: { number: int(l.fields?.port) ?? first } } },
          };
        });
        const rule: Obj = { http: { paths } };
        const host = (f.host ?? "").trim();
        if (host) rule.host = host;
        const spec: Obj = { rules: [rule] };
        if ((f.className ?? "").trim()) spec.ingressClassName = f.className.trim();
        out.push({ apiVersion: "networking.k8s.io/v1", kind: "Ingress", metadata: { name, namespace: ns }, spec });
        break;
      }
      case "ConfigMap":
        out.push({ apiVersion: "v1", kind: "ConfigMap", metadata: { name, namespace: ns }, data: parseData(f.data ?? "").data });
        break;
      case "PersistentVolumeClaim":
        if (!usedByStatefulSet.has(b.id)) out.push({ apiVersion: "v1", kind: "PersistentVolumeClaim", metadata: { name, namespace: ns }, spec: claimSpec(b) });
        break;
      case "HorizontalPodAutoscaler": {
        const w = targets("scales")[0];
        out.push({
          apiVersion: "autoscaling/v2",
          kind: "HorizontalPodAutoscaler",
          metadata: { name, namespace: ns },
          spec: {
            scaleTargetRef: { apiVersion: "apps/v1", kind: w?.kind ?? "Deployment", name: w?.name.trim() ?? "" },
            minReplicas: int(f.min) ?? 1,
            maxReplicas: int(f.max) ?? 1,
            metrics: [{ type: "Resource", resource: { name: "cpu", target: { type: "Utilization", averageUtilization: int(f.cpu) ?? 70 } } }],
          },
        });
        break;
      }
      case "Secret":
        break;
    }
  }
  return out;
}

/** A workload, plus the headless Service a StatefulSet needs. */
function workloadObjects(b: Block, ns: string, env: Block[], claims: Block[]): Obj[] {
  const f = b.fields;
  const name = b.name.trim();
  const obj = template(b.kind, ns);
  const meta = obj.metadata as Obj;
  meta.name = name;
  const spec = obj.spec as Obj;
  const pod = at(obj, podSpecPath(b.kind));
  const main = (pod.containers as Obj[])[0];
  const security = structuredClone(main.securityContext);
  main.name = name;
  main.image = (f.image ?? "").trim();
  const command = words(f.command);
  if (command.length) main.command = command;
  else delete main.command;
  const out: Obj[] = [];

  if (SERVED.has(b.kind)) {
    const labels = { app: name };
    meta.labels = labels;
    spec.selector = { matchLabels: { ...labels } };
    (spec.template as Obj).metadata = { labels: { ...labels } };
    // The template's probe fits its own image only.
    delete main.readinessProbe;
    const ports = parsePorts(f.ports).ports;
    if (ports.length) main.ports = ports.map((p) => ({ name: p.name, containerPort: p.port }));
    else delete main.ports;
    if (b.kind === "DaemonSet") delete spec.replicas;
    else spec.replicas = int(f.replicas) ?? 1;
    if (b.kind === "StatefulSet") {
      spec.serviceName = headlessName(name);
      out.push({
        apiVersion: "v1",
        kind: "Service",
        metadata: { name: headlessName(name), namespace: ns, labels: { ...labels } },
        spec: { clusterIP: "None", selector: { ...labels }, ports: ports.map((p) => ({ name: p.name, port: p.port, targetPort: p.name })) },
      });
    }
  } else if (b.kind === "CronJob") {
    spec.schedule = (f.schedule ?? "").trim();
  }

  const envFrom = [
    ...env.filter((t) => t.kind === "ConfigMap").map((t) => ({ configMapRef: { name: t.name.trim() } })),
    ...env.filter((t) => t.kind === "Secret").map((t) => ({ secretRef: { name: t.name.trim() } })),
  ];
  if (envFrom.length) main.envFrom = envFrom;

  if (claims.length) {
    const mounts = (main.volumeMounts as Obj[] | undefined) ?? [];
    const volumes = (pod.volumes as Obj[] | undefined) ?? [];
    const templates: Obj[] = [];
    for (const pvc of claims) {
      const claim = pvc.name.trim();
      if (b.kind === "StatefulSet") {
        // Each copy gets its own claim, named <claim>-<statefulset>-<n>.
        templates.push({ metadata: { name: claim }, spec: claimSpec(pvc) });
        mounts.push({ name: claim, mountPath: `/data/${claim}` });
      } else {
        const vol = volumeName(claim);
        volumes.push({ name: vol, persistentVolumeClaim: { claimName: claim } });
        mounts.push({ name: vol, mountPath: `/data/${claim}` });
      }
    }
    main.volumeMounts = mounts;
    pod.volumes = volumes;
    if (templates.length) spec.volumeClaimTemplates = templates;
  }

  for (const s of b.sidecars ?? []) {
    const c: Obj = {
      name: s.name.trim(),
      image: s.image.trim(),
      resources: { requests: { cpu: "50m", memory: "32Mi" }, limits: { memory: "128Mi" } },
      securityContext: structuredClone(security),
    };
    const cmd = words(s.command);
    if (cmd.length) c.command = cmd;
    (pod.containers as Obj[]).push(c);
  }

  // The headless Service goes before its StatefulSet.
  return [...out, obj];
}

/** One line under a block's name on the canvas. */
export function blockSummary(b: Block): string {
  const f = b.fields;
  const extra = (b.sidecars ?? []).length ? ` · +${(b.sidecars ?? []).length} sidecar${(b.sidecars ?? []).length === 1 ? "" : "s"}` : "";
  switch (b.kind) {
    case "Deployment":
    case "StatefulSet":
      return `${f.image || "no image"} · ×${f.replicas || "?"}${extra}`;
    case "DaemonSet":
      return `${f.image || "no image"} · every node${extra}`;
    case "Job":
      return `${f.image || "no image"}${extra}`;
    case "CronJob":
      return `${f.schedule || "no schedule"} · ${f.image || "no image"}${extra}`;
    case "Service": {
      const ports = parseServicePorts(f.ports).ports.map((p) => p.port);
      return `${f.type || "ClusterIP"} · ${ports.length ? `port ${ports.join(", ")}` : "no ports"}`;
    }
    case "Ingress":
      return f.host || "any host";
    case "ConfigMap": {
      const n = Object.keys(parseData(f.data ?? "").data).length;
      return `${n} ${n === 1 ? "key" : "keys"}`;
    }
    case "Secret":
      return "must already exist";
    case "PersistentVolumeClaim":
      return f.size || "no size";
    case "HorizontalPodAutoscaler":
      return `${f.min || "?"}–${f.max || "?"} copies at ${f.cpu || "?"}% CPU`;
  }
}
