# Design Document: AgentBridge Chat

## Overview

AgentBridge is a distributed system enabling users to interact with locally-running AI agent CLIs through a web-based chat interface. The architecture follows a hub-and-spoke model inspired by Multica: a central Go server acts as the relay hub, user-facing React/Next.js frontends connect via WebSocket for real-time chat, and per-machine Go daemons detect local agent CLIs and execute chat tasks.

The system comprises three primary components:
1. **Server** — Go backend (Chi router, gorilla/websocket, sqlc/PostgreSQL) managing authentication, session state, message persistence, and WebSocket relay between clients and daemons
2. **Daemon** — Go CLI background process running on each user's machine, responsible for agent detection, server registration, heartbeats, and agent CLI invocation
3. **Frontend** — React/Next.js application providing the chat UI with real-time message streaming

Communication between all components uses WebSocket connections with a typed JSON message protocol. The server maintains two separate WebSocket hubs: one for browser clients and one for daemons.

## Architecture

```mermaid
graph TB
    subgraph "User's Machine"
        FE[React/Next.js Frontend]
        D[Go Daemon]
        A1[Claude CLI]
        A2[Gemini CLI]
        A3[Kiro CLI]
    end

    subgraph "Cloud Infrastructure"
        S[Go Server]
        DB[(PostgreSQL)]
    end

    FE <-->|WebSocket: chat messages, events| S
    D <-->|WebSocket: register, heartbeat, tasks| S
    S <--> DB

    D --> A1
    D --> A2
    D --> A3
```

### High-Level Flow

1. User starts the daemon on their machine → daemon detects agent CLIs → registers with server via WebSocket
2. User opens the web app → authenticates → establishes client WebSocket connection
3. User creates a chat session → selects an available agent (from their daemon's runtimes) → sends a message
4. Server persists the message → relays to the user's daemon via daemon WebSocket
5. Daemon invokes the bound agent CLI → streams response tokens back to server
6. Server forwards tokens to the user's browser in real time → persists final response on completion

### Design Decisions

| Decision | Rationale |
|----------|-----------|
| WebSocket for daemon ↔ server (not HTTP polling) | Sub-100ms message relay; bidirectional streaming for token-by-token responses |
| Separate WebSocket hubs for clients vs daemons | Different auth mechanisms, message types, and lifecycle management |
| PostgreSQL for persistence | ACID guarantees for message ordering, mature tooling with sqlc |
| Chi router | Lightweight, idiomatic Go HTTP router; composable middleware |
| gorilla/websocket | Battle-tested WebSocket library for Go; used successfully in Multica |
| Session token auth (not OAuth initially) | Simpler for v1; email/password with JWT tokens |
| Message sequence numbers per session | Guarantees correct ordering even with concurrent writes |

## Components and Interfaces

### Server Component

```
server/
├── cmd/
│   └── agentbridge/        # Main entry point
├── internal/
│   ├── auth/               # Authentication middleware, JWT token management
│   ├── handler/            # HTTP route handlers (REST API)
│   ├── clientws/           # Client WebSocket hub (browser connections)
│   ├── daemonws/           # Daemon WebSocket hub (daemon connections)
│   ├── service/            # Business logic layer
│   ├── middleware/         # HTTP middleware (CORS, logging, rate limiting)
│   └── config/             # Server configuration
├── pkg/
│   ├── protocol/           # Shared WebSocket message types
│   └── db/                 # sqlc-generated database access layer
├── migrations/             # PostgreSQL migration files
├── go.mod
└── sqlc.yaml
```

#### Key Interfaces

```go
// ClientHub manages browser WebSocket connections
type ClientHub interface {
    HandleWebSocket(w http.ResponseWriter, r *http.Request, userID string)
    SendToUser(userID string, msg protocol.Message)
    BroadcastToUser(userID string, msg protocol.Message)
    ConnectionCount(userID string) int
}

// DaemonHub manages daemon WebSocket connections
type DaemonHub interface {
    HandleWebSocket(w http.ResponseWriter, r *http.Request, identity DaemonIdentity)
    SendToDaemon(daemonID string, msg protocol.Message) error
    IsOnline(daemonID string) bool
    SetHeartbeatHandler(fn HeartbeatHandler)
}

// ChatService encapsulates chat business logic
type ChatService interface {
    CreateSession(ctx context.Context, userID string) (*ChatSession, error)
    ListSessions(ctx context.Context, userID string, page, pageSize int) ([]ChatSession, int, error)
    GetSession(ctx context.Context, userID, sessionID string) (*ChatSession, error)
    DeleteSession(ctx context.Context, userID, sessionID string) error
    RenameSession(ctx context.Context, userID, sessionID, title string) error
    SendMessage(ctx context.Context, userID, sessionID, content string) (*ChatMessage, error)
    GetMessages(ctx context.Context, sessionID string) ([]ChatMessage, error)
}

// RuntimeService manages daemon registrations and agent runtimes
type RuntimeService interface {
    RegisterDaemon(ctx context.Context, reg DaemonRegistration) error
    DeregisterDaemon(ctx context.Context, daemonID string) error
    UpdateHeartbeat(ctx context.Context, daemonID string) error
    MarkOffline(ctx context.Context, daemonID string) error
    GetUserRuntimes(ctx context.Context, userID string) ([]Runtime, error)
    BindRuntime(ctx context.Context, sessionID, runtimeID string) error
}
```

### Daemon Component

```
daemon/
├── cmd/
│   └── agentbridge-daemon/ # CLI entry point (start, stop, status)
├── internal/
│   ├── agent/              # Agent detection and invocation
│   ├── connection/         # WebSocket connection management + reconnect
│   ├── heartbeat/          # Heartbeat ticker
│   ├── executor/           # Task execution (agent CLI invocation + streaming)
│   └── config/             # Daemon configuration
├── pkg/
│   └── protocol/           # Shared protocol types (symlinked or Go module)
└── go.mod
```

#### Key Interfaces

```go
// AgentDetector scans for available agent CLIs
type AgentDetector interface {
    Scan() []RuntimeInfo
    RescanInterval() time.Duration
}

// AgentExecutor invokes an agent CLI and streams output
type AgentExecutor interface {
    Execute(ctx context.Context, req ExecutionRequest) (<-chan StreamToken, error)
    Cancel(sessionID string) error
}

// ServerConnection manages the WebSocket link to the server
type ServerConnection interface {
    Connect(ctx context.Context) error
    Send(msg protocol.Message) error
    OnMessage(handler func(protocol.Message))
    Close() error
    IsConnected() bool
}
```

### Frontend Component

```
frontend/
├── app/                    # Next.js App Router
│   ├── layout.tsx
│   ├── page.tsx            # Landing/login
│   ├── chat/
│   │   ├── layout.tsx      # Chat shell (sidebar + main)
│   │   └── [sessionId]/
│   │       └── page.tsx    # Individual chat view
│   └── api/                # Next.js API routes (proxy to backend)
├── components/
│   ├── chat/
│   │   ├── ChatMessage.tsx
│   │   ├── ChatInput.tsx
│   │   ├── ChatStream.tsx  # Streaming response display
│   │   ├── SessionList.tsx
│   │   └── AgentSelector.tsx
│   └── ui/                 # Base UI components
├── lib/
│   ├── api.ts              # REST API client
│   ├── ws.ts               # WebSocket client with reconnection
│   └── store.ts            # Zustand stores for client state
├── hooks/
│   ├── useChat.ts          # Chat session hooks
│   ├── useWebSocket.ts     # WebSocket connection hook
│   └── useAgents.ts        # Agent/runtime hooks
└── package.json
```

### API Endpoints

#### Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/auth/register` | Create new user account |
| POST | `/api/auth/login` | Authenticate, returns JWT token |
| POST | `/api/auth/refresh` | Refresh expired token |
| GET | `/api/auth/me` | Get current user info |

#### Chat Sessions

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/sessions` | Create new chat session |
| GET | `/api/sessions` | List user's sessions (paginated) |
| GET | `/api/sessions/:id` | Get session details |
| DELETE | `/api/sessions/:id` | Delete session |
| PATCH | `/api/sessions/:id` | Rename session |
| GET | `/api/sessions/:id/messages` | Get message history |
| POST | `/api/sessions/:id/messages` | Send a message |

#### Runtimes & Binding

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/runtimes` | List user's available runtimes |
| POST | `/api/sessions/:id/bind` | Bind a runtime to a session |

#### WebSocket Endpoints

| Path | Description |
|------|-------------|
| `/ws/client` | Browser client WebSocket (auth via token query param) |
| `/ws/daemon` | Daemon WebSocket (auth via Authorization header) |

## Data Models

### Database Schema

```sql
-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Daemons table (registered daemon instances)
CREATE TABLE daemons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    daemon_id TEXT NOT NULL UNIQUE,  -- client-provided stable ID (hostname-based)
    status TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online', 'offline')),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_daemons_user ON daemons(user_id);
CREATE INDEX idx_daemons_status ON daemons(status);

-- Runtimes table (detected agent CLIs on a daemon)
CREATE TABLE runtimes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    daemon_id UUID NOT NULL REFERENCES daemons(id) ON DELETE CASCADE,
    agent_type TEXT NOT NULL,        -- e.g., "claude", "gemini", "kiro-cli"
    binary_path TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT 'unknown',
    status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'unavailable', 'offline')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runtimes_daemon ON runtimes(daemon_id);

-- Chat sessions
CREATE TABLE chat_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    runtime_id UUID REFERENCES runtimes(id) ON DELETE SET NULL,
    title TEXT NOT NULL DEFAULT 'New Chat',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_sessions_user ON chat_sessions(user_id, updated_at DESC);

-- Chat messages
CREATE TABLE chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,            -- monotonically increasing per session
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'complete' CHECK (status IN ('pending', 'streaming', 'complete', 'error')),
    elapsed_ms INTEGER,              -- response time for assistant messages
    failure_reason TEXT,             -- error details if status = 'error'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_chat_messages_session_seq ON chat_messages(chat_session_id, seq);
CREATE INDEX idx_chat_messages_session_created ON chat_messages(chat_session_id, created_at);

-- Message buffer for disconnected clients
CREATE TABLE message_buffer (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '5 minutes')
);

CREATE INDEX idx_message_buffer_user ON message_buffer(user_id, created_at);
CREATE INDEX idx_message_buffer_expires ON message_buffer(expires_at);
```

### Core Domain Types

```go
type ChatSession struct {
    ID        string    `json:"id"`
    UserID    string    `json:"user_id"`
    RuntimeID *string   `json:"runtime_id,omitempty"`
    Title     string    `json:"title"`
    Status    string    `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessage struct {
    ID            string    `json:"id"`
    ChatSessionID string    `json:"chat_session_id"`
    Seq           int       `json:"seq"`
    Role          string    `json:"role"`
    Content       string    `json:"content"`
    Status        string    `json:"status"`
    ElapsedMs     *int      `json:"elapsed_ms,omitempty"`
    FailureReason *string   `json:"failure_reason,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
}

type Runtime struct {
    ID         string    `json:"id"`
    DaemonID   string    `json:"daemon_id"`
    AgentType  string    `json:"agent_type"`
    BinaryPath string    `json:"binary_path"`
    Version    string    `json:"version"`
    Status     string    `json:"status"`
    CreatedAt  time.Time `json:"created_at"`
}

type Daemon struct {
    ID         string    `json:"id"`
    UserID     string    `json:"user_id"`
    DaemonID   string    `json:"daemon_id"`
    Status     string    `json:"status"`
    LastSeenAt time.Time `json:"last_seen_at"`
    CreatedAt  time.Time `json:"created_at"`
}
```

### WebSocket Protocol

All WebSocket messages use a typed envelope:

```go
type Message struct {
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"payload"`
}
```

#### Message Types

| Type | Direction | Description |
|------|-----------|-------------|
| `daemon:register` | Daemon → Server | Register daemon with runtime list |
| `daemon:register_ack` | Server → Daemon | Acknowledge registration |
| `daemon:register_error` | Server → Daemon | Reject registration with reason |
| `daemon:heartbeat` | Daemon → Server | Periodic liveness signal |
| `daemon:heartbeat_ack` | Server → Daemon | Heartbeat acknowledgment |
| `chat:send` | Client → Server | User sends a chat message |
| `chat:message` | Server → Client | New message (user or assistant) |
| `chat:stream` | Daemon → Server → Client | Streaming token from agent |
| `chat:done` | Daemon → Server → Client | Agent response complete |
| `chat:error` | Server → Client | Error during message processing |
| `chat:task` | Server → Daemon | Relay user message to daemon for execution |
| `chat:cancel` | Client → Server → Daemon | Cancel in-progress response |
| `session:created` | Server → Client | New session notification |
| `session:deleted` | Server → Client | Session deleted notification |
| `session:updated` | Server → Client | Session title/binding changed |
| `runtime:status` | Server → Client | Runtime online/offline change |
| `connection:ping` | Server → Client | Keep-alive ping |
| `connection:pong` | Client → Server | Keep-alive response |

#### Key Payload Structures

```go
// Daemon registration
type DaemonRegisterPayload struct {
    DaemonID string        `json:"daemon_id"`
    UserID   string        `json:"user_id"`
    Runtimes []RuntimeInfo `json:"runtimes"`
}

type RuntimeInfo struct {
    AgentType  string `json:"agent_type"`
    BinaryPath string `json:"binary_path"`
    Version    string `json:"version"`
    Status     string `json:"status"` // "available" or "unavailable"
}

// Chat task relay (server → daemon)
type ChatTaskPayload struct {
    SessionID string        `json:"session_id"`
    MessageID string        `json:"message_id"`
    Content   string        `json:"content"`
    History   []HistoryItem `json:"history"` // up to 200 recent messages
    RuntimeID string        `json:"runtime_id"`
}

type HistoryItem struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

// Streaming token (daemon → server → client)
type ChatStreamPayload struct {
    SessionID string `json:"session_id"`
    Seq       int    `json:"seq"`       // monotonically increasing token sequence
    Content   string `json:"content"`   // token text
}

// Chat done (daemon → server → client)
type ChatDonePayload struct {
    SessionID string `json:"session_id"`
    MessageID string `json:"message_id"`
    Content   string `json:"content"`   // full final response
    ElapsedMs int64  `json:"elapsed_ms"`
}

// Chat error
type ChatErrorPayload struct {
    SessionID string `json:"session_id"`
    MessageID string `json:"message_id,omitempty"`
    Error     string `json:"error"`
    Code      string `json:"code"` // "agent_timeout", "agent_error", "validation_error"
}
```

### Authentication Flow

```mermaid
sequenceDiagram
    participant U as User Browser
    participant S as Server
    participant D as Daemon

    Note over U,S: User Authentication
    U->>S: POST /api/auth/login {email, password}
    S->>U: {token, expires_at, user}
    U->>S: WS /ws/client?token=<jwt>
    S->>S: Validate JWT, register client connection

    Note over D,S: Daemon Authentication
    D->>S: POST /api/auth/login {email, password}
    S->>D: {token, expires_at, user}
    D->>S: WS /ws/daemon (Authorization: Bearer <token>)
    S->>S: Validate token, await daemon:register
    D->>S: daemon:register {daemon_id, user_id, runtimes}
    S->>D: daemon:register_ack
```

### Message Flow (Chat)

```mermaid
sequenceDiagram
    participant U as User Browser
    participant S as Server
    participant DB as PostgreSQL
    participant D as Daemon
    participant A as Agent CLI

    U->>S: chat:send {session_id, content}
    S->>DB: INSERT chat_message (role=user)
    S->>U: chat:message {message persisted}
    S->>D: chat:task {session_id, content, history}
    D->>A: Invoke CLI with message + context
    loop Streaming tokens
        A-->>D: stdout token
        D->>S: chat:stream {session_id, seq, content}
        S->>U: chat:stream {seq, content}
    end
    A-->>D: EOF (response complete)
    D->>S: chat:done {session_id, content, elapsed_ms}
    S->>DB: INSERT chat_message (role=assistant)
    S->>U: chat:done {message_id, content}
```


## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Agent Detection Correctness

*For any* system PATH configuration containing a subset of supported agent binaries (with optional environment variable overrides), the detection scan SHALL return a RuntimeInfo for each found binary with correct agent_type, binary_path, and status ("available" if version retrieval succeeded, "unavailable" otherwise), and SHALL NOT include entries for binaries not present on the system.

**Validates: Requirements 1.1, 1.3, 1.4**

### Property 2: Daemon Registration State Consistency

*For any* valid DaemonRegister payload containing a daemon_id, user_id, and list of N runtimes, after the server processes the registration, the stored daemon record SHALL have the correct user_id, and the stored runtime list SHALL contain exactly N entries matching the registration payload, replacing any previously registered runtimes for that daemon.

**Validates: Requirements 2.2**

### Property 3: Heartbeat Timeout Detection

*For any* heartbeat interval I and any time gap G since the last heartbeat, the server SHALL mark the daemon as offline if and only if G ≥ 3 × I. For gaps less than 3 × I, the daemon SHALL remain in "online" status.

**Validates: Requirements 2.5**

### Property 4: Exponential Backoff Sequence

*For any* retry attempt number N (starting at 1), the reconnection delay SHALL equal min(2^(N-1) seconds, 60 seconds). The sequence SHALL be deterministic and monotonically non-decreasing.

**Validates: Requirements 2.6**

### Property 5: Registration Validation

*For any* DaemonRegister payload with at least one missing required field (daemon_id, user_id, or runtimes) or a user_id that does not correspond to an existing user, the server SHALL reject the registration with a non-empty error message and SHALL NOT create or modify any daemon or runtime records.

**Validates: Requirements 2.9**

### Property 6: Session Creation Defaults

*For any* authenticated user, creating a new chat session SHALL produce a session with a unique ID (no collisions with existing sessions), title equal to "New Chat", status equal to "active", and a creation timestamp within 1 second of the current time.

**Validates: Requirements 4.1**

### Property 7: Session List Ordering

*For any* user with N chat sessions having various message timestamps, listing sessions SHALL return them ordered by most recent activity (last message timestamp, or creation timestamp if no messages) in descending order, with at most 50 sessions per page.

**Validates: Requirements 4.2**

### Property 8: Session Deletion Completeness

*For any* chat session with N messages, after deletion, querying for that session SHALL return not-found, and querying for any of its messages SHALL return an empty result set.

**Validates: Requirements 4.4**

### Property 9: Input Validation

*For any* string S intended as a chat message, the server SHALL accept it if and only if the trimmed length is between 1 and 32,000 characters (inclusive). *For any* string T intended as a session title, the server SHALL accept it if and only if the trimmed length is between 1 and 100 characters (inclusive). Invalid inputs SHALL be rejected without modifying any state.

**Validates: Requirements 4.5, 4.6, 6.8**

### Property 10: Runtime Filtering

*For any* set of runtimes across multiple users and daemons with mixed statuses (available, unavailable, offline), querying available runtimes for user U SHALL return only runtimes that belong to user U's daemon AND have status "available", and SHALL never include runtimes belonging to other users.

**Validates: Requirements 5.1**

### Property 11: Binding Replacement and Offline Rejection

*For any* chat session, after N sequential bind operations to valid online runtimes, the session SHALL have exactly one binding pointing to the most recently selected runtime, and all previously sent messages SHALL be preserved. *For any* runtime with status "offline", attempting to bind SHALL be rejected with an error.

**Validates: Requirements 5.2, 5.6, 5.7**

### Property 12: History Truncation

*For any* conversation with N messages (where N may exceed 200), the history passed to the agent CLI for execution SHALL contain exactly min(N, 200) messages, taken from the most recent messages in chronological order.

**Validates: Requirements 6.2**

### Property 13: Stream Sequence Monotonicity

*For any* sequence of stream tokens produced during a single agent response, each token's sequence number SHALL be strictly greater than the previous token's sequence number, starting from 1.

**Validates: Requirements 6.3**

### Property 14: Stream Concatenation Integrity

*For any* sequence of stream tokens produced during a single agent response, the ChatDone message's content field SHALL equal the concatenation of all token contents in sequence-number order.

**Validates: Requirements 6.5**

### Property 15: Message Queue FIFO Ordering

*For any* sequence of N messages sent by a user while a previous response is in progress, all N messages SHALL be queued and delivered to the daemon in the exact order they were received (FIFO), with delivery occurring only after the current response completes or fails.

**Validates: Requirements 6.9**

### Property 16: Multi-User Isolation

*For any* two distinct users A and B, user A SHALL NOT be able to: list user B's runtimes, bind to user B's runtimes, read user B's chat sessions, modify user B's chat sessions, or cause messages to be relayed to user B's daemon. All such cross-user operations SHALL be rejected with an authorization error that does not reveal whether the target resource exists.

**Validates: Requirements 7.1, 7.2, 7.3, 7.4, 7.6**

### Property 17: Message Persistence Round-Trip

*For any* valid chat message (user or assistant role) with content C, after persisting and then retrieving it, the returned message SHALL have identical content C, correct role, correct session_id, a valid timestamp, and a sequence number consistent with its position in the session.

**Validates: Requirements 6.1, 9.1**

### Property 18: Message Ordering Invariant

*For any* chat session with N messages persisted in any order, retrieving the message history SHALL return exactly N messages ordered by sequence number (1, 2, 3, ..., N) with no gaps or duplicates, and the sequence order SHALL correspond to chronological insertion order.

**Validates: Requirements 9.2, 9.4**

### Property 19: Persist-Before-Relay Invariant

*For any* user message, the server SHALL confirm successful persistence before relaying the message to the daemon. If persistence fails, the message SHALL NOT be relayed, and an error SHALL be returned to the client.

**Validates: Requirements 9.5**

### Property 20: Token Authentication Validity

*For any* JWT token, the server SHALL accept it for WebSocket authentication if and only if it is well-formed, not expired (within 24 hours of issuance), and corresponds to an existing user. All other tokens SHALL be rejected with an authentication error.

**Validates: Requirements 3.2, 10.2**

### Property 21: Buffered Message Delivery

*For any* set of N messages buffered during a client disconnection within a 5-minute window, upon reconnection the server SHALL deliver min(N, 100) messages in chronological order. Messages older than 5 minutes SHALL NOT be delivered.

**Validates: Requirements 10.5**

### Property 22: Malformed Message Rate Limiting

*For any* WebSocket connection, the server SHALL tolerate up to 10 malformed messages within any 60-second sliding window without closing the connection. Upon receiving the 11th malformed message within 60 seconds, the server SHALL close the connection.

**Validates: Requirements 10.7**

## Error Handling

### Server Error Handling

| Error Scenario | Response | Recovery |
|---------------|----------|----------|
| Invalid auth token | 401 Unauthorized / WS close with auth error | Client redirects to login |
| Resource not found | 404 Not Found | Client shows appropriate UI |
| Authorization failure (cross-user) | 403 Forbidden (no resource existence leak) | Client shows access denied |
| Message validation failure | 400 Bad Request with field-level errors | Client shows inline validation |
| Daemon offline during message send | WS `chat:error` with code "agent_unavailable" | Client disables input, shows reconnect prompt |
| Database persistence failure | 500 Internal Server Error / WS `chat:error` | Message not relayed; client can retry |
| WebSocket upgrade failure | HTTP 400/500 | Client retries with backoff |
| Rate limit exceeded | 429 Too Many Requests | Client backs off |

### Daemon Error Handling

| Error Scenario | Response | Recovery |
|---------------|----------|----------|
| Server connection lost | Log warning, begin exponential backoff reconnect | Auto-reconnect with re-registration |
| Agent CLI timeout (300s) | Send `chat:error` to server with "agent_timeout" | Session remains active for retry |
| Agent CLI crash/non-zero exit | Send `chat:error` with "agent_error" + stderr | Partial tokens preserved in UI |
| Agent CLI not found on rescan | Update RuntimeInfo status to "unavailable" | Server notifies bound sessions |
| PID file locked (duplicate start) | Print error message, exit with code 1 | User informed daemon already running |
| Invalid server response | Log error, continue operation | Daemon remains stable |

### Frontend Error Handling

| Error Scenario | Response | Recovery |
|---------------|----------|----------|
| WebSocket disconnection | Show "Reconnecting..." banner | Auto-reconnect with backoff; deliver buffered messages |
| Token expiry | Show re-auth modal | Re-authenticate without losing session state |
| Message send failure | Show error inline with retry button | User can retry or edit message |
| Agent unavailable | Disable input, show agent status | Auto-enable when runtime comes back online |
| Network timeout | Show timeout error | Retry with exponential backoff |

### Error Code Taxonomy

```go
const (
    ErrCodeValidation      = "validation_error"
    ErrCodeAuthentication  = "authentication_error"
    ErrCodeAuthorization   = "authorization_error"
    ErrCodeNotFound        = "not_found"
    ErrCodeAgentTimeout    = "agent_timeout"
    ErrCodeAgentError      = "agent_error"
    ErrCodeAgentUnavailable = "agent_unavailable"
    ErrCodePersistFailed   = "persist_failed"
    ErrCodeRateLimit       = "rate_limit"
    ErrCodeInternal        = "internal_error"
)
```

## Testing Strategy

### Dual Testing Approach

This feature uses both unit/example-based tests and property-based tests for comprehensive coverage.

**Property-Based Testing Library**: [rapid](https://github.com/flyingmutant/rapid) for Go (server + daemon), [fast-check](https://github.com/dubzzz/fast-check) for TypeScript (frontend logic).

### Property-Based Tests (Go — Server & Daemon)

Each property test runs a minimum of **100 iterations** with randomized inputs.

| Property | Test File | Tag |
|----------|-----------|-----|
| Property 1: Agent Detection | `daemon/internal/agent/detect_test.go` | Feature: agent-bridge-chat, Property 1: Agent detection correctness |
| Property 2: Registration State | `server/internal/service/runtime_test.go` | Feature: agent-bridge-chat, Property 2: Daemon registration state consistency |
| Property 3: Heartbeat Timeout | `server/internal/service/heartbeat_test.go` | Feature: agent-bridge-chat, Property 3: Heartbeat timeout detection |
| Property 4: Exponential Backoff | `daemon/internal/connection/backoff_test.go` | Feature: agent-bridge-chat, Property 4: Exponential backoff sequence |
| Property 5: Registration Validation | `server/internal/service/runtime_test.go` | Feature: agent-bridge-chat, Property 5: Registration validation |
| Property 6: Session Creation | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 6: Session creation defaults |
| Property 7: Session Ordering | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 7: Session list ordering |
| Property 8: Session Deletion | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 8: Session deletion completeness |
| Property 9: Input Validation | `server/internal/handler/validation_test.go` | Feature: agent-bridge-chat, Property 9: Input validation |
| Property 10: Runtime Filtering | `server/internal/service/runtime_test.go` | Feature: agent-bridge-chat, Property 10: Runtime filtering |
| Property 11: Binding Replacement | `server/internal/service/binding_test.go` | Feature: agent-bridge-chat, Property 11: Binding replacement and offline rejection |
| Property 12: History Truncation | `daemon/internal/executor/history_test.go` | Feature: agent-bridge-chat, Property 12: History truncation |
| Property 13: Stream Monotonicity | `daemon/internal/executor/stream_test.go` | Feature: agent-bridge-chat, Property 13: Stream sequence monotonicity |
| Property 14: Stream Concatenation | `daemon/internal/executor/stream_test.go` | Feature: agent-bridge-chat, Property 14: Stream concatenation integrity |
| Property 15: Message Queue FIFO | `server/internal/service/queue_test.go` | Feature: agent-bridge-chat, Property 15: Message queue FIFO ordering |
| Property 16: Multi-User Isolation | `server/internal/service/isolation_test.go` | Feature: agent-bridge-chat, Property 16: Multi-user isolation |
| Property 17: Message Round-Trip | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 17: Message persistence round-trip |
| Property 18: Message Ordering | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 18: Message ordering invariant |
| Property 19: Persist-Before-Relay | `server/internal/service/chat_test.go` | Feature: agent-bridge-chat, Property 19: Persist-before-relay invariant |
| Property 20: Token Validation | `server/internal/auth/token_test.go` | Feature: agent-bridge-chat, Property 20: Token authentication validity |
| Property 21: Buffered Delivery | `server/internal/clientws/buffer_test.go` | Feature: agent-bridge-chat, Property 21: Buffered message delivery |
| Property 22: Rate Limiting | `server/internal/clientws/ratelimit_test.go` | Feature: agent-bridge-chat, Property 22: Malformed message rate limiting |

### Unit/Example-Based Tests

| Area | Test Focus | Framework |
|------|-----------|-----------|
| Server HTTP handlers | Request/response format, status codes, error messages | Go `testing` + `httptest` |
| Server WebSocket | Connection lifecycle, ping/pong, auth handshake | Go `testing` + gorilla/websocket test helpers |
| Daemon CLI commands | start/stop/status output, PID file management | Go `testing` |
| Daemon agent invocation | CLI spawning, stdout capture, timeout handling | Go `testing` with mock binaries |
| Frontend components | Chat UI rendering, message display, input handling | Vitest + React Testing Library |
| Frontend WebSocket | Connection state management, reconnection | Vitest with mock WebSocket |
| API client | Request formatting, response parsing | Vitest |

### Integration Tests

| Scenario | Components | Approach |
|----------|-----------|----------|
| Full chat flow | Server + Daemon + mock agent | Docker Compose with test agent binary |
| Daemon registration + heartbeat | Server + Daemon | Real WebSocket connection, verify state |
| Multi-user isolation | Server + 2 clients | Concurrent requests verifying isolation |
| Reconnection + message buffer | Server + Client | Simulate disconnect, verify buffer delivery |
| Load: 5000 concurrent sessions | Server + load generator | k6 or custom Go load test |
| Load: 10000 WebSocket connections | Server | Custom connection stress test |

### Test Infrastructure

- **Go tests**: `go test ./...` with `-race` flag for concurrency issues
- **Frontend tests**: `vitest --run` for single execution
- **Integration tests**: Docker Compose environment with PostgreSQL
- **CI pipeline**: Run unit + property tests on every PR; integration tests on merge to main

## Container Compatibility (Docker & Podman)

The project uses `docker compose` commands throughout the Makefile and scripts. Both Docker and Podman are fully supported:

- **Docker**: Works out of the box with Docker Desktop or Docker Engine + Compose plugin.
- **Podman**: Users running Podman should set the `DOCKER_HOST` environment variable to point to Podman's socket. This allows all `docker compose` commands to transparently route through Podman.

```bash
# Podman compatibility — add to shell profile or .env
export DOCKER_HOST=unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')

# Or for rootless Podman on Linux:
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
```

The `docker-compose.yml` file uses only standard Compose v2 features, ensuring compatibility with both `docker compose` (Docker Compose plugin) and `podman-compose`. No Docker-specific extensions or build features are used in the development compose file.

### Design Decision

| Decision | Rationale |
|----------|-----------|
| Use `docker compose` as the canonical command | Docker Compose v2 is the most widely adopted; Podman supports it via socket compatibility |
| Document `DOCKER_HOST` for Podman users | Avoids maintaining two sets of scripts; single code path with env-based routing |
| Avoid Docker-specific Compose extensions | Ensures `podman-compose` can also parse the file without modification |

## Docker Compose for Local Development

The `docker-compose.yml` file at the project root provides all infrastructure services needed for local development.

### Services

```yaml
name: agentbridge

services:
  postgres:
    image: pgvector/pgvector:pg17
    environment:
      POSTGRES_DB: ${POSTGRES_DB:-agentbridge}
      POSTGRES_USER: ${POSTGRES_USER:-agentbridge}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-agentbridge}
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-agentbridge}"]
      interval: 5s
      timeout: 5s
      retries: 5

  mailhog:
    image: mailhog/mailhog:latest
    ports:
      - "${MAILHOG_SMTP_PORT:-1025}:1025"   # SMTP port
      - "${MAILHOG_UI_PORT:-8025}:8025"     # Web UI for viewing emails
    environment:
      MH_STORAGE: memory

volumes:
  pgdata:
```

### Service Details

| Service | Image | Purpose | Ports |
|---------|-------|---------|-------|
| `postgres` | `pgvector/pgvector:pg17` | Primary database with pgvector extension for future embedding support | 5432 (configurable) |
| `mailhog` | `mailhog/mailhog:latest` | Mock SMTP server for email verification flows | 1025 (SMTP), 8025 (Web UI) |

### Notes

- **pgvector/pgvector:pg17** is used instead of plain `postgres:17` to include the pgvector extension, which may be needed for future agent embedding/search features. It is a drop-in replacement for standard PostgreSQL.
- **MailHog** captures all outbound emails in a web UI (http://localhost:8025) without actually sending them. This is used for any email verification flows (password reset, email confirmation) during local development.
- All port mappings are configurable via environment variables to support running multiple instances (e.g., worktrees).

## Mock Services

For local development, all external services are mocked via the Docker Compose stack:

| External Service | Mock Implementation | Configuration |
|-----------------|-------------------|---------------|
| SMTP / Email delivery | MailHog (captures emails in web UI) | `SMTP_HOST=localhost`, `SMTP_PORT=1025` |

### Design Rationale

- AgentBridge uses email/password authentication. While v1 does not require email verification, MailHog is included for future email verification needs (password reset, email confirmation).
- All emails sent by the server in local development are captured by MailHog and viewable at http://localhost:8025.
- No real email provider (Resend, SendGrid, etc.) is needed for local development.
- The server code uses a standard SMTP interface, making it trivial to swap MailHog for a real provider in production by changing environment variables.

## Local Development Setup

### One-Command Bootstrap

```bash
make dev
```

This single command performs the full local development setup:

1. Creates `.env` from `.env.example` if it doesn't exist (generates a random `JWT_SECRET`)
2. Starts Docker Compose services (PostgreSQL + MailHog)
3. Waits for PostgreSQL to be ready
4. Runs database migrations
5. Starts the Go server and Next.js frontend concurrently

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make dev` | Full bootstrap: env setup → compose up → migrate → start all |
| `make start` | Start server + frontend (assumes services are running) |
| `make stop` | Stop server + frontend processes |
| `make check` | Run full verification pipeline (typecheck, tests, lint) |
| `make db-up` | Start only the Docker Compose services |
| `make db-down` | Stop Docker Compose services |
| `make db-reset` | Drop and recreate the database, re-run migrations |
| `make migrate-up` | Apply pending database migrations |
| `make migrate-down` | Roll back the last migration |
| `make test` | Run Go tests (ensures DB is ready first) |

### Podman Compatibility

For users running Podman instead of Docker:

```bash
# Option 1: Set DOCKER_HOST in your shell profile
export DOCKER_HOST=unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')

# Option 2: Add to .env file
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```

All Makefile targets and scripts use `docker compose` commands which transparently route through Podman when `DOCKER_HOST` is set.

### scripts/ensure-postgres.sh

This script (modeled after Multica's pattern) ensures PostgreSQL is available before running migrations or starting the server:

1. Parses `DATABASE_URL` from the env file to determine if the database is local or remote
2. **Local database**: Starts the Docker Compose `postgres` service, waits for readiness, creates the database if it doesn't exist
3. **Remote database**: Skips Docker, verifies connectivity via `pg_isready`
4. Works with both Docker and Podman (uses `docker compose` which respects `DOCKER_HOST`)

```bash
#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"
# ... loads env vars, parses DATABASE_URL ...

if is_local; then
  echo "==> Ensuring PostgreSQL container is running..."
  docker compose up -d postgres
  # Wait for readiness, create DB if needed
else
  echo "==> Remote database detected. Skipping Docker."
  # Verify connectivity only
fi
```

### Environment Variables (.env.example)

```bash
# Database
POSTGRES_DB=agentbridge
POSTGRES_USER=agentbridge
POSTGRES_PASSWORD=agentbridge
POSTGRES_PORT=5432
DATABASE_URL=postgres://agentbridge:agentbridge@localhost:5432/agentbridge?sslmode=disable

# Server
PORT=8080
JWT_SECRET=change-me-to-a-random-string
CORS_ORIGINS=http://localhost:3000

# Frontend
FRONTEND_PORT=3000
NEXT_PUBLIC_API_URL=http://localhost:8080
NEXT_PUBLIC_WS_URL=ws://localhost:8080/ws

# Email (MailHog for local dev)
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_FROM=noreply@agentbridge.local

# MailHog UI
MAILHOG_SMTP_PORT=1025
MAILHOG_UI_PORT=8025

# Container runtime (uncomment for Podman)
# DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
```
