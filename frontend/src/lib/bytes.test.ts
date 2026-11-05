import { describe, expect, it } from "vitest";
import { base64ToBytes, textToBase64 } from "./bytes";

describe("base64 terminal encoding", () => {
  it("round-trips UTF-8", () => {
    const text = "ls -la ✓ é\r";
    const back = new TextDecoder().decode(base64ToBytes(textToBase64(text)));
    expect(back).toBe(text);
  });

  it("encodes control characters", () => {
    expect(textToBase64("\x03")).toBe("Aw==");
  });
});
