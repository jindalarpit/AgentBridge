import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  ApiError,
  API_BASE_URL,
  getToken,
  setToken,
  clearToken,
  register,
  login,
  refreshToken,
  getMe,
  logout,
  createSession,
  listSessions,
  getSession,
  deleteSession,
  updateSession,
  getMessages,
  sendMessage,
  listRuntimes,
  bindRuntime,
} from "./api";

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => {
      store[key] = value;
    },
    removeItem: (key: string) => {
      delete store[key];
    },
    clear: () => {
      store = {};
    },
  };
})();

Object.defineProperty(global, "localStorage", { value: localStorageMock });

// Mock fetch
const mockFetch = vi.fn();
global.fetch = mockFetch;

function jsonResponse(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Error",
    json: () => Promise.resolve(data),
  };
}

function noContentResponse() {
  return {
    ok: true,
    status: 204,
    statusText: "No Content",
    json: () => Promise.reject(new Error("No content")),
  };
}

describe("Token Storage", () => {
  beforeEach(() => localStorageMock.clear());

  it("stores and retrieves a token", () => {
    setToken("test-token-123");
    expect(getToken()).toBe("test-token-123");
  });

  it("returns null when no token is stored", () => {
    expect(getToken()).toBeNull();
  });

  it("clears the token", () => {
    setToken("test-token-123");
    clearToken();
    expect(getToken()).toBeNull();
  });
});

describe("ApiError", () => {
  it("creates an error with status and code", () => {
    const err = new ApiError(401, {
      error: "Unauthorized",
      code: "authentication_error",
    });
    expect(err.message).toBe("Unauthorized");
    expect(err.status).toBe(401);
    expect(err.code).toBe("authentication_error");
    expect(err.name).toBe("ApiError");
  });

  it("defaults code to unknown_error when not provided", () => {
    const err = new ApiError(500, { error: "Something went wrong" });
    expect(err.code).toBe("unknown_error");
  });

  it("includes field-level errors", () => {
    const err = new ApiError(400, {
      error: "Validation failed",
      code: "validation_error",
      fields: { email: "Invalid email format" },
    });
    expect(err.fields).toEqual({ email: "Invalid email format" });
  });
});

describe("Auth API", () => {
  beforeEach(() => {
    localStorageMock.clear();
    mockFetch.mockReset();
  });

  it("register stores token and returns auth response", async () => {
    const authRes = {
      token: "jwt-token",
      expires_at: "2025-01-01T00:00:00Z",
      user: {
        id: "user-1",
        email: "test@example.com",
        display_name: "Test",
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(authRes));

    const result = await register({
      email: "test@example.com",
      password: "password123",
    });

    expect(result).toEqual(authRes);
    expect(getToken()).toBe("jwt-token");
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/auth/register`,
      expect.objectContaining({ method: "POST" })
    );
  });

  it("login stores token and returns auth response", async () => {
    const authRes = {
      token: "jwt-token-login",
      expires_at: "2025-01-01T00:00:00Z",
      user: {
        id: "user-1",
        email: "test@example.com",
        display_name: "Test",
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(authRes));

    const result = await login({
      email: "test@example.com",
      password: "password123",
    });

    expect(result).toEqual(authRes);
    expect(getToken()).toBe("jwt-token-login");
  });

  it("refreshToken updates stored token", async () => {
    setToken("old-token");
    const authRes = {
      token: "new-token",
      expires_at: "2025-02-01T00:00:00Z",
      user: {
        id: "user-1",
        email: "test@example.com",
        display_name: "Test",
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(authRes));

    const result = await refreshToken();

    expect(result.token).toBe("new-token");
    expect(getToken()).toBe("new-token");
  });

  it("getMe returns user info", async () => {
    setToken("valid-token");
    const user = {
      id: "user-1",
      email: "test@example.com",
      display_name: "Test",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(user));

    const result = await getMe();

    expect(result).toEqual(user);
  });

  it("logout clears the token", () => {
    setToken("some-token");
    logout();
    expect(getToken()).toBeNull();
  });

  it("attaches Authorization header when token exists", async () => {
    setToken("my-token");
    mockFetch.mockResolvedValueOnce(
      jsonResponse({
        id: "user-1",
        email: "t@t.com",
        display_name: "",
        created_at: "",
        updated_at: "",
      })
    );

    await getMe();

    const callArgs = mockFetch.mock.calls[0];
    expect(callArgs[1].headers["Authorization"]).toBe("Bearer my-token");
  });

  it("does not attach Authorization header when no token", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ error: "Unauthorized" }, 401)
    );

    await expect(getMe()).rejects.toThrow(ApiError);

    const callArgs = mockFetch.mock.calls[0];
    expect(callArgs[1].headers["Authorization"]).toBeUndefined();
  });
});

describe("Sessions API", () => {
  beforeEach(() => {
    localStorageMock.clear();
    setToken("test-token");
    mockFetch.mockReset();
  });

  it("createSession sends POST and returns session", async () => {
    const session = {
      id: "sess-1",
      user_id: "user-1",
      title: "New Chat",
      status: "active",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(session));

    const result = await createSession();

    expect(result).toEqual(session);
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions`,
      expect.objectContaining({ method: "POST" })
    );
  });

  it("listSessions sends GET with pagination params", async () => {
    const paginated = {
      sessions: [],
      total: 0,
      page: 1,
      page_size: 50,
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(paginated));

    const result = await listSessions(2, 25);

    expect(result).toEqual(paginated);
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions?page=2&page_size=25`,
      expect.anything()
    );
  });

  it("getSession fetches a single session", async () => {
    const session = {
      id: "sess-1",
      user_id: "user-1",
      title: "My Chat",
      status: "active",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(session));

    const result = await getSession("sess-1");

    expect(result).toEqual(session);
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions/sess-1`,
      expect.anything()
    );
  });

  it("deleteSession sends DELETE", async () => {
    mockFetch.mockResolvedValueOnce(noContentResponse());

    await deleteSession("sess-1");

    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions/sess-1`,
      expect.objectContaining({ method: "DELETE" })
    );
  });

  it("updateSession sends PATCH with title", async () => {
    const session = {
      id: "sess-1",
      user_id: "user-1",
      title: "Renamed",
      status: "active",
      created_at: "2024-01-01T00:00:00Z",
      updated_at: "2024-01-01T00:00:00Z",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(session));

    const result = await updateSession("sess-1", { title: "Renamed" });

    expect(result.title).toBe("Renamed");
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions/sess-1`,
      expect.objectContaining({ method: "PATCH" })
    );
  });
});

describe("Messages API", () => {
  beforeEach(() => {
    localStorageMock.clear();
    setToken("test-token");
    mockFetch.mockReset();
  });

  it("getMessages returns message list", async () => {
    const messages = [
      {
        id: "msg-1",
        chat_session_id: "sess-1",
        seq: 1,
        role: "user",
        content: "Hello",
        status: "complete",
        created_at: "2024-01-01T00:00:00Z",
      },
    ];
    mockFetch.mockResolvedValueOnce(jsonResponse(messages));

    const result = await getMessages("sess-1");

    expect(result).toEqual(messages);
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/sessions/sess-1/messages`,
      expect.anything()
    );
  });

  it("sendMessage sends POST with content", async () => {
    const message = {
      id: "msg-2",
      chat_session_id: "sess-1",
      seq: 2,
      role: "user",
      content: "How are you?",
      status: "pending",
      created_at: "2024-01-01T00:00:01Z",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(message));

    const result = await sendMessage("sess-1", { content: "How are you?" });

    expect(result).toEqual(message);
    const callArgs = mockFetch.mock.calls[0];
    expect(callArgs[0]).toBe(`${API_BASE_URL}/api/sessions/sess-1/messages`);
    expect(callArgs[1].method).toBe("POST");
    expect(JSON.parse(callArgs[1].body)).toEqual({
      content: "How are you?",
    });
  });
});

describe("Runtimes API", () => {
  beforeEach(() => {
    localStorageMock.clear();
    setToken("test-token");
    mockFetch.mockReset();
  });

  it("listRuntimes returns runtime list", async () => {
    const runtimes = [
      {
        id: "rt-1",
        daemon_id: "daemon-1",
        agent_type: "claude",
        binary_path: "/usr/local/bin/claude",
        version: "1.0.0",
        status: "available",
        created_at: "2024-01-01T00:00:00Z",
      },
    ];
    mockFetch.mockResolvedValueOnce(jsonResponse(runtimes));

    const result = await listRuntimes();

    expect(result).toEqual(runtimes);
    expect(mockFetch).toHaveBeenCalledWith(
      `${API_BASE_URL}/api/runtimes`,
      expect.anything()
    );
  });

  it("bindRuntime sends POST with runtime_id", async () => {
    const bindRes = {
      session_id: "sess-1",
      runtime_id: "rt-1",
      agent_type: "claude",
      version: "1.0.0",
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(bindRes));

    const result = await bindRuntime("sess-1", { runtime_id: "rt-1" });

    expect(result).toEqual(bindRes);
    const callArgs = mockFetch.mock.calls[0];
    expect(callArgs[0]).toBe(`${API_BASE_URL}/api/sessions/sess-1/bind`);
    expect(callArgs[1].method).toBe("POST");
    expect(JSON.parse(callArgs[1].body)).toEqual({ runtime_id: "rt-1" });
  });
});

describe("Error Handling", () => {
  beforeEach(() => {
    localStorageMock.clear();
    mockFetch.mockReset();
  });

  it("throws ApiError on non-OK response with JSON body", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse(
        { error: "Invalid credentials", code: "authentication_error" },
        401
      )
    );

    await expect(
      login({ email: "bad@test.com", password: "wrong" })
    ).rejects.toMatchObject({
      status: 401,
      code: "authentication_error",
      message: "Invalid credentials",
    });
  });

  it("throws ApiError with statusText when response body is not JSON", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
      json: () => Promise.reject(new Error("not json")),
    });

    await expect(getMe()).rejects.toMatchObject({
      status: 500,
      message: "Internal Server Error",
    });
  });

  it("throws ApiError with field-level validation errors", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse(
        {
          error: "Validation failed",
          code: "validation_error",
          fields: { title: "Title must be between 1 and 100 characters" },
        },
        400
      )
    );

    try {
      await updateSession("sess-1", { title: "" });
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      const apiErr = err as ApiError;
      expect(apiErr.status).toBe(400);
      expect(apiErr.fields?.title).toBe(
        "Title must be between 1 and 100 characters"
      );
    }
  });
});
