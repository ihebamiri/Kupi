/**
 * Parses .env text as Docker Compose and the common dotenv libraries read
 * it: KEY=value lines, optional "export ", # comments, single quotes taken
 * literally, double quotes with \n, \", \\ and \$ escapes.
 */
export interface ParsedEnv {
  values: Record<string, string>;
  errors: string[];
}

const KEY = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

export function parseEnv(text: string): ParsedEnv {
  const values: Record<string, string> = {};
  const errors: string[] = [];
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  for (let i = 0; i < lines.length; i++) {
    let line = lines[i].trim();
    if (line === "" || line.startsWith("#")) continue;
    if (line.startsWith("export ")) line = line.slice(7).trim();
    const eq = line.indexOf("=");
    if (eq <= 0) {
      errors.push(`Line ${i + 1}: expected KEY=value.`);
      continue;
    }
    const key = line.slice(0, eq).trim();
    if (!KEY.test(key)) {
      errors.push(`Line ${i + 1}: "${key}" isn't a valid name.`);
      continue;
    }
    let rest = line.slice(eq + 1).trim();
    let value: string;
    if (rest.startsWith("'")) {
      const end = rest.indexOf("'", 1);
      if (end < 0) {
        errors.push(`Line ${i + 1}: missing closing quote.`);
        continue;
      }
      value = rest.slice(1, end);
    } else if (rest.startsWith('"')) {
      // A double-quoted value may span lines.
      let body = rest.slice(1);
      let end = closingQuote(body);
      while (end < 0 && i + 1 < lines.length) {
        i++;
        body += "\n" + lines[i];
        end = closingQuote(body);
      }
      if (end < 0) {
        errors.push(`Line ${i + 1}: missing closing quote.`);
        continue;
      }
      value = body.slice(0, end).replace(/\\(.)/g, (_, c: string) => (c === "n" ? "\n" : c === "r" ? "\r" : c === "t" ? "\t" : c));
    } else {
      const hash = rest.search(/\s#/);
      if (hash >= 0) rest = rest.slice(0, hash);
      value = rest.trim();
    }
    values[key] = value;
  }
  return { values, errors };
}

function closingQuote(s: string): number {
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\") i++;
    else if (s[i] === '"') return i;
  }
  return -1;
}
