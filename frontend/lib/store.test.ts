import { describe, it, expect, beforeEach } from "vitest";
import {
  useAuthStore,
  useSessionStore,
  useMessageStore,
  useRuntimeStore,
  useConnectionStore,
  handleChatStream,
  handleChatDone,
  handleChatError,
  handleRuntimeStatus,
  handleSessionCreated,
  handleSessionDeleted,
  handleSessionUpdated,
  type User,
  type ChatSession,
  type ChatMessage,
  type Runtime,
} from "./store";

// ─── Auth Store ──────────────────────────────────────────────────────────────

describe("useAuthStore", () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, token: null, isAuthenticated: false });
  });

  it("starts unauthenticated", () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
    expect(state.isAuthenticated).toBe(false);
  });

  it("login sets user, token, and isAuthenticated", () => {
    const user: User = {
      id: "u1",
      email: "test@example.com",
      display_name: "Test",
      created_at: "2024-01-01T00:00:00Z",
    };
    useAuthStore.getState().login(user, "jwt-token-123");

    const state = useAuthStore.getState();
    expect(state.user).toEqual(user);
    expect(state.token).toBe("jwt-token-123");
    expect(state.isAuthenticated).toBe(true);
  });

  it("logout clears all auth state", () => {
    const user: User = {
      id: "u1",
      email: "test@example.com",
      display_name: "Test",
      created_at: "2024-01-01T00:00:00Z",
    };
    useAuthStore.getState().login(user, "jwt-token-123");
    useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.token).toBeNull();
    expect(state.isAuthenticated).toBe(false);
  });

  it("setUser updates user without changing token", () => {
    const user: User = {
      id: "u1",
      email: "test@example.com",
      display_name: "Test",
      created_at: "2024-01-01T00:00:00Z",
    };
    useAuthStore.getState().login(user, "jwt-token-123");

    const updatedUser: User = { ...user, display_name: "Updated" };
    useAuthStore.getState().setUser(updatedUser);

    const state = useAuthStore.getState();
    expect(state.user?.display_name).toBe("Updated");
    expect(state.token).toBe("jwt-token-123");
  });
});

// ─── Session Store ───────────────────────────────────────────────────────────

describe("useSessionStore", () => {
  beforeEach(() => {
    useSessionStore.setState({ sessions: [], activeSessionId: null });
  });

  const makeSession = (id: string, title = "New Chat"): ChatSession => ({
    id,
    user_id: "u1",
    title,
    status: "active",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  });

  it("starts with empty sessions", () => {
    const state = useSessionStore.getState();
    expect(state.sessions).toEqual([]);
    expect(state.activeSessionId).toBeNull();
  });

  it("setSessions replaces the session list", () => {
    const sessions = [makeSession("s1"), makeSession("s2")];
    useSessionStore.getState().setSessions(sessions);
    expect(useSessionStore.getState().sessions).toEqual(sessions);
  });

  it("addSession prepends to the list", () => {
    useSessionStore.getState().setSessions([makeSession("s1")]);
    useSessionStore.getState().addSession(makeSession("s2"));

    const sessions = useSessionStore.getState().sessions;
    expect(sessions[0].id).toBe("s2");
    expect(sessions[1].id).toBe("s1");
  });

  it("removeSession removes by id and clears activeSessionId if matching", () => {
    useSessionStore.getState().setSessions([makeSession("s1"), makeSession("s2")]);
    useSessionStore.getState().setActiveSessionId("s1");
    useSessionStore.getState().removeSession("s1");

    const state = useSessionStore.getState();
    expect(state.sessions).toHaveLength(1);
    expect(state.sessions[0].id).toBe("s2");
    expect(state.activeSessionId).toBeNull();
  });

  it("removeSession does not clear activeSessionId if different", () => {
    useSessionStore.getState().setSessions([makeSession("s1"), makeSession("s2")]);
    useSessionStore.getState().setActiveSessionId("s2");
    useSessionStore.getState().removeSession("s1");

    expect(useSessionStore.getState().activeSessionId).toBe("s2");
  });

  it("updateSession merges partial updates", () => {
    useSessionStore.getState().setSessions([makeSession("s1")]);
    useSessionStore.getState().updateSession("s1", { title: "Renamed" });

    expect(useSessionStore.getState().sessions[0].title).toBe("Renamed");
  });
});

// ─── Message Store ───────────────────────────────────────────────────────────

describe("useMessageStore", () => {
  beforeEach(() => {
    useMessageStore.setState({
      messages: [],
      isStreaming: false,
      streamingContent: "",
    });
  });

  const makeMessage = (id: string, seq: number, role: "user" | "assistant" = "user"): ChatMessage => ({
    id,
    chat_session_id: "s1",
    seq,
    role,
    content: `Message ${seq}`,
    status: "complete",
    created_at: "2024-01-01T00:00:00Z",
  });

  it("starts with empty messages and no streaming", () => {
    const state = useMessageStore.getState();
    expect(state.messages).toEqual([]);
    expect(state.isStreaming).toBe(false);
    expect(state.streamingContent).toBe("");
  });

  it("addMessage appends to the list", () => {
    useMessageStore.getState().addMessage(makeMessage("m1", 1));
    useMessageStore.getState().addMessage(makeMessage("m2", 2));

    const messages = useMessageStore.getState().messages;
    expect(messages).toHaveLength(2);
    expect(messages[0].id).toBe("m1");
    expect(messages[1].id).toBe("m2");
  });

  it("appendStreamToken accumulates content and sets isStreaming", () => {
    useMessageStore.getState().appendStreamToken("s1", 1, "Hello");
    useMessageStore.getState().appendStreamToken("s1", 2, " world");

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(true);
    expect(state.streamingContent).toBe("Hello world");
  });

  it("finalizeStream resets streaming state and updates message", () => {
    useMessageStore.getState().addMessage({
      ...makeMessage("m1", 1, "assistant"),
      status: "streaming",
      content: "",
    });
    useMessageStore.getState().appendStreamToken("s1", 1, "Final content");
    useMessageStore.getState().finalizeStream("m1", "Final content", 1500);

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(false);
    expect(state.streamingContent).toBe("");
    expect(state.messages[0].content).toBe("Final content");
    expect(state.messages[0].status).toBe("complete");
    expect(state.messages[0].elapsed_ms).toBe(1500);
  });

  it("setStreamError marks message as error and resets streaming", () => {
    useMessageStore.getState().addMessage({
      ...makeMessage("m1", 1, "assistant"),
      status: "streaming",
      content: "",
    });
    useMessageStore.getState().setStreamError("m1", "Agent timeout");

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(false);
    expect(state.messages[0].status).toBe("error");
    expect(state.messages[0].failure_reason).toBe("Agent timeout");
  });

  it("clearMessages resets all message state", () => {
    useMessageStore.getState().addMessage(makeMessage("m1", 1));
    useMessageStore.getState().appendStreamToken("s1", 1, "partial");
    useMessageStore.getState().clearMessages();

    const state = useMessageStore.getState();
    expect(state.messages).toEqual([]);
    expect(state.isStreaming).toBe(false);
    expect(state.streamingContent).toBe("");
  });
});

// ─── Runtime Store ───────────────────────────────────────────────────────────

describe("useRuntimeStore", () => {
  beforeEach(() => {
    useRuntimeStore.setState({ runtimes: [] });
  });

  const makeRuntime = (id: string, status: Runtime["status"] = "available"): Runtime => ({
    id,
    daemon_id: "d1",
    agent_type: "claude",
    binary_path: "/usr/local/bin/claude",
    version: "1.0.0",
    status,
    created_at: "2024-01-01T00:00:00Z",
  });

  it("starts with empty runtimes", () => {
    expect(useRuntimeStore.getState().runtimes).toEqual([]);
  });

  it("setRuntimes replaces the list", () => {
    const runtimes = [makeRuntime("r1"), makeRuntime("r2")];
    useRuntimeStore.getState().setRuntimes(runtimes);
    expect(useRuntimeStore.getState().runtimes).toEqual(runtimes);
  });

  it("updateRuntimeStatus changes status for matching runtime", () => {
    useRuntimeStore.getState().setRuntimes([makeRuntime("r1"), makeRuntime("r2")]);
    useRuntimeStore.getState().updateRuntimeStatus("r1", "offline");

    const runtimes = useRuntimeStore.getState().runtimes;
    expect(runtimes[0].status).toBe("offline");
    expect(runtimes[1].status).toBe("available");
  });
});

// ─── Connection Store ────────────────────────────────────────────────────────

describe("useConnectionStore", () => {
  beforeEach(() => {
    useConnectionStore.setState({ status: "disconnected", tokenExpired: false });
  });

  it("starts disconnected", () => {
    expect(useConnectionStore.getState().status).toBe("disconnected");
  });

  it("starts with tokenExpired false", () => {
    expect(useConnectionStore.getState().tokenExpired).toBe(false);
  });

  it("setStatus updates connection status", () => {
    useConnectionStore.getState().setStatus("connected");
    expect(useConnectionStore.getState().status).toBe("connected");

    useConnectionStore.getState().setStatus("reconnecting");
    expect(useConnectionStore.getState().status).toBe("reconnecting");
  });

  it("setTokenExpired updates tokenExpired flag", () => {
    useConnectionStore.getState().setTokenExpired(true);
    expect(useConnectionStore.getState().tokenExpired).toBe(true);

    useConnectionStore.getState().setTokenExpired(false);
    expect(useConnectionStore.getState().tokenExpired).toBe(false);
  });
});

// ─── WebSocket Event Handlers ────────────────────────────────────────────────

describe("WebSocket event handlers", () => {
  beforeEach(() => {
    useMessageStore.setState({ messages: [], isStreaming: false, streamingContent: "" });
    useRuntimeStore.setState({ runtimes: [] });
    useSessionStore.setState({ sessions: [], activeSessionId: null });
  });

  it("handleChatStream appends tokens to streaming content", () => {
    handleChatStream({ session_id: "s1", seq: 1, content: "Hello" });
    handleChatStream({ session_id: "s1", seq: 2, content: " world" });

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(true);
    expect(state.streamingContent).toBe("Hello world");
  });

  it("handleChatDone adds finalized message and resets streaming", () => {
    handleChatStream({ session_id: "s1", seq: 1, content: "Response" });
    handleChatDone({
      session_id: "s1",
      message_id: "m1",
      content: "Response",
      elapsed_ms: 2000,
    });

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(false);
    expect(state.streamingContent).toBe("");
    expect(state.messages).toHaveLength(1);
    expect(state.messages[0].id).toBe("m1");
    expect(state.messages[0].role).toBe("assistant");
    expect(state.messages[0].content).toBe("Response");
    expect(state.messages[0].status).toBe("complete");
  });

  it("handleChatError sets error on specific message", () => {
    useMessageStore.getState().addMessage({
      id: "m1",
      chat_session_id: "s1",
      seq: 1,
      role: "assistant",
      content: "",
      status: "streaming",
      created_at: "2024-01-01T00:00:00Z",
    });

    handleChatError({
      session_id: "s1",
      message_id: "m1",
      error: "Agent timeout",
      code: "agent_timeout",
    });

    const msg = useMessageStore.getState().messages[0];
    expect(msg.status).toBe("error");
    expect(msg.failure_reason).toBe("Agent timeout");
  });

  it("handleChatError without message_id resets streaming state", () => {
    useMessageStore.setState({ isStreaming: true, streamingContent: "partial" });

    handleChatError({
      session_id: "s1",
      error: "Session error",
      code: "internal_error",
    });

    const state = useMessageStore.getState();
    expect(state.isStreaming).toBe(false);
    expect(state.streamingContent).toBe("");
  });

  it("handleRuntimeStatus updates runtime status", () => {
    useRuntimeStore.getState().setRuntimes([
      {
        id: "r1",
        daemon_id: "d1",
        agent_type: "claude",
        binary_path: "/usr/local/bin/claude",
        version: "1.0.0",
        status: "available",
        created_at: "2024-01-01T00:00:00Z",
      },
    ]);

    handleRuntimeStatus({ runtime_id: "r1", status: "offline" });
    expect(useRuntimeStore.getState().runtimes[0].status).toBe("offline");
  });

  it("handleSessionCreated adds session to the list", () => {
    const session: ChatSession = {
      id: "s1",
      user_id: "u1",
      title: "New Chat",
      status: "active",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    };

    handleSessionCreated(session);
    expect(useSessionStore.getState().sessions).toHaveLength(1);
    expect(useSessionStore.getState().sessions[0].id).toBe("s1");
  });

  it("handleSessionDeleted removes session from the list", () => {
    useSessionStore.getState().setSessions([
      {
        id: "s1",
        user_id: "u1",
        title: "Chat",
        status: "active",
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    ]);

    handleSessionDeleted({ session_id: "s1" });
    expect(useSessionStore.getState().sessions).toHaveLength(0);
  });

  it("handleSessionUpdated updates session title", () => {
    useSessionStore.getState().setSessions([
      {
        id: "s1",
        user_id: "u1",
        title: "Old Title",
        status: "active",
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    ]);

    handleSessionUpdated({ session_id: "s1", title: "New Title" });
    expect(useSessionStore.getState().sessions[0].title).toBe("New Title");
  });
});
