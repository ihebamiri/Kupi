// The form tree produced by the Go schema package.
export interface SchemaField {
  name: string;
  required?: boolean;
  node: SchemaNode;
}

export interface SchemaNode {
  type: "object" | "array" | "map" | "string" | "integer" | "number" | "boolean" | "intOrString" | "quantity" | "any";
  description?: string;
  format?: string;
  enum?: string[];
  fields?: SchemaField[];
  items?: SchemaNode;
  values?: SchemaNode;
  truncated?: boolean;
}

/** Finds the schema node for a value path (numbers step into array items). */
export function nodeAt(root: SchemaNode, path: (string | number)[]): SchemaNode | undefined {
  let cur: SchemaNode | undefined = root;
  for (const key of path) {
    if (!cur) return undefined;
    if (typeof key === "number") {
      cur = cur.type === "array" ? cur.items : undefined;
    } else if (cur.type === "map") {
      cur = cur.values;
    } else {
      cur = cur.fields?.find((f) => f.name === key)?.node;
    }
  }
  return cur;
}

/** First sentence of a description, for a one-line hint. */
export function firstSentence(text: string | undefined): string {
  if (!text) return "";
  const m = text.match(/^(.+?[.!?])(\s|$)/s);
  return (m ? m[1] : text).replace(/\s+/g, " ").trim();
}

/** True when a field or anything below it matches the search text. */
export function matchesSearch(name: string, node: SchemaNode, query: string, depth = 0): boolean {
  if (!query) return true;
  if (name.toLowerCase().includes(query)) return true;
  if (depth > 6) return false;
  if (node.type === "object") return (node.fields ?? []).some((f) => matchesSearch(f.name, f.node, query, depth + 1));
  if (node.type === "array" && node.items?.type === "object") {
    return (node.items.fields ?? []).some((f) => matchesSearch(f.name, f.node, query, depth + 1));
  }
  return false;
}
