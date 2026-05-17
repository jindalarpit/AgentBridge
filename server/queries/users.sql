-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING id, email, password_hash, display_name, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, email, password_hash, display_name, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, display_name, created_at, updated_at
FROM users
WHERE email = $1;

-- name: UpdateUser :one
UPDATE users
SET display_name = $2, updated_at = now()
WHERE id = $1
RETURNING id, email, password_hash, display_name, created_at, updated_at;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: UserExists :one
SELECT EXISTS(SELECT 1 FROM users WHERE id = $1);
