-- Пользователи (FR-14.1).

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, role, status, tenant_id, display_name, created_at, updated_at)
VALUES (@email, @role, 'invited', sqlc.narg(tenant_id), @display_name, @now, @now)
RETURNING *;

-- name: ActivateUser :one
UPDATE users
SET status = 'active', password_hash = @password_hash, password_changed_at = @now,
    policy_accepted_version = COALESCE(sqlc.narg(policy_version)::text, policy_accepted_version),
    policy_accepted_at = CASE WHEN sqlc.narg(policy_version)::text IS NULL THEN policy_accepted_at ELSE @now END
WHERE id = @id
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2, password_changed_at = $3, failed_login_count = 0, locked_until = NULL WHERE id = $1;

-- name: SetUserPasswordHash :exec
-- Пересчёт хеша с новыми параметрами argon2id при входе.
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: UpdateUser :one
UPDATE users SET display_name = $2, status = $3 WHERE id = $1 RETURNING *;

-- name: RecordLoginSuccess :exec
UPDATE users SET failed_login_count = 0, locked_until = NULL, last_login_at = $2 WHERE id = $1;

-- name: RecordLoginFailure :exec
UPDATE users SET failed_login_count = @failed_login_count, locked_until = sqlc.narg(locked_until) WHERE id = @id;

-- name: AcceptPolicy :exec
UPDATE users SET policy_accepted_version = $2, policy_accepted_at = $3 WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
WHERE (sqlc.narg(role)::text IS NULL OR role = sqlc.narg(role))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(q)::text IS NULL OR email ILIKE '%' || sqlc.narg(q) || '%' OR display_name ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY role, display_name, email
LIMIT @lim OFFSET @off;

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE (sqlc.narg(role)::text IS NULL OR role = sqlc.narg(role))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(q)::text IS NULL OR email ILIKE '%' || sqlc.narg(q) || '%' OR display_name ILIKE '%' || sqlc.narg(q) || '%');

-- name: ListActiveOwners :many
SELECT * FROM users WHERE role = 'owner' AND status = 'active' ORDER BY created_at;
