// REST API client for AgentBridge backend

export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// ─── Token Storage ───────────────────────────────────────────────────────────

const TOKEN_KEY = "agentbridge_token";

export function getToken(): string | null {
  if (typeof localStorage === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  if (typeof localStorage === "undefined") return;
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  if (typeof localStorage === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
}

// ─── Error Handling ──────────────────────────────────────────────────────────

export interface ApiErrorBody {
  error: string;
  code?: string;
  fields?: Record<string, string>;
}

export class ApiError extends Error {
  status: number;
  code: string;
  fields?: Record<string, string>;

  constructor(status: number, body: ApiErrorBody) {
    super(body.error);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code ?? "unknown_error";
    this.fields = body.fields;
  }
}

// ─── Types ───────────────────────────────────────────────────────────────────

// Auth types
export interface RegisterRequest {
  email: string;
  password: string;
  display_name?: string;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface AuthResponse {
  token: string;
  expires_at: string;
  user: User;
}

export interface User {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
  updated_at: string;
}

// Session types
export interface ChatSession {
  id: string;
  user_id: string;
  runtime_id?: string;
  title: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSessionRequest {
  title?: string;
}

export interface UpdateSessionRequest {
  title: string;
}

export interface PaginatedSessions {
  sessions: ChatSession[];
  total: number;
  page: number;
  page_size: number;
}

// Message types
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

export interface SendMessageRequest {
  content: string;
}

// Runtime types
export interface Runtime {
  id: string;
  daemon_id: string;
  agent_type: string;
  binary_path: string;
  version: string;
  status: string;
  created_at: string;
}

export interface BindRuntimeRequest {
  runtime_id: string;
}

export interface BindRuntimeResponse {
  session_id: string;
  runtime_id: string;
  agent_type: string;
  version: string;
}

// ─── HTTP Client ─────────────────────────────────────────────────────────────

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const url = `${API_BASE_URL}${path}`;
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(options.headers as Record<string, string>),
  };

  const token = getToken();
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const response = await fetch(url, {
    ...options,
    headers,
  });

  if (!response.ok) {
    let body: ApiErrorBody;
    try {
      body = await response.json();
    } catch {
      body = { error: response.statusText || "Request failed" };
    }
    throw new ApiError(response.status, body);
  }

  // Handle 204 No Content
  if (response.status === 204) {
    return undefined as unknown as T;
  }

  return response.json();
}

// ─── Auth API ────────────────────────────────────────────────────────────────

export async function register(data: RegisterRequest): Promise<AuthResponse> {
  const res = await request<AuthResponse>("/api/auth/register", {
    method: "POST",
    body: JSON.stringify(data),
  });
  setToken(res.token);
  return res;
}

export async function login(data: LoginRequest): Promise<AuthResponse> {
  const res = await request<AuthResponse>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify(data),
  });
  setToken(res.token);
  return res;
}

export async function refreshToken(): Promise<AuthResponse> {
  const res = await request<AuthResponse>("/api/auth/refresh", {
    method: "POST",
  });
  setToken(res.token);
  return res;
}

export async function getMe(): Promise<User> {
  return request<User>("/api/auth/me");
}

export function logout(): void {
  clearToken();
}

// ─── Sessions API ────────────────────────────────────────────────────────────

export async function createSession(
  data?: CreateSessionRequest
): Promise<ChatSession> {
  return request<ChatSession>("/api/sessions", {
    method: "POST",
    body: JSON.stringify(data ?? {}),
  });
}

export async function listSessions(
  page = 1,
  pageSize = 50
): Promise<PaginatedSessions> {
  return request<PaginatedSessions>(
    `/api/sessions?page=${page}&page_size=${pageSize}`
  );
}

export async function getSession(sessionId: string): Promise<ChatSession> {
  return request<ChatSession>(`/api/sessions/${sessionId}`);
}

export async function deleteSession(sessionId: string): Promise<void> {
  return request<void>(`/api/sessions/${sessionId}`, {
    method: "DELETE",
  });
}

export async function updateSession(
  sessionId: string,
  data: UpdateSessionRequest
): Promise<ChatSession> {
  return request<ChatSession>(`/api/sessions/${sessionId}`, {
    method: "PATCH",
    body: JSON.stringify(data),
  });
}

// ─── Messages API ────────────────────────────────────────────────────────────

export async function getMessages(sessionId: string): Promise<ChatMessage[]> {
  return request<ChatMessage[]>(`/api/sessions/${sessionId}/messages`);
}

export async function sendMessage(
  sessionId: string,
  data: SendMessageRequest
): Promise<ChatMessage> {
  return request<ChatMessage>(`/api/sessions/${sessionId}/messages`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

// ─── Runtimes API ────────────────────────────────────────────────────────────

export async function listRuntimes(): Promise<Runtime[]> {
  return request<Runtime[]>("/api/runtimes");
}

export async function bindRuntime(
  sessionId: string,
  data: BindRuntimeRequest
): Promise<BindRuntimeResponse> {
  return request<BindRuntimeResponse>(`/api/sessions/${sessionId}/bind`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}
