// Zustand stores for client state management
// Validates: Requirements 3.3, 6.4
import { create } from "zustand";

// ─── Domain Types ────────────────────────────────────────────────────────────

export interface User {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
}

export interface ChatSession {
  id: string;
  user_id: string;
  runtime_id?: string;
  title: string;
  status: "active" | "archived";
  created_at: string;
  updated_at: string;
}

export interface ChatMessage {
  id: string;
  chat_session_id: string;
  seq: number;
  role: "user" | "assistant" | "system";
  content: string;
  status: "pending" | "streaming" | "complete" | "error";
  elapsed_ms?: number;
  failure_reason?: string;
  created_at: string;
}

export interface Runtime {
  id: string;
  daemon_id: string;
  agent_type: string;
  binary_path: string;
  version: string;
  status: "available" | "unavailable" | "offline";
  created_at: string;
}

// ─── Auth Store ──────────────────────────────────────────────────────────────

export interface AuthState {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  login: (user: User, token: string) => void;
  logout: () => void;
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  token: null,
  isAuthenticated: false,
  login: (user, token) => set({ user, token, isAuthenticated: true }),
  logout: () => set({ user: null, token: null, isAuthenticated: false }),
  setUser: (user) => set({ user }),
}));

// ─── Session Store ───────────────────────────────────────────────────────────

export interface SessionState {
  sessions: ChatSession[];
  activeSessionId: string | null;
  setSessions: (sessions: ChatSession[]) => void;
  setActiveSessionId: (id: string | null) => void;
  addSession: (session: ChatSession) => void;
  removeSession: (id: string) => void;
  updateSession: (id: string, updates: Partial<ChatSession>) => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  activeSessionId: null,
  setSessions: (sessions) => set({ sessions }),
  setActiveSessionId: (id) => set({ activeSessionId: id }),
  addSession: (session) =>
    set((state) => ({ sessions: [session, ...state.sessions] })),
  removeSession: (id) =>
    set((state) => ({
      sessions: state.sessions.filter((s) => s.id !== id),
      activeSessionId: state.activeSessionId === id ? null : state.activeSessionId,
    })),
  updateSession: (id, updates) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === id ? { ...s, ...updates } : s
      ),
    })),
}));

// ─── Message Store ───────────────────────────────────────────────────────────

export interface MessageState {
  messages: ChatMessage[];
  isStreaming: boolean;
  streamingContent: string;
  setMessages: (messages: ChatMessage[]) => void;
  addMessage: (message: ChatMessage) => void;
  appendStreamToken: (sessionId: string, seq: number, content: string) => void;
  finalizeStream: (messageId: string, content: string, elapsedMs?: number) => void;
  setStreamError: (messageId: string, reason: string) => void;
  clearMessages: () => void;
}

export const useMessageStore = create<MessageState>((set) => ({
  messages: [],
  isStreaming: false,
  streamingContent: "",
  setMessages: (messages) => set({ messages }),
  addMessage: (message) =>
    set((state) => {
      // Deduplicate: if a message with the same id already exists, skip.
      // Also deduplicate optimistic user messages: if a temp-* message exists
      // with the same role and content, replace it with the server-confirmed one.
      const existsById = state.messages.some((m) => m.id === message.id);
      if (existsById) return state;

      // Check if this is a server echo of an optimistic user message
      if (message.role === "user") {
        const optimisticIdx = state.messages.findIndex(
          (m) => m.id.startsWith("temp-") && m.role === "user" && m.content === message.content
        );
        if (optimisticIdx >= 0) {
          // Replace the optimistic message with the server-confirmed one (preserving position)
          const updated = [...state.messages];
          updated[optimisticIdx] = message;
          return { messages: updated };
        }
      }

      return { messages: [...state.messages, message] };
    }),
  appendStreamToken: (_sessionId, _seq, content) =>
    set((state) => ({
      isStreaming: true,
      streamingContent: state.streamingContent + content,
    })),
  finalizeStream: (messageId, content, elapsedMs) =>
    set((state) => ({
      isStreaming: false,
      streamingContent: "",
      messages: state.messages.map((m) =>
        m.id === messageId
          ? { ...m, content, status: "complete" as const, elapsed_ms: elapsedMs }
          : m
      ),
    })),
  setStreamError: (messageId, reason) =>
    set((state) => ({
      isStreaming: false,
      streamingContent: "",
      messages: state.messages.map((m) =>
        m.id === messageId
          ? { ...m, status: "error" as const, failure_reason: reason }
          : m
      ),
    })),
  clearMessages: () => set({ messages: [], isStreaming: false, streamingContent: "" }),
}));

// ─── Runtime Store ───────────────────────────────────────────────────────────

export interface RuntimeState {
  runtimes: Runtime[];
  setRuntimes: (runtimes: Runtime[]) => void;
  updateRuntimeStatus: (id: string, status: Runtime["status"]) => void;
}

export const useRuntimeStore = create<RuntimeState>((set) => ({
  runtimes: [],
  setRuntimes: (runtimes) => set({ runtimes }),
  updateRuntimeStatus: (id, status) =>
    set((state) => ({
      runtimes: state.runtimes.map((r) =>
        r.id === id ? { ...r, status } : r
      ),
    })),
}));

// ─── Connection Store ────────────────────────────────────────────────────────

export type ConnectionStatus = "connected" | "disconnected" | "reconnecting";

export interface ConnectionState {
  status: ConnectionStatus;
  tokenExpired: boolean;
  setStatus: (status: ConnectionStatus) => void;
  setTokenExpired: (expired: boolean) => void;
}

export const useConnectionStore = create<ConnectionState>((set) => ({
  status: "disconnected",
  tokenExpired: false,
  setStatus: (status) => set({ status }),
  setTokenExpired: (expired) => set({ tokenExpired: expired }),
}));

// ─── WebSocket Event Handlers ────────────────────────────────────────────────
// These functions are called by the WebSocket client (ws.ts) to reactively
// update stores when server events arrive.

export function handleChatStream(payload: {
  session_id: string;
  seq: number;
  content: string;
}) {
  useMessageStore.getState().appendStreamToken(
    payload.session_id,
    payload.seq,
    payload.content
  );
}

export function handleChatDone(payload: {
  session_id: string;
  message_id: string;
  content: string;
  elapsed_ms: number;
}) {
  const { finalizeStream, messages } = useMessageStore.getState();
  // The message_id from the daemon is actually the user message ID (from the task payload).
  // Generate a unique ID for the assistant message to avoid overwriting the user message.
  const assistantMsgId = `assistant-${payload.message_id}-${Date.now()}`;
  
  // Check if an assistant message with this content already exists (from chat:message broadcast)
  const exists = messages.some(
    (m) => m.role === "assistant" && m.content === payload.content && m.chat_session_id === payload.session_id
  );
  if (!exists) {
    useMessageStore.getState().addMessage({
      id: assistantMsgId,
      chat_session_id: payload.session_id,
      seq: 0,
      role: "assistant",
      content: payload.content,
      status: "complete",
      elapsed_ms: payload.elapsed_ms,
      created_at: new Date().toISOString(),
    });
  }
  finalizeStream(assistantMsgId, payload.content, payload.elapsed_ms);
}

export function handleChatError(payload: {
  session_id: string;
  message_id?: string;
  error: string;
  code: string;
}) {
  if (payload.message_id) {
    useMessageStore.getState().setStreamError(payload.message_id, payload.error);
  } else {
    // General session error — stop streaming state
    useMessageStore.setState({ isStreaming: false, streamingContent: "" });
  }
}

export function handleRuntimeStatus(payload: {
  runtime_id: string;
  status: Runtime["status"];
}) {
  useRuntimeStore.getState().updateRuntimeStatus(payload.runtime_id, payload.status);
}

export function handleSessionCreated(payload: ChatSession) {
  useSessionStore.getState().addSession(payload);
}

export function handleSessionDeleted(payload: { session_id: string }) {
  useSessionStore.getState().removeSession(payload.session_id);
}

export function handleSessionUpdated(payload: {
  session_id: string;
  title?: string;
  runtime_id?: string;
}) {
  const updates: Partial<ChatSession> = {};
  if (payload.title !== undefined) updates.title = payload.title;
  if (payload.runtime_id !== undefined) updates.runtime_id = payload.runtime_id;
  useSessionStore.getState().updateSession(payload.session_id, updates);
}
