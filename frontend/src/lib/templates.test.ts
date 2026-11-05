import { describe, expect, it } from "vitest";
import { getIn } from "./objpath";
import { KINDS, selectorWarnings, splitApiVersion } from "./templates";

describe("templates", () => {
  it("every templated kind has its essentials", () => {
    for (const k of KINDS) {
      if (!k.template) continue;
      const obj = k.template("default");
      expect(obj.apiVersion).toBe(k.apiVersion);
      expect(obj.kind).toBe(k.kind);
      for (const e of k.essentials) {
        if (e.label === "Namespace" && !k.namespaced) continue;
        // Optional fields may be absent, but their parent must exist.
        const parent = e.path.slice(0, Math.max(1, Math.min(2, e.path.length - 1)));
        expect(getIn(obj, parent), `${k.kind}: ${e.label}`).toBeDefined();
      }
    }
  });

  it("workload templates start secure", () => {
    const deploy = KINDS.find((k) => k.kind === "Deployment")!.template!("shop");
    const pod = getIn(deploy, ["spec", "template", "spec"]) as Record<string, unknown>;
    expect(getIn(pod, ["securityContext", "runAsNonRoot"])).toBe(true);
    expect(getIn(pod, ["containers", 0, "securityContext", "readOnlyRootFilesystem"])).toBe(true);
    expect(getIn(pod, ["containers", 0, "securityContext", "allowPrivilegeEscalation"])).toBe(false);
    expect(getIn(pod, ["containers", 0, "resources", "limits", "memory"])).toBeDefined();
    expect(selectorWarnings(deploy)).toEqual([]);
  });

  it("flags a selector that doesn't match the pods", () => {
    const bad = { kind: "Deployment", spec: { selector: { matchLabels: { app: "a" } }, template: { metadata: { labels: { app: "b" } } } } };
    expect(selectorWarnings(bad)[0]).toContain("app=a");
  });

  it("splits apiVersion", () => {
    expect(splitApiVersion("v1")).toEqual({ group: "", version: "v1" });
    expect(splitApiVersion("apps/v1")).toEqual({ group: "apps", version: "v1" });
  });
});
