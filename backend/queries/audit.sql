-- Журнал действий (FR-12, NFR-Sec-04).

-- name: InsertAudit :one
INSERT INTO audit_log (occurred_at, actor_user_id, actor_role, action, entity_type, entity_id, summary,
                       before, after, ip, user_agent, request_id)
VALUES (@occurred_at, sqlc.narg(actor_user_id), @actor_role, @action, @entity_type, @entity_id, @summary,
        sqlc.narg(before), sqlc.narg(after), sqlc.narg(ip), sqlc.narg(user_agent), sqlc.narg(request_id))
RETURNING id;

-- name: ListAudit :many
SELECT a.id, a.occurred_at, a.actor_user_id, a.actor_role, a.action, a.entity_type, a.entity_id, a.summary,
       a.before, a.after, a.ip, u.display_name AS actor_name
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (sqlc.narg(from_at)::timestamptz IS NULL OR a.occurred_at >= sqlc.narg(from_at))
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR a.occurred_at < sqlc.narg(to_at))
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR a.actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(entity_type)::text IS NULL OR a.entity_type = sqlc.narg(entity_type))
  AND (sqlc.narg(entity_id)::text IS NULL OR a.entity_id = sqlc.narg(entity_id))
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action))
  AND (sqlc.narg(q)::text IS NULL OR a.summary ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountAudit :one
SELECT count(*) FROM audit_log a
WHERE (sqlc.narg(from_at)::timestamptz IS NULL OR a.occurred_at >= sqlc.narg(from_at))
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR a.occurred_at < sqlc.narg(to_at))
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR a.actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(entity_type)::text IS NULL OR a.entity_type = sqlc.narg(entity_type))
  AND (sqlc.narg(entity_id)::text IS NULL OR a.entity_id = sqlc.narg(entity_id))
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action))
  AND (sqlc.narg(q)::text IS NULL OR a.summary ILIKE '%' || sqlc.narg(q) || '%');

-- name: AuditChain :many
-- Пересчёт хеша каждой записи по той же формуле, что в триггере audit_log_chain (04 Модель данных).
SELECT id, prev_hash, hash,
       digest(prev_hash || convert_to(concat_ws('|',
         id::text,
         to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
         COALESCE(actor_user_id::text, ''), actor_role, action, entity_type, entity_id,
         summary, COALESCE(before::text, ''), COALESCE(after::text, '')), 'UTF8'), 'sha256')::bytea AS computed
FROM audit_log
WHERE id > @after_id
ORDER BY id
LIMIT @lim;
