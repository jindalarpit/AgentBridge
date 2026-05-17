# Implementation Plan: AgentBridge Chat

## Overview

This plan implements the AgentBridge distributed chat system as a greenfield project with three components: a Go server (Chi router, gorilla/websocket, sqlc/PostgreSQL), a Go daemon (CLI background process for agent detection and execution), and a Next.js frontend (React, Zustand, WebSocket). Tasks are ordered to build foundational infrastructure first, then layer on business logic, and finally wire everything together.

## Tasks

- [x] 1. Project scaffolding and shared protocol
  - [x] 1.1 Initialize Go server module and directory structure
    - Create `server/` with `cmd/agentbridge/main.go`, `internal/` (auth, handler, clientws, daemonws, service, middleware, config), `pkg/` (protocol, db), `migrations/`
    - Initialize `go.mod` with module path `github.com/user/agentbridge/server`
    - Add dependencies: chi, gorilla/websocket, sqlc, pgx, golang-jwt, bcrypt, rapid (testing)
    - _Requirements: 3.1, 6.1_

  - [x] 1.2 Initialize Go daemon module and directory structure
    - Create `daemon/` with `cmd/agentbridge-daemon/main.go`, `internal/` (agent, connection, heartbeat, executor, config)
    - Initialize `go.mod` with module path `github.com/user/agentbridge/daemon`
    - Add dependencies: gorilla/websocket, rapid (testing)
    - _Requirements: 1.1, 8.1_

  - [x] 1.3 Initialize Next.js frontend project
    - Create `frontend/` with Next.js App Router, TypeScript, Tailwind CSS, Zustand, fast-check (testing)
    - Set up directory structure: `app/`, `components/chat/`, `components/ui/`, `lib/`, `hooks/`
    - Configure `tsconfig.json`, `package.json`, ESLint
    - _Requirements: 3.3, 6.4_

  - [x] 1.4 Define shared WebSocket protocol types
    - Create `server/pkg/protocol/message.go` with Message envelope struct and all message type constants
    - Define payload structs: DaemonRegisterPayload, ChatTaskPayload, ChatStreamPayload, ChatDonePayload, ChatErrorPayload, HistoryItem
    - Define error code constants (ErrCodeValidation, ErrCodeAuthentication, etc.)
    - Create `daemon/pkg/protocol/` as a copy or Go workspace reference
    - _Requirements: 6.1, 6.3, 6.5_

- [x] 2. Local development infrastructure (Docker Compose, Makefile, scripts)
  - [x] 2.1 Create docker-compose.yml with PostgreSQL and MailHog
    - Add `postgres` service using `pgvector/pgvector:pg17` with configurable port, user, password, and named volume
    - Add `mailhog` service using `mailhog/mailhog:latest` with SMTP (1025) and Web UI (8025) ports
    - Add healthcheck for postgres service
    - Ensure compose file uses only standard Compose v2 features (compatible with both `docker compose` and `podman-compose`)
    - _Requirements: N/A (infrastructure)_

  - [x] 2.2 Create .env.example with all required environment variables
    - Include database config (POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_PORT, DATABASE_URL)
    - Include server config (PORT, JWT_SECRET, CORS_ORIGINS)
    - Include frontend config (FRONTEND_PORT, NEXT_PUBLIC_API_URL, NEXT_PUBLIC_WS_URL)
    - Include email/SMTP config (SMTP_HOST, SMTP_PORT, SMTP_FROM, MAILHOG_SMTP_PORT, MAILHOG_UI_PORT)
    - Include commented-out DOCKER_HOST for Podman users
    - _Requirements: N/A (infrastructure)_

  - [x] 2.3 Create Makefile with dev/start/stop/check targets
    - `make dev`: full bootstrap (create .env if missing, start compose services, run migrations, start server + frontend)
    - `make start`: start server + frontend (assumes services running)
    - `make stop`: stop server + frontend processes
    - `make check`: run full verification pipeline (typecheck, Go tests, frontend tests)
    - `make db-up` / `make db-down` / `make db-reset`: manage compose services
    - `make migrate-up` / `make migrate-down`: database migration targets
    - `make test`: run Go tests (ensures DB ready first)
    - Use `docker compose` commands throughout (works with Podman via DOCKER_HOST)
    - _Requirements: N/A (infrastructure)_

  - [x] 2.4 Create scripts/ensure-postgres.sh
    - Parse DATABASE_URL from env file to determine local vs remote database
    - For local: start `docker compose up -d postgres`, wait for readiness, create database if not exists
    - For remote: skip Docker, verify connectivity via `pg_isready`
    - Works with both Docker and Podman (respects DOCKER_HOST env var)
    - Follow Multica's ensure-postgres.sh pattern
    - _Requirements: N/A (infrastructure)_

  - [x] 2.5 Document Podman compatibility in README
    - Add section explaining DOCKER_HOST setup for Podman users
    - Include examples for macOS (podman machine) and Linux (rootless podman)
    - Note that all Makefile targets work transparently with Podman
    - _Requirements: N/A (infrastructure)_

- [x] 3. Database schema and migrations
  - [x] 3.1 Create PostgreSQL migration files
    - Create `server/migrations/001_initial_schema.up.sql` with users, daemons, runtimes, chat_sessions, chat_messages, message_buffer tables
    - Create `server/migrations/001_initial_schema.down.sql` with DROP TABLE statements
    - Include all indexes defined in the design (idx_daemons_user, idx_chat_messages_session_seq, etc.)
    - _Requirements: 9.1, 9.2, 9.4_

  - [x] 3.2 Configure sqlc and generate database access layer
    - Create `server/sqlc.yaml` configuration
    - Write SQL queries for all CRUD operations: users, daemons, runtimes, chat_sessions, chat_messages, message_buffer
    - Run `sqlc generate` to produce `server/pkg/db/` Go code
    - _Requirements: 9.1, 9.3_

- [x] 4. Authentication system
  - [x] 3.1 Implement JWT token management
    - Create `server/internal/auth/token.go` with GenerateToken, ValidateToken, RefreshToken functions
    - Token validity: 24 hours, signed with HS256
    - Include user ID and email in claims
    - _Requirements: 3.2, 10.2_

  - [x] 3.2 Write property test for token authentication validity
    - **Property 20: Token Authentication Validity**
    - **Validates: Requirements 3.2, 10.2**

  - [x] 3.3 Implement auth HTTP handlers
    - Create `server/internal/handler/auth.go` with Register, Login, Refresh, Me handlers
    - Password hashing with bcrypt, email validation
    - Return JWT token on successful login/register
    - _Requirements: 3.1, 3.2_

  - [x] 3.4 Implement auth middleware
    - Create `server/internal/middleware/auth.go` extracting JWT from Authorization header
    - Inject user ID into request context on success
    - Return 401 on invalid/expired token
    - _Requirements: 3.1, 7.6_


- [x] 5. Checkpoint - Foundation verification
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. WebSocket infrastructure — Client Hub
  - [x] 5.1 Implement ClientHub connection management
    - Create `server/internal/clientws/hub.go` implementing ClientHub interface
    - Handle WebSocket upgrade with token authentication (query param `?token=`)
    - Maintain per-user connection map (support multiple tabs)
    - Implement SendToUser and BroadcastToUser methods
    - _Requirements: 3.3, 10.1, 10.6_

  - [x] 5.2 Implement ping/pong keep-alive for client connections
    - Send ping frame every 30 seconds
    - Close connection if pong not received within 10 seconds
    - Mark client as disconnected on timeout
    - _Requirements: 10.3, 10.4_

  - [x] 5.3 Implement message buffering for disconnected clients
    - Create `server/internal/clientws/buffer.go` for storing messages during disconnection
    - Buffer up to 100 messages per user within 5-minute window
    - Deliver buffered messages in chronological order on reconnection
    - Clean up expired buffer entries
    - _Requirements: 10.5_

  - [x] 5.4 Write property test for buffered message delivery
    - **Property 21: Buffered Message Delivery**
    - **Validates: Requirements 10.5**

  - [x] 5.5 Implement malformed message rate limiting
    - Track malformed message count per connection in 60-second sliding window
    - Close connection after 10+ malformed messages within window
    - Log each malformed message
    - _Requirements: 10.7_

  - [x] 5.6 Write property test for malformed message rate limiting
    - **Property 22: Malformed Message Rate Limiting**
    - **Validates: Requirements 10.7**

- [x] 7. WebSocket infrastructure — Daemon Hub
  - [x] 6.1 Implement DaemonHub connection management
    - Create `server/internal/daemonws/hub.go` implementing DaemonHub interface
    - Handle WebSocket upgrade with Bearer token authentication (Authorization header)
    - Maintain daemon connection map keyed by daemon_id
    - Implement SendToDaemon and IsOnline methods
    - _Requirements: 2.1, 2.2_

  - [x] 6.2 Implement daemon registration message handling
    - Parse `daemon:register` messages, validate required fields (daemon_id, user_id, runtimes)
    - Reject invalid registrations with `daemon:register_error` and close connection
    - Acknowledge valid registrations with `daemon:register_ack`
    - _Requirements: 2.1, 2.2, 2.9_

  - [x] 6.3 Write property test for registration validation
    - **Property 5: Registration Validation**
    - **Validates: Requirements 2.9**

  - [x] 6.4 Implement heartbeat handling
    - Process `daemon:heartbeat` messages, update last_seen_at in database
    - Respond with `daemon:heartbeat_ack`
    - Run background goroutine to check for missed heartbeats (3× interval threshold)
    - Mark daemon and runtimes as offline when threshold exceeded
    - _Requirements: 2.3, 2.4, 2.5_

  - [x] 6.5 Write property test for heartbeat timeout detection
    - **Property 3: Heartbeat Timeout Detection**
    - **Validates: Requirements 2.5**

- [x] 8. Checkpoint - WebSocket infrastructure verification
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. Daemon agent detection
  - [x] 8.1 Implement agent detector
    - Create `daemon/internal/agent/detect.go` implementing AgentDetector interface
    - Scan system PATH for supported binaries: claude, kiro-cli, gemini, codex, copilot, opencode, hermes, pi, cursor-agent, kimi
    - Check environment variable overrides (e.g., MULTICA_CLAUDE_PATH)
    - Execute version flag with 10-second timeout
    - Mark as unavailable if version retrieval fails
    - _Requirements: 1.1, 1.2, 1.3, 1.4_

  - [x] 8.2 Write property test for agent detection correctness
    - **Property 1: Agent Detection Correctness**
    - **Validates: Requirements 1.1, 1.3, 1.4**

  - [x] 8.3 Implement periodic rescan
    - Run detection at configurable interval (default: 60 seconds)
    - Update runtime list on changes, trigger re-registration if runtimes changed
    - Log warning when no agents found
    - _Requirements: 1.5, 1.6_


- [x] 10. Daemon registration and connection management
  - [x] 9.1 Implement ServerConnection with WebSocket client
    - Create `daemon/internal/connection/connection.go` implementing ServerConnection interface
    - Establish WebSocket connection to server with Bearer token auth
    - Implement Send, OnMessage, Close, IsConnected methods
    - _Requirements: 2.1, 2.6_

  - [x] 9.2 Implement exponential backoff reconnection
    - Create `daemon/internal/connection/backoff.go` with delay calculation: min(2^(N-1)s, 60s)
    - Auto-reconnect on connection loss with backoff
    - Re-send DaemonRegister on successful reconnection
    - _Requirements: 2.6, 2.7, 2.8_

  - [x] 9.3 Write property test for exponential backoff sequence
    - **Property 4: Exponential Backoff Sequence**
    - **Validates: Requirements 2.6**

  - [x] 9.4 Implement heartbeat ticker
    - Create `daemon/internal/heartbeat/heartbeat.go` sending heartbeat at configurable interval (default: 15s)
    - Start/stop with daemon lifecycle
    - _Requirements: 2.3_

- [x] 11. Runtime service (server-side daemon/runtime management)
  - [x] 10.1 Implement RuntimeService
    - Create `server/internal/service/runtime.go` implementing RuntimeService interface
    - RegisterDaemon: upsert daemon record, replace runtimes
    - DeregisterDaemon: mark offline, update runtimes
    - UpdateHeartbeat: update last_seen_at
    - GetUserRuntimes: return only available runtimes for user's daemons
    - _Requirements: 2.2, 5.1, 7.1_

  - [x] 10.2 Write property test for daemon registration state consistency
    - **Property 2: Daemon Registration State Consistency**
    - **Validates: Requirements 2.2**

  - [x] 10.3 Write property test for runtime filtering
    - **Property 10: Runtime Filtering**
    - **Validates: Requirements 5.1**

- [x] 12. Chat session management
  - [x] 11.1 Implement ChatService — session CRUD
    - Create `server/internal/service/chat.go` implementing ChatService interface
    - CreateSession: generate UUID, set title "New Chat", status "active"
    - ListSessions: paginated, ordered by most recent activity, max 50 per page
    - GetSession: load session with ownership check
    - DeleteSession: cascade delete messages, cancel in-progress tasks
    - RenameSession: validate title 1-100 chars
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6_

  - [x] 11.2 Write property test for session creation defaults
    - **Property 6: Session Creation Defaults**
    - **Validates: Requirements 4.1**

  - [x] 11.3 Write property test for session list ordering
    - **Property 7: Session List Ordering**
    - **Validates: Requirements 4.2**

  - [x] 11.4 Write property test for session deletion completeness
    - **Property 8: Session Deletion Completeness**
    - **Validates: Requirements 4.4**

  - [x] 11.5 Implement input validation
    - Create `server/internal/handler/validation.go` with message validation (1-32000 chars) and title validation (1-100 chars)
    - Return field-level error messages
    - _Requirements: 4.5, 4.6, 6.8_

  - [x] 11.6 Write property test for input validation
    - **Property 9: Input Validation**
    - **Validates: Requirements 4.5, 4.6, 6.8**

- [x] 13. Agent binding
  - [x] 12.1 Implement runtime binding in ChatService
    - Add BindRuntime method: validate runtime belongs to user, is online, replace existing binding
    - Reject binding to offline runtimes with error
    - Preserve existing message history on rebind
    - _Requirements: 5.2, 5.3, 5.6, 5.7_

  - [x] 12.2 Write property test for binding replacement and offline rejection
    - **Property 11: Binding Replacement and Offline Rejection**
    - **Validates: Requirements 5.2, 5.6, 5.7**

  - [x] 12.3 Implement runtime and binding HTTP handlers
    - Create `server/internal/handler/runtime.go` with GET /api/runtimes and POST /api/sessions/:id/bind
    - Enforce user-scoped access (only own runtimes)
    - _Requirements: 5.1, 5.2, 7.1, 7.2_

- [x] 14. Checkpoint - Core business logic verification
  - Ensure all tests pass, ask the user if questions arise.


- [x] 15. Real-time message flow and streaming
  - [x] 14.1 Implement SendMessage in ChatService
    - Persist user message with sequence number, validate content length
    - Enforce persist-before-relay: only relay after successful DB write
    - Build chat:task payload with history (up to 200 recent messages)
    - Relay to daemon via DaemonHub.SendToDaemon
    - _Requirements: 6.1, 6.2, 9.4, 9.5_

  - [x] 14.2 Write property test for persist-before-relay invariant
    - **Property 19: Persist-Before-Relay Invariant**
    - **Validates: Requirements 9.5**

  - [x] 14.3 Write property test for message persistence round-trip
    - **Property 17: Message Persistence Round-Trip**
    - **Validates: Requirements 6.1, 9.1**

  - [x] 14.4 Write property test for message ordering invariant
    - **Property 18: Message Ordering Invariant**
    - **Validates: Requirements 9.2, 9.4**

  - [x] 14.5 Implement stream relay in server
    - Handle `chat:stream` from daemon, forward to client via ClientHub
    - Handle `chat:done` from daemon, persist assistant message, notify client
    - Handle `chat:error` from daemon, update message status, notify client
    - _Requirements: 6.3, 6.4, 6.5, 6.6, 6.7_

  - [x] 14.6 Implement message queuing for concurrent sends
    - Queue messages per session when a response is in progress
    - Deliver queued messages to daemon in FIFO order after current response completes/fails
    - _Requirements: 6.9_

  - [x] 14.7 Write property test for message queue FIFO ordering
    - **Property 15: Message Queue FIFO Ordering**
    - **Validates: Requirements 6.9**

  - [x] 14.8 Write property test for history truncation
    - **Property 12: History Truncation**
    - **Validates: Requirements 6.2**

- [x] 16. Daemon task executor
  - [x] 15.1 Implement AgentExecutor
    - Create `daemon/internal/executor/executor.go` implementing AgentExecutor interface
    - Spawn agent CLI process with message content and conversation history
    - Stream stdout tokens with monotonically increasing sequence numbers
    - Handle 300-second timeout, send chat:error on timeout/crash
    - Support cancellation via Cancel method
    - _Requirements: 6.2, 6.3, 6.5, 6.7_

  - [x] 15.2 Write property test for stream sequence monotonicity
    - **Property 13: Stream Sequence Monotonicity**
    - **Validates: Requirements 6.3**

  - [x] 15.3 Write property test for stream concatenation integrity
    - **Property 14: Stream Concatenation Integrity**
    - **Validates: Requirements 6.5**

  - [x] 15.4 Implement task message handler in daemon
    - Listen for `chat:task` messages from server
    - Invoke AgentExecutor with the task payload
    - Stream tokens back as `chat:stream`, send `chat:done` on completion
    - _Requirements: 6.2, 6.3, 6.5_

- [x] 17. Multi-user isolation
  - [x] 16.1 Implement user-scoped access controls
    - Add ownership checks to all ChatService and RuntimeService methods
    - Verify daemon belongs to user before relay
    - Return 403 without revealing resource existence on cross-user access
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7_

  - [x] 16.2 Write property test for multi-user isolation
    - **Property 16: Multi-User Isolation**
    - **Validates: Requirements 7.1, 7.2, 7.3, 7.4, 7.6**

- [x] 18. Checkpoint - Message flow and isolation verification
  - Ensure all tests pass, ask the user if questions arise.

- [x] 19. Chat session and message HTTP handlers
  - [x] 18.1 Implement session HTTP handlers
    - Create `server/internal/handler/session.go` with POST/GET/DELETE/PATCH /api/sessions routes
    - GET /api/sessions/:id/messages for message history
    - POST /api/sessions/:id/messages for sending messages (triggers WebSocket relay)
    - All routes enforce auth middleware and user-scoped access
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 9.2_

  - [x] 18.2 Wire HTTP router with all handlers
    - Create `server/internal/handler/router.go` with Chi router setup
    - Mount auth routes, session routes, runtime routes
    - Mount WebSocket upgrade endpoints (/ws/client, /ws/daemon)
    - Add CORS, logging, rate limiting middleware
    - _Requirements: 3.1, 3.4, 10.6_


- [x] 20. Daemon lifecycle CLI
  - [x] 19.1 Implement daemon start command
    - Create `daemon/cmd/agentbridge-daemon/main.go` with cobra or flag-based CLI
    - `start`: check PID file, fork background process, run agent detection, connect to server, register
    - Write PID to `~/.agentbridge/daemon.pid`, output confirmation within 10 seconds
    - Exit with error if already running or server connection fails
    - _Requirements: 8.1, 8.2, 8.8_

  - [x] 19.2 Implement daemon stop command
    - `stop`: read PID file, send deregister to server, close WebSocket, terminate process
    - Clean up PID file, output confirmation within 10 seconds
    - Handle case where no daemon is running
    - _Requirements: 8.3, 8.4_

  - [x] 19.3 Implement daemon status command
    - `status`: report connection state (connected/disconnected/reconnecting), uptime, detected agents with types/versions, task status (idle/executing)
    - _Requirements: 8.5_

  - [x] 19.4 Implement daemon logging
    - Write logs to configurable file (default: ~/.agentbridge/daemon.log)
    - Rotate at 50 MB, retain 3 most recent rotated files
    - _Requirements: 8.6_

- [x] 21. Frontend — API client and WebSocket hook
  - [x] 20.1 Implement REST API client
    - Create `frontend/lib/api.ts` with typed functions for all endpoints (auth, sessions, runtimes, messages)
    - Handle token storage, auto-attach Authorization header
    - Handle error responses with typed error objects
    - _Requirements: 3.2, 4.1, 4.2, 5.1_

  - [x] 20.2 Implement WebSocket client with reconnection
    - Create `frontend/lib/ws.ts` with connection management
    - Connect to /ws/client with token query param
    - Implement exponential backoff reconnection (1s to 60s)
    - Parse typed message envelope, dispatch to handlers
    - Implement ping/pong response
    - _Requirements: 3.3, 10.3, 10.5_

  - [x] 20.3 Implement Zustand stores for client state
    - Create `frontend/lib/store.ts` with stores for: auth state, active session, message list, runtime list, connection status
    - WebSocket events update stores reactively
    - _Requirements: 3.3, 6.4_

- [x] 22. Frontend — Chat UI components
  - [x] 21.1 Implement authentication pages
    - Create login and register pages at `frontend/app/page.tsx`
    - Form validation, error display, redirect to chat on success
    - _Requirements: 3.1, 3.2_

  - [x] 21.2 Implement chat layout and session sidebar
    - Create `frontend/app/chat/layout.tsx` with sidebar listing sessions
    - SessionList component with create, rename, delete actions
    - Show sessions ordered by recent activity
    - _Requirements: 4.1, 4.2, 4.4, 4.5_

  - [x] 21.3 Implement chat message display and streaming
    - Create `frontend/components/chat/ChatMessage.tsx` for individual messages
    - Create `frontend/components/chat/ChatStream.tsx` for streaming response display
    - Append tokens in real-time as `chat:stream` events arrive
    - Show final message on `chat:done`
    - Display errors inline on `chat:error`
    - _Requirements: 6.3, 6.4, 6.6, 6.7_

  - [x] 21.4 Implement chat input and message sending
    - Create `frontend/components/chat/ChatInput.tsx` with textarea, send button
    - Validate message length (1-32000 chars) client-side
    - Send via WebSocket `chat:send` message
    - Disable input while agent is unavailable
    - _Requirements: 6.1, 6.8, 5.5_

  - [x] 21.5 Implement agent selector component
    - Create `frontend/components/chat/AgentSelector.tsx` showing available runtimes
    - Display agent type and version for each runtime
    - Call bind endpoint on selection
    - Show "no agents available" message with daemon start instructions when empty
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

  - [x] 21.6 Implement connection status and reconnection UI
    - Show "Reconnecting..." banner on WebSocket disconnect
    - Show re-auth modal on token expiry
    - Auto-enable input when runtime comes back online
    - _Requirements: 10.3, 10.5, 3.5_

- [x] 23. Checkpoint - Frontend verification
  - Ensure all tests pass, ask the user if questions arise.

- [x] 24. Server main entry point and configuration
  - [x] 23.1 Implement server main and configuration
    - Create `server/cmd/agentbridge/main.go` wiring all components
    - Load configuration from environment variables (DB URL, JWT secret, port, CORS origins)
    - Initialize database connection pool, run migrations
    - Start HTTP server with graceful shutdown
    - _Requirements: 3.4, 10.6_

  - [x] 23.2 Implement daemon main loop wiring
    - Wire daemon components: AgentDetector → ServerConnection → HeartbeatTicker → TaskHandler
    - Handle graceful shutdown (SIGTERM/SIGINT)
    - _Requirements: 8.1, 8.3_

- [x] 25. Integration wiring and end-to-end flow
  - [x] 24.1 Wire client WebSocket message routing
    - Route incoming `chat:send` to ChatService.SendMessage
    - Route incoming `chat:cancel` to daemon via DaemonHub
    - Broadcast session events (created, deleted, updated) to user's connections
    - Broadcast runtime status changes to affected users
    - _Requirements: 6.1, 4.4, 5.5_

  - [x] 24.2 Wire daemon WebSocket message routing
    - Route `chat:stream`, `chat:done`, `chat:error` from daemon to ClientHub
    - Route `daemon:heartbeat` to RuntimeService.UpdateHeartbeat
    - Handle daemon disconnect: mark offline, notify affected sessions
    - _Requirements: 6.3, 6.5, 6.6, 2.4, 2.5_

  - [x] 24.3 Write integration tests for full chat flow
    - Test: user sends message → server persists → relays to daemon → daemon streams back → client receives tokens → done persisted
    - Test: daemon disconnect → heartbeat timeout → runtimes marked offline → client notified
    - _Requirements: 6.1, 6.3, 6.5, 6.6, 2.5_

- [x] 26. Final checkpoint - Full system verification
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- Go server and daemon use `rapid` for property-based testing
- Frontend uses `fast-check` for property-based testing of client logic
- The project uses Go (server + daemon) and TypeScript/Next.js (frontend) as specified in the design

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3"] },
    { "id": 1, "tasks": ["1.4", "2.1", "2.2", "2.3", "2.4", "2.5"] },
    { "id": 2, "tasks": ["3.1"] },
    { "id": 3, "tasks": ["3.2", "4.1"] },
    { "id": 4, "tasks": ["4.2", "4.3", "4.4"] },
    { "id": 5, "tasks": ["6.1", "7.1", "9.1"] },
    { "id": 6, "tasks": ["6.2", "6.3", "6.5", "7.2", "7.4", "9.2", "9.3"] },
    { "id": 7, "tasks": ["6.4", "6.6", "7.3", "7.5", "10.1", "10.4"] },
    { "id": 8, "tasks": ["10.2", "10.3", "11.1"] },
    { "id": 9, "tasks": ["11.2", "11.3", "12.1"] },
    { "id": 10, "tasks": ["12.2", "12.3", "12.4", "12.5", "13.1"] },
    { "id": 11, "tasks": ["12.6", "13.2", "13.3"] },
    { "id": 12, "tasks": ["15.1", "16.1"] },
    { "id": 13, "tasks": ["15.2", "15.3", "15.4", "15.5", "15.6", "16.2", "16.3", "16.4"] },
    { "id": 14, "tasks": ["15.7", "15.8", "17.1"] },
    { "id": 15, "tasks": ["17.2", "19.1"] },
    { "id": 16, "tasks": ["19.2", "20.1", "20.2", "20.3", "20.4"] },
    { "id": 17, "tasks": ["21.1", "21.2", "21.3"] },
    { "id": 18, "tasks": ["22.1", "22.2", "22.3", "22.4", "22.5", "22.6"] },
    { "id": 19, "tasks": ["24.1", "24.2"] },
    { "id": 20, "tasks": ["25.1", "25.2"] },
    { "id": 21, "tasks": ["25.3"] }
  ]
}
```
