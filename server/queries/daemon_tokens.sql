-- name: CreateDaemonToken :one
INSERT INTO daemon_tokens (user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, name, token_hash, expires_at, created_at, last_used_at;

-- name: GetDaemonTokenByHash :one
SELECT id, user_id, name, token_hash, expires_at, created_at, last_used_at
FROM daemon_tokens
WHERE token_hash = $1 AND expires_at > now();

-- name: UpdateDaemonTokenLastUsed :exec
UPDATE daemon_tokens
SET last_used_at = now()
WHERE id = $1;

-- name: DeleteDaemonTokenByHash :exec
DELETE FROM daemon_tokens
WHERE token_hash = $1;

-- name: DeleteDaemonTokensByUserID :exec
DELETE FROM daemon_tokens
WHERE user_id = $1;
