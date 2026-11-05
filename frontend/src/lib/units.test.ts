import { describe, expect, it } from "vitest";
import { formatBytes, formatCPU, percent } from "./units";

describe("units", () => {
  it("formats CPU", () => {
    expect(formatCPU(250)).toBe("250m");
    expect(formatCPU(1000)).toBe("1 core");
    expect(formatCPU(4000)).toBe("4 cores");
    expect(formatCPU(1500)).toBe("1.5 cores");
  });

  it("formats bytes", () => {
    expect(formatBytes(512)).toBe("512B");
    expect(formatBytes(64 * 1024 * 1024)).toBe("64.0Mi");
    expect(formatBytes(3 * 1024 ** 3)).toBe("3.0Gi");
  });

  it("computes percent", () => {
    expect(percent(250, 1000)).toBe(25);
    expect(percent(1, 0)).toBeNull();
  });
});
