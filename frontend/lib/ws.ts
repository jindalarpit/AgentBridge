/**
 * WebSocket client with exponential backoff reconnection.
 *
 * Connects to /ws/client with token query param, parses typed message
 * envelopes, dispatches to handlers, and responds to connection:ping
 * with connection:pong.
 *
 * Requirements: 3.3, 10.3, 10.5
 */

export const WS_URL = process.env.NEXT_PUBLIC_WS_URL ?? "ws://localhost:8080";

// --- Message types ---

export type MessageType =
  | "chat:message"
  | "chat:stream"
  | "chat:done"
  | "chat:error"
  | "chat:send"
  | "session:created"
  | "session:deleted"
  | "session:updated"
  | "runtime:status"
  | "connection:ping"
  | "connection:pong";

export interface WsMessage<T = unknown> {
  type: MessageType;
  payload: T;
}

// --- Callback types ---

export type MessageHandler = (msg: WsMessage) => void;
export type ConnectHandler = () => void;
export type DisconnectHandler = () => void;
export type TokenExpiredHandler = () => void;

// --- Backoff calculation ---

/**
 * Calculate exponential backoff delay.
 * delay = min(2^(attempt-1) * 1000, 60000) ms
 */
export function calculateBackoff(attempt: number): number {
  return Math.min(Math.pow(2, attempt - 1) * 1000, 60000);
}

// --- WebSocket Client ---

export interface WsClientOptions {
  /** Base WebSocket URL (defaults to WS_URL env var) */
  url?: string;
  /** Auth token for connection */
  token?: string;
  /** Called when a message is received */
  onMessage?: MessageHandler;
  /** Called when connection is established */
  onConnect?: ConnectHandler;
  /** Called when connection is lost */
  onDisconnect?: DisconnectHandler;
  /** Called when connection is closed due to token expiry */
  onTokenExpired?: TokenExpiredHandler;
}

// WebSocket readyState constants
const WS_OPEN = 1;

export class WsClient {
  private ws: WebSocket | null = null;
  private url: string;
  private token: string;
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private intentionalClose = false;

  private onMessage: MessageHandler | null = null;
  private onConnect: ConnectHandler | null = null;
  private onDisconnect: DisconnectHandler | null = null;
  private onTokenExpired: TokenExpiredHandler | null = null;

  constructor(options: WsClientOptions = {}) {
    this.url = options.url ?? WS_URL;
    this.token = options.token ?? "";
    this.onMessage = options.onMessage ?? null;
    this.onConnect = options.onConnect ?? null;
    this.onDisconnect = options.onDisconnect ?? null;
    this.onTokenExpired = options.onTokenExpired ?? null;
  }

  /** Update the auth token (e.g. after refresh) */
  setToken(token: string): void {
    this.token = token;
  }

  /** Set the message handler */
  setOnMessage(handler: MessageHandler): void {
    this.onMessage = handler;
  }

  /** Set the connect handler */
  setOnConnect(handler: ConnectHandler): void {
    this.onConnect = handler;
  }

  /** Set the disconnect handler */
  setOnDisconnect(handler: DisconnectHandler): void {
    this.onDisconnect = handler;
  }

  /** Set the token expired handler */
  setOnTokenExpired(handler: TokenExpiredHandler): void {
    this.onTokenExpired = handler;
  }

  /** Whether the WebSocket is currently open */
  get isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WS_OPEN;
  }

  /** Connect to the WebSocket server */
  connect(): void {
    if (this.ws && this.ws.readyState === WS_OPEN) {
      return; // Already connected
    }

    this.intentionalClose = false;
    const wsUrl = `${this.url}/ws/client?token=${encodeURIComponent(this.token)}`;

    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      this.reconnectAttempt = 0;
      this.onConnect?.();
    };

    this.ws.onmessage = (event: MessageEvent) => {
      this.handleRawMessage(event.data);
    };

    this.ws.onclose = (event: CloseEvent) => {
      this.onDisconnect?.();
      // Close code 4001 or 4003 indicates authentication failure / token expiry
      if (event.code === 4001 || event.code === 4003) {
        this.onTokenExpired?.();
        // Don't auto-reconnect on auth failure — user must re-authenticate
        return;
      }
      if (!this.intentionalClose) {
        this.scheduleReconnect();
      }
    };

    this.ws.onerror = () => {
      // The close event will fire after error, triggering reconnect
    };
  }

  /** Disconnect from the WebSocket server */
  disconnect(): void {
    this.intentionalClose = true;
    this.clearReconnectTimer();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  /** Send a typed message to the server */
  send<T = unknown>(type: MessageType, payload: T): void {
    if (!this.ws || this.ws.readyState !== WS_OPEN) {
      return;
    }
    const message: WsMessage<T> = { type, payload };
    this.ws.send(JSON.stringify(message));
  }

  // --- Private methods ---

  private handleRawMessage(data: unknown): void {
    if (typeof data !== "string") {
      return;
    }

    let msg: WsMessage;
    try {
      msg = JSON.parse(data) as WsMessage;
    } catch {
      // Malformed message, ignore
      return;
    }

    if (!msg.type) {
      return;
    }

    // Respond to server ping with pong
    if (msg.type === "connection:ping") {
      this.send("connection:pong", {});
      return;
    }

    // Dispatch to handler
    this.onMessage?.(msg);
  }

  private scheduleReconnect(): void {
    this.clearReconnectTimer();
    this.reconnectAttempt++;
    const delay = calculateBackoff(this.reconnectAttempt);

    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, delay);
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}

// --- Singleton instance ---

let defaultClient: WsClient | null = null;

/** Get or create the default WsClient singleton */
export function getWsClient(options?: WsClientOptions): WsClient {
  if (!defaultClient) {
    defaultClient = new WsClient(options);
  }
  return defaultClient;
}

/** Reset the singleton (useful for testing) */
export function resetWsClient(): void {
  if (defaultClient) {
    defaultClient.disconnect();
    defaultClient = null;
  }
}
