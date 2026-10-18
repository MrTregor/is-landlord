-- Чеки (FR-06.2, FR-06.4; docs/spec/05 разд. 11, 08 разд. 3).

-- name: InsertFiscalReceipt :one
-- Один чек на платёж или возврат (уникальные индексы); повтор — нет строк в ответе.
-- updated_at — время сервера БД: по нему считаются технические интервалы (fiscal.reconcile, ожидание отчёта).
INSERT INTO fiscal_receipts (payment_id, refund_id, operation, provider, status, total_kop, created_at, updated_at)
VALUES (sqlc.narg(payment_id), sqlc.narg(refund_id), @operation, @provider, @status, @total_kop, @now, now())
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetFiscalReceipt :one
SELECT * FROM fiscal_receipts WHERE id = $1;

-- name: GetFiscalReceiptForUpdate :one
-- Чек под блокировкой и сколько секунд он в текущем состоянии (по часам сервера БД).
SELECT f.*, extract(epoch FROM now() - f.updated_at)::bigint AS age_seconds
FROM fiscal_receipts f WHERE f.id = $1 FOR UPDATE;

-- name: FiscalReceiptByUUID :one
SELECT * FROM fiscal_receipts WHERE external_uuid = $1;

-- name: RefundFiscalReceipt :one
SELECT * FROM fiscal_receipts WHERE refund_id = $1;

-- name: SetFiscalPayload :exec
UPDATE fiscal_receipts SET request_payload = @request_payload WHERE id = @id;

-- name: SetFiscalWait :exec
UPDATE fiscal_receipts
SET status = 'wait', external_uuid = @external_uuid, last_response = @last_response, attempts = attempts + 1,
    error_code = NULL, error_text = NULL
WHERE id = @id;

-- name: SetFiscalFail :one
-- attempts_inc — 1, если ошибка получена при отправке (новая попытка), 0 — при опросе отчёта.
UPDATE fiscal_receipts
SET status = 'fail', error_code = sqlc.narg(error_code), error_text = @error_text,
    last_response = COALESCE(sqlc.narg(last_response), last_response), attempts = attempts + @attempts_inc::int
WHERE id = @id
RETURNING *;

-- name: SetFiscalDone :one
UPDATE fiscal_receipts
SET status = 'done', last_response = @last_response, fiscal_receipt_number = @fiscal_receipt_number,
    shift_number = @shift_number, receipt_datetime = @receipt_datetime, fn_number = @fn_number,
    fiscal_document_number = @fiscal_document_number, fiscal_document_attribute = @fiscal_document_attribute,
    ofd_receipt_url = sqlc.narg(ofd_receipt_url), error_code = NULL, error_text = NULL
WHERE id = @id
RETURNING *;

-- name: RequeueFiscal :one
-- Повтор чека с ошибкой: запрос формируется заново (данные нанимателя могли быть исправлены).
UPDATE fiscal_receipts
SET status = 'queued', request_payload = NULL, external_uuid = NULL, error_code = NULL, error_text = NULL
WHERE id = @id
RETURNING *;

-- name: SetFiscalManual :one
UPDATE fiscal_receipts SET status = 'done', npd_receipt_url = @npd_receipt_url WHERE id = @id
RETURNING *;

-- name: StaleWaitFiscal :many
-- fiscal.reconcile: чеки в wait дольше минуты (07 Фоновые задачи).
SELECT id FROM fiscal_receipts WHERE status = 'wait' AND updated_at < now() - interval '1 minute' ORDER BY created_at;

-- name: StaleQueuedFiscal :many
-- fiscal.reconcile: чеки в queued дольше 5 минут.
SELECT id FROM fiscal_receipts WHERE status = 'queued' AND updated_at < now() - interval '5 minutes' ORDER BY created_at;

-- name: StuckQueuedFiscal :many
-- fiscal.reconcile: чеки в queued дольше суток — письмо собственникам (№ 63); queued_since — начало ожидания.
SELECT id, updated_at AS queued_since FROM fiscal_receipts
WHERE status = 'queued' AND updated_at < now() - interval '24 hours' ORDER BY created_at;

-- name: ListFiscalReceipts :many
SELECT f.*, COALESCE(p.receipt_number, rp.receipt_number) AS receipt_number,
       c.id AS contract_id, c.number AS contract_number
FROM fiscal_receipts f
LEFT JOIN refunds r ON r.id = f.refund_id
LEFT JOIN payments p ON p.id = f.payment_id
LEFT JOIN payments rp ON rp.id = r.payment_id
LEFT JOIN contracts c ON c.id = COALESCE(p.contract_id, rp.contract_id)
WHERE (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(date_from)::date IS NULL OR (f.created_at AT TIME ZONE 'Europe/Moscow')::date >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR (f.created_at AT TIME ZONE 'Europe/Moscow')::date <= sqlc.narg(date_to))
  AND (sqlc.narg(id)::uuid IS NULL OR f.id = sqlc.narg(id))
  AND (sqlc.narg(payment_id)::uuid IS NULL OR f.payment_id = sqlc.narg(payment_id))
ORDER BY f.created_at DESC, f.id DESC
LIMIT @lim OFFSET @off;

-- name: CountFiscalReceipts :one
SELECT count(*)::bigint FROM fiscal_receipts f
WHERE (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(date_from)::date IS NULL OR (f.created_at AT TIME ZONE 'Europe/Moscow')::date >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR (f.created_at AT TIME ZONE 'Europe/Moscow')::date <= sqlc.narg(date_to));

-- name: FiscalPaymentContext :one
-- Данные для чека: платёж, договор, наниматель, объект.
SELECT p.id AS payment_id, p.amount_kop, p.method, p.source, p.status AS payment_status, p.purpose, p.receipt_number,
       c.id AS contract_id, c.number AS contract_number, c.tenant_id,
       t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name, t.inn AS tenant_inn,
       t.email AS tenant_email, t.phone AS tenant_phone, t.anonymized_at
FROM payments p
JOIN contracts c ON c.id = p.contract_id
JOIN tenants t ON t.id = c.tenant_id
WHERE p.id = $1;

-- name: InsertAtolWebhookEvent :one
-- Колбэк АТОЛ сохраняется один раз (uuid:status); повтор — нет строк в ответе (08 разд. 3.4).
INSERT INTO webhook_events (provider, event_key, payload, received_at, processed_at, result)
VALUES ('atol', @event_key, @payload, @now, @now, @result)
ON CONFLICT (provider, event_key) DO NOTHING
RETURNING id;

-- name: GetRefund :one
SELECT * FROM refunds WHERE id = $1;

-- name: DBTimeAfter :one
-- Момент «через N секунд» по часам сервера БД: по ним River планирует задачи (не зависит от сдвига демо-даты).
SELECT (now() + make_interval(secs => @seconds::int))::timestamptz AS at;
