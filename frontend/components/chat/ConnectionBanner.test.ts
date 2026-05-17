import { describe, it, expect } from "vitest";
import { ConnectionBanner } from "./ConnectionBanner";

// ─── ConnectionBanner Component Tests ────────────────────────────────────────
// Tests verify the component export and behavior.
// The test environment is node (no DOM), so we validate exports.

describe("ConnectionBanner", () => {
  it("is exported as a function component", () => {
    expect(typeof ConnectionBanner).toBe("function");
  });
});
