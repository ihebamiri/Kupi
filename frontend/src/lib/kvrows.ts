/**
 * Rows of a secret-values editor. Existing keys start as "keep": their
 * values are never loaded, and only keys the user replaces, adds or removes
 * are sent to Go.
 */
export type RowState = "new" | "keep" | "replace" | "remove";

export interface KVRow {
  id: number;
  key: string;
  value: string;
  state: RowState;
  show: boolean;
}

let nextId = 1;

export function newRow(key = "", value = ""): KVRow {
  return { id: nextId++, key, value, state: "new", show: false };
}

export function rowsFromKeys(keys: string[]): KVRow[] {
  return keys.map((key) => ({ ...newRow(key), state: "keep" as const }));
}

const KEY = /^[A-Za-z0-9._-]{1,253}$/;

export interface Collected {
  set: Record<string, string>;
  del: string[];
  error: string | null;
}

/** Turns rows into the change to send. Empty new rows are ignored. */
export function collect(rows: KVRow[]): Collected {
  const set: Record<string, string> = {};
  const del: string[] = [];
  const seen = new Set<string>();
  for (const r of rows) {
    const key = r.key.trim();
    if (r.state === "new" && key === "" && r.value === "") continue;
    if (r.state === "remove") {
      del.push(key);
      continue;
    }
    if (!KEY.test(key) || key === "." || key === "..") {
      return { set, del, error: `"${key || "(empty)"}" isn't a valid key. Use letters, digits, "-", "_" and ".".` };
    }
    if (seen.has(key)) return { set, del, error: `The key ${key} appears twice.` };
    seen.add(key);
    if (r.state === "new" || r.state === "replace") set[key] = r.value;
  }
  return { set, del, error: null };
}

/** Merges parsed .env values into rows: existing keys are replaced, new ones added. */
export function mergeValues(rows: KVRow[], values: Record<string, string>): KVRow[] {
  const out = rows.filter((r) => !(r.state === "new" && r.key === "" && r.value === ""));
  for (const [key, value] of Object.entries(values)) {
    const i = out.findIndex((r) => r.key === key);
    if (i >= 0) out[i] = { ...out[i], value, state: out[i].state === "new" ? "new" : "replace" };
    else out.push(newRow(key, value));
  }
  return out;
}

/** True when the rows would change something. */
export function hasChanges(rows: KVRow[]): boolean {
  const c = collect(rows);
  return Object.keys(c.set).length > 0 || c.del.length > 0;
}
