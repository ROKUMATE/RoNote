-- name: CreateUser :one
INSERT INTO users (email, display_name)
VALUES ($1, $2)
RETURNING id, email, display_name, created_at;

-- name: GetUserByEmail :one
SELECT id, email, display_name, created_at
FROM users
WHERE lower(email) = lower(sqlc.arg(email));

-- name: UpsertUserByEmail :one
INSERT INTO users (email, display_name)
VALUES ($1, $2)
ON CONFLICT ((lower(email))) DO UPDATE SET email = users.email
RETURNING id, email, display_name, created_at;
