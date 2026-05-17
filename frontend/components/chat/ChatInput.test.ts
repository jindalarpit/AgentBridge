import { describe, it, expect } from "vitest";
import { ChatInput, validateMessageContent, MIN_MESSAGE_LENGTH, MAX_MESSAGE_LENGTH } from "./ChatInput";
import * as fc from "fast-check";

// ─── ChatInput Component Tests ───────────────────────────────────────────────
// Tests verify the component export, validation logic, and constants.
// The test environment is node (no DOM), so we validate logic and exports.

describe("ChatInput", () => {
  it("is exported as a function component", () => {
    expect(typeof ChatInput).toBe("function");
  });

  it("exports MIN_MESSAGE_LENGTH as 1", () => {
    expect(MIN_MESSAGE_LENGTH).toBe(1);
  });

  it("exports MAX_MESSAGE_LENGTH as 32000", () => {
    expect(MAX_MESSAGE_LENGTH).toBe(32000);
  });
});

// ─── validateMessageContent Tests ────────────────────────────────────────────

describe("validateMessageContent", () => {
  it("returns null for a valid message", () => {
    expect(validateMessageContent("Hello, agent!")).toBeNull();
  });

  it("returns error for empty string", () => {
    const result = validateMessageContent("");
    expect(result).toBe("Message cannot be empty");
  });

  it("returns error for whitespace-only string", () => {
    const result = validateMessageContent("   \n\t  ");
    expect(result).toBe("Message cannot be empty");
  });

  it("returns error for message exceeding 32000 chars", () => {
    const longMessage = "a".repeat(32001);
    const result = validateMessageContent(longMessage);
    expect(result).toContain("exceeds maximum length");
  });

  it("accepts message at exactly 32000 chars", () => {
    const maxMessage = "a".repeat(32000);
    expect(validateMessageContent(maxMessage)).toBeNull();
  });

  it("accepts message at exactly 1 char", () => {
    expect(validateMessageContent("x")).toBeNull();
  });

  it("trims whitespace before validation", () => {
    // A message with leading/trailing whitespace but valid trimmed content
    expect(validateMessageContent("  hello  ")).toBeNull();
  });

  it("rejects message that is only whitespace even if long", () => {
    const whitespace = " ".repeat(100);
    const result = validateMessageContent(whitespace);
    expect(result).toBe("Message cannot be empty");
  });
});

// ─── Property-Based Tests ────────────────────────────────────────────────────
// **Validates: Requirements 6.8**

describe("validateMessageContent - property tests", () => {
  it("accepts any non-empty string with trimmed length between 1 and 32000", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 32000 }).filter(
          (s) => s.trim().length >= 1 && s.trim().length <= 32000
        ),
        (content) => {
          expect(validateMessageContent(content)).toBeNull();
        }
      ),
      { numRuns: 100 }
    );
  });

  it("rejects any string with trimmed length exceeding 32000", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 32001, maxLength: 33000 }),
        (content) => {
          // Ensure trimmed length exceeds limit
          const padded = "x" + content; // guarantee non-whitespace
          if (padded.trim().length > 32000) {
            const result = validateMessageContent(padded);
            expect(result).not.toBeNull();
            expect(result).toContain("exceeds maximum length");
          }
        }
      ),
      { numRuns: 100 }
    );
  });

  it("rejects any whitespace-only string regardless of length", () => {
    fc.assert(
      fc.property(
        fc.stringOf(fc.constantFrom(" ", "\t", "\n", "\r"), { minLength: 0, maxLength: 100 }),
        (content) => {
          const result = validateMessageContent(content);
          expect(result).toBe("Message cannot be empty");
        }
      ),
      { numRuns: 100 }
    );
  });

  it("validation result depends only on trimmed length", () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1, maxLength: 100 }),
        fc.string({ minLength: 0, maxLength: 10 }).map((s) =>
          s.replace(/\S/g, " ")
        ),
        (core, padding) => {
          // Same core content with different padding should give same result
          const withPadding = padding + core + padding;
          const resultDirect = validateMessageContent(core);
          const resultPadded = validateMessageContent(withPadding);
          expect(resultDirect).toBe(resultPadded);
        }
      ),
      { numRuns: 100 }
    );
  });
});
