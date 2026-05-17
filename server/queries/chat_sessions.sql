-- name: CreateChatSession :one
INSERT INTO chat_sessions (user_id, title, status)
VALUES ($1, 'New Chat', 'active')
RETURNING id, user_id, runtime_id, title, status, created_at, updated_at;

-- name: GetChatSession :one
SELECT id, user_id, runtime_id, title, status, created_at, updated_at
FROM chat_sessions
WHERE id = $1;

-- name: GetChatSessionByIDAndUser :one
SELECT id, user_id, runtime_id, title, status, created_at, updated_at
FROM chat_sessions
WHERE id = $1 AND user_id = $2;

-- name: ListChatSessionsByUser :many
SELECT cs.id, cs.user_id, cs.runtime_id, cs.title, cs.status, cs.created_at, cs.updated_at
FROM chat_sessions cs
WHERE cs.user_id = $1 AND cs.status = 'active'
ORDER BY cs.updated_at DESC
LIMIT $2 OFFSET $3;

-- name: CountChatSessionsByUser :one
SELECT COUNT(*) FROM chat_sessions
WHERE user_id = $1 AND status = 'active';

-- name: UpdateChatSessionTitle :one
UPDATE chat_sessions
SET title = $2, updated_at = now()
WHERE id = $1
RETURNING id, user_id, runtime_id, title, status, created_at, updated_at;

-- name: UpdateChatSessionRuntime :exec
UPDATE chat_sessions
SET runtime_id = $2, updated_at = now()
WHERE id = $1;

-- name: DeleteChatSession :exec
DELETE FROM chat_sessions WHERE id = $1;

-- name: ArchiveChatSession :exec
UPDATE chat_sessions
SET status = 'archived', updated_at = now()
WHERE id = $1;
