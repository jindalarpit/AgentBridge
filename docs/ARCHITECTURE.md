# AgentBridge — Architecture Document

## Overview

AgentBridge is a distributed system enabling users to interact with locally-running AI agent CLIs through a web-based chat interface. The architecture follows a hub-and-spoke model: a central Go server acts as the relay hub, user-facing React/Next.js frontends connect via WebSocket for real-time chat, and per-machine Go daemons detect local agent CLIs and execute chat tasks.

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        User's Machine                                │
│                                                                      │
│  ┌──────────────────┐         ┌──────────────────────────────────┐  │
│  │  Browser (React)  │         │         Go Daemon                │  │
│  │  Next.js App      │         │                                  │  │
│  │  ─────────────    │         │  ┌─────────┐  ┌──────────────┐  │  │
│  │  • Chat UI        │         │  │ Agent   │  │  Executor    │  │  │
│  │  • Session Mgmt   │         │  │ Detect  │  │  (CLI spawn) │  │  │
│  │  • Agent Selector │         │  └─────────┘  └──────────────┘  │  │
│  │  • Zustand State  │         │  ┌─────────┐  ┌──────────────┐  │  │
│  └────────┬─────────┘         │  │ Heartbt │  │  Connection  │  │  │
│           │                    │  │ Ticker  │  │  (Reconnect) │  │  │
│           │                    │  └─────────┘  └──────┬───────┘  │  │
│           │                    └──────────────────────┼──────────┘  │
│           │                                           │              │
└───────────┼───────────────────────────────────────────┼──────────────┘
            │ WebSocket                                  │ WebSocket
            │ /ws/client?token=<jwt>                     │ /ws/daemon
            │                                           │ (Bearer token)
┌───────────┼───────────────────────────────────────────┼──────────────┐
│           ▼                                           ▼              │
│  ┌─────────────────┐                      ┌─────────────────────┐   │
│  │   Client Hub    │                      │    Daemon Hub       │   │
│  │  (per-user      │                      │  (per-daemon        │   │
│  │   connections)  │                      │   connections)      │   │
│  └────────┬────────┘                      └──────────┬──────────┘   │
│           │                                           │              │
│           ▼                                           ▼              │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                     Go Server (Chi Router)                    │   │
│  │                                                               │   │
│  │  ┌────────────┐  ┌──────────────┐  ┌───────────────────┐    │   │
│  │  │ Auth       │  │ Chat Service │  │ Runtime Service   │    │   │
│  │  │ (JWT/bcrypt)│  │ (CRUD, relay)│  │ (register, bind) │    │   │
│  │  └────────────┘  └──────────────┘  └───────────────────┘    │   │
│  │  ┌────────────┐  ┌──────────────┐  ┌───────────────────┐    │   │
│  │  │ WS Router  │  │ Daemon Relay │  │ Message Queue     │    │   │
│  │  │ (dispatch) │  │ (stream fwd) │  │ (per-session FIFO)│    │   │
│  │  └────────────┘  └──────────────┘  └───────────────────┘    │   │
│  └──────────────────────────┬───────────────────────────────────┘   │
│                              │                                        │
│                              ▼                                        │
│                    ┌──────────────────┐                               │
│                    │   PostgreSQL 17  │                               │
│                    │   (pgvector)     │                               │
│                    └──────────────────┘                               │
│                                                                      │
│                         Cloud / Server                                │
└──────────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Go Server

The central backend application managing authentication, session state, message persistence, and WebSocket relay between clients and daemons.

**Technology:** Go, Chi router, gorilla/websocket, sqlc, pgx/PostgreSQL

```
server/
├── cmd/agentbridge/         # Main entry point, graceful shutdown
├── internal/
│   ├── auth/                # JWT token generation/validation
│   ├── handler/             # HTTP route handlers + WS router
│   ├── clientws/            # Client WebSocket hub (browser connections)
│   ├── daemonws/            # Daemon WebSocket hub + heartbeat checker
│   ├── service/             # Business logic (ChatService, RuntimeService, DaemonRelay)
│   ├── middleware/          # Auth middleware, CORS, rate limiting
│   ├── config/              # Environment-based configuration
│   └── integration/         # End-to-end integration tests
├── pkg/
│   ├── protocol/            # Shared WebSocket message types
│   └── db/                  # sqlc-generated database access layer
└── migrations/              # PostgreSQL migration files
```

**Key responsibilities:**
- User authentication (email/password, JWT tokens)
- Chat session CRUD with user-scoped access controls
- Message persistence with monotonic sequence numbering
- WebSocket relay between clients and daemons
- Heartbeat monitoring and daemon offline detection
- Message buffering for disconnected clients (up to 100 messages, 5-minute window)
- Rate limiting (malformed message detection, per-IP request throttling)

### 2. Go Daemon

A lightweight background process running on each user's machine that detects local agent CLIs, registers with the server, sends heartbeats, and executes chat tasks.

**Technology:** Go, gorilla/websocket

```
daemon/
├── cmd/agentbridge-daemon/  # CLI entry point (start, stop, status)
├── internal/
│   ├── agent/               # Agent detection (PATH scan, version check)
│   ├── connection/          # WebSocket client + exponential backoff reconnect
│   ├── heartbeat/           # Periodic heartbeat ticker
│   ├── executor/            # Agent CLI invocation + stdout streaming
│   ├── logging/             # Rotating file logger (50MB, 3 backups)
│   └── config/              # Daemon configuration
└── pkg/protocol/            # Shared protocol types
```

**Key responsibilities:**
- Scan system PATH for supported agent CLIs (claude, kiro-cli, gemini, codex, copilot, opencode, hermes, pi, cursor-agent, kimi)
- Register detected runtimes with the server via WebSocket
- Send periodic heartbeats (default: 15s interval)
- Execute agent CLIs with conversation context, stream stdout tokens back to server
- Reconnect with exponential backoff (1s to 60s) on connection loss
- Periodic rescan for newly installed/removed agents (default: 60s)
- Write operational logs with rotation

### 3. Next.js Frontend

A React application providing the chat UI with real-time message streaming.

**Technology:** Next.js (App Router), TypeScript, Tailwind CSS, Zustand, fast-check

```
frontend/
├── app/                     # Next.js App Router pages
│   ├── page.tsx             # Login/register
│   └── chat/               # Chat interface
├── components/chat/         # Chat UI components
│   ├── ChatMessage.tsx      # Individual message display
│   ├── ChatStream.tsx       # Streaming response display
│   ├── ChatInput.tsx        # Message input with validation
│   ├── AgentSelector.tsx    # Runtime selection dropdown
│   ├── SessionList.tsx      # Session sidebar
│   ├── ConnectionBanner.tsx # Reconnection status banner
│   └── ReAuthModal.tsx      # Token expiry re-auth modal
├── lib/
│   ├── api.ts               # REST API client
│   ├── ws.ts                # WebSocket client with reconnection
│   └── store.ts             # Zustand stores (auth, sessions, messages, runtimes, connection)
└── hooks/
    └── useWebSocket.ts      # WebSocket lifecycle hook
```

**Key responsibilities:**
- User authentication (login/register forms)
- Chat session management (create, list, rename, delete)
- Real-time message streaming via WebSocket
- Agent selection and binding
- Connection status feedback (reconnecting banner, re-auth modal)
- Client-side input validation (1-32,000 characters)

## Communication Protocol

All WebSocket messages use a typed JSON envelope:

```json
{
  "type": "message_type",
  "payload": { ... }
}
```

### Message Flow

```
User types message
       │
       ▼
[Frontend] ──chat:send──▶ [Server] ──chat:task──▶ [Daemon]
                              │                        │
                              │ persist user msg       │ invoke CLI
                              │                        │
                              │                        ▼
[Frontend] ◀──chat:stream── [Server] ◀──chat:stream── [Daemon] (token by token)
       │                      │                        │
       │ append tokens        │ forward                │ stdout
       │                      │                        │
       ▼                      ▼                        ▼
[Frontend] ◀──chat:done──── [Server] ◀──chat:done──── [Daemon] (response complete)
                              │
                              │ persist assistant msg
```

### Message Types

| Type | Direction | Purpose |
|------|-----------|---------|
| `daemon:register` | Daemon → Server | Register daemon with runtime list |
| `daemon:heartbeat` | Daemon → Server | Periodic liveness signal |
| `chat:send` | Client → Server | User sends a message |
| `chat:task` | Server → Daemon | Relay message for execution |
| `chat:stream` | Daemon → Server → Client | Streaming token |
| `chat:done` | Daemon → Server → Client | Response complete |
| `chat:error` | Any → Client | Error notification |
| `chat:cancel` | Client → Server → Daemon | Cancel in-progress response |
| `session:*` | Server → Client | Session lifecycle events |
| `runtime:status` | Server → Client | Runtime online/offline change |
| `connection:ping/pong` | Server ↔ Client | Keep-alive |

## Data Model

### Entity Relationships

```
users (1) ──── (N) daemons (1) ──── (N) runtimes
  │                                         │
  │                                         │ (bound to)
  │                                         │
  └──── (N) chat_sessions ──── (N) chat_messages
                │
                └── message_buffer (disconnected client messages)
```

### Key Tables

| Table | Purpose |
|-------|---------|
| `users` | User accounts (email, password hash) |
| `daemons` | Registered daemon instances per user |
| `runtimes` | Detected agent CLIs on each daemon |
| `chat_sessions` | Conversation threads with optional runtime binding |
| `chat_messages` | Persisted messages with sequence numbers |
| `message_buffer` | Buffered messages for disconnected clients (5-min TTL) |

## Authentication & Security

- **User auth:** Email/password with bcrypt hashing, JWT tokens (24h validity, HS256)
- **Client WebSocket:** Token passed as query parameter (`/ws/client?token=<jwt>`)
- **Daemon WebSocket:** Bearer token in Authorization header (`/ws/daemon`)
- **Multi-user isolation:** All resources are user-scoped; cross-user access returns 403 without revealing resource existence
- **Rate limiting:** Per-IP token bucket (configurable RPS), malformed message detection (10 per 60s window)

## Resilience & Reliability

| Mechanism | Implementation |
|-----------|---------------|
| Daemon reconnection | Exponential backoff: min(2^(N-1)s, 60s) |
| Heartbeat timeout | Server marks daemon offline after 3× missed intervals |
| Client reconnection | WebSocket auto-reconnect with backoff |
| Message buffering | Up to 100 messages buffered during 5-min disconnection |
| Persist-before-relay | Messages are persisted to DB before being relayed to daemon |
| Graceful shutdown | Server drains connections on SIGTERM/SIGINT (30s timeout) |
| Log rotation | Daemon logs rotate at 50MB, retains 3 backups |

## Deployment

### Local Development

```bash
make dev   # Starts PostgreSQL (Docker), runs migrations, starts server + frontend
```

### Infrastructure Requirements

- PostgreSQL 17 (with pgvector extension)
- Go 1.21+ runtime for server and daemon
- Node.js 18+ for frontend build
- Docker/Podman for local database

### Environment Variables

| Variable | Component | Default | Description |
|----------|-----------|---------|-------------|
| `DATABASE_URL` | Server | — | PostgreSQL connection string |
| `JWT_SECRET` | Server | dev-secret | JWT signing secret |
| `PORT` | Server | 8080 | HTTP server port |
| `CORS_ORIGINS` | Server | * | Allowed CORS origins |
| `AGENTBRIDGE_SERVER_URL` | Daemon | ws://localhost:8080/ws/daemon | Server WebSocket URL |
| `AGENTBRIDGE_TOKEN` | Daemon | — | Auth token for server |
| `AGENTBRIDGE_USER_ID` | Daemon | — | User ID for registration |

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| WebSocket for all real-time communication | Sub-100ms message relay; bidirectional streaming for token-by-token responses |
| Separate WebSocket hubs (client vs daemon) | Different auth mechanisms, message types, and lifecycle management |
| PostgreSQL with sqlc | ACID guarantees for message ordering; type-safe generated Go code |
| In-memory services with DB optional | Enables fast development and testing without database dependency |
| Chi router | Lightweight, idiomatic Go HTTP router with composable middleware |
| Zustand for frontend state | Minimal boilerplate, reactive updates from WebSocket events |
| Property-based testing (rapid + fast-check) | Formal correctness verification for critical invariants |
| Daemon as separate binary | Runs on user's machine with direct access to local agent CLIs |
