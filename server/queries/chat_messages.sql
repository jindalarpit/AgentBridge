-- name: InsertChatMessage :one
INSERT INTO chat_messages (chat_session_id, seq, role, content, status, elapsed_ms, failure_reason)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, chat_session_id, seq, role, content, status, elapsed_ms, failure_reason, created_at;

-- name: GetChatMessagesBySession :many
SELECT id, chat_session_id, seq, role, content, status, elapsed_ms, failure_reason, created_at
FROM chat_messages
WHERE chat_session_id = $1
ORDER BY seq ASC;

-- name: GetChatMessageByID :one
SELECT id, chat_session_id, seq, role, content, status, elapsed_ms, failure_reason, created_at
FROM chat_messages
WHERE id = $1;

-- name: GetRecentMessagesBySession :many
SELECT id, chat_session_id, seq, role, content, status, elapsed_ms, failure_reason, created_at
FROM chat_messages
WHERE chat_session_id = $1
ORDER BY seq DESC
LIMIT $2;

-- name: GetNextSeqForSession :one
SELECT COALESCE(MAX(seq), 0) + 1 AS next_seq
FROM chat_messages
WHERE chat_session_id = $1;

-- name: UpdateMessageStatus :exec
UPDATE chat_messages
SET status = $2, elapsed_ms = $3, failure_reason = $4
WHERE id = $1;

-- name: CountMessagesBySession :one
SELECT COUNT(*) FROM chat_messages
WHERE chat_session_id = $1;

-- name: DeleteMessagesBySession :exec
DELETE FROM chat_messages WHERE chat_session_id = $1;
