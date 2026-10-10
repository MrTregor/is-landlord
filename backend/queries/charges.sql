-- Начисления (FR-05), распределения и пени.

-- name: InsertAutoCharge :one
-- Автоначисление; повтор с тем же idempotency_key ничего не создаёт (нет строк в ответе).
INSERT INTO charges (contract_id, kind, title, period_month, period_start, period_end, due_date, amount_kop,
                     original_amount_kop, status, penalty_state, source_charge_id, calc_details, origin,
                     idempotency_key, created_at, updated_at)
VALUES (@contract_id, @kind, @title, sqlc.narg(period_month), sqlc.narg(period_start), sqlc.narg(period_end),
        sqlc.narg(due_date), @amount_kop, @amount_kop, @status, sqlc.narg(penalty_state), sqlc.narg(source_charge_id),
        @calc_details, 'auto', @idempotency_key, @now, @now)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;

-- name: InsertManualCharge :one
INSERT INTO charges (contract_id, kind, title, due_date, amount_kop, original_amount_kop, calc_details, origin,
                     created_by, created_at, updated_at)
VALUES (sqlc.arg(contract_id), sqlc.arg(kind), sqlc.arg(title), sqlc.arg(due_date)::date, sqlc.arg(amount_kop),
        sqlc.arg(amount_kop), sqlc.arg(calc_details), 'manual', sqlc.narg(created_by), sqlc.arg(now), sqlc.arg(now))
RETURNING id;

-- name: GetChargeForUpdate :one
SELECT * FROM charges WHERE id = $1 FOR UPDATE;

-- name: ChargeByKey :one
SELECT * FROM charges WHERE idempotency_key = $1;

-- name: ContractChargesForUpdate :many
-- Неотменённые начисления договора (пересчёт при допсоглашении и расторжении).
SELECT * FROM charges WHERE contract_id = $1 AND status <> 'cancelled' ORDER BY period_month NULLS LAST, created_at FOR UPDATE;

-- name: SetChargeAmount :exec
UPDATE charges SET amount_kop = @amount_kop, period_end = sqlc.narg(period_end), calc_details = @calc_details WHERE id = @id;

-- name: RefreshChargePaid :one
-- paid_kop = сумма активных распределений; статус по оплате (у отменённого не меняется).
UPDATE charges c
SET paid_kop = x.paid,
    status = CASE WHEN c.status = 'cancelled' THEN 'cancelled'
                  WHEN x.paid >= c.amount_kop THEN 'paid'
                  WHEN x.paid = 0 THEN 'open'
                  ELSE 'partially_paid' END
FROM (SELECT COALESCE(sum(amount_kop), 0)::bigint AS paid FROM payment_allocations
      WHERE charge_id = @id AND reversed_at IS NULL) x
WHERE c.id = @id
RETURNING c.*;

-- name: CancelCharge :exec
UPDATE charges SET status = 'cancelled', cancelled_at = @now, cancelled_by = sqlc.narg(cancelled_by), cancel_reason = @reason,
  idempotency_key = NULL, paid_kop = 0
WHERE id = @id;

-- name: InsertAdjustment :exec
INSERT INTO charge_adjustments (charge_id, kind, delta_kop, reason, created_by, created_at)
VALUES (@charge_id, @kind, @delta_kop, @reason, sqlc.narg(created_by), @now);

-- name: ListAdjustments :many
SELECT a.*, u.display_name AS created_by_name FROM charge_adjustments a LEFT JOIN users u ON u.id = a.created_by
WHERE a.charge_id = $1 ORDER BY a.created_at, a.id;

-- name: ActiveAllocations :many
-- Активные распределения на начисление, последние первыми (отмена излишка — с последних по времени).
SELECT pa.*, p.received_on, p.receipt_number FROM payment_allocations pa JOIN payments p ON p.id = pa.payment_id
WHERE pa.charge_id = $1 AND pa.reversed_at IS NULL
ORDER BY pa.allocated_at DESC, pa.id DESC;

-- name: ReverseAllocation :exec
UPDATE payment_allocations SET reversed_at = @now, reversed_reason = @reason WHERE id = @id;

-- name: InsertAllocation :exec
INSERT INTO payment_allocations (payment_id, charge_id, amount_kop, allocated_at) VALUES (@payment_id, @charge_id, @amount_kop, @now);

-- name: PaymentsWithAdvance :many
-- Платежи договора «за начисления» с авансом > 0 (05 разд. 10.4; аванс вычисляется, примечание к схеме 04).
SELECT p.id, p.received_on, p.created_at,
  (p.amount_kop
   - COALESCE((SELECT sum(amount_kop) FROM payment_allocations a WHERE a.payment_id = p.id AND a.reversed_at IS NULL), 0)
   - COALESCE((SELECT sum(amount_kop) FROM refunds r WHERE r.payment_id = p.id), 0)
   - COALESCE((SELECT sum(amount_kop) FROM deposit_transactions d
               WHERE d.payment_id = p.id AND d.kind = 'receipt' AND d.from_advance AND d.cancelled_at IS NULL), 0))::bigint AS advance_kop
FROM payments p
WHERE p.contract_id = $1 AND p.purpose = 'charges' AND p.status = 'succeeded'
ORDER BY p.received_on, p.created_at;

-- name: OpenChargesForAllocation :many
-- Порядок погашения (05 разд. 10.3, шаги 2–3): основной долг по сроку и виду, затем пени final/manual.
SELECT * FROM charges
WHERE contract_id = $1 AND status IN ('open', 'partially_paid') AND amount_kop > paid_kop
  AND NOT (kind = 'penalty' AND penalty_state = 'accruing')
ORDER BY (kind = 'penalty'), due_date,
  CASE kind WHEN 'rent' THEN 1 WHEN 'utilities' THEN 2 WHEN 'damage' THEN 3 WHEN 'other' THEN 4 ELSE 5 END, created_at
FOR UPDATE;

-- Пени

-- name: PenaltyBases :many
-- Начисления, по которым могут идти пени на дату (05 разд. 7.1): rent (и utilities при penalty_on_utilities),
-- договор с пенями не в черновике, первый день просрочки не позже вчерашнего.
SELECT ch.*, c.penalty_rate_pct AS rate_pct, c.penalty_grace_days AS grace_days, c.penalty_cap_pct AS cap_pct
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE c.penalty_enabled AND c.status <> 'draft' AND ch.status <> 'cancelled' AND ch.due_date IS NOT NULL
  AND (ch.kind = 'rent' OR (ch.kind = 'utilities' AND c.penalty_on_utilities))
  AND (ch.due_date + c.penalty_grace_days + 1) < sqlc.arg(today)::date
ORDER BY ch.contract_id, ch.due_date;

-- name: ChargePayments :many
-- Распределения на начисление с датами поступления платежей (для расчёта пеней).
SELECT p.received_on, pa.amount_kop FROM payment_allocations pa JOIN payments p ON p.id = pa.payment_id
WHERE pa.charge_id = $1 AND pa.reversed_at IS NULL AND p.received_on IS NOT NULL
ORDER BY p.received_on;

-- name: PenaltyOf :one
SELECT * FROM charges WHERE source_charge_id = $1 AND status <> 'cancelled' FOR UPDATE;

-- name: UpdatePenalty :exec
UPDATE charges SET amount_kop = @amount_kop, original_amount_kop = @amount_kop, penalty_state = @penalty_state,
  due_date = sqlc.narg(due_date), calc_details = @calc_details
WHERE id = @id;

-- name: SetPenaltyManual :exec
UPDATE charges SET amount_kop = @amount_kop, penalty_state = 'manual', due_date = @due_date WHERE id = @id;

-- Список и сводки

-- name: ListCharges :many
SELECT ch.*, c.number AS contract_number, c.legal_basis, p.id AS property_id, p.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name
FROM charges ch
JOIN contracts c ON c.id = ch.contract_id
JOIN properties p ON p.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
WHERE (sqlc.narg(contract_id)::uuid IS NULL OR ch.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (cardinality(@kinds::text[]) = 0 OR ch.kind = ANY(@kinds::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR ch.status = ANY(@statuses::text[]))
  AND (NOT @overdue::boolean OR (ch.status IN ('open', 'partially_paid') AND ch.due_date < @today::date AND ch.amount_kop > ch.paid_kop))
  AND (sqlc.narg(period_from)::date IS NULL OR ch.period_month >= sqlc.narg(period_from))
  AND (sqlc.narg(period_to)::date IS NULL OR ch.period_month <= sqlc.narg(period_to))
  AND (sqlc.narg(due_from)::date IS NULL OR ch.due_date >= sqlc.narg(due_from))
  AND (sqlc.narg(due_to)::date IS NULL OR ch.due_date <= sqlc.narg(due_to))
  AND (sqlc.narg(q)::text IS NULL OR ch.title ILIKE '%' || sqlc.narg(q) || '%' OR c.number ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY
  CASE WHEN @sort::text = '-dueDate' THEN ch.due_date END DESC NULLS LAST,
  CASE WHEN @sort::text = 'periodMonth' THEN ch.period_month END ASC NULLS LAST,
  CASE WHEN @sort::text = '-periodMonth' THEN ch.period_month END DESC NULLS LAST,
  CASE WHEN @sort::text = 'amountKop' THEN ch.amount_kop END ASC,
  CASE WHEN @sort::text = '-amountKop' THEN ch.amount_kop END DESC,
  ch.due_date ASC NULLS LAST, ch.created_at
LIMIT @lim OFFSET @off;

-- name: CountCharges :one
SELECT count(*)::bigint AS total,
       COALESCE(sum(ch.amount_kop) FILTER (WHERE ch.status <> 'cancelled'), 0)::bigint AS amount_kop,
       COALESCE(sum(ch.paid_kop) FILTER (WHERE ch.status <> 'cancelled'), 0)::bigint AS paid_kop
FROM charges ch
JOIN contracts c ON c.id = ch.contract_id
JOIN tenants t ON t.id = c.tenant_id
WHERE (sqlc.narg(contract_id)::uuid IS NULL OR ch.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (cardinality(@kinds::text[]) = 0 OR ch.kind = ANY(@kinds::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR ch.status = ANY(@statuses::text[]))
  AND (NOT @overdue::boolean OR (ch.status IN ('open', 'partially_paid') AND ch.due_date < @today::date AND ch.amount_kop > ch.paid_kop))
  AND (sqlc.narg(period_from)::date IS NULL OR ch.period_month >= sqlc.narg(period_from))
  AND (sqlc.narg(period_to)::date IS NULL OR ch.period_month <= sqlc.narg(period_to))
  AND (sqlc.narg(due_from)::date IS NULL OR ch.due_date >= sqlc.narg(due_from))
  AND (sqlc.narg(due_to)::date IS NULL OR ch.due_date <= sqlc.narg(due_to))
  AND (sqlc.narg(q)::text IS NULL OR ch.title ILIKE '%' || sqlc.narg(q) || '%' OR c.number ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%');

-- name: GetChargeRow :one
SELECT ch.*, c.number AS contract_number, c.legal_basis, p.id AS property_id, p.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name
FROM charges ch
JOIN contracts c ON c.id = ch.contract_id
JOIN properties p ON p.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
WHERE ch.id = $1;

-- name: ContractBalances :many
-- Долг, просрочка и начисляемые пени по договорам (карточка, списки).
SELECT ch.contract_id,
  COALESCE(sum(ch.amount_kop - ch.paid_kop) FILTER (WHERE NOT (ch.kind = 'penalty' AND ch.penalty_state = 'accruing')), 0)::bigint AS debt_kop,
  COALESCE(sum(ch.amount_kop - ch.paid_kop) FILTER (WHERE ch.kind <> 'penalty' AND ch.due_date < @today::date), 0)::bigint AS overdue_kop,
  COALESCE(sum(ch.amount_kop) FILTER (WHERE ch.kind = 'penalty' AND ch.penalty_state = 'accruing'), 0)::bigint AS penalties_accruing_kop
FROM charges ch
WHERE ch.contract_id = ANY(@ids::uuid[]) AND ch.status IN ('open', 'partially_paid')
GROUP BY ch.contract_id;

-- name: NextDueCharges :one
-- Ближайший срок оплаты по открытым начислениям не раньше даты и сумма к оплате на этот срок.
SELECT ch.due_date::date AS due_date, sum(ch.amount_kop - ch.paid_kop)::bigint AS amount_kop
FROM charges ch
WHERE ch.contract_id = @contract_id AND ch.status IN ('open', 'partially_paid') AND ch.due_date >= @today::date
GROUP BY ch.due_date ORDER BY ch.due_date LIMIT 1;

-- name: ChargesInRange :many
-- Начисления с сроком в диапазоне (календарь, 05 разд. 13.2).
SELECT ch.*, c.number AS contract_number, c.property_id, p.code AS property_code
FROM charges ch JOIN contracts c ON c.id = ch.contract_id JOIN properties p ON p.id = c.property_id
WHERE ch.status <> 'cancelled' AND ch.due_date BETWEEN @from_date::date AND @to_date::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
ORDER BY ch.due_date;

-- name: ContractMonthsWithRent :many
-- Месяцы, за которые у договоров есть начисления платы (чтобы не показывать прогноз для них).
SELECT DISTINCT contract_id, period_month::date AS period_month FROM charges
WHERE contract_id = ANY(@ids::uuid[]) AND kind = 'rent' AND status <> 'cancelled' AND period_month IS NOT NULL;

-- name: ContractsForRent :many
-- Договоры для billing.rent_daily (05 разд. 5.3).
SELECT * FROM contracts WHERE status IN ('active', 'extended', 'terminated', 'finished') AND start_date <= @horizon::date
ORDER BY number;

-- name: ActiveOwnerUsers :many
SELECT id, email FROM users WHERE role = 'owner' AND status = 'active' ORDER BY created_at;

-- name: DebtByProperty :many
SELECT c.property_id AS id, COALESCE(sum(ch.amount_kop - ch.paid_kop), 0)::bigint AS debt_kop
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE c.property_id = ANY(@ids::uuid[]) AND ch.status IN ('open', 'partially_paid')
  AND NOT (ch.kind = 'penalty' AND ch.penalty_state = 'accruing')
GROUP BY c.property_id;

-- name: DebtByTenant :many
SELECT c.tenant_id AS id, COALESCE(sum(ch.amount_kop - ch.paid_kop), 0)::bigint AS debt_kop
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE c.tenant_id = ANY(@ids::uuid[]) AND ch.status IN ('open', 'partially_paid')
  AND NOT (ch.kind = 'penalty' AND ch.penalty_state = 'accruing')
GROUP BY c.tenant_id;
