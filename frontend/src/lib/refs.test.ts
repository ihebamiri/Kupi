import { describe, expect, it } from "vitest";
import { referenceFor, sliderFor } from "./refs";
import { CPU_SCALE, MEMORY_SCALE, describeQuantity, nearestStop, parseBytes, parseCPU } from "./quantity";

const deployment = {
  metadata: { name: "web", namespace: "shop" },
  spec: {
    replicas: 2,
    template: {
      metadata: { namespace: "ignored" },
      spec: {
        serviceAccountName: "web",
        containers: [
          {
            env: [{ name: "PASSWORD", valueFrom: { secretKeyRef: { name: "creds", key: "password" } } }, { valueFrom: { configMapKeyRef: { key: "x" } } }],
            envFrom: [{ configMapRef: { name: "settings" } }],
          },
        ],
        volumes: [{ persistentVolumeClaim: { claimName: "data" } }, { secret: { secretName: "tls" } }],
        imagePullSecrets: [{ name: "registry" }],
      },
    },
  },
};

describe("referenceFor", () => {
  it("finds references in a Deployment", () => {
    expect(referenceFor(["metadata", "namespace"], deployment)).toEqual({ type: "namespaces" });
    expect(referenceFor(["spec", "template", "metadata", "namespace"], deployment)).toBeNull();
    const c = ["spec", "template", "spec", "containers", 0];
    expect(referenceFor([...c, "env", 0, "valueFrom", "secretKeyRef", "name"], deployment)).toEqual({ type: "secrets" });
    expect(referenceFor([...c, "env", 0, "valueFrom", "secretKeyRef", "key"], deployment)).toEqual({ type: "secrets", keysOf: { type: "secrets", name: "creds" } });
    // A key with no ConfigMap chosen yet has nothing to suggest.
    expect(referenceFor([...c, "env", 1, "valueFrom", "configMapKeyRef", "key"], deployment)).toBeNull();
    expect(referenceFor([...c, "envFrom", 0, "configMapRef", "name"], deployment)).toEqual({ type: "configmaps" });
    expect(referenceFor(["spec", "template", "spec", "serviceAccountName"], deployment)).toEqual({ type: "serviceaccounts" });
    expect(referenceFor(["spec", "template", "spec", "volumes", 0, "persistentVolumeClaim", "claimName"], deployment)).toEqual({ type: "persistentvolumeclaims" });
    expect(referenceFor(["spec", "template", "spec", "volumes", 1, "secret", "secretName"], deployment)).toEqual({ type: "secrets" });
    expect(referenceFor(["spec", "template", "spec", "imagePullSecrets", 0, "name"], deployment)).toEqual({ type: "secrets" });
    expect(referenceFor([...c, "name"], deployment)).toBeNull();
  });

  it("follows the kind next to the name", () => {
    const hpa = { spec: { scaleTargetRef: { kind: "StatefulSet", name: "db" } } };
    expect(referenceFor(["spec", "scaleTargetRef", "name"], hpa)).toEqual({ type: "statefulsets" });
    const rb = { roleRef: { kind: "ClusterRole" }, subjects: [{ kind: "ServiceAccount", namespace: "ci" }, { kind: "User" }] };
    expect(referenceFor(["roleRef", "name"], rb)).toEqual({ type: "clusterroles" });
    expect(referenceFor(["subjects", 0, "name"], rb)).toEqual({ type: "serviceaccounts", namespace: "ci" });
    expect(referenceFor(["subjects", 1, "name"], rb)).toBeNull();
  });
});

describe("sliders", () => {
  it("picks a slider for resources and replicas", () => {
    const r = ["spec", "template", "spec", "containers", 0, "resources"];
    expect(sliderFor([...r, "requests", "cpu"], "Deployment")).toBe("cpu");
    expect(sliderFor([...r, "limits", "memory"], "Deployment")).toBe("memory");
    expect(sliderFor(["spec", "resources", "requests", "storage"], "PersistentVolumeClaim")).toBe("storage");
    expect(sliderFor(["spec", "replicas"], "Deployment")).toBe("replicas");
    expect(sliderFor(["spec", "minReplicas"], "HorizontalPodAutoscaler")).toBe("replicas");
    expect(sliderFor(["spec", "template", "spec", "replicas"], "Deployment")).toBeNull();
  });

  it("parses quantities and finds the nearest step", () => {
    expect(parseCPU("250m")).toBe(250);
    expect(parseCPU("1.5")).toBe(1500);
    expect(parseCPU(2)).toBe(2000);
    expect(parseBytes("512Mi")).toBe(512 * 2 ** 20);
    expect(parseBytes("1G")).toBe(1e9);
    expect(parseCPU("lots")).toBeNull();
    expect(CPU_SCALE.stops[nearestStop(CPU_SCALE, "260m")]).toBe("250m");
    expect(MEMORY_SCALE.stops[nearestStop(MEMORY_SCALE, "1G")]).toBe("1Gi");
    expect(describeQuantity(CPU_SCALE, "250m")).toBe("0.25 CPUs");
    expect(describeQuantity(MEMORY_SCALE, "1536Mi")).toBe("1.5 GiB");
  });
});
