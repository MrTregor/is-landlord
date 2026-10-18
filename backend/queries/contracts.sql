-- Договоры (FR-03).

-- name: NextSequenceValue :one
-- Следующий номер в году (number_sequences); строка блокируется до конца транзакции.
INSERT INTO number_sequences (name, year, last_value) VALUES (@name, @year, 1)
ON CONFLICT (name, year) DO UPDATE SET last_value = number_sequences.last_value + 1
RETURNING last_value;

-- name: CreateContract :one
INSERT INTO contracts (number, legal_basis, property_id, tenant_id, start_date, end_date, first_due_date, deposit_kop,
                       utilities_mode, penalty_enabled, penalty_rate_pct, penalty_grace_days, penalty_cap_pct,
                       penalty_on_utilities, indexation_enabled, indexation_pct, indexation_period_months,
                       next_indexation_on, special_terms, notes, created_by, created_at, updated_at)
VALUES (@number, @legal_basis, @property_id, @tenant_id, @start_date, @end_date, @first_due_date, @deposit_kop,
        @utilities_mode, @penalty_enabled, sqlc.narg(penalty_rate_pct), @penalty_grace_days, sqlc.narg(penalty_cap_pct),
        @penalty_on_utilities, @indexation_enabled, sqlc.narg(indexation_pct), @indexation_period_months,
        sqlc.narg(next_indexation_on), sqlc.narg(special_terms), sqlc.narg(notes), sqlc.narg(created_by), @now, @now)
RETURNING *;

-- name: UpdateDraftContract :one
UPDATE contracts
SET legal_basis = @legal_basis, property_id = @property_id, tenant_id = @tenant_id, start_date = @start_date,
    end_date = @end_date, first_due_date = @first_due_date, deposit_kop = @deposit_kop, utilities_mode = @utilities_mode,
    penalty_enabled = @penalty_enabled, penalty_rate_pct = sqlc.narg(penalty_rate_pct), penalty_grace_days = @penalty_grace_days,
    penalty_cap_pct = sqlc.narg(penalty_cap_pct), penalty_on_utilities = @penalty_on_utilities,
    indexation_enabled = @indexation_enabled, indexation_pct = sqlc.narg(indexation_pct),
    indexation_period_months = @indexation_period_months, next_indexation_on = sqlc.narg(next_indexation_on),
    special_terms = sqlc.narg(special_terms), notes = sqlc.narg(notes)
WHERE id = @id
RETURNING *;

-- name: PatchContract :one
-- Прямо изменяемые условия действующего договора (05 разд. 4.5).
UPDATE contracts
SET penalty_enabled = @penalty_enabled, penalty_rate_pct = sqlc.narg(penalty_rate_pct), penalty_grace_days = @penalty_grace_days,
    penalty_cap_pct = sqlc.narg(penalty_cap_pct), penalty_on_utilities = @penalty_on_utilities,
    indexation_enabled = @indexation_enabled, indexation_pct = sqlc.narg(indexation_pct),
    indexation_period_months = @indexation_period_months, next_indexation_on = sqlc.narg(next_indexation_on),
    special_terms = sqlc.narg(special_terms), notes = sqlc.narg(notes),
    registration_done_on = sqlc.narg(registration_done_on), registration_number = sqlc.narg(registration_number)
WHERE id = @id
RETURNING *;

-- name: GetContract :one
SELECT * FROM contracts WHERE id = $1;

-- name: GetContractForUpdate :one
SELECT * FROM contracts WHERE id = $1 FOR UPDATE;

-- name: DeleteContract :exec
DELETE FROM contracts WHERE id = $1;

-- name: ActivateContract :one
UPDATE contracts
SET status = 'active', concluded_on = @concluded_on, activated_at = @now, activated_by = sqlc.narg(activated_by),
    next_indexation_on = sqlc.narg(next_indexation_on)
WHERE id = @id
RETURNING *;

-- name: TerminateContract :one
UPDATE contracts
SET status = 'terminated', termination_date = @termination_date, termination_initiator = @termination_initiator,
    termination_reason = @termination_reason, closed_at = @now
WHERE id = @id
RETURNING *;

-- name: ExtendContract :one
UPDATE contracts SET end_date = @end_date, status = 'extended' WHERE id = @id RETURNING *;

-- name: SetNextIndexation :exec
UPDATE contracts SET next_indexation_on = sqlc.narg(next_indexation_on) WHERE id = @id;

-- name: FinishContract :one
UPDATE contracts SET status = 'finished', closed_at = @now WHERE id = @id RETURNING *;

-- name: OverlappingContracts :many
-- Не черновые договоры объекта, пересекающиеся с периодом [from, to] (05 разд. 4.4, п. 3).
SELECT id, number, start_date, actual_end_date FROM contracts
WHERE property_id = @property_id AND status <> 'draft' AND id <> @exclude_id
  AND daterange(start_date, actual_end_date, '[]') && daterange(@from_date::date, @to_date::date, '[]')
ORDER BY start_date;

-- name: ListContracts :many
SELECT c.*, p.code AS property_code, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       (SELECT ct.rent_kop FROM contract_terms ct WHERE ct.contract_id = c.id AND ct.effective_from <= GREATEST(c.start_date, @today::date)
        ORDER BY ct.effective_from DESC LIMIT 1)::bigint AS rent_kop
FROM contracts c
JOIN properties p ON p.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
WHERE (cardinality(@statuses::text[]) = 0 OR c.status = ANY(@statuses::text[]))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(ending_before)::date IS NULL OR (c.status IN ('active', 'extended') AND c.end_date <= sqlc.narg(ending_before)))
  AND (sqlc.narg(q)::text IS NULL OR c.number ILIKE '%' || sqlc.narg(q) || '%' OR p.code ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY
  CASE WHEN @sort::text = 'startDate' THEN c.start_date END ASC,
  CASE WHEN @sort::text = '-startDate' THEN c.start_date END DESC,
  CASE WHEN @sort::text = 'endDate' THEN c.end_date END ASC,
  CASE WHEN @sort::text = '-endDate' THEN c.end_date END DESC,
  CASE WHEN @sort::text = 'number' THEN c.number END ASC,
  c.number DESC
LIMIT @lim OFFSET @off;

-- name: CountContracts :one
SELECT count(*) FROM contracts c
JOIN properties p ON p.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
WHERE (cardinality(@statuses::text[]) = 0 OR c.status = ANY(@statuses::text[]))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(ending_before)::date IS NULL OR (c.status IN ('active', 'extended') AND c.end_date <= sqlc.narg(ending_before)))
  AND (sqlc.narg(q)::text IS NULL OR c.number ILIKE '%' || sqlc.narg(q) || '%' OR p.code ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%');

-- name: ListPropertyContracts :many
-- Договоры объекта для вкладки «Договоры» карточки объекта.
SELECT c.*, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name
FROM contracts c JOIN tenants t ON t.id = c.tenant_id
WHERE c.property_id = $1
ORDER BY c.start_date DESC;

-- name: ContractsToFinish :many
-- Договоры, срок которых истёк (contracts.daily).
SELECT * FROM contracts WHERE status IN ('active', 'extended') AND end_date < @today::date ORDER BY number;

-- name: ContractsStartingBy :many
-- Действующие договоры, начавшиеся не позже даты, у которых объект ещё не «Сдан» на дату начала (contracts.daily).
SELECT c.* FROM contracts c JOIN properties p ON p.id = c.property_id
WHERE c.status IN ('active', 'extended') AND c.start_date <= @today::date AND c.actual_end_date >= @today::date
  AND p.status <> 'rented'
ORDER BY c.number;

-- name: ContractsEndedBy :many
-- Договоры, фактический конец которых раньше даты, а объект всё ещё «Сдан» по этому договору (contracts.daily).
SELECT c.* FROM contracts c
JOIN property_status_history h ON h.property_id = c.property_id AND h.valid_to IS NULL AND h.status = 'rented' AND h.contract_id = c.id
WHERE c.status IN ('terminated', 'finished') AND c.actual_end_date < @today::date
ORDER BY c.number;

-- name: ContractStartingOn :one
-- Не черновой договор объекта, начинающийся в дату (чтобы не освобождать объект между договорами).
SELECT * FROM contracts WHERE property_id = @property_id AND status IN ('active', 'extended') AND start_date = @on_date::date
LIMIT 1;

-- Условия и проживающие

-- name: InsertTerms :one
INSERT INTO contract_terms (contract_id, version, effective_from, rent_kop, utilities_fixed_kop, payment_day, amendment_id, created_at)
VALUES (@contract_id, @version, @effective_from, @rent_kop, @utilities_fixed_kop, @payment_day, sqlc.narg(amendment_id), @now)
RETURNING *;

-- name: UpdateFirstTerms :exec
UPDATE contract_terms SET effective_from = @effective_from, rent_kop = @rent_kop, utilities_fixed_kop = @utilities_fixed_kop,
  payment_day = @payment_day
WHERE contract_id = @contract_id AND version = 1;

-- name: ListTerms :many
SELECT * FROM contract_terms WHERE contract_id = $1 ORDER BY version;

-- name: ListTermsForContracts :many
SELECT * FROM contract_terms WHERE contract_id = ANY(@ids::uuid[]) ORDER BY contract_id, version;

-- name: DeleteTerms :exec
DELETE FROM contract_terms WHERE contract_id = $1;

-- name: ListOccupants :many
SELECT * FROM contract_occupants WHERE contract_id = $1 ORDER BY created_at, id;

-- name: DeleteOccupants :exec
DELETE FROM contract_occupants WHERE contract_id = $1;

-- name: InsertOccupant :exec
INSERT INTO contract_occupants (contract_id, full_name, birth_date, relation, created_at)
VALUES (@contract_id, @full_name, sqlc.narg(birth_date), sqlc.narg(relation), @now);

-- Статусы объекта по договору (05 разд. 4.4, 4.7, 4.8)

-- name: StatusHistoryFrom :many
-- Записи истории объекта, пересекающие дату и позже (для постановки «Сдан» с даты начала).
SELECT * FROM property_status_history
WHERE property_id = @property_id AND (valid_to IS NULL OR valid_to >= @from_date::date)
ORDER BY valid_from;

-- name: DeleteStatusHistory :exec
DELETE FROM property_status_history WHERE id = $1;

-- name: SetStatusHistoryValidTo :exec
UPDATE property_status_history SET valid_to = sqlc.narg(valid_to) WHERE id = @id;

-- Допсоглашения

-- name: ListAmendments :many
SELECT * FROM contract_amendments WHERE contract_id = $1 ORDER BY number;

-- name: GetAmendment :one
SELECT * FROM contract_amendments WHERE id = $1;

-- name: GetAmendmentForUpdate :one
SELECT * FROM contract_amendments WHERE id = $1 FOR UPDATE;

-- name: CreateAmendment :one
INSERT INTO contract_amendments (contract_id, number, kind, effective_from, new_rent_kop, new_utilities_fixed_kop,
                                 new_payment_day, new_end_date, body_text, auto_created, created_by, created_at, updated_at)
VALUES (@contract_id, COALESCE((SELECT max(number) + 1 FROM contract_amendments WHERE contract_id = @contract_id), 1),
        @kind, @effective_from, sqlc.narg(new_rent_kop), sqlc.narg(new_utilities_fixed_kop), sqlc.narg(new_payment_day),
        sqlc.narg(new_end_date), sqlc.narg(body_text), @auto_created, sqlc.narg(created_by), @now, @now)
RETURNING *;

-- name: UpdateAmendment :one
UPDATE contract_amendments
SET kind = @kind, effective_from = @effective_from, new_rent_kop = sqlc.narg(new_rent_kop),
    new_utilities_fixed_kop = sqlc.narg(new_utilities_fixed_kop), new_payment_day = sqlc.narg(new_payment_day),
    new_end_date = sqlc.narg(new_end_date), body_text = sqlc.narg(body_text)
WHERE id = @id
RETURNING *;

-- name: SignAmendment :one
UPDATE contract_amendments SET status = 'signed', signed_on = @signed_on, applied_at = @now WHERE id = @id RETURNING *;

-- name: SetAmendmentPrevEndDate :one
-- Дата окончания договора до продления (№ 107).
UPDATE contract_amendments SET prev_end_date = @prev_end_date WHERE id = @id RETURNING *;

-- name: CancelAmendment :one
UPDATE contract_amendments SET status = 'cancelled', cancel_reason = @cancel_reason WHERE id = @id RETURNING *;

-- name: CancelDraftAmendments :many
UPDATE contract_amendments SET status = 'cancelled', cancel_reason = @cancel_reason
WHERE contract_id = @contract_id AND status = 'draft'
RETURNING id;

-- name: LastRentIncrease :one
-- Дата последнего повышения платы: версия условий с платой выше предыдущей версии (нет строк — повышений не было).
SELECT ct.effective_from FROM contract_terms ct
JOIN contract_terms prev ON prev.contract_id = ct.contract_id AND prev.version = ct.version - 1
WHERE ct.contract_id = $1 AND ct.rent_kop > prev.rent_kop
ORDER BY ct.effective_from DESC LIMIT 1;

-- Акты

-- name: ListActs :many
SELECT * FROM contract_acts WHERE contract_id = $1 ORDER BY kind;

-- name: GetActForUpdate :one
SELECT * FROM contract_acts WHERE id = $1 FOR UPDATE;

-- name: GetAct :one
SELECT * FROM contract_acts WHERE id = $1;

-- name: CreateAct :one
INSERT INTO contract_acts (contract_id, kind, act_date, inventory, keys_count, created_by, created_at, updated_at)
VALUES (@contract_id, @kind, @act_date, @inventory, 0, sqlc.narg(created_by), @now, @now)
RETURNING *;

-- name: UpdateAct :one
UPDATE contract_acts
SET act_date = @act_date, inventory = @inventory, keys_count = @keys_count, condition_notes = sqlc.narg(condition_notes),
    damages = sqlc.narg(damages)
WHERE id = @id
RETURNING *;

-- name: SignAct :one
UPDATE contract_acts SET signed = true, signed_on = @signed_on WHERE id = @id RETURNING *;

-- name: ActReadings :many
-- Показания, внесённые в акт (черновые — submitted, после подписания — accepted).
SELECT * FROM meter_readings WHERE act_id = $1;

-- name: DeleteActReadings :exec
DELETE FROM meter_readings WHERE act_id = $1;

-- name: InsertActReading :exec
INSERT INTO meter_readings (meter_id, contract_id, act_id, period_month, reading_date, value, source, status, submitted_by, submitted_at)
VALUES (@meter_id, @contract_id, @act_id, @period_month, @reading_date, @value, @source, 'submitted', sqlc.narg(submitted_by), @now);

-- name: AcceptActReadings :exec
UPDATE meter_readings SET status = 'accepted', decided_by = sqlc.narg(decided_by), decided_at = @now WHERE act_id = @act_id;

-- name: ActivePropertyMeters :many
SELECT * FROM meters WHERE property_id = $1 AND archived_at IS NULL ORDER BY type, created_at;

-- name: PreviousReading :one
-- Последнее принятое показание счётчика до даты (подсказка «предыдущее: …» в акте).
SELECT value, reading_date FROM meter_readings
WHERE meter_id = @meter_id AND status = 'accepted' AND reading_date <= @on_date::date AND (act_id IS NULL OR act_id <> @act_id)
ORDER BY reading_date DESC, submitted_at DESC LIMIT 1;

-- name: CountActiveMeters :one
SELECT count(*) FROM meters WHERE property_id = $1 AND archived_at IS NULL;

-- Журнал по договору

-- name: ContractTimeline :many
-- Записи журнала по договору и связанным сущностям (допсоглашения, акты).
SELECT a.id, a.occurred_at, a.actor_user_id, a.actor_role, a.action, a.entity_type, a.entity_id, a.summary,
       a.before, a.after, a.ip, u.display_name AS actor_name
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (a.entity_type = 'contract' AND a.entity_id = @contract_id::text)
   OR (a.entity_type = 'amendment' AND a.entity_id IN (SELECT id::text FROM contract_amendments WHERE contract_id::text = @contract_id::text))
   OR (a.entity_type = 'act' AND a.entity_id IN (SELECT id::text FROM contract_acts WHERE contract_id::text = @contract_id::text))
ORDER BY a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountContractTimeline :one
SELECT count(*) FROM audit_log a
WHERE (a.entity_type = 'contract' AND a.entity_id = @contract_id::text)
   OR (a.entity_type = 'amendment' AND a.entity_id IN (SELECT id::text FROM contract_amendments WHERE contract_id::text = @contract_id::text))
   OR (a.entity_type = 'act' AND a.entity_id IN (SELECT id::text FROM contract_acts WHERE contract_id::text = @contract_id::text));

-- name: ContractsForIndexation :many
-- Договоры, по которым пора готовить черновик индексации (05 разд. 8.2).
SELECT * FROM contracts
WHERE status IN ('active', 'extended') AND indexation_enabled AND next_indexation_on IS NOT NULL
  AND next_indexation_on <= actual_end_date AND (next_indexation_on - sqlc.arg(days_before)::int) <= sqlc.arg(today)::date
ORDER BY number;

-- name: HasIndexationAmendment :one
SELECT EXISTS (SELECT 1 FROM contract_amendments WHERE contract_id = @contract_id AND kind = 'indexation'
               AND status <> 'cancelled' AND effective_from = @effective_from)::boolean;
