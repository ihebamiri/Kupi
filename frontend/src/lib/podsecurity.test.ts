import { describe, expect, it } from "vitest";
import { evaluatePod, podRequests, podSpecOf } from "./podsecurity";
import { KINDS } from "./templates";

const secure = () => structuredClone(podSpecOf(KINDS.find((k) => k.kind === "Deployment")!.template!("shop"))!) as Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

describe("pod security standards", () => {
  it("rates Kupi's templates restricted", () => {
    for (const k of KINDS.filter((x) => x.template && ["Deployment", "Job", "CronJob"].includes(x.kind))) {
      expect(evaluatePod(podSpecOf(k.template!("shop"))!)).toEqual({ level: "restricted", problems: [] });
    }
  });

  it("drops to baseline for missing restricted settings", () => {
    const spec = secure();
    delete spec.securityContext;
    spec.containers[0].securityContext = {};
    const r = evaluatePod(spec);
    expect(r.level).toBe("baseline");
    expect(r.problems).toEqual([
      "container web may gain more privileges (allowPrivilegeEscalation isn't false)",
      "container web doesn't drop all capabilities",
      "may run as root (runAsNonRoot isn't set)",
      "has no seccomp profile (RuntimeDefault)",
    ]);
  });

  it("drops to privileged for host access", () => {
    const spec = secure();
    spec.hostNetwork = true;
    spec.volumes.push({ name: "docker", hostPath: { path: "/var/run/docker.sock" } });
    spec.containers[0].securityContext.privileged = true;
    spec.containers[0].securityContext.capabilities.add = ["SYS_ADMIN"];
    const r = evaluatePod(spec);
    expect(r.level).toBe("privileged");
    expect(r.problems).toEqual([
      "uses the node's network (hostNetwork)",
      "mounts a path from the node (volume docker)",
      "container web is privileged",
      "container web adds SYS_ADMIN",
    ]);
  });

  it("checks every container, sidecars included", () => {
    const spec = secure();
    spec.containers.push({ name: "side", image: "x:1", securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ["ALL"] }, runAsNonRoot: false } });
    expect(evaluatePod(spec).problems).toEqual(["may run as root (runAsNonRoot isn't set)"]);
  });

  it("sums requests, counting a bigger init container instead", () => {
    const spec = secure();
    spec.containers.push({ name: "side", resources: { requests: { cpu: "50m", memory: "32Mi" } } });
    expect(podRequests(spec)).toEqual({ cpu: 150, mem: 160 * 2 ** 20 });
    spec.initContainers = [{ name: "migrate", resources: { requests: { cpu: "1", memory: "64Mi" } } }];
    expect(podRequests(spec)).toEqual({ cpu: 1000, mem: 160 * 2 ** 20 });
  });
});
