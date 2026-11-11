-- name: CreateUser :one
INSERT INTO users (email, display_name)
VALUES ($1, $2)
RETURNING id, email, display_name, created_at;

-- name: GetUserByEmail :one
SELECT id, email, display_name, created_at
FROM users
WHERE lower(email) = lower(sqlc.arg(email));

-- name: GetUser :one
SELECT id, email, display_name, created_at
FROM users
WHERE id = $1;

-- name: DeleteUser :execrows
-- Everything the user owns goes with them; shared sources stay.
DELETE FROM users
WHERE id = $1;

-- name: GetIdentity :one
SELECT provider, provider_user_id, user_id, login, created_at
FROM user_identities
WHERE provider = $1 AND provider_user_id = $2;

-- name: CreateIdentity :exec
INSERT INTO user_identities (provider, provider_user_id, user_id, login)
VALUES ($1, $2, $3, $4);

-- name: SetIdentityLogin :exec
UPDATE user_identities
SET login = $3
WHERE provider = $1 AND provider_user_id = $2 AND login <> $3;

-- name: GetAccount :one
SELECT u.id, u.email, u.display_name, i.login
FROM users u
JOIN user_identities i ON i.user_id = u.id
WHERE u.id = $1 AND i.provider = $2;
