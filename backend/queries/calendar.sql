-- Календарь (FR-09) и история плановых задач.

-- name: ListCalendarEvents :many
SELECT e.*, p.code AS property_code, c.number AS contract_number
FROM calendar_events e
LEFT JOIN properties p ON p.id = e.property_id
LEFT JOIN contracts c ON c.id = e.contract_id
WHERE e.starts_on BETWEEN @from_date::date AND @to_date::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR e.property_id = sqlc.narg(property_id))
  AND (cardinality(@kinds::text[]) = 0 OR e.kind = ANY(@kinds::text[]))
ORDER BY e.starts_on, e.start_time NULLS FIRST, e.title;

-- name: GetCalendarEvent :one
SELECT e.*, p.code AS property_code, c.number AS contract_number
FROM calendar_events e
LEFT JOIN properties p ON p.id = e.property_id
LEFT JOIN contracts c ON c.id = e.contract_id
WHERE e.id = $1;

-- name: GetCalendarEventForUpdate :one
SELECT * FROM calendar_events WHERE id = $1 FOR UPDATE;

-- name: CreateManualEvent :one
INSERT INTO calendar_events (kind, title, starts_on, start_time, end_time, property_id, contract_id, source, notes,
                             created_by, created_at, updated_at)
VALUES (@kind, @title, @starts_on, sqlc.narg(start_time), sqlc.narg(end_time), sqlc.narg(property_id), sqlc.narg(contract_id),
        'manual', sqlc.narg(notes), sqlc.narg(created_by), @now, @now)
RETURNING id;

-- name: UpdateManualEvent :exec
UPDATE calendar_events
SET kind = @kind, title = @title, starts_on = @starts_on, start_time = sqlc.narg(start_time), end_time = sqlc.narg(end_time),
    property_id = sqlc.narg(property_id), contract_id = sqlc.narg(contract_id), notes = sqlc.narg(notes)
WHERE id = @id;

-- name: SetEventNotes :exec
UPDATE calendar_events SET notes = sqlc.narg(notes) WHERE id = @id;

-- name: DeleteCalendarEvent :exec
DELETE FROM calendar_events WHERE id = $1;

-- name: SetEventDone :exec
UPDATE calendar_events SET done = @done, done_at = sqlc.narg(done_at) WHERE id = @id;

-- name: SetAutoEventDone :exec
UPDATE calendar_events SET done = @done, done_at = sqlc.narg(done_at) WHERE auto_key = @auto_key;

-- name: MoveAutoEvent :exec
-- Перенести автособытие на дату и отметить выполненным (окончание договора при расторжении).
UPDATE calendar_events SET starts_on = @starts_on, done = true, done_at = @now WHERE auto_key = @auto_key;

-- name: DeleteFutureAutoEvents :exec
-- Удалить будущие автособытия договора, кроме указанного ключа (расторжение, 05 разд. 4.7).
DELETE FROM calendar_events
WHERE contract_id = @contract_id AND source = 'auto' AND starts_on > @after_date::date AND auto_key <> @keep_key;

-- name: DeleteAutoEventsByPrefix :exec
-- Удалить невыполненные автособытия с ключом, начинающимся с prefix (пересоздание при изменении сроков).
DELETE FROM calendar_events WHERE source = 'auto' AND NOT done AND auto_key LIKE @prefix::text || '%';

-- name: ScheduleContracts :many
-- Договоры для вычисляемых сроков оплаты в календаре: действующие, пересекающие диапазон.
SELECT c.*, p.code AS property_code FROM contracts c JOIN properties p ON p.id = c.property_id
WHERE c.status IN ('active', 'extended', 'terminated', 'finished')
  AND c.start_date <= @to_date::date AND c.actual_end_date >= @from_date::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id));

-- name: StartJobRun :one
INSERT INTO job_runs (job_name, business_date, trigger, started_at, status)
VALUES (@job_name, @business_date, @trigger, @now, 'running')
RETURNING id;

-- name: FinishJobRun :exec
UPDATE job_runs SET finished_at = @now, status = @status, stats = @stats, error_text = sqlc.narg(error_text) WHERE id = @id;

-- name: HasSucceededJobRun :one
SELECT EXISTS (SELECT 1 FROM job_runs WHERE job_name = @job_name AND business_date = @business_date AND status = 'succeeded')::boolean;

-- name: LastJobRuns :many
-- Последний запуск каждой задачи.
SELECT DISTINCT ON (job_name) * FROM job_runs ORDER BY job_name, started_at DESC;

-- name: ListJobRuns :many
SELECT * FROM job_runs WHERE (sqlc.narg(job_name)::text IS NULL OR job_name = sqlc.narg(job_name))
ORDER BY started_at DESC LIMIT @lim OFFSET @off;

-- name: CountJobRuns :one
SELECT count(*) FROM job_runs WHERE (sqlc.narg(job_name)::text IS NULL OR job_name = sqlc.narg(job_name));
