-- Заявки на обслуживание (FR-08) и затраты (FR-05.6).

-- name: InsertRequest :one
INSERT INTO maintenance_requests (number, property_id, contract_id, author_id, author_role, category, title, description,
                                  priority, created_at, updated_at)
VALUES (@number, @property_id, sqlc.narg(contract_id), @author_id, @author_role, @category, @title, @description,
        @priority, @now, @now)
RETURNING *;

-- name: InsertRequestPhoto :exec
INSERT INTO maintenance_request_photos (request_id, file_id) VALUES (@request_id, @file_id) ON CONFLICT DO NOTHING;

-- name: InsertRequestEvent :one
INSERT INTO maintenance_request_events (request_id, author_id, kind, from_status, to_status, text, created_at)
VALUES (@request_id, sqlc.narg(author_id), @kind, sqlc.narg(from_status), sqlc.narg(to_status), sqlc.narg(text), @now)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM maintenance_requests WHERE id = $1;

-- name: GetRequestForUpdate :one
SELECT * FROM maintenance_requests WHERE id = $1 FOR UPDATE;

-- name: GetRequestRow :one
SELECT r.*, p.code AS property_code, c.number AS contract_number
FROM maintenance_requests r JOIN properties p ON p.id = r.property_id LEFT JOIN contracts c ON c.id = r.contract_id
WHERE r.id = $1;

-- name: SetRequestStatus :one
UPDATE maintenance_requests SET status = @status, result_note = COALESCE(sqlc.narg(result_note), result_note),
  done_at = CASE WHEN @status::text = 'done' THEN @now::timestamptz ELSE done_at END,
  closed_at = CASE WHEN @status::text = 'closed' THEN @now::timestamptz ELSE NULL END,
  updated_at = @now::timestamptz
WHERE id = @id
RETURNING *;

-- name: UpdateRequestFields :one
UPDATE maintenance_requests SET priority = @priority, contractor = sqlc.narg(contractor),
  planned_date = sqlc.narg(planned_date), tenant_fault = @tenant_fault, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: TouchRequest :exec
UPDATE maintenance_requests SET updated_at = @now WHERE id = @id;

-- name: ListRequests :many
SELECT r.*, p.code AS property_code, c.number AS contract_number
FROM maintenance_requests r JOIN properties p ON p.id = r.property_id LEFT JOIN contracts c ON c.id = r.contract_id
WHERE (cardinality(@statuses::text[]) = 0 OR r.status = ANY(@statuses::text[]))
  AND (sqlc.narg(property_id)::uuid IS NULL OR r.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(contract_id)::uuid IS NULL OR r.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(category)::text IS NULL OR r.category = sqlc.narg(category))
  AND (sqlc.narg(priority)::text IS NULL OR r.priority = sqlc.narg(priority))
  AND (sqlc.narg(q)::text IS NULL OR r.number ILIKE '%' || sqlc.narg(q) || '%' OR r.title ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY
  CASE WHEN @sort::text = 'createdAt' THEN r.created_at END ASC,
  CASE WHEN @sort::text = 'plannedDate' THEN r.planned_date END ASC NULLS LAST,
  CASE WHEN @sort::text = '-plannedDate' THEN r.planned_date END DESC NULLS LAST,
  CASE WHEN @sort::text = 'number' THEN r.number END ASC,
  CASE WHEN @sort::text = '-number' THEN r.number END DESC,
  r.created_at DESC, r.id DESC
LIMIT @lim OFFSET @off;

-- name: CountRequests :one
SELECT count(*)::bigint FROM maintenance_requests r
WHERE (cardinality(@statuses::text[]) = 0 OR r.status = ANY(@statuses::text[]))
  AND (sqlc.narg(property_id)::uuid IS NULL OR r.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(contract_id)::uuid IS NULL OR r.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(category)::text IS NULL OR r.category = sqlc.narg(category))
  AND (sqlc.narg(priority)::text IS NULL OR r.priority = sqlc.narg(priority))
  AND (sqlc.narg(q)::text IS NULL OR r.number ILIKE '%' || sqlc.narg(q) || '%' OR r.title ILIKE '%' || sqlc.narg(q) || '%');

-- name: RequestPhotos :many
SELECT f.* FROM maintenance_request_photos ph JOIN files f ON f.id = ph.file_id
WHERE ph.request_id = $1 AND f.deleted_at IS NULL ORDER BY f.created_at;

-- name: RequestEvents :many
SELECT e.*, u.display_name AS author_name, u.role AS author_role
FROM maintenance_request_events e LEFT JOIN users u ON u.id = e.author_id
WHERE e.request_id = $1 ORDER BY e.created_at, e.id;

-- name: ActiveContractOfProperty :one
-- Действующий договор объекта на дату (подставляется в новую заявку).
SELECT * FROM contracts
WHERE property_id = @property_id AND status IN ('active', 'extended') AND start_date <= @on_date::date AND actual_end_date >= @on_date::date
ORDER BY start_date DESC LIMIT 1;

-- name: LastContractOfProperty :one
-- Действующий или последний договор объекта (возмещение затраты за счёт нанимателя, 05 разд. 14.2).
SELECT * FROM contracts WHERE property_id = $1 AND status <> 'draft'
ORDER BY (status IN ('active', 'extended')) DESC, start_date DESC LIMIT 1;

-- name: OpenRequestsOfProperty :one
SELECT count(*)::bigint FROM maintenance_requests WHERE property_id = $1 AND status <> 'closed';

-- Затраты

-- name: InsertExpense :one
INSERT INTO expenses (property_id, category, amount_kop, paid_on, period_month, description, counterparty, file_id,
                      maintenance_request_id, recovery, created_by, created_at, updated_at)
VALUES (@property_id, @category, @amount_kop, @paid_on, @period_month, @description, sqlc.narg(counterparty),
        sqlc.narg(file_id), sqlc.narg(maintenance_request_id), 'none', sqlc.narg(created_by), @now, @now)
RETURNING *;

-- name: SetExpenseRecovery :one
UPDATE expenses SET recovery = @recovery, deposit_transaction_id = sqlc.narg(deposit_transaction_id),
  charge_id = sqlc.narg(charge_id)
WHERE id = @id
RETURNING *;

-- name: UpdateExpense :one
UPDATE expenses SET property_id = @property_id, category = @category, amount_kop = @amount_kop, paid_on = @paid_on,
  period_month = @period_month, description = @description, counterparty = sqlc.narg(counterparty),
  file_id = sqlc.narg(file_id), maintenance_request_id = sqlc.narg(maintenance_request_id), updated_at = @now
WHERE id = @id
RETURNING *;

-- name: GetExpenseForUpdate :one
SELECT * FROM expenses WHERE id = $1 FOR UPDATE;

-- name: DeleteExpense :exec
DELETE FROM expenses WHERE id = $1;

-- name: GetExpenseRow :one
SELECT e.*, p.code AS property_code, r.number AS request_number,
       f.original_name AS file_name, f.content_type AS file_content_type, f.size_bytes AS file_size
FROM expenses e JOIN properties p ON p.id = e.property_id
LEFT JOIN maintenance_requests r ON r.id = e.maintenance_request_id
LEFT JOIN files f ON f.id = e.file_id
WHERE e.id = $1;

-- name: ListExpenses :many
SELECT e.*, p.code AS property_code, r.number AS request_number,
       f.original_name AS file_name, f.content_type AS file_content_type, f.size_bytes AS file_size
FROM expenses e JOIN properties p ON p.id = e.property_id
LEFT JOIN maintenance_requests r ON r.id = e.maintenance_request_id
LEFT JOIN files f ON f.id = e.file_id
WHERE (sqlc.narg(property_id)::uuid IS NULL OR e.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(category)::text IS NULL OR e.category = sqlc.narg(category))
  AND (sqlc.narg(date_from)::date IS NULL OR e.paid_on >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR e.paid_on <= sqlc.narg(date_to))
  AND (sqlc.narg(request_id)::uuid IS NULL OR e.maintenance_request_id = sqlc.narg(request_id))
ORDER BY
  CASE WHEN @sort::text = 'paidOn' THEN e.paid_on END ASC,
  CASE WHEN @sort::text = 'amountKop' THEN e.amount_kop END ASC,
  CASE WHEN @sort::text = '-amountKop' THEN e.amount_kop END DESC,
  e.paid_on DESC, e.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountExpenses :one
SELECT count(*)::bigint AS total, COALESCE(sum(e.amount_kop), 0)::bigint AS amount_kop FROM expenses e
WHERE (sqlc.narg(property_id)::uuid IS NULL OR e.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(category)::text IS NULL OR e.category = sqlc.narg(category))
  AND (sqlc.narg(date_from)::date IS NULL OR e.paid_on >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR e.paid_on <= sqlc.narg(date_to))
  AND (sqlc.narg(request_id)::uuid IS NULL OR e.maintenance_request_id = sqlc.narg(request_id));

-- name: ExpensesOfProperty :one
-- Затраты объекта с даты оплаты (показатель карточки объекта «расходы с начала года»).
SELECT COALESCE(sum(amount_kop), 0)::bigint FROM expenses WHERE property_id = @property_id AND paid_on >= @since::date;
