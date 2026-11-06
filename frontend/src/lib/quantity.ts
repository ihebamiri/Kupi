// Kubernetes quantities for the form's sliders: parse what the user typed,
// and the steps a slider moves through.

const BINARY: Record<string, number> = { Ki: 2 ** 10, Mi: 2 ** 20, Gi: 2 ** 30, Ti: 2 ** 40, Pi: 2 ** 50, Ei: 2 ** 60 };
const DECIMAL: Record<string, number> = { "": 1, k: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18, m: 1e-3 };

/** Parses a quantity ("512Mi", "1.5G", "250m", "2") to a plain number, or null. */
export function parseQuantity(q: unknown): number | null {
  if (typeof q === "number") return Number.isFinite(q) ? q : null;
  if (typeof q !== "string") return null;
  const m = q.trim().match(/^([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E|m)?$/);
  if (!m) return null;
  const n = Number(m[1]);
  const suffix = m[2] ?? "";
  const mult = BINARY[suffix] ?? DECIMAL[suffix];
  return mult === undefined ? null : n * mult;
}

/** CPU in millicores ("250m" -> 250, "1.5" -> 1500), or null. */
export function parseCPU(q: unknown): number | null {
  const v = parseQuantity(q);
  return v === null ? null : Math.round(v * 1000);
}

/** Bytes ("512Mi" -> 536870912), or null. */
export function parseBytes(q: unknown): number | null {
  const v = parseQuantity(q);
  return v === null ? null : Math.round(v);
}

/** A slider scale: quantities in increasing order with their parsed value. */
export interface Scale {
  kind: "cpu" | "memory" | "storage" | "count";
  stops: string[];
}

export const CPU_SCALE: Scale = {
  kind: "cpu",
  stops: ["10m", "25m", "50m", "100m", "150m", "200m", "250m", "300m", "400m", "500m", "750m", "1", "1500m", "2", "3", "4", "6", "8", "12", "16"],
};

export const MEMORY_SCALE: Scale = {
  kind: "memory",
  stops: ["16Mi", "32Mi", "64Mi", "96Mi", "128Mi", "192Mi", "256Mi", "384Mi", "512Mi", "768Mi", "1Gi", "1536Mi", "2Gi", "3Gi", "4Gi", "6Gi", "8Gi", "12Gi", "16Gi", "24Gi", "32Gi", "64Gi"],
};

export const STORAGE_SCALE: Scale = {
  kind: "storage",
  stops: ["100Mi", "256Mi", "512Mi", "1Gi", "2Gi", "5Gi", "10Gi", "20Gi", "50Gi", "100Gi", "200Gi", "500Gi", "1Ti", "2Ti"],
};

export function scaleValue(scale: Scale, q: unknown): number | null {
  return scale.kind === "cpu" ? parseCPU(q) : parseBytes(q);
}

/** Index of the stop closest to a value (compared on a log scale). */
export function nearestStop(scale: Scale, q: unknown): number {
  const v = scaleValue(scale, q);
  if (v === null || v <= 0) return 0;
  let best = 0;
  let bestDist = Infinity;
  scale.stops.forEach((s, i) => {
    const sv = scaleValue(scale, s) ?? 1;
    const d = Math.abs(Math.log(sv) - Math.log(v));
    if (d < bestDist) {
      best = i;
      bestDist = d;
    }
  });
  return best;
}

/** Human reading of a quantity: "250m" -> "0.25 CPU", "1536Mi" -> "1.5 GiB". */
export function describeQuantity(scale: Scale, q: unknown): string {
  const v = scaleValue(scale, q);
  if (v === null) return "";
  if (scale.kind === "cpu") {
    const cores = v / 1000;
    return `${Number(cores.toFixed(3))} ${cores === 1 ? "CPU" : "CPUs"}`;
  }
  const units = ["bytes", "KiB", "MiB", "GiB", "TiB"];
  let n = v;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${Number(n.toFixed(2))} ${units[i]}`;
}
