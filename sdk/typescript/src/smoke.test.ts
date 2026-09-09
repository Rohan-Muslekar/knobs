import { describe, expect, it } from "vitest";

describe("toolchain smoke test", () => {
  it("runs vitest against TypeScript source", () => {
    expect(1 + 1).toBe(2);
  });
});
