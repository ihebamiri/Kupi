import { describe, expect, it } from "vitest";
import { isDockerHub, nameFromImage } from "./imageref";

describe("image references", () => {
  it("derives workload names", () => {
    expect(nameFromImage("nginx:1.27")).toBe("nginx");
    expect(nameFromImage("ghcr.io/org/My_App:1.2")).toBe("my-app");
    expect(nameFromImage("localhost:5000/team/api@sha256:abc")).toBe("api");
  });

  it("recognises Docker Hub", () => {
    expect(isDockerHub("nginx")).toBe(true);
    expect(isDockerHub("bitnami/redis")).toBe(true);
    expect(isDockerHub("ghcr.io/org/app")).toBe(false);
    expect(isDockerHub("localhost:5000/app")).toBe(false);
  });
});
