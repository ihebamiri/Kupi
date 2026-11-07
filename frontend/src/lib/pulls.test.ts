import { describe, expect, it } from "vitest";
import { mergePull } from "./pulls";
import type { PullEvent } from "./api";

const ev = (id: string, ref: string, extra: Partial<PullEvent> = {}): PullEvent => ({ id, ref, percent: 0, status: "", done: false, error: "", ...extra });

describe("mergePull", () => {
  it("replaces an earlier failure of the same image", () => {
    let s = mergePull({}, ev("a", "localhost:5000/app:1", { done: true, error: "i/o timeout" }));
    s = mergePull(s, ev("b", "nginx:1.27", { percent: 40 }));
    s = mergePull(s, ev("c", "localhost:5000/app:1", { status: "Starting" }));
    expect(Object.keys(s).sort()).toEqual(["b", "c"]);
    s = mergePull(s, ev("c", "localhost:5000/app:1", { done: true, percent: 100 }));
    expect(s.c.done).toBe(true);
    expect(s.b.percent).toBe(40);
  });
});
