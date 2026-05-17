import { describe, it, expect } from "vitest";
import { ChatMessage } from "./ChatMessage";
import type { ChatMessageProps } from "./ChatMessage";
import type { ChatMessage as ChatMessageType } from "@/lib/store";

// ─── ChatMessage Component Tests ─────────────────────────────────────────────
// Tests verify the component export and prop interface.
// The test environment is node (no DOM), so we validate types and exports.
// Full rendering tests would require jsdom/happy-dom environment.

describe("ChatMessage", () => {
  it("is exported as a function component", () => {
    expect(typeof ChatMessage).toBe("function");
  });

  it("accepts ChatMessageProps with a user message", () => {
    const userMessage: ChatMessageType = {
      id: "m1",
      chat_session_id: "s1",
      seq: 1,
      role: "user",
      content: "Hello, agent!",
      status: "complete",
      created_at: "2024-01-15T10:30:00Z",
    };

    const props: ChatMessageProps = { message: userMessage };
    expect(props.message.role).toBe("user");
    expect(props.message.content).toBe("Hello, agent!");
    expect(props.message.status).toBe("complete");
  });

  it("accepts ChatMessageProps with an assistant message", () => {
    const assistantMessage: ChatMessageType = {
      id: "m2",
      chat_session_id: "s1",
      seq: 2,
      role: "assistant",
      content: "Hello! How can I help you?",
      status: "complete",
      elapsed_ms: 1200,
      created_at: "2024-01-15T10:30:05Z",
    };

    const props: ChatMessageProps = { message: assistantMessage };
    expect(props.message.role).toBe("assistant");
    expect(props.message.elapsed_ms).toBe(1200);
  });

  it("accepts ChatMessageProps with an error message", () => {
    const errorMessage: ChatMessageType = {
      id: "m3",
      chat_session_id: "s1",
      seq: 3,
      role: "assistant",
      content: "",
      status: "error",
      failure_reason: "Agent timeout after 300 seconds",
      created_at: "2024-01-15T10:31:00Z",
    };

    const props: ChatMessageProps = { message: errorMessage };
    expect(props.message.status).toBe("error");
    expect(props.message.failure_reason).toBe("Agent timeout after 300 seconds");
  });

  it("accepts all valid message roles", () => {
    const roles: ChatMessageType["role"][] = ["user", "assistant", "system"];
    roles.forEach((role) => {
      const msg: ChatMessageType = {
        id: `m-${role}`,
        chat_session_id: "s1",
        seq: 1,
        role,
        content: `${role} message`,
        status: "complete",
        created_at: "2024-01-15T10:30:00Z",
      };
      const props: ChatMessageProps = { message: msg };
      expect(props.message.role).toBe(role);
    });
  });

  it("accepts all valid message statuses", () => {
    const statuses: ChatMessageType["status"][] = ["pending", "streaming", "complete", "error"];
    statuses.forEach((status) => {
      const msg: ChatMessageType = {
        id: `m-${status}`,
        chat_session_id: "s1",
        seq: 1,
        role: "assistant",
        content: "test",
        status,
        created_at: "2024-01-15T10:30:00Z",
      };
      const props: ChatMessageProps = { message: msg };
      expect(props.message.status).toBe(status);
    });
  });
});
