# AgentBridge — Functional Specification

## Purpose

AgentBridge enables users to interact with AI agent CLIs installed on their local machines through a web-based chat interface. Users can chat with Claude, Gemini, Kiro, Codex, and other supported agents without switching between terminal windows — the system detects available agents, relays messages in real time, and streams responses token-by-token.

## User Roles

| Role | Description |
|------|-------------|
| **User** | A person who registers, logs in, creates chat sessions, and interacts with AI agents |
| **Daemon** | A background process on the user's machine (not a human role, but an actor in the system) |

## Functional Areas

### 1. User Authentication

Users register with email and password, then authenticate to access the chat interface.

| Function | Behavior |
|----------|----------|
| Register | Create account with email + password. Password stored as bcrypt hash. Returns JWT token. |
| Login | Authenticate with email + password. Returns JWT token valid for 24 hours. |
| Token refresh | Exchange a valid token for a new one before expiry. |
| Session persistence | Chat history persists across browser refreshes. Token expiry prompts re-auth without losing data. |

### 2. Local Agent Detection

The daemon automatically discovers AI agent CLIs installed on the user's machine.

| Function | Behavior |
|----------|----------|
| PATH scan | Scans system PATH for: claude, kiro-cli, gemini, codex, copilot, opencode, hermes, pi, cursor-agent, kimi |
| Environment overrides | Checks agent-specific env vars (e.g., `MULTICA_CLAUDE_PATH`) for custom binary locations |
| Version detection | Executes each binary with a version flag (10s timeout). Records version or marks as "unavailable" |
| Periodic rescan | Re-scans every 60 seconds (configurable) to detect newly installed or removed agents |
| Runtime list | Produces a list of RuntimeInfo objects: agent type, binary path, version, availability status |

### 3. Daemon Registration & Heartbeat

The daemon maintains a persistent connection to the server to advertise available agents.

| Function | Behavior |
|----------|----------|
| Registration | On startup, connects via WebSocket and sends daemon ID, user ID, and detected runtimes |
| Heartbeat | Sends a heartbeat every 15 seconds (configurable: 5s–120s) |
| Offline detection | Server marks daemon offline if 3 consecutive heartbeats are missed (45s default) |
| Reconnection | On connection loss, retries with exponential backoff: 1s, 2s, 4s, 8s, ... up to 60s max |
| Re-registration | After reconnecting, re-sends the full runtime list |

### 4. Chat Session Management

Users create and manage conversation threads.

| Function | Behavior |
|----------|----------|
| Create session | Creates a new session with title "New Chat", status "active", unique UUID |
| List sessions | Returns up to 50 sessions per page, ordered by most recent activity (last message or creation time) |
| Load session | Returns full message history for a selected session |
| Rename session | Updates title (1–100 characters after trimming whitespace) |
| Delete session | Cancels any in-progress task, removes session and all messages, notifies other connected clients |

### 5. Agent Binding

Users select which detected agent to use for a chat session.

| Function | Behavior |
|----------|----------|
| List available agents | Returns online runtimes from the user's daemon with agent type and version |
| Bind agent | Associates a runtime with the current session. Replaces any existing binding. |
| Rebind | Changing the bound agent preserves existing message history |
| Offline rejection | Binding to an offline runtime is rejected with an error |
| Unavailability notice | When a bound runtime goes offline, the user is notified and input is disabled |
| No agents state | When no agents are available, shows instructions to start the daemon |

### 6. Real-Time Chat

Users send messages and receive streamed agent responses in real time.

| Function | Behavior |
|----------|----------|
| Send message | User message is persisted, then relayed to daemon as a `chat:task` with up to 200 recent messages as context |
| Stream response | Agent CLI output is streamed token-by-token with monotonically increasing sequence numbers |
| Complete response | On completion, the full response is persisted as an assistant message |
| Error handling | If the agent fails or times out (300s), an error is displayed; partial tokens are preserved |
| Message queuing | Messages sent while a response is in progress are queued and delivered FIFO after completion |
| Cancellation | Users can cancel an in-progress response |

**Validation rules:**
- Message content: 1–32,000 characters (trimmed)
- Session title: 1–100 characters (trimmed)
- Empty or whitespace-only inputs are rejected

### 7. Multi-User Isolation

Each user's data and agents are completely isolated from other users.

| Guarantee | Implementation |
|-----------|---------------|
| Runtime isolation | Users can only see and bind to their own daemon's runtimes |
| Session isolation | Users cannot read, modify, or delete another user's sessions |
| Relay isolation | Messages are only relayed to the authenticated user's own daemon |
| Error opacity | Cross-user access attempts return 403 without revealing whether the resource exists |
| WebSocket isolation | Broadcast messages for one user are never delivered to another user's connections |

### 8. Daemon Lifecycle

Users control the daemon process on their machine via CLI commands.

| Command | Behavior |
|---------|----------|
| `start` | Starts daemon as background process, detects agents, registers with server. Outputs confirmation within 10s. Fails if already running. |
| `stop` | Deregisters from server, closes WebSocket, terminates process. Outputs confirmation within 10s. |
| `status` | Reports: connection state (connected/disconnected/reconnecting), uptime, detected agents with versions, task status (idle/executing) |

**Logging:** Writes to `~/.agentbridge/daemon.log`, rotates at 50MB, retains 3 most recent rotated files.

### 9. Message Persistence

All messages are durably stored for history retrieval.

| Guarantee | Implementation |
|-----------|---------------|
| Complete persistence | Every user and assistant message is stored with ID, session ID, role, content, timestamp |
| Ordering | Messages have monotonically increasing sequence numbers per session (1, 2, 3, ...) |
| Persist-before-relay | User messages are confirmed persisted before being relayed to the daemon |
| Content limit | Messages support up to 100,000 characters of content |
| Performance | Sessions with up to 1,000 messages load in under 500ms |

### 10. Connection Management

The system handles network interruptions gracefully.

| Function | Behavior |
|----------|----------|
| WebSocket auth | Client connections authenticated via token within 5 seconds |
| Keep-alive | Server sends ping every 30s; closes connection if pong not received within 10s |
| Message buffering | Up to 100 messages buffered during disconnection (5-minute window) |
| Reconnection delivery | Buffered messages delivered in chronological order on reconnection |
| Malformed message handling | Tolerates up to 10 malformed messages per 60s; closes connection on the 11th |
| Capacity | Supports 10,000+ concurrent WebSocket connections per server instance |

## User Workflows

### First-Time Setup

1. User installs one or more AI agent CLIs on their machine
2. User starts the daemon: `agentbridge-daemon start`
3. Daemon detects agents, registers with server
4. User opens the web app, registers an account
5. User creates a chat session, selects an available agent
6. User starts chatting

### Typical Chat Session

1. User opens existing session (or creates new one)
2. Selects an agent from the dropdown (if not already bound)
3. Types a message and presses Enter
4. Message appears in chat, agent response streams in token-by-token
5. Response completes, user can continue the conversation

### Agent Goes Offline

1. User is chatting, daemon process crashes or loses network
2. Server detects missed heartbeats (within 45s)
3. Runtime marked offline, user notified ("Agent unavailable")
4. Chat input disabled
5. When daemon reconnects and re-registers, runtime comes back online
6. Chat input re-enabled automatically

### Network Interruption (Browser)

1. User's browser loses WebSocket connection
2. "Reconnecting..." banner appears
3. Browser auto-reconnects with exponential backoff
4. On reconnection, any buffered messages are delivered
5. Banner disappears, chat resumes normally

### Token Expiry

1. User's JWT token expires (after 24 hours)
2. WebSocket connection closed with auth error code
3. Re-authentication modal appears (email pre-filled)
4. User enters password, new token issued
5. WebSocket reconnects, session state preserved

## API Endpoints

### REST API

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/auth/register` | Create new user account |
| POST | `/api/auth/login` | Authenticate, returns JWT |
| POST | `/api/auth/refresh` | Refresh token |
| GET | `/api/auth/me` | Get current user info |
| POST | `/api/sessions` | Create new chat session |
| GET | `/api/sessions` | List sessions (paginated) |
| GET | `/api/sessions/:id` | Get session details |
| DELETE | `/api/sessions/:id` | Delete session |
| PATCH | `/api/sessions/:id` | Rename session |
| GET | `/api/sessions/:id/messages` | Get message history |
| POST | `/api/sessions/:id/messages` | Send a message |
| GET | `/api/runtimes` | List available runtimes |
| POST | `/api/sessions/:id/bind` | Bind runtime to session |

### WebSocket Endpoints

| Path | Auth | Description |
|------|------|-------------|
| `/ws/client` | Token query param | Browser client connection |
| `/ws/daemon` | Bearer token header | Daemon connection |

## Supported Agent CLIs

| Agent | Binary Name | Description |
|-------|-------------|-------------|
| Claude | `claude` | Anthropic's Claude Code CLI |
| Kiro | `kiro-cli` | Kiro CLI |
| Gemini | `gemini` | Google's Gemini CLI |
| Codex | `codex` | OpenAI Codex CLI |
| Copilot | `copilot` | GitHub Copilot CLI |
| OpenCode | `opencode` | OpenCode CLI |
| Hermes | `hermes` | Hermes CLI |
| Pi | `pi` | Pi CLI |
| Cursor Agent | `cursor-agent` | Cursor Agent CLI |
| Kimi | `kimi` | Kimi CLI |

Custom binary locations can be specified via environment variables (e.g., `MULTICA_CLAUDE_PATH=/custom/path/claude`).

## Error Handling

### User-Facing Errors

| Scenario | User Experience |
|----------|----------------|
| Invalid login | "Invalid email or password" |
| Message too long | "Message exceeds maximum length of 32,000 characters" |
| Agent unavailable | "Agent is currently unavailable" + input disabled |
| Agent timeout (300s) | Error displayed inline; partial response preserved |
| Agent crash | Error displayed inline with failure reason |
| Network disconnect | "Reconnecting..." banner; auto-reconnect |
| Token expired | Re-auth modal; session state preserved |
| Rate limited | "Rate limit exceeded" with retry-after |

### Error Codes

| Code | Meaning |
|------|---------|
| `validation_error` | Input validation failed |
| `authentication_error` | Invalid or expired credentials |
| `authorization_error` | Access denied (cross-user) |
| `not_found` | Resource does not exist |
| `agent_timeout` | Agent CLI did not respond within 300s |
| `agent_error` | Agent CLI crashed or returned non-zero exit |
| `agent_unavailable` | No online runtime bound to session |
| `persist_failed` | Database write failed |
| `rate_limit` | Too many requests |
| `internal_error` | Unexpected server error |

## Non-Functional Requirements

| Requirement | Target |
|-------------|--------|
| Concurrent users | 5,000+ authenticated sessions |
| Concurrent WebSocket connections | 10,000+ (clients + daemons) |
| Message relay latency (server) | < 100ms at p95 |
| Stream token forwarding | < 50ms per token |
| Session history load | < 500ms for 1,000 messages |
| Agent response timeout | 300 seconds |
| Heartbeat interval | 15 seconds (configurable 5–120s) |
| Offline detection | Within 45 seconds (3× heartbeat interval) |
| Message buffer | 100 messages, 5-minute window |
| Log rotation | 50MB per file, 3 backups retained |
