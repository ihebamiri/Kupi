import { describe, expect, it } from "vitest";
import { diffStats, parseDiff } from "./diff";

describe("parseDiff", () => {
  it("classifies every line", () => {
    const lines = parseDiff("--- live\n+++ after change\n@@ -1,2 +1,2 @@\n spec:\n-  replicas: 2\n+  replicas: 3\n");
    expect(lines.map((l) => l.kind)).toEqual(["header", "header", "hunk", "context", "remove", "add"]);
    expect(diffStats(lines)).toEqual({ added: 1, removed: 1 });
  });

  it("returns nothing for an empty diff", () => {
    expect(parseDiff("")).toEqual([]);
  });
});
