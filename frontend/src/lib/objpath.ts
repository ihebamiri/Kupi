// Immutable get/set/delete on plain JSON objects by path. Numbers index arrays.

export type Path = (string | number)[];
type Json = unknown;

export function getIn(obj: Json, path: Path): Json {
  let cur: Json = obj;
  for (const key of path) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string | number, Json>)[key];
  }
  return cur;
}

/** Returns a copy of obj with value at path, creating parents as needed. */
export function setIn(obj: Json, path: Path, value: Json): Json {
  if (path.length === 0) return value;
  const [key, ...rest] = path;
  const isIndex = typeof key === "number";
  const base: Json = obj !== null && typeof obj === "object" ? obj : isIndex ? [] : {};
  const child = setIn((base as Record<string | number, Json>)[key], rest, value);
  if (Array.isArray(base)) {
    const copy = [...base];
    copy[key as number] = child;
    return copy;
  }
  return { ...(base as Record<string, Json>), [key]: child };
}

/** Returns a copy of obj without the value at path. Array items are removed, not left as holes. */
export function deleteIn(obj: Json, path: Path): Json {
  if (path.length === 0 || obj === null || typeof obj !== "object") return obj;
  const [key, ...rest] = path;
  if (rest.length === 0) {
    if (Array.isArray(obj)) return obj.filter((_, i) => i !== key);
    const copy = { ...(obj as Record<string, Json>) };
    delete copy[key as string];
    return copy;
  }
  const child = (obj as Record<string | number, Json>)[key];
  if (child === undefined) return obj;
  const updated = deleteIn(child, rest);
  if (Array.isArray(obj)) {
    const copy = [...obj];
    copy[key as number] = updated;
    return copy;
  }
  return { ...(obj as Record<string, Json>), [key]: updated };
}

/** Sets value, or deletes the key when value is undefined. */
export function updateIn(obj: Json, path: Path, value: Json): Json {
  return value === undefined ? deleteIn(obj, path) : setIn(obj, path, value);
}

export function pathLabel(path: Path): string {
  return path.map((p, i) => (typeof p === "number" ? `[${p}]` : i === 0 ? p : `.${p}`)).join("");
}
