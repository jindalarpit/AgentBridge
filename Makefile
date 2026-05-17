.PHONY: help dev start stop check test db-up db-down db-reset migrate-up migrate-down server frontend clean

# ---------- Environment ----------

-include .env

POSTGRES_DB ?= agentbridge
POSTGRES_USER ?= agentbridge
POSTGRES_PASSWORD ?= agentbridge
POSTGRES_PORT ?= 5432
PORT ?= 8080
FRONTEND_PORT ?= 3000
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

export

COMPOSE := docker compose

.DEFAULT_GOAL := help

##@ Help

help: ## Show available make targets
	@awk 'BEGIN {FS = ":.*## "; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nQuick start:\n  \033[36mmake dev\033[0m      Full bootstrap (env, services, migrations, start)\n  \033[36mmake check\033[0m    Run full verification pipeline\n\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next} \
		/^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------- One-click commands ----------
##@ One-click

dev: ## Full bootstrap: create .env if missing, start services, run migrations, start server + frontend
	@if [ ! -f .env ]; then \
		echo "==> Creating .env from .env.example..."; \
		cp .env.example .env; \
	fi
	@echo "==> Starting compose services..."
	$(COMPOSE) up -d
	@bash scripts/ensure-postgres.sh .env
	@echo "==> Running migrations..."
	cd server && go run ./cmd/agentbridge migrate up
	@echo ""
	@echo "✓ Ready. Starting services..."
	@echo "  Backend:  http://localhost:$(PORT)"
	@echo "  Frontend: http://localhost:$(FRONTEND_PORT)"
	@echo ""
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/agentbridge) & \
		(cd frontend && npm run dev) & \
		wait

start: ## Start server + frontend (assumes compose services already running)
	@echo "Backend:  http://localhost:$(PORT)"
	@echo "Frontend: http://localhost:$(FRONTEND_PORT)"
	@trap 'kill 0' EXIT; \
		(cd server && go run ./cmd/agentbridge) & \
		(cd frontend && npm run dev) & \
		wait

stop: ## Stop server + frontend processes
	@echo "Stopping services..."
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null
	@-lsof -ti:$(FRONTEND_PORT) | xargs kill -9 2>/dev/null
	@echo "✓ App processes stopped."

check: ## Run full verification pipeline (typecheck, Go tests, frontend tests)
	@echo "==> Running Go tests..."
	@bash scripts/ensure-postgres.sh .env
	cd server && go test ./...
	@echo "==> Running frontend typecheck..."
	cd frontend && npm run typecheck
	@echo "==> Running frontend tests..."
	cd frontend && npm run test
	@echo ""
	@echo "✓ All checks passed."

# ---------- Database ----------
##@ Database

db-up: ## Start compose services (PostgreSQL)
	$(COMPOSE) up -d

db-down: ## Stop compose services
	$(COMPOSE) down

db-reset: ## Drop and recreate the database, then re-run migrations
	@bash scripts/ensure-postgres.sh .env
	@echo "==> Dropping and recreating database '$(POSTGRES_DB)'..."
	$(COMPOSE) exec -T postgres psql -U $(POSTGRES_USER) -d postgres -v ON_ERROR_STOP=1 \
		-c "DROP DATABASE IF EXISTS \"$(POSTGRES_DB)\" WITH (FORCE);" \
		-c "CREATE DATABASE \"$(POSTGRES_DB)\";"
	@echo "==> Running migrations..."
	cd server && go run ./cmd/agentbridge migrate up
	@echo "✓ Database '$(POSTGRES_DB)' reset."

migrate-up: ## Apply database migrations
	@bash scripts/ensure-postgres.sh .env
	cd server && go run ./cmd/agentbridge migrate up

migrate-down: ## Roll back database migrations
	@bash scripts/ensure-postgres.sh .env
	cd server && go run ./cmd/agentbridge migrate down

# ---------- Testing ----------
##@ Testing

test: ## Run Go tests (ensures DB is ready first)
	@bash scripts/ensure-postgres.sh .env
	cd server && go test ./...

# ---------- Individual commands ----------
##@ Individual

server: ## Run only the Go server
	@bash scripts/ensure-postgres.sh .env
	cd server && go run ./cmd/agentbridge

frontend: ## Run only the Next.js frontend
	cd frontend && npm run dev

# ---------- Cleanup ----------
##@ Cleanup

clean: ## Remove generated binaries and temp files
	rm -rf server/bin server/tmp
