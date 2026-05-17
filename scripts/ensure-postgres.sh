#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env}"

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing env file: $ENV_FILE"
  echo "Create .env from .env.example first."
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

POSTGRES_DB="${POSTGRES_DB:-agentbridge}"
POSTGRES_USER="${POSTGRES_USER:-agentbridge}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-agentbridge}"
DATABASE_URL="${DATABASE_URL:-}"

export PGPASSWORD="$POSTGRES_PASSWORD"

db_host=""
db_port="${POSTGRES_PORT:-5432}"
db_name="$POSTGRES_DB"

# Parse DATABASE_URL to extract host, port, and database name.
# Supports formats: postgres://user:pass@host:port/dbname?params
parse_database_url() {
  local rest authority hostport path port_part

  rest="${DATABASE_URL#*://}"
  rest="${rest%%\?*}"
  authority="${rest%%/*}"
  path="${rest#*/}"

  if [ "$authority" = "$rest" ]; then
    path=""
  fi

  hostport="${authority##*@}"

  if [[ "$hostport" == \[* ]]; then
    # IPv6 address in brackets
    db_host="${hostport#\[}"
    db_host="${db_host%%]*}"
    port_part="${hostport#*\]}"
    if [[ "$port_part" == :* ]] && [ -n "${port_part#:}" ]; then
      db_port="${port_part#:}"
    fi
  else
    db_host="${hostport%%:*}"
    if [[ "$hostport" == *:* ]] && [ -n "${hostport##*:}" ]; then
      db_port="${hostport##*:}"
    fi
  fi

  if [ -n "$path" ]; then
    db_name="${path%%/*}"
  fi
}

if [ -n "$DATABASE_URL" ]; then
  parse_database_url
fi

is_local() {
  [ -z "$DATABASE_URL" ] || [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ] || [ "$db_host" = "::1" ]
}

wait_for_ready() {
  local max_wait=30
  local elapsed=0

  echo "==> Waiting for PostgreSQL to be ready (max ${max_wait}s)..."
  while [ $elapsed -lt $max_wait ]; do
    if "$@" > /dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  echo "ERROR: PostgreSQL did not become ready within ${max_wait}s"
  exit 1
}

if is_local; then
  # ---------- Local: use Docker/Podman ----------
  echo "==> Ensuring PostgreSQL container is running on localhost:${db_port}..."
  docker compose up -d postgres

  wait_for_ready docker compose exec -T postgres pg_isready -U "$POSTGRES_USER" -d postgres

  echo "==> Ensuring database '$POSTGRES_DB' exists..."
  db_exists="$(docker compose exec -T postgres \
    psql -U "$POSTGRES_USER" -d postgres -Atqc "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"

  if [ "$db_exists" != "1" ]; then
    docker compose exec -T postgres \
      psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
      -c "CREATE DATABASE \"$POSTGRES_DB\"" \
      > /dev/null
    echo "==> Created database '$POSTGRES_DB'"
  fi

  echo "✓ PostgreSQL ready (local Docker). Database: $POSTGRES_DB on port $db_port"
else
  # ---------- Remote: skip Docker, verify connectivity ----------
  echo "==> Remote database detected (host: $db_host). Skipping Docker."
  if command -v pg_isready > /dev/null 2>&1; then
    wait_for_ready pg_isready -h "$db_host" -p "$db_port" -d "$db_name"
    echo "✓ PostgreSQL ready (remote: $db_host:$db_port). Database: $db_name"
  else
    echo "==> pg_isready not found. Skipping remote connectivity preflight."
    echo "✓ PostgreSQL configured (remote: $db_host:$db_port). Database: $db_name"
  fi
fi
