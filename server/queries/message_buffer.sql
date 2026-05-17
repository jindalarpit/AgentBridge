-- name: AddToMessageBuffer :one
INSERT INTO message_buffer (user_id, payload, expires_at)
VALUES ($1, $2, now() + INTERVAL '5 minutes')
RETURNING id, user_id, payload, created_at, expires_at;

-- name: DrainMessageBuffer :many
SELECT id, user_id, payload, created_at, expires_at
FROM message_buffer
WHERE user_id = $1
  AND expires_at > now()
ORDER BY created_at ASC
LIMIT 100;

-- name: DeleteBufferedMessages :exec
DELETE FROM message_buffer
WHERE user_id = $1
  AND id = ANY($2::uuid[]);

-- name: CleanupExpiredBufferMessages :exec
DELETE FROM message_buffer
WHERE expires_at <= now();

-- name: CountBufferedMessages :one
SELECT COUNT(*) FROM message_buffer
WHERE user_id = $1 AND expires_at > now();
