-- Вход, сессии, токены (NFR-Sec-01).

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (email, ip, success, at) VALUES ($1, $2, $3, $4);

-- name: FailedAttemptsByEmail :one
-- Неудачные попытки по e-mail после начала окна и после последнего успешного входа.
SELECT count(*)::int AS failures, COALESCE(max(la.at), 'epoch'::timestamptz)::timestamptz AS last_failure
FROM login_attempts la
WHERE la.email = @email AND NOT la.success AND la.at > @since::timestamptz
  AND la.at > COALESCE((SELECT max(s.at) FROM login_attempts s WHERE s.email = @email AND s.success), '-infinity'::timestamptz);

-- name: FailedAttemptsByIP :one
SELECT count(*)::int FROM login_attempts WHERE ip = $1 AND NOT success AND at > $2;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)
VALUES (@user_id, @token_hash, @csrf_token, @now, @now, @expires_at, sqlc.narg(ip), sqlc.narg(user_agent))
RETURNING id;

-- name: GetSessionByTokenHash :one
SELECT s.id AS session_id, s.csrf_token, s.last_seen_at, s.expires_at, s.revoked_at, sqlc.embed(u)
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = $2 WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
-- Отзыв всех сессий пользователя, кроме указанной (NULL — всех).
UPDATE sessions SET revoked_at = @now
WHERE user_id = @user_id AND revoked_at IS NULL AND (sqlc.narg(except_id)::uuid IS NULL OR id <> sqlc.narg(except_id));

-- name: CreateAuthToken :one
INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: InvalidateAuthTokens :exec
UPDATE auth_tokens SET used_at = $3 WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL;

-- name: GetAuthToken :one
SELECT t.id AS token_id, t.expires_at, t.used_at, sqlc.embed(u)
FROM auth_tokens t JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.purpose = $2;

-- name: UseAuthToken :exec
UPDATE auth_tokens SET used_at = $2 WHERE id = $1;
