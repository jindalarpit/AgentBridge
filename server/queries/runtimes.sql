-- name: CreateRuntime :one
INSERT INTO runtimes (daemon_id, agent_type, binary_path, version, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, daemon_id, agent_type, binary_path, version, status, created_at, updated_at;

-- name: GetRuntimeByID :one
SELECT id, daemon_id, agent_type, binary_path, version, status, created_at, updated_at
FROM runtimes
WHERE id = $1;

-- name: GetRuntimesByDaemonID :many
SELECT id, daemon_id, agent_type, binary_path, version, status, created_at, updated_at
FROM runtimes
WHERE daemon_id = $1
ORDER BY agent_type;

-- name: GetAvailableRuntimesByUserID :many
SELECT r.id, r.daemon_id, r.agent_type, r.binary_path, r.version, r.status, r.created_at, r.updated_at
FROM runtimes r
JOIN daemons d ON r.daemon_id = d.id
WHERE d.user_id = $1
  AND r.status = 'available'
  AND d.status = 'online'
ORDER BY r.agent_type;

-- name: DeleteRuntimesByDaemonID :exec
DELETE FROM runtimes WHERE daemon_id = $1;

-- name: MarkRuntimesOfflineByDaemonID :exec
UPDATE runtimes
SET status = 'offline', updated_at = now()
WHERE daemon_id = $1;

-- name: UpdateRuntimeStatus :exec
UPDATE runtimes
SET status = $2, updated_at = now()
WHERE id = $1;
