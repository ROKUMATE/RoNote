-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES ($1, $2, now() + make_interval(secs => sqlc.arg(lifetime_seconds)::double precision));

-- name: GetSession :one
-- A session past its absolute limit, or unused for longer than the idle
-- limit, is no session. Stale says its last use is older than touch_seconds,
-- by the database's clock, which wrote it.
SELECT user_id, last_seen_at,
       (last_seen_at < now() - make_interval(secs => sqlc.arg(touch_seconds)::double precision))::boolean AS stale
FROM sessions
WHERE token_hash = $1
  AND expires_at > now()
  AND last_seen_at > now() - make_interval(secs => sqlc.arg(idle_seconds)::double precision);

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions
WHERE user_id = $1;
