import { describe, expect, it } from "vitest";
import { collect, hasChanges, mergeValues, newRow, rowsFromKeys } from "./kvrows";

describe("secret value rows", () => {
  it("sends only replaced, new and removed keys", () => {
    const rows = rowsFromKeys(["keep-me", "replace-me", "drop-me"]);
    rows[1] = { ...rows[1], state: "replace", value: "new" };
    rows[2] = { ...rows[2], state: "remove" };
    rows.push(newRow("ADDED", "v"), newRow());
    expect(collect(rows)).toEqual({ set: { "replace-me": "new", ADDED: "v" }, del: ["drop-me"], error: null });
  });

  it("rejects bad and duplicate keys", () => {
    expect(collect([newRow("has space", "x")]).error).toMatch(/isn't a valid key/);
    expect(collect([...rowsFromKeys(["A"]), newRow("A", "x")]).error).toMatch(/twice/);
    expect(collect([newRow("", "orphan value")]).error).toMatch(/\(empty\)/);
  });

  it("merges imported values", () => {
    const merged = mergeValues([...rowsFromKeys(["DB"]), newRow()], { DB: "pw", API: "k" });
    expect(merged.map((r) => [r.key, r.state, r.value])).toEqual([
      ["DB", "replace", "pw"],
      ["API", "new", "k"],
    ]);
    expect(hasChanges(rowsFromKeys(["A"]))).toBe(false);
  });
});
