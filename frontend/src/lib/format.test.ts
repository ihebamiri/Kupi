import { describe, expect, it } from "vitest";
import { age, compareCells, statusTone } from "./format";

describe("age", () => {
  const now = Date.parse("2026-11-05T12:00:00Z");
  it("formats like kubectl", () => {
    expect(age("2026-11-05T11:59:15Z", now)).toBe("45s");
    expect(age("2026-11-05T11:48:00Z", now)).toBe("12m");
    expect(age("2026-11-05T07:00:00Z", now)).toBe("5h");
    expect(age("2026-11-02T12:00:00Z", now)).toBe("3d");
    expect(age("not a date", now)).toBe("not a date");
  });
});

describe("statusTone", () => {
  it("classifies common states", () => {
    expect(statusTone("Running")).toBe("good");
    expect(statusTone("CrashLoopBackOff")).toBe("bad");
    expect(statusTone("Pending")).toBe("warn");
    expect(statusTone("Init:1/2")).toBe("warn");
    expect(statusTone("Completed")).toBe("muted");
    expect(statusTone("nginx:1.27")).toBeNull();
  });
});

describe("compareCells", () => {
  it("sorts numbers numerically", () => {
    expect(["10", "9", "100"].sort(compareCells)).toEqual(["9", "10", "100"]);
    expect(["pod-10", "pod-9"].sort(compareCells)).toEqual(["pod-9", "pod-10"]);
  });
});
