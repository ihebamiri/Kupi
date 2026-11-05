import { describe, expect, it } from "vitest";
import { parseEnv } from "./dotenv";

describe("parseEnv", () => {
  it("reads the common forms", () => {
    const { values, errors } = parseEnv(
      [
        "# comment",
        "PLAIN=abc-123",
        "export EXPORTED=yes",
        "SPACED = value with spaces # trailing comment",
        "SINGLE='it is $literal \\n'",
        'DOUBLE="line one\\nline \\"two\\" \\$HOME"',
        'MULTI="first',
        'second"',
        "EMPTY=",
        "bad line",
        "1BAD=x",
      ].join("\n"),
    );
    expect(values).toEqual({
      PLAIN: "abc-123",
      EXPORTED: "yes",
      SPACED: "value with spaces",
      SINGLE: "it is $literal \\n",
      DOUBLE: 'line one\nline "two" $HOME',
      MULTI: "first\nsecond",
      EMPTY: "",
    });
    expect(errors).toHaveLength(2);
  });

  it("round-trips Kupi's own .env output", () => {
    // Matches internal/secrets FormatEnv quoting.
    const { values } = parseEnv("PLAIN=abc-123\nQUOTE=\"it's\\nmulti \\$HOME\"\nSPACED='a b'\n");
    expect(values).toEqual({ PLAIN: "abc-123", QUOTE: "it's\nmulti $HOME", SPACED: "a b" });
  });
});
