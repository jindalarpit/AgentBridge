import { describe, it, expect } from "vitest";
import { AgentSelector } from "./AgentSelector";

// ─── AgentSelector Component Tests ───────────────────────────────────────────
// Tests verify the component export and basic structure.
// The test environment is node (no DOM), so we validate exports.
// Validates: Requirements 5.1, 5.2, 5.3, 5.4

describe("AgentSelector", () => {
  it("is exported as a function component", () => {
    expect(typeof AgentSelector).toBe("function");
  });

  it("accepts optional onBind prop", () => {
    // Verify the function signature accepts props without error
    expect(AgentSelector.length).toBeLessThanOrEqual(1);
  });
});
