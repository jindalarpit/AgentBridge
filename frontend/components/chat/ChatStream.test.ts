import { describe, it, expect, beforeEach } from "vitest";
import { ChatStream } from "./ChatStream";
import { useMessageStore } from "@/lib/store";

// ─── ChatStream Component Tests ──────────────────────────────────────────────
// Tests verify the component export and its store integration.
// The test environment is node (no DOM), so we validate the store behavior
// that drives the ChatStream component. Full rendering tests would require
// jsdom/happy-dom environment.

describe("ChatStream", () => {
  beforeEach(() => {
    useMessageStore.setState({
      messages: [],
      isStreaming: false,
      streamingContent: "",
    });
  });

  it("is exported as a function component", () => {
    expect(typeof ChatStream).toBe("function");
  });

  describe("store integration for streaming behavior", () => {
    it("streaming starts when appendStreamToken is called", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "Hello");

      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(true);
      expect(state.streamingContent).toBe("Hello");
    });

    it("streaming content accumulates across multiple tokens", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "Hello");
      useMessageStore.getState().appendStreamToken("s1", 2, " ");
      useMessageStore.getState().appendStreamToken("s1", 3, "world");

      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(true);
      expect(state.streamingContent).toBe("Hello world");
    });

    it("streaming resets after finalizeStream (chat:done)", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "Response");
      useMessageStore.getState().addMessage({
        id: "m1",
        chat_session_id: "s1",
        seq: 1,
        role: "assistant",
        content: "",
        status: "streaming",
        created_at: new Date().toISOString(),
      });
      useMessageStore.getState().finalizeStream("m1", "Response", 500);

      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(false);
      expect(state.streamingContent).toBe("");
    });

    it("streaming resets after setStreamError (chat:error)", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "Partial");
      useMessageStore.getState().addMessage({
        id: "m1",
        chat_session_id: "s1",
        seq: 1,
        role: "assistant",
        content: "",
        status: "streaming",
        created_at: new Date().toISOString(),
      });
      useMessageStore.getState().setStreamError("m1", "Agent crashed");

      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(false);
      expect(state.streamingContent).toBe("");
    });

    it("isStreaming is false initially (component would return null)", () => {
      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(false);
      expect(state.streamingContent).toBe("");
    });

    it("handles empty token content gracefully", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "");

      const state = useMessageStore.getState();
      expect(state.isStreaming).toBe(true);
      expect(state.streamingContent).toBe("");
    });

    it("handles special characters in streaming content", () => {
      useMessageStore.getState().appendStreamToken("s1", 1, "```typescript\n");
      useMessageStore.getState().appendStreamToken("s1", 2, "const x = 1;\n");
      useMessageStore.getState().appendStreamToken("s1", 3, "```");

      const state = useMessageStore.getState();
      expect(state.streamingContent).toBe("```typescript\nconst x = 1;\n```");
    });
  });
});
