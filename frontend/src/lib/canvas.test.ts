import { describe, expect, it } from "vitest";
import { dependents, exposed, neighbourhood, securityLevel, type GraphEdge, type GraphNode } from "./canvas";

const node = (id: string, flags: string[] = []): GraphNode => ({
  id, kind: id.split("/")[0], group: "", version: "v1", resource: "", name: id, namespace: "shop", uid: "", created: "",
  labels: null, health: "good", summary: "", pods: 0, ready: 0, restarts: 0, flags,
});

const nodes = [
  node("Ingress/web", ["public"]),
  node("Service/api"),
  node("Deployment/api", ["mayRunAsRoot"]),
  node("ConfigMap/cfg"),
  node("Deployment/worker", ["privileged"]),
];
const edges: GraphEdge[] = [
  { id: "1", source: "Ingress/web", target: "Service/api", kind: "routes" },
  { id: "2", source: "Service/api", target: "Deployment/api", kind: "selects" },
  { id: "3", source: "Deployment/api", target: "ConfigMap/cfg", kind: "uses" },
  { id: "4", source: "Deployment/worker", target: "ConfigMap/cfg", kind: "uses" },
];

describe("canvas overlays", () => {
  it("follows traffic from public entry points, not config links", () => {
    expect([...exposed(nodes, edges)].sort()).toEqual(["Deployment/api", "Ingress/web", "Service/api"]);
  });

  it("ranks security flags", () => {
    expect(securityLevel(nodes[4])).toBe("serious");
    expect(securityLevel(nodes[2])).toBe("risky");
    expect(securityLevel(nodes[1])).toBe("ok");
  });

  it("finds the blast radius of a ConfigMap", () => {
    expect(dependents("ConfigMap/cfg", edges).sort()).toEqual(["Deployment/api", "Deployment/worker"]);
    const near = neighbourhood("ConfigMap/cfg", edges, 1);
    expect(near.has("Deployment/worker")).toBe(true);
    expect(near.has("Service/api")).toBe(false);
  });
});
