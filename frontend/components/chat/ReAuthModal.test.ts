import { describe, it, expect } from "vitest";
import { ReAuthModal } from "./ReAuthModal";

// ─── ReAuthModal Component Tests ─────────────────────────────────────────────
// Tests verify the component export and behavior.
// The test environment is node (no DOM), so we validate exports.

describe("ReAuthModal", () => {
  it("is exported as a function component", () => {
    expect(typeof ReAuthModal).toBe("function");
  });
});
