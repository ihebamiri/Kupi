import { describe, expect, it } from "vitest";
import { managerLabel } from "./managers";

describe("managerLabel", () => {
  it("names the common tools", () => {
    expect(managerLabel("kubectl-client-side-apply")).toBe("kubectl apply");
    expect(managerLabel("kubectl-edit")).toBe("kubectl edit");
    expect(managerLabel("argocd-controller")).toBe("Argo CD");
    expect(managerLabel("kustomize-controller")).toBe("Flux");
    expect(managerLabel("kupi")).toBe("Kupi");
    expect(managerLabel("my-operator")).toBe("my-operator");
    expect(managerLabel("")).toBe("unknown");
  });
});
