-- Показания счётчиков (FR-05.5).

-- name: MonthReadings :many
-- Принятые месячные показания счётчиков объекта за месяц (источник — собственник или наниматель).
SELECT r.* FROM meter_readings r JOIN meters m ON m.id = r.meter_id
WHERE m.property_id = @property_id AND r.period_month = @period_month AND r.status = 'accepted' AND r.source IN ('owner', 'tenant');

-- name: PrevAccepted :one
-- Предыдущее принятое показание счётчика раньше даты (любого источника, включая акты), кроме указанного.
SELECT value, reading_date FROM meter_readings
WHERE meter_id = @meter_id AND status = 'accepted' AND reading_date < sqlc.arg(before_date)::date AND id <> @exclude_id
ORDER BY reading_date DESC, submitted_at DESC LIMIT 1;

-- name: RecentAccepted :many
-- Последние принятые показания счётчика до даты (для правила «больше трёх средних»).
SELECT value, reading_date FROM meter_readings
WHERE meter_id = @meter_id AND status = 'accepted' AND reading_date < sqlc.arg(before_date)::date
ORDER BY reading_date DESC, submitted_at DESC LIMIT 4;

-- name: AllTariffs :many
SELECT * FROM tariffs ORDER BY effective_from;

-- name: ContractForPropertyMonth :one
-- Договор объекта (не черновик), срок которого пересекает месяц; при нескольких — начавшийся позже.
SELECT * FROM contracts
WHERE property_id = @property_id AND status <> 'draft'
  AND start_date <= (sqlc.arg(month_start)::date + interval '1 month' - interval '1 day')::date AND actual_end_date >= sqlc.arg(month_start)::date
ORDER BY start_date DESC LIMIT 1;

-- name: ListReadings :many
SELECT r.*, m.type AS meter_type, m.serial_number, m.property_id, p.code AS property_code, c.number AS contract_number,
       u.display_name AS submitted_by_name
FROM meter_readings r
JOIN meters m ON m.id = r.meter_id
JOIN properties p ON p.id = m.property_id
LEFT JOIN contracts c ON c.id = r.contract_id
LEFT JOIN users u ON u.id = r.submitted_by
WHERE NOT (r.act_id IS NOT NULL AND r.status = 'submitted')
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (sqlc.narg(property_id)::uuid IS NULL OR m.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(period_month)::date IS NULL OR r.period_month = sqlc.narg(period_month))
ORDER BY r.period_month DESC, p.code, m.type, r.submitted_at DESC
LIMIT @lim OFFSET @off;

-- name: CountReadings :one
SELECT count(*) FROM meter_readings r JOIN meters m ON m.id = r.meter_id
WHERE NOT (r.act_id IS NOT NULL AND r.status = 'submitted')
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status))
  AND (sqlc.narg(property_id)::uuid IS NULL OR m.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(period_month)::date IS NULL OR r.period_month = sqlc.narg(period_month));

-- name: GetReading :one
SELECT r.*, m.type AS meter_type, m.serial_number, m.property_id, p.code AS property_code, c.number AS contract_number,
       u.display_name AS submitted_by_name
FROM meter_readings r
JOIN meters m ON m.id = r.meter_id
JOIN properties p ON p.id = m.property_id
LEFT JOIN contracts c ON c.id = r.contract_id
LEFT JOIN users u ON u.id = r.submitted_by
WHERE r.id = $1;

-- name: GetReadingForUpdate :one
SELECT * FROM meter_readings WHERE id = $1 FOR UPDATE;

-- name: InsertOwnerReading :one
INSERT INTO meter_readings (meter_id, contract_id, period_month, reading_date, value, source, status, submitted_by,
                            submitted_at, decided_by, decided_at)
VALUES (@meter_id, sqlc.narg(contract_id), @period_month, @reading_date, @value, 'owner', 'accepted', sqlc.narg(user_id),
        @now, sqlc.narg(user_id), @now)
RETURNING id;

-- name: AcceptReading :exec
UPDATE meter_readings SET status = 'accepted', decided_by = sqlc.narg(decided_by), decided_at = @now WHERE id = @id;

-- name: RejectReading :exec
UPDATE meter_readings SET status = 'rejected', decided_by = sqlc.narg(decided_by), decided_at = @now, reject_reason = @reason
WHERE id = @id;

-- name: MonthReadingExists :one
SELECT EXISTS (SELECT 1 FROM meter_readings WHERE meter_id = @meter_id AND period_month = @period_month
               AND status <> 'rejected' AND source IN ('owner', 'tenant') AND id <> @exclude_id)::boolean;
