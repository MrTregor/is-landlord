-- Письма (FR-10).

-- name: GetEmailTemplate :one
SELECT * FROM email_templates WHERE code = $1;

-- name: InsertEmailLog :one
INSERT INTO email_log (template_code, to_email, subject, body_html, attachments, status, dedupe_key,
                       related_entity_type, related_entity_id, created_at)
VALUES (@template_code, @to_email, @subject, @body_html, @attachments, @status, @dedupe_key,
        sqlc.narg(related_entity_type), sqlc.narg(related_entity_id), @created_at)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING id;

-- name: GetEmailLog :one
SELECT * FROM email_log WHERE id = $1;

-- name: MarkEmailSent :exec
UPDATE email_log SET status = 'sent', attempts = attempts + 1, last_error = NULL, sent_at = $2 WHERE id = $1;

-- name: MarkEmailAttemptFailed :exec
UPDATE email_log SET status = $2, attempts = attempts + 1, last_error = $3 WHERE id = $1;

-- Шаблоны писем (FR-10.3)

-- name: ListEmailTemplates :many
SELECT * FROM email_templates ORDER BY recipient DESC, code;

-- name: UpdateEmailTemplate :one
UPDATE email_templates SET subject = @subject, body_html = @body_html, enabled = @enabled,
  updated_by = sqlc.narg(updated_by), updated_at = @now
WHERE code = @code
RETURNING *;

-- Журнал отправки (экран «Письма»)

-- name: ListEmailLog :many
SELECT * FROM email_log
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(template_code)::text IS NULL OR template_code = sqlc.narg(template_code))
  AND (sqlc.narg(date_from)::date IS NULL OR (created_at AT TIME ZONE 'Europe/Moscow')::date >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR (created_at AT TIME ZONE 'Europe/Moscow')::date <= sqlc.narg(date_to))
  AND (sqlc.narg(q)::text IS NULL OR to_email::text ILIKE '%' || sqlc.narg(q) || '%' OR subject ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY created_at DESC, id DESC
LIMIT @lim OFFSET @off;

-- name: CountEmailLog :one
SELECT count(*)::bigint FROM email_log
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(template_code)::text IS NULL OR template_code = sqlc.narg(template_code))
  AND (sqlc.narg(date_from)::date IS NULL OR (created_at AT TIME ZONE 'Europe/Moscow')::date >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR (created_at AT TIME ZONE 'Europe/Moscow')::date <= sqlc.narg(date_to))
  AND (sqlc.narg(q)::text IS NULL OR to_email::text ILIKE '%' || sqlc.narg(q) || '%' OR subject ILIKE '%' || sqlc.narg(q) || '%');

-- name: CountResends :one
-- Повторные отправки письма: dedupe_key с суффиксом «:resend:<n>».
SELECT count(*)::bigint FROM email_log WHERE dedupe_key LIKE @prefix::text || ':resend:%';

-- name: InsertResend :one
INSERT INTO email_log (template_code, to_email, subject, body_html, attachments, status, dedupe_key,
                       related_entity_type, related_entity_id, created_at)
SELECT src.template_code, src.to_email, src.subject, src.body_html, src.attachments, 'queued', @dedupe_key::text,
       src.related_entity_type, src.related_entity_id, @now::timestamptz
FROM email_log src WHERE src.id = @source_id
RETURNING *;

-- Плановые рассылки (07 Фоновые задачи, 8–10; 10 Уведомления, раздел 2)

-- name: ContractMailInfo :one
-- Адрес нанимателя и общие переменные письма по договору.
SELECT c.id, c.number, c.status, c.end_date, c.penalty_grace_days, t.kind AS tenant_kind, t.first_name, t.middle_name,
       t.email, t.anonymized_at, p.address AS property_address, p.code AS property_code
FROM contracts c JOIN tenants t ON t.id = c.tenant_id JOIN properties p ON p.id = c.property_id
WHERE c.id = $1;

-- name: DueMainCharges :many
-- Начисления основного долга с долгом и сроком оплаты @due_date (напоминание об оплате).
SELECT ch.id, ch.contract_id, ch.title, ch.due_date::date AS due_date, (ch.amount_kop - ch.paid_kop)::bigint AS outstanding_kop
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE c.status <> 'draft' AND ch.kind <> 'penalty' AND ch.status IN ('open', 'partially_paid')
  AND ch.due_date = @due_date::date AND ch.amount_kop > ch.paid_kop
ORDER BY ch.contract_id, ch.due_date, ch.created_at;

-- name: OverdueMainCharges :many
-- Просроченные начисления основного долга: срок + льгота договора раньше @today.
SELECT ch.id, ch.contract_id, ch.title, ch.due_date::date AS due_date, (ch.amount_kop - ch.paid_kop)::bigint AS outstanding_kop,
       (ch.due_date + c.penalty_grace_days + 1)::date AS first_overdue
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE c.status <> 'draft' AND ch.kind <> 'penalty' AND ch.status IN ('open', 'partially_paid')
  AND ch.amount_kop > ch.paid_kop AND ch.due_date + c.penalty_grace_days < @today::date
ORDER BY ch.contract_id, ch.due_date, ch.created_at;

-- name: AccruingPenaltiesOf :one
-- Начисляемые на сегодня пени договора (остаток).
SELECT COALESCE(sum(amount_kop - paid_kop), 0)::bigint FROM charges
WHERE contract_id = $1 AND kind = 'penalty' AND penalty_state = 'accruing' AND status IN ('open', 'partially_paid');

-- name: EndingContracts :many
-- Действующие договоры, срок которых заканчивается не позже @until (письмо об окончании договора).
SELECT id, number, start_date, end_date FROM contracts
WHERE status IN ('active', 'extended') AND end_date >= @today::date AND end_date <= @until::date
ORDER BY end_date, number;

-- name: MeterContracts :many
-- Действующие договоры с ЖКУ по счётчикам (напоминание о показаниях), у которых за месяц не по всем счётчикам
-- объекта переданы или приняты показания (№ 79).
SELECT c.id, c.number FROM contracts c
WHERE c.status IN ('active', 'extended') AND c.utilities_mode = 'meters' AND c.start_date <= @today::date
  AND EXISTS (SELECT 1 FROM meters m
              WHERE m.property_id = c.property_id AND m.archived_at IS NULL
                AND NOT EXISTS (SELECT 1 FROM meter_readings r
                                WHERE r.meter_id = m.id AND r.period_month = @month::date
                                  AND r.source IN ('owner', 'tenant') AND r.status IN ('submitted', 'accepted')))
ORDER BY c.number;

-- Утренняя сводка собственнику

-- name: DigestOverdue :many
-- Договоры с просроченным долгом основного долга.
SELECT c.id, c.number, p.code AS property_code, sum(ch.amount_kop - ch.paid_kop)::bigint AS overdue_kop
FROM charges ch JOIN contracts c ON c.id = ch.contract_id JOIN properties p ON p.id = c.property_id
WHERE c.status <> 'draft' AND ch.kind <> 'penalty' AND ch.status IN ('open', 'partially_paid')
  AND ch.amount_kop > ch.paid_kop AND ch.due_date + c.penalty_grace_days < @today::date
GROUP BY c.id, c.number, p.code
ORDER BY c.number;

-- name: DigestNewRequests :many
SELECT r.id, r.number, r.title, p.code AS property_code FROM maintenance_requests r JOIN properties p ON p.id = r.property_id
WHERE r.status = 'new' ORDER BY r.created_at;

-- name: DigestDiscrepancies :many
SELECT kind, count(*)::bigint AS n, sum(amount_kop)::bigint AS amount_kop FROM discrepancies
WHERE status = 'open' GROUP BY kind ORDER BY kind;

-- name: DigestFiscalErrors :one
SELECT count(*)::bigint FROM fiscal_receipts WHERE status IN ('fail', 'manual_required');

-- name: DigestQueuedFiscal :one
-- Чеки в очереди дольше часа (касса недоступна, № 63) — по часам сервера БД, как технические интервалы.
SELECT count(*)::bigint FROM fiscal_receipts WHERE status = 'queued' AND updated_at < now() - interval '1 hour';

-- name: DigestFailedJobs :many
-- Плановые задачи, завершившиеся с ошибкой за вчера и сегодня (бизнес-дата).
SELECT DISTINCT job_name FROM job_runs WHERE status = 'failed' AND business_date >= @since::date ORDER BY job_name;
