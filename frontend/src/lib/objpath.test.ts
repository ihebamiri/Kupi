import { describe, expect, it } from "vitest";
import { deleteIn, getIn, pathLabel, setIn, updateIn } from "./objpath";

describe("objpath", () => {
  const obj = { spec: { replicas: 2, containers: [{ name: "app", image: "a:1" }] } };

  it("reads nested values", () => {
    expect(getIn(obj, ["spec", "containers", 0, "image"])).toBe("a:1");
    expect(getIn(obj, ["spec", "missing", "x"])).toBeUndefined();
  });

  it("sets without mutating and creates parents", () => {
    const next = setIn(obj, ["spec", "containers", 0, "image"], "a:2") as typeof obj;
    expect(next.spec.containers[0].image).toBe("a:2");
    expect(obj.spec.containers[0].image).toBe("a:1");
    expect(setIn({}, ["spec", "ports", 0, "port"], 80)).toEqual({ spec: { ports: [{ port: 80 }] } });
  });

  it("deletes keys and array items", () => {
    expect(deleteIn(obj, ["spec", "replicas"])).toEqual({ spec: { containers: [{ name: "app", image: "a:1" }] } });
    expect(deleteIn({ a: [1, 2, 3] }, ["a", 1])).toEqual({ a: [1, 3] });
    expect(updateIn(obj, ["spec", "replicas"], undefined)).not.toHaveProperty("spec.replicas");
  });

  it("labels paths", () => {
    expect(pathLabel(["spec", "containers", 0, "image"])).toBe("spec.containers[0].image");
  });
});
