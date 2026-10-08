-- Счётчики и тарифы (FR-01.1, FR-05.5).

-- name: ListMeters :many
SELECT * FROM meters m WHERE m.property_id = $1 ORDER BY m.archived_at NULLS FIRST, m.type, m.created_at;

-- name: LastAcceptedReading :one
-- Последнее принятое показание счётчика.
SELECT value, reading_date, period_month FROM meter_readings
WHERE meter_id = $1 AND status = 'accepted'
ORDER BY reading_date DESC, submitted_at DESC LIMIT 1;

-- name: GetMeter :one
SELECT * FROM meters WHERE id = $1;

-- name: CreateMeter :one
INSERT INTO meters (property_id, type, serial_number, initial_value, initial_date, verification_due, note, created_at, updated_at)
VALUES (@property_id, @type, sqlc.narg(serial_number), @initial_value, @initial_date, sqlc.narg(verification_due),
        sqlc.narg(note), @now, @now)
RETURNING *;

-- name: UpdateMeter :one
UPDATE meters SET type = @type, serial_number = sqlc.narg(serial_number), initial_value = @initial_value,
  initial_date = @initial_date, verification_due = sqlc.narg(verification_due), note = sqlc.narg(note)
WHERE id = @id
RETURNING *;

-- name: ArchiveMeter :one
UPDATE meters SET archived_at = $2 WHERE id = $1 RETURNING *;

-- name: MeterHasReadings :one
SELECT EXISTS (SELECT 1 FROM meter_readings WHERE meter_id = $1)::boolean;

-- name: UpsertAutoEvent :exec
-- Автоматическое событие календаря (05 разд. 13.1); повтор по тому же ключу обновляет дату и название.
INSERT INTO calendar_events (kind, title, starts_on, property_id, contract_id, source, auto_key, created_at, updated_at)
VALUES (@kind, @title, @starts_on, sqlc.narg(property_id), sqlc.narg(contract_id), 'auto', @auto_key, @now, @now)
ON CONFLICT (auto_key) DO UPDATE SET title = EXCLUDED.title, starts_on = EXCLUDED.starts_on, done = false, done_at = NULL;

-- name: DeleteAutoEvent :exec
DELETE FROM calendar_events WHERE auto_key = $1;

-- name: ListTariffs :many
SELECT t.*, p.code AS property_code
FROM tariffs t LEFT JOIN properties p ON p.id = t.property_id
WHERE (sqlc.narg(meter_type)::text IS NULL OR t.meter_type = sqlc.narg(meter_type))
  AND (sqlc.narg(property_id)::uuid IS NULL OR t.property_id = sqlc.narg(property_id))
ORDER BY t.meter_type, p.code NULLS FIRST, t.effective_from DESC;

-- name: GetTariff :one
SELECT * FROM tariffs WHERE id = $1;

-- name: CreateTariff :one
INSERT INTO tariffs (meter_type, property_id, price, effective_from, note, created_by, created_at)
VALUES (@meter_type, sqlc.narg(property_id), @price, @effective_from, sqlc.narg(note), sqlc.narg(created_by), @now)
RETURNING *;

-- name: DeleteTariff :exec
DELETE FROM tariffs WHERE id = $1;

-- name: TariffInUse :one
-- Есть ли неотменённое начисление ЖКУ с месяцем не раньше даты тарифа по объекту, к которому тариф применим.
SELECT EXISTS (
  SELECT 1 FROM charges ch JOIN contracts c ON c.id = ch.contract_id
  WHERE ch.kind = 'utilities' AND ch.status <> 'cancelled' AND ch.period_month >= @effective_from::date
    AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
)::boolean;
