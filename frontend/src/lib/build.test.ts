import { describe, expect, it } from "vitest";
import { buildObjects, check, freeName, linkFor, newBlock, parseData, parsePorts, parseServicePorts, type Block, type BlockKind, type BuildModel, type Link } from "./build";

type Obj = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

function model(blocks: Block[], links: Link[] = [], extra: Partial<BuildModel> = {}): BuildModel {
  return { namespace: "shop", createNamespace: false, blocks, links, ...extra };
}

function connect(blocks: Block[], pairs: [string, string][]): Link[] {
  const links: Link[] = [];
  for (const [a, b] of pairs) {
    const r = linkFor(blocks.find((x) => x.id === a)!, blocks.find((x) => x.id === b)!, links);
    if (!r.ok) throw new Error(r.reason);
    links.push({ id: `${a}-${b}`, ...r.link });
  }
  return links;
}

function find(objs: Obj[], kind: string, name?: string): Obj {
  return objs.find((o) => o.kind === kind && (!name || o.metadata.name === name))!;
}

function block(kind: BlockKind, id: string, patch: Partial<Block> = {}): Block {
  const b = newBlock(kind, id, []);
  return { ...b, ...patch, fields: { ...b.fields, ...(patch.fields ?? {}) } };
}

function errors(m: BuildModel): string[] {
  return check(m)
    .filter((i) => i.level === "error")
    .map((i) => i.text);
}

describe("build canvas", () => {
  const dep = block("Deployment", "d");
  const svc = block("Service", "s");
  const ing = block("Ingress", "i");
  const cm = block("ConfigMap", "c");
  const sec = block("Secret", "x");
  const pvc = block("PersistentVolumeClaim", "p");
  const hpa = block("HorizontalPodAutoscaler", "h");

  it("names new blocks without clashing", () => {
    expect(freeName("Deployment", "web", [dep])).toBe("web-2");
    expect(freeName("Service", "web", [dep])).toBe("web");
  });

  it("orients links whichever way they were dragged", () => {
    expect(linkFor(dep, svc, [])).toEqual({ ok: true, link: { from: "s", to: "d", kind: "selects" } });
    expect(linkFor(cm, dep, [])).toEqual({ ok: true, link: { from: "d", to: "c", kind: "env" } });
  });

  it("refuses links that mean nothing, with a reason", () => {
    const r = linkFor(ing, dep, []);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.reason).toMatch(/Service between/);
    expect(linkFor(svc, cm, []).ok).toBe(false);
    expect(linkFor(dep, dep, []).ok).toBe(false);
    const ds = block("DaemonSet", "ds");
    const r2 = linkFor(hpa, ds, []);
    expect(r2.ok).toBe(false);
    if (!r2.ok) expect(r2.reason).toMatch(/one copy per node/);
  });

  it("lets a Service select only one workload", () => {
    const dep2 = { ...dep, id: "d2", name: "web-2" };
    const links = connect([svc, dep, dep2], [["s", "d"]]);
    expect(linkFor(svc, dep2, links).ok).toBe(false);
    expect(linkFor(svc, dep, links).ok).toBe(false);
  });

  it("parses container and Service ports", () => {
    expect(parsePorts("http=8080\n9090\nBad=1\nx=99999")).toEqual({ ports: [{ name: "http", port: 8080 }, { name: "port-9090", port: 9090 }], bad: ["Bad=1", "x=99999"] });
    expect(parseServicePorts("80:http\n9090\n443:8443\n80:Bad_Name")).toEqual({
      ports: [
        { port: 80, target: "http" },
        { port: 9090, target: 9090 },
        { port: 443, target: 8443 },
      ],
      bad: ["80:Bad_Name"],
    });
  });

  it("builds a connected app", () => {
    const blocks = [dep, svc, ing, cm, sec, pvc, hpa];
    const links = connect(blocks, [
      ["i", "s"],
      ["s", "d"],
      ["d", "c"],
      ["d", "x"],
      ["d", "p"],
      ["h", "d"],
    ]);
    const objs = buildObjects(model(blocks, links)) as Obj[];
    expect(objs.map((o) => o.kind)).toEqual(["ConfigMap", "PersistentVolumeClaim", "Deployment", "Service", "Ingress", "HorizontalPodAutoscaler"]);
    expect(objs.every((o) => o.metadata.namespace === "shop")).toBe(true);

    const d = find(objs, "Deployment");
    const c = d.spec.template.spec.containers[0];
    expect(d.spec.selector.matchLabels).toEqual({ app: "web" });
    expect(d.spec.template.metadata.labels).toEqual({ app: "web" });
    expect(c.ports).toEqual([{ name: "http", containerPort: 8080 }]);
    expect(c.command).toBeUndefined();
    expect(c.envFrom).toEqual([{ configMapRef: { name: "settings" } }, { secretRef: { name: "app-secret" } }]);
    expect(c.volumeMounts).toContainEqual({ name: "pvc-data", mountPath: "/data/data" });
    expect(d.spec.template.spec.volumes).toContainEqual({ name: "pvc-data", persistentVolumeClaim: { claimName: "data" } });
    // Secure defaults survive.
    expect(c.securityContext.readOnlyRootFilesystem).toBe(true);
    expect(d.spec.template.spec.securityContext.runAsNonRoot).toBe(true);
    expect(c.readinessProbe).toBeUndefined();

    const s = find(objs, "Service");
    expect(s.spec.selector).toEqual({ app: "web" });
    expect(s.spec.ports).toEqual([{ name: "http", port: 80, targetPort: "http" }]);

    const i = find(objs, "Ingress");
    expect(i.spec.rules[0].http.paths).toEqual([{ path: "/", pathType: "Prefix", backend: { service: { name: "web", port: { number: 80 } } } }]);
    expect(i.spec.rules[0].host).toBeUndefined();

    expect(find(objs, "HorizontalPodAutoscaler").spec.scaleTargetRef).toEqual({ apiVersion: "apps/v1", kind: "Deployment", name: "web" });
    expect(objs.some((o) => o.kind === "Secret")).toBe(false);
  });

  it("maps several ports through the Service", () => {
    const app = block("Deployment", "d", { fields: { ports: "http=8080\nmetrics=9090" } });
    const s = block("Service", "s", { fields: { ports: "80:http\n9090:metrics" } });
    const links = connect([app, s], [["s", "d"]]);
    expect(errors(model([app, s], links))).toEqual([]);
    const objs = buildObjects(model([app, s], links)) as Obj[];
    expect(find(objs, "Deployment").spec.template.spec.containers[0].ports).toEqual([
      { name: "http", containerPort: 8080 },
      { name: "metrics", containerPort: 9090 },
    ]);
    expect(find(objs, "Service").spec.ports).toEqual([
      { name: "http", port: 80, targetPort: "http" },
      { name: "metrics", port: 9090, targetPort: "metrics" },
    ]);
  });

  it("catches a Service port aimed at a container port that doesn't exist", () => {
    const s = block("Service", "s", { fields: { ports: "80:web" } });
    const links = connect([dep, s], [["s", "d"]]);
    expect(errors(model([dep, s], links)).some((t) => /has no container port with that name/.test(t))).toBe(true);
  });

  it("routes several paths from one Ingress", () => {
    const api = block("Service", "s2", { name: "api", fields: { ports: "8080:http" } });
    const apiDep = block("Deployment", "d2", { name: "api" });
    const blocks = [dep, svc, api, apiDep, ing];
    const links = connect(blocks, [
      ["s", "d"],
      ["s2", "d2"],
      ["i", "s"],
      ["i", "s2"],
    ]);
    // The first route gets "/", the next one a path named after its Service.
    expect(links.filter((l) => l.kind === "routes").map((l) => l.fields?.path)).toEqual(["/", "/api"]);
    const paths = find(buildObjects(model(blocks, links)) as Obj[], "Ingress").spec.rules[0].http.paths;
    expect(paths).toEqual([
      { path: "/", pathType: "Prefix", backend: { service: { name: "web", port: { number: 80 } } } },
      { path: "/api", pathType: "Prefix", backend: { service: { name: "api", port: { number: 8080 } } } },
    ]);

    const dup = links.map((l) => (l.id === "i-s2" ? { ...l, fields: { path: "/", port: "" } } : l));
    expect(errors(model(blocks, dup)).some((t) => /two routes use the path \//.test(t))).toBe(true);
    const badPort = links.map((l) => (l.id === "i-s2" ? { ...l, fields: { path: "/api", port: "9999" } } : l));
    expect(errors(model(blocks, badPort))).toContain("Ingress web: Service api has no port 9999.");
  });

  it("adds sidecars with the same secure defaults", () => {
    const app = block("Deployment", "d", { sidecars: [{ name: "log-shipper", image: "fluent/fluent-bit:3.2", command: "" }] });
    const pod = find(buildObjects(model([app])) as Obj[], "Deployment").spec.template.spec;
    expect(pod.containers.map((c: Obj) => c.name)).toEqual(["web", "log-shipper"]);
    const side = pod.containers[1];
    expect(side.image).toBe("fluent/fluent-bit:3.2");
    expect(side.securityContext).toEqual(pod.containers[0].securityContext);
    expect(side.resources.limits.memory).toBe("128Mi");
    expect(side.command).toBeUndefined();

    const clash = block("Deployment", "d", { sidecars: [{ name: "web", image: "busybox:1.36", command: "sleep 3600" }] });
    expect(errors(model([clash]))).toContain("Deployment web: two containers are called web.");
    const noImage = block("Deployment", "d", { sidecars: [{ name: "side", image: "", command: "" }] });
    expect(errors(model([noImage]))).toContain("Deployment web, sidecar side needs an image.");
  });

  it("builds a StatefulSet with a headless Service and a claim per copy", () => {
    const sts = block("StatefulSet", "st");
    const blocks = [sts, pvc, svc, hpa];
    const links = connect(blocks, [
      ["st", "p"],
      ["s", "st"],
      ["h", "st"],
    ]);
    expect(errors(model(blocks, links))).toEqual([]);
    const objs = buildObjects(model(blocks, links)) as Obj[];
    // The claim becomes a template, not an object of its own.
    expect(objs.some((o) => o.kind === "PersistentVolumeClaim")).toBe(false);
    const headless = find(objs, "Service", "db-headless");
    expect(headless.spec).toEqual({ clusterIP: "None", selector: { app: "db" }, ports: [{ name: "http", port: 8080, targetPort: "http" }] });
    expect(objs.indexOf(headless)).toBeLessThan(objs.indexOf(find(objs, "StatefulSet")));
    const s = find(objs, "StatefulSet");
    expect(s.apiVersion).toBe("apps/v1");
    expect(s.spec.serviceName).toBe("db-headless");
    expect(s.spec.replicas).toBe(3);
    expect(s.spec.volumeClaimTemplates).toEqual([{ metadata: { name: "data" }, spec: { accessModes: ["ReadWriteOnce"], resources: { requests: { storage: "1Gi" } } } }]);
    expect(s.spec.template.spec.containers[0].volumeMounts).toContainEqual({ name: "data", mountPath: "/data/data" });
    expect(s.spec.template.spec.volumes).toEqual([{ name: "tmp", emptyDir: {} }]);
    expect(find(objs, "Service", "web").spec.selector).toEqual({ app: "db" });
    expect(find(objs, "HorizontalPodAutoscaler").spec.scaleTargetRef.kind).toBe("StatefulSet");
  });

  it("won't share a StatefulSet's per-copy claim", () => {
    const sts = block("StatefulSet", "st");
    const links = connect([sts, dep, pvc], [
      ["st", "p"],
      ["d", "p"],
    ]);
    expect(errors(model([sts, dep, pvc], links)).some((t) => /can't also be used by other workloads/.test(t))).toBe(true);
  });

  it("builds a DaemonSet without a copy count", () => {
    const ds = block("DaemonSet", "ds");
    const d = (buildObjects(model([ds])) as Obj[])[0];
    expect(d.kind).toBe("DaemonSet");
    expect(d.spec.replicas).toBeUndefined();
    expect(d.spec.selector.matchLabels).toEqual({ app: "agent" });
    expect(d.spec.template.spec.containers[0].ports).toBeUndefined();
  });

  it("stops two workloads sharing one pod label", () => {
    const sts = block("StatefulSet", "st", { name: "web" });
    expect(errors(model([dep, sts])).some((t) => /would both label their pods app=web/.test(t))).toBe(true);
  });

  it("does not change the shared templates", () => {
    const a = buildObjects(model([{ ...dep, name: "one" }])) as Obj[];
    const b = buildObjects(model([dep])) as Obj[];
    expect(a[0].metadata.name).toBe("one");
    expect(b[0].metadata.name).toBe("web");
    expect(b[0].spec.template.spec.containers[0].readinessProbe).toBeUndefined();
    expect(b[0].spec.template.spec.containers).toHaveLength(1);
  });

  it("adds the namespace first when asked", () => {
    const objs = buildObjects(model([cm], [], { createNamespace: true })) as Obj[];
    expect(objs[0]).toEqual({ apiVersion: "v1", kind: "Namespace", metadata: { name: "shop" } });
  });

  it("splits commands without a shell", () => {
    const job = block("Job", "j", { fields: { image: "busybox:1.36", command: "  echo  hi there " } });
    expect((buildObjects(model([job])) as Obj[])[0].spec.template.spec.containers[0].command).toEqual(["echo", "hi", "there"]);
    const app = block("Deployment", "d", { fields: { command: "nginx -g daemon-off" } });
    expect((buildObjects(model([app])) as Obj[])[0].spec.template.spec.containers[0].command).toEqual(["nginx", "-g", "daemon-off"]);
  });

  it("parses ConfigMap lines", () => {
    expect(parseData("A=1\n# note\n\nB = two=2\nbad line\n")).toEqual({ data: { A: "1", B: " two=2" }, bad: ["bad line"] });
  });

  it("reports problems before anything is built", () => {
    const badDep = { ...dep, name: "Web_App", fields: { image: "", ports: "http=99999", replicas: "x" } };
    const texts = check(model([badDep, ing, hpa], [], { namespace: "" })).map((i) => `${i.level}: ${i.text}`);
    expect(texts.some((t) => t.startsWith("error: Pick a namespace"))).toBe(true);
    expect(texts.some((t) => /error: Deployment Web_App: names use/.test(t))).toBe(true);
    expect(texts.some((t) => /needs an image/.test(t))).toBe(true);
    expect(texts.some((t) => /container ports aren't name=port/.test(t))).toBe(true);
    expect(texts.some((t) => /needs a Service to route to/.test(t))).toBe(true);
    expect(texts.some((t) => /needs a Deployment or StatefulSet to scale/.test(t))).toBe(true);
  });

  it("is clean for a valid build, apart from the Secret reminder", () => {
    const blocks = [dep, svc, sec];
    const links = connect(blocks, [
      ["s", "d"],
      ["d", "x"],
    ]);
    const issues = check(model(blocks, links));
    expect(issues.filter((i) => i.level === "error")).toEqual([]);
    expect(issues.map((i) => i.block)).toEqual(["x"]);
  });

  it("catches duplicate names and a Service pointing at a portless app", () => {
    const quiet = { ...dep, fields: { ...dep.fields, ports: "" } };
    const dup = { ...quiet, id: "d2" };
    const links = connect([svc, quiet], [["s", "d"]]);
    const texts = errors(model([quiet, dup, svc], links));
    expect(texts).toContain("Two Deployment blocks are called web.");
    expect(texts.some((t) => /has no container port with that name/.test(t))).toBe(true);
  });

  it("warns about shared ReadWriteOnce storage", () => {
    const links = connect([dep, pvc], [["d", "p"]]);
    expect(check(model([dep, pvc], links)).some((i) => /ReadWriteOnce/.test(i.text))).toBe(true);
  });
});
