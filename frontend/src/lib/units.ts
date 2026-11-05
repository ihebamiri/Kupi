/** 1500 -> "1.5 cores", 250 -> "250m". */
export function formatCPU(millicores: number): string {
  if (millicores >= 1000) {
    const cores = (millicores / 1000).toFixed(2).replace(/\.?0+$/, "");
    return `${cores} ${cores === "1" ? "core" : "cores"}`;
  }
  return `${Math.round(millicores)}m`;
}

/** Bytes as Ki/Mi/Gi, like Kubernetes quantities. */
export function formatBytes(bytes: number): string {
  const units = ["B", "Ki", "Mi", "Gi", "Ti"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)}${units[i]}`;
}

/** Share of capacity as a whole percent, or null when capacity is unknown. */
export function percent(used: number, capacity: number): number | null {
  if (!capacity) return null;
  return Math.min(999, Math.round((used / capacity) * 100));
}
