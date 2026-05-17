import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import * as fc from "fast-check";
import {
  calculateBackoff,
  WsClient,
  getWsClient,
  resetWsClient,
  type WsMessage,
} from "./ws";

// WebSocket readyState constants (not available in Node test env)
const WS_CONNECTING = 0;
const WS_OPEN = 1;
const WS_CLOSING = 2;
const WS_CLOSED = 3;

// --- Unit tests ---

describe("calculateBackoff", () => {
  it("returns 1000ms for attempt 1", () => {
    expect(calculateBackoff(1)).toBe(1000);
  });

  it("returns 2000ms for attempt 2", () => {
    expect(calculateBackoff(2)).toBe(2000);
  });

  it("returns 4000ms for attempt 3", () => {
    expect(calculateBackoff(3)).toBe(4000);
  });

  it("caps at 60000ms", () => {
    expect(calculateBackoff(7)).toBe(60000); // 2^6 * 1000 = 64000 → capped at 60000
    expect(calculateBackoff(10)).toBe(60000);
    expect(calculateBackoff(100)).toBe(60000);
  });
});

describe("WsClient", () => {
  let mockWs: {
    readyState: number;
    close: ReturnType<typeof vi.fn>;
    send: ReturnType<typeof vi.fn>;
    onopen: (() => void) | null;
    onmessage: ((event: { data: string }) => void) | null;
    onclose: ((event: { code: number; reason: string }) => void) | null;
    onerror: (() => void) | null;
  };

  beforeEach(() => {
    mockWs = {
      readyState: WS_CONNECTING,
      close: vi.fn(),
      send: vi.fn(),
      onopen: null,
      onmessage: null,
      onclose: null,
      onerror: null,
    };

    const MockWebSocket = vi.fn(() => mockWs) as unknown as typeof WebSocket;
    // Define static constants on the mock constructor
    Object.defineProperties(MockWebSocket, {
      CONNECTING: { value: WS_CONNECTING },
      OPEN: { value: WS_OPEN },
      CLOSING: { value: WS_CLOSING },
      CLOSED: { value: WS_CLOSED },
    });
    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    resetWsClient();
  });

  it("connects with token in query param", () => {
    const client = new WsClient({ url: "ws://test.com", token: "my-token" });
    client.connect();

    expect(WebSocket).toHaveBeenCalledWith(
      "ws://test.com/ws/client?token=my-token"
    );
  });

  it("encodes special characters in token", () => {
    const client = new WsClient({
      url: "ws://test.com",
      token: "token with spaces&special=chars",
    });
    client.connect();

    expect(WebSocket).toHaveBeenCalledWith(
      `ws://test.com/ws/client?token=${encodeURIComponent("token with spaces&special=chars")}`
    );
  });

  it("calls onConnect when connection opens", () => {
    const onConnect = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onConnect });
    client.connect();

    mockWs.readyState = WS_OPEN;
    mockWs.onopen?.();

    expect(onConnect).toHaveBeenCalledOnce();
  });

  it("calls onDisconnect when connection closes", () => {
    const onDisconnect = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onDisconnect });
    client.connect();

    mockWs.onclose?.({ code: 1006, reason: "" });

    expect(onDisconnect).toHaveBeenCalledOnce();
  });

  it("dispatches parsed messages to onMessage handler", () => {
    const onMessage = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onMessage });
    client.connect();

    const msg: WsMessage = { type: "chat:message", payload: { text: "hi" } };
    mockWs.onmessage?.({ data: JSON.stringify(msg) });

    expect(onMessage).toHaveBeenCalledWith(msg);
  });

  it("responds to connection:ping with connection:pong", () => {
    const onMessage = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onMessage });
    client.connect();
    mockWs.readyState = WS_OPEN;

    const ping: WsMessage = { type: "connection:ping", payload: {} };
    mockWs.onmessage?.({ data: JSON.stringify(ping) });

    // Ping should NOT be dispatched to onMessage
    expect(onMessage).not.toHaveBeenCalled();

    // Should send pong
    expect(mockWs.send).toHaveBeenCalledWith(
      JSON.stringify({ type: "connection:pong", payload: {} })
    );
  });

  it("ignores malformed JSON messages", () => {
    const onMessage = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onMessage });
    client.connect();

    mockWs.onmessage?.({ data: "not json{{{" });

    expect(onMessage).not.toHaveBeenCalled();
  });

  it("ignores messages without a type field", () => {
    const onMessage = vi.fn();
    const client = new WsClient({ url: "ws://test.com", onMessage });
    client.connect();

    mockWs.onmessage?.({ data: JSON.stringify({ payload: "no type" }) });

    expect(onMessage).not.toHaveBeenCalled();
  });

  it("sends typed messages as JSON", () => {
    const client = new WsClient({ url: "ws://test.com" });
    client.connect();
    mockWs.readyState = WS_OPEN;

    client.send("chat:send", { content: "hello" });

    expect(mockWs.send).toHaveBeenCalledWith(
      JSON.stringify({ type: "chat:send", payload: { content: "hello" } })
    );
  });

  it("does not send when not connected", () => {
    const client = new WsClient({ url: "ws://test.com" });
    client.connect();
    mockWs.readyState = WS_CLOSED;

    client.send("chat:send", { content: "hello" });

    expect(mockWs.send).not.toHaveBeenCalled();
  });

  it("reports isConnected correctly", () => {
    const client = new WsClient({ url: "ws://test.com" });
    expect(client.isConnected).toBe(false);

    client.connect();
    mockWs.readyState = WS_OPEN;
    expect(client.isConnected).toBe(true);

    mockWs.readyState = WS_CLOSED;
    expect(client.isConnected).toBe(false);
  });

  it("does not reconnect after intentional disconnect", () => {
    const client = new WsClient({ url: "ws://test.com" });
    client.connect();
    client.disconnect();

    // Simulate close event
    mockWs.onclose?.({ code: 1000, reason: "" });

    vi.advanceTimersByTime(60000);
    // WebSocket constructor called only once (initial connect)
    expect(WebSocket).toHaveBeenCalledTimes(1);
  });

  it("reconnects with exponential backoff on unintentional close", () => {
    const client = new WsClient({ url: "ws://test.com", token: "t" });
    client.connect();

    // First unintentional close
    mockWs.onclose?.({ code: 1006, reason: "" });
    expect(WebSocket).toHaveBeenCalledTimes(1);

    // After 1s (attempt 1 backoff), should reconnect
    vi.advanceTimersByTime(1000);
    expect(WebSocket).toHaveBeenCalledTimes(2);
  });

  it("resets reconnect attempt counter on successful connection", () => {
    const client = new WsClient({ url: "ws://test.com", token: "t" });
    client.connect();

    // Simulate disconnect and reconnect cycle
    mockWs.readyState = WS_CLOSED;
    mockWs.onclose?.({ code: 1006, reason: "" });
    vi.advanceTimersByTime(1000); // attempt 1 → reconnect

    // Simulate successful open on the new connection
    mockWs.readyState = WS_OPEN;
    mockWs.onopen?.();

    // Disconnect again (readyState changes to CLOSED as in real WebSocket)
    mockWs.readyState = WS_CLOSED;
    mockWs.onclose?.({ code: 1006, reason: "" });

    // Should use attempt 1 delay again (1s), not attempt 2 (2s)
    vi.advanceTimersByTime(1000);
    expect(WebSocket).toHaveBeenCalledTimes(3);
  });

  it("calls onTokenExpired and does not reconnect on close code 4001", () => {
    const onTokenExpired = vi.fn();
    const onDisconnect = vi.fn();
    const client = new WsClient({
      url: "ws://test.com",
      token: "t",
      onDisconnect,
      onTokenExpired,
    });
    client.connect();

    // Simulate auth failure close
    mockWs.onclose?.({ code: 4001, reason: "token expired" });

    expect(onDisconnect).toHaveBeenCalledOnce();
    expect(onTokenExpired).toHaveBeenCalledOnce();

    // Should NOT attempt to reconnect
    vi.advanceTimersByTime(60000);
    expect(WebSocket).toHaveBeenCalledTimes(1);
  });

  it("calls onTokenExpired and does not reconnect on close code 4003", () => {
    const onTokenExpired = vi.fn();
    const client = new WsClient({
      url: "ws://test.com",
      token: "t",
      onTokenExpired,
    });
    client.connect();

    // Simulate auth failure close
    mockWs.onclose?.({ code: 4003, reason: "forbidden" });

    expect(onTokenExpired).toHaveBeenCalledOnce();

    // Should NOT attempt to reconnect
    vi.advanceTimersByTime(60000);
    expect(WebSocket).toHaveBeenCalledTimes(1);
  });

  it("setOnTokenExpired allows setting handler after construction", () => {
    const client = new WsClient({ url: "ws://test.com", token: "t" });
    const handler = vi.fn();
    client.setOnTokenExpired(handler);
    client.connect();

    mockWs.onclose?.({ code: 4001, reason: "expired" });

    expect(handler).toHaveBeenCalledOnce();
  });

  it("allows updating handlers after construction", () => {
    const client = new WsClient({ url: "ws://test.com" });
    const handler = vi.fn();
    client.setOnMessage(handler);
    client.connect();

    const msg: WsMessage = { type: "chat:done", payload: {} };
    mockWs.onmessage?.({ data: JSON.stringify(msg) });

    expect(handler).toHaveBeenCalledWith(msg);
  });

  it("allows updating token", () => {
    const client = new WsClient({ url: "ws://test.com", token: "old" });
    client.setToken("new-token");
    client.connect();

    expect(WebSocket).toHaveBeenCalledWith(
      "ws://test.com/ws/client?token=new-token"
    );
  });
});

describe("getWsClient singleton", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    resetWsClient();
  });

  it("returns the same instance on multiple calls", () => {
    const MockWebSocket = vi.fn(() => ({
      readyState: WS_CONNECTING,
      close: vi.fn(),
      send: vi.fn(),
      onopen: null,
      onmessage: null,
      onclose: null,
      onerror: null,
    })) as unknown as typeof WebSocket;
    Object.defineProperties(MockWebSocket, {
      CONNECTING: { value: WS_CONNECTING },
      OPEN: { value: WS_OPEN },
      CLOSING: { value: WS_CLOSING },
      CLOSED: { value: WS_CLOSED },
    });
    vi.stubGlobal("WebSocket", MockWebSocket);

    const a = getWsClient({ url: "ws://test.com" });
    const b = getWsClient();
    expect(a).toBe(b);
  });
});

// --- Property-based tests ---

/**
 * Property 4: Exponential Backoff Sequence
 * Validates: Requirements 2.6
 *
 * For any retry attempt number N (starting at 1), the reconnection delay
 * SHALL equal min(2^(N-1) seconds, 60 seconds). The sequence SHALL be
 * deterministic and monotonically non-decreasing.
 */
describe("Property 4: Exponential Backoff Sequence (frontend)", () => {
  it("delay equals min(2^(N-1) * 1000, 60000) for any attempt N >= 1", () => {
    fc.assert(
      fc.property(fc.integer({ min: 1, max: 1000 }), (attempt) => {
        const expected = Math.min(Math.pow(2, attempt - 1) * 1000, 60000);
        expect(calculateBackoff(attempt)).toBe(expected);
      }),
      { numRuns: 200 }
    );
  });

  it("sequence is monotonically non-decreasing", () => {
    fc.assert(
      fc.property(fc.integer({ min: 1, max: 999 }), (attempt) => {
        const current = calculateBackoff(attempt);
        const next = calculateBackoff(attempt + 1);
        expect(next).toBeGreaterThanOrEqual(current);
      }),
      { numRuns: 200 }
    );
  });

  it("never exceeds 60 seconds", () => {
    fc.assert(
      fc.property(fc.integer({ min: 1, max: 10000 }), (attempt) => {
        expect(calculateBackoff(attempt)).toBeLessThanOrEqual(60000);
      }),
      { numRuns: 200 }
    );
  });

  it("starts at 1 second for attempt 1", () => {
    expect(calculateBackoff(1)).toBe(1000);
  });
});
