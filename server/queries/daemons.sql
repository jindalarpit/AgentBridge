-- name: UpsertDaemon :one
INSERT INTO daemons (user_id, daemon_id, status, last_seen_at)
VALUES ($1, $2, 'online', now())
ON CONFLICT (daemon_id) DO UPDATE
SET user_id = EXCLUDED.user_id,
    status = 'online',
    last_seen_at = now(),
    updated_at = now()
RETURNING id, user_id, daemon_id, status, last_seen_at, created_at, updated_at;

-- name: GetDaemonByDaemonID :one
SELECT id, user_id, daemon_id, status, last_seen_at, created_at, updated_at
FROM daemons
WHERE daemon_id = $1;

-- name: GetDaemonByID :one
SELECT id, user_id, daemon_id, status, last_seen_at, created_at, updated_at
FROM daemons
WHERE id = $1;

-- name: GetDaemonsByUserID :many
SELECT id, user_id, daemon_id, status, last_seen_at, created_at, updated_at
FROM daemons
WHERE user_id = $1
ORDER BY last_seen_at DESC;

-- name: UpdateDaemonHeartbeat :exec
UPDATE daemons
SET last_seen_at = now(), updated_at = now()
WHERE daemon_id = $1;

-- name: MarkDaemonOffline :exec
UPDATE daemons
SET status = 'offline', updated_at = now()
WHERE daemon_id = $1;

-- name: MarkDaemonOfflineByID :exec
UPDATE daemons
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: GetStaleDaemons :many
SELECT id, user_id, daemon_id, status, last_seen_at, created_at, updated_at
FROM daemons
WHERE status = 'online'
  AND last_seen_at < $1;

-- name: DeleteDaemon :exec
DELETE FROM daemons WHERE id = $1;
