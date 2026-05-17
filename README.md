# AgentBridge

A distributed chat system that enables users to interact with locally-running AI agent CLIs (Claude, Gemini, Kiro, Codex, etc.) through a web-based chat interface. A central server relays messages between the browser and per-machine daemons that detect and invoke local agent CLIs in real time.

## Architecture

- **Server** — Go backend (Chi router, gorilla/websocket, sqlc/PostgreSQL) handling auth, session state, message persistence, and WebSocket relay
- **Daemon** — Go CLI background process running on each user's machine for agent detection, server registration, heartbeats, and agent CLI invocation
- **Frontend** — Next.js (React, Zustand, WebSocket) providing the chat UI with real-time message streaming

## Prerequisites

- Go 1.21+
- Node.js 18+
- Docker or Podman (for PostgreSQL and MailHog)
- PostgreSQL 17 (provided via Docker Compose)

## Quick Start

```bash
# Clone the repository
git clone https://github.com/user/agentbridge.git
cd agentbridge

# Start everything (services, migrations, server, frontend)
make dev
```

This will:
1. Create a `.env` file from `.env.example` if one doesn't exist
2. Start PostgreSQL and MailHog via Docker Compose
3. Run database migrations
4. Start the Go server and Next.js frontend

## Podman Compatibility

All development infrastructure uses standard `docker compose` commands, which work transparently with [Podman](https://podman.io/) when the `DOCKER_HOST` environment variable is configured. No changes to the Makefile or scripts are needed.

### macOS (Podman Machine)

Podman on macOS runs containers inside a Linux VM managed by `podman machine`. Export the socket path:

```bash
export DOCKER_HOST="unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
```

### Linux (Rootless Podman)

On Linux with rootless Podman, the socket is available under `XDG_RUNTIME_DIR`:

```bash
export DOCKER_HOST="unix://${XDG_RUNTIME_DIR}/podman/podman.sock"
```

### Persisting the Configuration

Add the appropriate `DOCKER_HOST` export to your project `.env` file so all Makefile targets pick it up automatically:

```bash
# .env (uncomment and adjust for your setup)
# DOCKER_HOST=unix:///run/user/1000/podman/podman.sock        # Linux
# DOCKER_HOST=unix:///Users/you/.local/share/containers/podman/machine/podman.sock  # macOS (path varies)
```

Or add it to your shell profile (`~/.bashrc`, `~/.zshrc`) for system-wide effect.

### Verification

After setting `DOCKER_HOST`, verify that compose commands work:

```bash
docker compose version   # Should print Compose version info
docker compose ps        # Should list running services (if any)
```

All Makefile targets (`make dev`, `make db-up`, `make db-down`, `make db-reset`, etc.) use `docker compose` under the hood and will work identically with Podman once `DOCKER_HOST` is set.

## Available Make Targets

| Target | Description |
|--------|-------------|
| `make dev` | Full bootstrap: create .env, start services, run migrations, start server + frontend |
| `make start` | Start server + frontend (assumes services are already running) |
| `make stop` | Stop server + frontend processes |
| `make db-up` | Start Docker Compose services (PostgreSQL, MailHog) |
| `make db-down` | Stop Docker Compose services |
| `make db-reset` | Stop services, remove volumes, restart fresh |
| `make migrate-up` | Run database migrations |
| `make migrate-down` | Roll back database migrations |
| `make test` | Run Go tests |
| `make check` | Full verification pipeline (typecheck, Go tests, frontend tests) |

## Project Structure

```
AgentBridge/
├── server/          # Go backend (Chi, gorilla/websocket, sqlc)
├── daemon/          # Go daemon CLI (agent detection, execution)
├── frontend/        # Next.js frontend (React, Zustand)
├── docker-compose.yml
├── Makefile
└── README.md
```

## License

See [LICENSE](LICENSE) for details.
