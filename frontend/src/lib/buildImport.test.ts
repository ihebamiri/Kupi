import { describe, expect, it } from "vitest";
import { blockChanged, buildObjects, buildPlan, check, linkFor, newBlock, podLabels, rebase, type Block, type BlockKind, type BuildModel, type Link } from "./build";
import { importGraph } from "./buildImport";
import type { GraphEdge, GraphNode } from "./canvas";

type Obj = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

function block(kind: BlockKind, id: string, patch: Partial<Block> = {}): Block {
  const b = newBlock(kind, id, []);
  return { ...b, ...patch, fields: { ...b.fields, ...(patch.fields ?? {}) } };
}

function connect(blocks: Block[], pairs: [string, string][], existing: Link[] = []): Link[] {
  const links = [...existing];
  for (const [a, b] of pairs) {
    const r = linkFor(blocks.find((x) => x.id === a)!, blocks.find((x) => x.id === b)!, links);
    if (!r.ok) throw new Error(r.reason);
    links.push({ id: `${a}-${b}`, ...r.link });
  }
  return links;
}

function model(blocks: Block[], links: Link[], extra: Partial<BuildModel> = {}): BuildModel {
  return { namespace: "shop", createNamespace: false, blocks, links, ...extra };
}

const errors = (m: BuildModel) =>
  check(m)
    .filter((i) => i.level === "error")
    .map((i) => i.text);

describe("traffic lockdown", () => {
  const web = block("Deployment", "w");
  const api = block("Deployment", "a", { name: "api" });
  const svc = block("Service", "s");
  const ing = block("Ingress", "i");
  const blocks = [web, api, svc, ing];
  const links = connect(blocks, [
    ["w", "a"],
    ["s", "w"],
    ["i", "s"],
  ]);

  it("draws a call from the caller to the callee", () => {
    expect(links[0]).toEqual({ id: "w-a", from: "w", to: "a", kind: "talks", fields: { port: "" } });
    // Dragged the other way, api calls web.
    const back = linkFor(api, web, []);
    expect(back.ok && back.link.from).toBe("a");
  });

  it("writes one policy per workload that lets in only what was drawn", () => {
    const withPort = links.map((l) => (l.kind === "talks" ? { ...l, fields: { port: "http" } } : l));
    const objs = buildObjects(model(blocks, withPort, { lockdown: true, ingressNamespace: "kube-system" })) as Obj[];
    const pol = (name: string) => objs.find((o) => o.kind === "NetworkPolicy" && o.metadata.name === name)!;
    expect(pol("api-allow").spec).toEqual({
      podSelector: { matchLabels: { app: "api" } },
      policyTypes: ["Ingress"],
      ingress: [{ from: [{ podSelector: { matchLabels: { app: "web" } } }], ports: [{ protocol: "TCP", port: "http" }] }],
    });
    expect(pol("web-allow").spec.ingress).toEqual([{ from: [{ namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "kube-system" } } }], ports: [{ protocol: "TCP", port: "http" }] }]);
    expect(pol("web-allow").metadata.labels).toEqual({ "app.kubernetes.io/managed-by": "kupi" });
  });

  it("writes no policies without lockdown, and says the links do nothing yet", () => {
    const m = model(blocks, links);
    expect(buildObjects(m).some((o) => o.kind === "NetworkPolicy")).toBe(false);
    expect(check(m).map((i) => i.text)).toContain(`"May call" links only take effect with Lock down traffic switched on.`);
  });

  it("needs the Ingress controller's namespace", () => {
    expect(errors(model(blocks, links, { lockdown: true, ingressNamespace: "" })).some((t) => /Ingress controller runs in/.test(t))).toBe(true);
  });

  it("refuses a port the callee doesn't have", () => {
    const bad = links.map((l) => (l.kind === "talks" ? { ...l, fields: { port: "grpc" } } : l));
    expect(errors(model(blocks, bad, { lockdown: true, ingressNamespace: "kube-system" }))).toContain(`Deployment web may call Deployment api on port "grpc", which isn't one of its container ports.`);
  });

  it("lets StatefulSet copies reach each other, and opens public Service ports", () => {
    const db = block("StatefulSet", "d");
    const lb = block("Service", "l", { name: "db-public", fields: { ports: "8080:http", type: "LoadBalancer" } });
    const l = connect([db, lb], [["l", "d"]]);
    const pol = (buildObjects(model([db, lb], l, { lockdown: true })) as Obj[]).find((o) => o.kind === "NetworkPolicy")!;
    expect(pol.spec.ingress).toEqual([{ from: [{ podSelector: { matchLabels: { app: "db" } } }] }, { ports: [{ protocol: "TCP", port: "http" }] }]);
  });

  it("labels Job and CronJob pods so they can be callers", () => {
    const job = block("Job", "j");
    const cron = block("CronJob", "c");
    const [jo, co] = buildObjects(model([job, cron], [])) as Obj[];
    expect(jo.spec.template.metadata.labels).toEqual({ app: "once" });
    expect(co.spec.jobTemplate.spec.template.metadata.labels).toEqual({ app: "nightly" });
  });
});

// A small live namespace, as the Map's graph and FormService.Object report it.
const liveWeb: Obj = {
  apiVersion: "apps/v1",
  kind: "Deployment",
  metadata: { name: "web", namespace: "shop", labels: { "app.kubernetes.io/managed-by": "Helm" }, annotations: { "deployment.kubernetes.io/revision": "4" } },
  spec: {
    replicas: 3,
    selector: { matchLabels: { "app.kubernetes.io/name": "web" } },
    template: {
      metadata: { labels: { "app.kubernetes.io/name": "web" } },
      spec: {
        containers: [
          { name: "web", image: "shop/web:2.1", ports: [{ name: "http", containerPort: 3000 }], env: [{ name: "MODE", value: "prod" }], livenessProbe: { httpGet: { path: "/healthz", port: "http" } } },
          { name: "proxy", image: "envoyproxy/envoy:v1.31.0", command: ["envoy", "-c", "/etc/envoy/envoy.yaml"] },
        ],
      },
    },
  },
};
const liveSvc: Obj = { apiVersion: "v1", kind: "Service", metadata: { name: "web", namespace: "shop" }, spec: { selector: { "app.kubernetes.io/name": "web" }, ports: [{ port: 80, targetPort: "http" }] } };
const liveIng: Obj = {
  apiVersion: "networking.k8s.io/v1",
  kind: "Ingress",
  metadata: { name: "shop", namespace: "shop" },
  spec: { tls: [{ hosts: ["shop.example.com"], secretName: "shop-tls" }], rules: [{ host: "shop.example.com", http: { paths: [{ path: "/", pathType: "Prefix", backend: { service: { name: "web", port: { number: 80 } } } }] } }] },
};
const liveCm: Obj = { apiVersion: "v1", kind: "ConfigMap", metadata: { name: "cfg", namespace: "shop" }, data: { LEVEL: "info", "nginx.conf": "server {\n}\n" } };
const liveDb: Obj = {
  apiVersion: "apps/v1",
  kind: "StatefulSet",
  metadata: { name: "db", namespace: "shop" },
  spec: { replicas: 2, selector: { matchLabels: { app: "db" } }, volumeClaimTemplates: [{ metadata: { name: "data" } }], template: { metadata: { labels: { app: "db" } }, spec: { containers: [{ name: "db", image: "postgres:17" }] } } },
};

function gnode(kind: string, name: string, flags: string[] = []): GraphNode {
  return { id: `${kind}/shop/${name}`, kind, group: "", version: "v1", resource: "", name, namespace: "shop", uid: "", created: "", labels: null, health: "good", summary: "", pods: 0, ready: 0, restarts: 0, flags };
}

function gedge(source: string, target: string, kind: string): GraphEdge {
  return { id: `${source}->${target}`, source, target, kind };
}

function imported() {
  const nodes = [
    gnode("Ingress", "shop"),
    gnode("Service", "web"),
    gnode("Deployment", "web"),
    gnode("ConfigMap", "cfg"),
    gnode("Secret", "db-pass"),
    gnode("StatefulSet", "db"),
    gnode("PersistentVolumeClaim", "data-db-0"),
    gnode("ConfigMap", "gone", ["missing"]),
    gnode("HTTPRoute", "beta"),
  ];
  const edges = [
    gedge("Ingress/shop/shop", "Service/shop/web", "routes"),
    gedge("Service/shop/web", "Deployment/shop/web", "selects"),
    gedge("Deployment/shop/web", "ConfigMap/shop/cfg", "uses"),
    gedge("StatefulSet/shop/db", "Secret/shop/db-pass", "uses"),
    gedge("Deployment/shop/web", "ConfigMap/shop/gone", "uses"),
  ];
  const objects = new Map<string, Obj>([
    ["Ingress/shop/shop", liveIng],
    ["Service/shop/web", liveSvc],
    ["Deployment/shop/web", liveWeb],
    ["ConfigMap/shop/cfg", liveCm],
    ["StatefulSet/shop/db", liveDb],
    ["PersistentVolumeClaim/shop/data-db-0", { kind: "PersistentVolumeClaim", spec: {} }],
  ]);
  let n = 0;
  return importGraph(nodes, edges, objects, () => `x${++n}`);
}

describe("opening a live namespace", () => {
  it("turns objects into blocks and connections into locked links", () => {
    const r = imported();
    expect(r.blocks.map((b) => `${b.kind}/${b.name}`)).toEqual(["Ingress/shop", "Service/web", "Deployment/web", "ConfigMap/cfg", "Secret/db-pass", "StatefulSet/db"]);
    expect(r.skipped).toEqual([
      "PersistentVolumeClaim data-db-0: a per-copy claim of StatefulSet db",
      "ConfigMap gone: referred to, but doesn't exist",
      "HTTPRoute beta: no block for this kind yet",
    ]);
    expect(r.links.map((l) => `${l.kind}${l.imported ? "" : "!"}`)).toEqual(["routes", "selects", "env", "env"]);
    expect(r.links[0].fields).toEqual({ path: "/", port: "80" });

    const web = r.blocks.find((b) => b.kind === "Deployment")!;
    expect(web.fields).toEqual({ image: "shop/web:2.1", command: "", ports: "http=3000", replicas: "3" });
    expect(web.sidecars).toEqual([{ name: "proxy", image: "envoyproxy/envoy:v1.31.0", command: "envoy -c /etc/envoy/envoy.yaml" }]);
    expect(web.managedBy).toBe("Helm");
    expect(podLabels(web)).toEqual({ "app.kubernetes.io/name": "web" });
    // A multi-line value can't be edited as KEY=value lines.
    expect(r.blocks.find((b) => b.kind === "ConfigMap")!.locked).toEqual(["data"]);
  });

  it("writes nothing for what wasn't changed", () => {
    const r = imported();
    const m = model(r.blocks, r.links);
    expect(buildObjects(m)).toEqual([]);
    expect(r.blocks.every((b) => !blockChanged(b, r.links))).toBe(true);
    expect(errors(m)).toEqual([]);
    // The plan still knows each block's live object, for its badges.
    expect(buildPlan(m).byBlock.get(r.blocks[2].id)?.[0].spec).toEqual(liveWeb.spec);
  });

  it("changes only what was edited, on top of the live object", () => {
    const r = imported();
    const web = r.blocks.find((b) => b.kind === "Deployment")!;
    web.fields.image = "shop/web:2.2";
    const [obj] = buildObjects(model(r.blocks, r.links)) as Obj[];
    const expected = structuredClone(liveWeb);
    expected.spec.template.spec.containers[0].image = "shop/web:2.2";
    // The controller's own revision annotation isn't written back.
    delete expected.metadata.annotations;
    // No template defaults, labels or probes added: just the new image.
    expect(obj).toEqual(expected);
    expect(check(model(r.blocks, r.links)).some((i) => /managed by Helm/.test(i.text))).toBe(true);
  });

  it("adds new connections to live objects", () => {
    const r = imported();
    const web = r.blocks.find((b) => b.kind === "Deployment")!;
    const extra = block("ConfigMap", "new", { name: "features" });
    const api = block("Service", "api", { name: "web-internal", fields: { ports: "8080:http" } });
    const blocks = [...r.blocks, extra, api];
    const links = connect(blocks, [
      [web.id, "new"],
      ["api", web.id],
    ], r.links);
    const objs = buildObjects(model(blocks, links)) as Obj[];
    const dep = objs.find((o) => o.kind === "Deployment")!;
    expect(dep.spec.template.spec.containers[0].envFrom).toEqual([{ configMapRef: { name: "features" } }]);
    expect(dep.spec.template.spec.containers[0].env).toEqual([{ name: "MODE", value: "prod" }]);
    // A new Service finds the live workload by its real labels.
    expect(objs.find((o) => o.kind === "Service")!.spec.selector).toEqual({ "app.kubernetes.io/name": "web" });
  });

  it("edits a live route in place and keeps the rest of the Ingress", () => {
    const r = imported();
    r.links[0] = { ...r.links[0], fields: { path: "/shop", port: "80" } };
    const [ing] = buildObjects(model(r.blocks, r.links)) as Obj[];
    expect(ing.spec.rules[0].http.paths).toEqual([{ path: "/shop", pathType: "Prefix", backend: { service: { name: "web", port: { number: 80 } } } }]);
    expect(ing.spec.tls).toEqual(liveIng.spec.tls);
  });

  it("takes what was applied as the new baseline", () => {
    const r = imported();
    const web = r.blocks.find((b) => b.kind === "Deployment")!;
    web.fields.image = "shop/web:2.2";
    const extra = block("ConfigMap", "new", { name: "features" });
    const links = connect([...r.blocks, extra], [[web.id, "new"]], r.links);
    const m = model([...r.blocks, extra], links);
    const after = rebase(m, buildPlan(m).byBlock);
    const web2 = after.blocks.find((b) => b.id === web.id)!;
    expect(blockChanged(web2, after.links)).toBe(false);
    const main = (web2.source as Obj).spec.template.spec.containers[0];
    expect(main.image).toBe("shop/web:2.2");
    expect(main.envFrom).toEqual([{ configMapRef: { name: "features" } }]);
    // The ConfigMap was new: it's still a new block, so applying again re-sends it unchanged.
    expect(buildObjects(after).map((o) => o.kind)).toEqual(["ConfigMap"]);
  });

  it("keeps a changed sidecar's other settings", () => {
    const r = imported();
    const web = r.blocks.find((b) => b.kind === "Deployment")!;
    web.sidecars = [{ ...web.sidecars![0], image: "envoyproxy/envoy:v1.32.0" }];
    const [obj] = buildObjects(model(r.blocks, r.links)) as Obj[];
    expect(obj.spec.template.spec.containers[1]).toEqual({ name: "proxy", image: "envoyproxy/envoy:v1.32.0", command: ["envoy", "-c", "/etc/envoy/envoy.yaml"] });
  });

  it("refuses changes Kubernetes won't accept", () => {
    const r = imported();
    const db = r.blocks.find((b) => b.kind === "StatefulSet")!;
    const pvc = block("PersistentVolumeClaim", "p");
    const links = connect([...r.blocks, pvc], [[db.id, "p"]], r.links);
    expect(errors(model([...r.blocks, pvc], links))).toContain("StatefulSet db already exists, and per-copy storage can't be added to a running StatefulSet. Create a new StatefulSet for it.");
    const dup = block("Deployment", "d2", { name: "web" });
    expect(errors(model([...r.blocks, dup], r.links))).toContain("Deployment web already exists in the cluster: a new block with that name would replace it. Rename the new one.");
  });

  it("locks down live workloads by their real labels", () => {
    const r = imported();
    const objs = buildObjects(model(r.blocks, r.links, { lockdown: true, ingressNamespace: "kube-system" })) as Obj[];
    const pols = objs.filter((o) => o.kind === "NetworkPolicy");
    expect(pols.map((p) => p.metadata.name)).toEqual(["web-allow", "db-allow"]);
    expect(pols[0].spec.podSelector).toEqual({ matchLabels: { "app.kubernetes.io/name": "web" } });
    expect(check(model(r.blocks, r.links, { lockdown: true, ingressNamespace: "kube-system" })).some((i) => /blocks every connection to them you haven't drawn/.test(i.text))).toBe(true);
  });
});
