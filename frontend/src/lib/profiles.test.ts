import { describe, expect, it } from "vitest";
import { isProfileKind, profileMeta } from "./profiles";

describe("profiles", () => {
  it("recognises the four kinds", () => {
    expect(isProfileKind("local")).toBe(true);
    expect(isProfileKind("prod")).toBe(false);
  });

  it("treats unknown kinds as production", () => {
    expect(profileMeta("mystery").label).toBe("Production");
  });
});
