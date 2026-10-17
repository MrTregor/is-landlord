-- Отчёты (FR-11) и сводка (FR-14): исходные данные; расчёты — в пакете report.

-- name: IncomePaymentLines :many
-- Платежи «за начисления» периода (кассовый метод, 05 разд. 12.2) с активными распределениями; строка без начисления —
-- платёж без распределений (весь — аванс).
SELECT p.id AS payment_id, p.received_on::date AS received_on, p.receipt_number, p.amount_kop AS payment_kop,
       c.id AS contract_id, c.number AS contract_number, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       ch.kind AS charge_kind, ch.title AS charge_title, COALESCE(pa.amount_kop, 0)::bigint AS allocated_kop
FROM payments p
JOIN contracts c ON c.id = p.contract_id
JOIN properties pr ON pr.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN payment_allocations pa ON pa.payment_id = p.id AND pa.reversed_at IS NULL
LEFT JOIN charges ch ON ch.id = pa.charge_id
WHERE p.status = 'succeeded' AND p.purpose = 'charges' AND p.received_on BETWEEN @date_from::date AND @date_to::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
ORDER BY p.received_on, p.receipt_number, pa.allocated_at, pa.id;

-- name: IncomeDepositOps :many
-- Удержания из залога и переносы переплаты в залог периода (без отменённых), внесения и возвраты залога (справочно).
SELECT d.id, d.kind, d.from_advance, d.amount_kop, d.op_date, d.reason,
       c.id AS contract_id, c.number AS contract_number, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       p.receipt_number
FROM deposit_transactions d
JOIN contracts c ON c.id = d.contract_id
JOIN properties pr ON pr.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN payments p ON p.id = d.payment_id
WHERE d.cancelled_at IS NULL AND d.op_date BETWEEN @date_from::date AND @date_to::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
ORDER BY d.op_date, d.created_at;

-- name: IncomeRefunds :many
-- Возвраты переплаты периода (доход со знаком минус).
SELECT r.id, r.amount_kop, r.refunded_on, p.receipt_number,
       c.id AS contract_id, c.number AS contract_number, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name
FROM refunds r
JOIN payments p ON p.id = r.payment_id
JOIN contracts c ON c.id = p.contract_id
JOIN properties pr ON pr.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
WHERE r.refunded_on BETWEEN @date_from::date AND @date_to::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
ORDER BY r.refunded_on, r.created_at;

-- name: ReportExpenses :many
SELECT e.id, e.property_id, e.paid_on, e.amount_kop, e.description, e.category, e.counterparty
FROM expenses e
WHERE e.paid_on BETWEEN @date_from::date AND @date_to::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR e.property_id = sqlc.narg(property_id))
ORDER BY e.paid_on, e.created_at;

-- name: ChargedByProperty :many
-- Начислено (11 разд. 2): неотменённые начисления со сроком в периоде; начисляемые пени — по текущей сумме, если срок
-- их исходного начисления в периоде.
SELECT c.property_id, COALESCE(sum(ch.amount_kop), 0)::bigint AS charged_kop
FROM charges ch
JOIN contracts c ON c.id = ch.contract_id
LEFT JOIN charges src ON src.id = ch.source_charge_id
WHERE ch.status <> 'cancelled'
  AND COALESCE(ch.due_date, src.due_date) BETWEEN @date_from::date AND @date_to::date
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
GROUP BY c.property_id;

-- name: ReportProperties :many
-- Объекты для отчётов: неархивные и архивированные позже начала периода.
SELECT id, code, expected_rent_kop, status, archived_at FROM properties
WHERE (sqlc.narg(property_id)::uuid IS NULL OR id = sqlc.narg(property_id))
  AND (archived_at IS NULL OR (archived_at AT TIME ZONE 'Europe/Moscow')::date >= @date_from::date)
ORDER BY code;

-- name: StatusHistoryInRange :many
-- Интервалы истории статусов, пересекающиеся с периодом (05 разд. 2.2).
SELECT property_id, status, valid_from, valid_to FROM property_status_history
WHERE valid_from <= @date_to::date AND (valid_to IS NULL OR valid_to >= @date_from::date)
  AND (sqlc.narg(property_id)::uuid IS NULL OR property_id = sqlc.narg(property_id))
ORDER BY property_id, valid_from;

-- name: FirstStatusDates :many
SELECT property_id, min(valid_from)::date AS first_from FROM property_status_history GROUP BY property_id;

-- Задолженность (11 разд. 4)

-- name: DebtCharges :many
-- Неотменённые начисления, созданные не позднее даты, и их оплата платежами, поступившими не позднее даты.
SELECT ch.id, ch.contract_id, ch.kind, ch.due_date, ch.amount_kop, ch.title, ch.period_month,
       COALESCE((SELECT sum(pa.amount_kop) FROM payment_allocations pa JOIN payments p ON p.id = pa.payment_id
                 WHERE pa.charge_id = ch.id AND pa.reversed_at IS NULL AND p.received_on <= @as_of::date), 0)::bigint AS paid_kop
FROM charges ch
WHERE ch.status <> 'cancelled' AND (ch.created_at AT TIME ZONE 'Europe/Moscow')::date <= @as_of::date
ORDER BY ch.contract_id, ch.due_date NULLS LAST;

-- name: DebtContracts :many
-- Договоры (кроме черновиков): наниматель, объект, остаток залога, аванс и последний платёж на дату.
SELECT c.id, c.number, c.status, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       COALESCE((SELECT sum(CASE WHEN d.kind = 'receipt' THEN d.amount_kop ELSE -d.amount_kop END) FROM deposit_transactions d
                 WHERE d.contract_id = c.id AND d.cancelled_at IS NULL AND d.op_date <= @as_of::date), 0)::bigint AS deposit_kop,
       COALESCE((SELECT sum(p.amount_kop
                   - COALESCE((SELECT sum(a.amount_kop) FROM payment_allocations a WHERE a.payment_id = p.id AND a.reversed_at IS NULL), 0)
                   - COALESCE((SELECT sum(r.amount_kop) FROM refunds r WHERE r.payment_id = p.id AND r.refunded_on <= @as_of::date), 0)
                   - COALESCE((SELECT sum(d.amount_kop) FROM deposit_transactions d WHERE d.payment_id = p.id AND d.kind = 'receipt'
                               AND d.from_advance AND d.cancelled_at IS NULL AND d.op_date <= @as_of::date), 0))
                 FROM payments p WHERE p.contract_id = c.id AND p.status = 'succeeded' AND p.purpose = 'charges'
                   AND p.received_on <= @as_of::date), 0)::bigint AS advance_kop,
       (SELECT max(p.received_on) FROM payments p WHERE p.contract_id = c.id AND p.status = 'succeeded'
          AND p.received_on <= @as_of::date)::date AS last_payment_on
FROM contracts c JOIN properties pr ON pr.id = c.property_id JOIN tenants t ON t.id = c.tenant_id
WHERE c.status <> 'draft'
ORDER BY c.number;

-- name: OverdueCandidates :many
-- Начисления платы и ЖКУ со сроком в периоде (случаи просрочки, 11 разд. 4) и льгота договора.
SELECT ch.id, ch.contract_id, ch.kind, ch.title, ch.period_month, ch.due_date::date AS due_date, ch.amount_kop,
       c.number AS contract_number, c.penalty_grace_days, pr.id AS property_id, pr.code AS property_code
FROM charges ch JOIN contracts c ON c.id = ch.contract_id JOIN properties pr ON pr.id = c.property_id
WHERE ch.status <> 'cancelled' AND ch.kind IN ('rent', 'utilities')
  AND ch.due_date BETWEEN @date_from::date AND @date_to::date
ORDER BY ch.due_date, c.number;

-- name: ChargePaymentsByIDs :many
-- Оплата начислений по датам поступления (для даты полного погашения).
SELECT pa.charge_id, p.received_on::date AS received_on, pa.amount_kop
FROM payment_allocations pa JOIN payments p ON p.id = pa.payment_id
WHERE pa.charge_id = ANY(@ids::uuid[]) AND pa.reversed_at IS NULL AND p.status = 'succeeded'
ORDER BY pa.charge_id, p.received_on, pa.allocated_at;

-- Сводка (06 разд. 5)

-- name: DashboardCounters :one
SELECT
  (SELECT count(*) FROM maintenance_requests WHERE status = 'new')::bigint AS new_requests,
  (SELECT count(*) FROM discrepancies WHERE status = 'open')::bigint AS open_discrepancies,
  (SELECT count(*) FROM fiscal_receipts WHERE status IN ('fail', 'manual_required'))::bigint AS fiscal_problems,
  (SELECT count(*) FROM meter_readings WHERE status = 'submitted' AND act_id IS NULL)::bigint AS pending_readings,
  (SELECT count(*) FROM properties WHERE archived_at IS NULL)::bigint AS properties_total,
  (SELECT count(*) FROM properties WHERE archived_at IS NULL AND status = 'rented')::bigint AS properties_rented;

-- name: DashboardDebts :one
-- Задолженность на сегодня: срок наступил (и начисляемые пени); просрочено — срок + льгота прошли.
SELECT COALESCE(sum(ch.amount_kop - ch.paid_kop) FILTER (WHERE ch.due_date IS NULL OR ch.due_date <= @today::date), 0)::bigint AS debt_kop,
       COALESCE(sum(ch.amount_kop - ch.paid_kop) FILTER (WHERE ch.kind <> 'penalty'
         AND ch.due_date + c.penalty_grace_days < @today::date), 0)::bigint AS overdue_kop,
       count(DISTINCT ch.contract_id) FILTER (WHERE ch.kind <> 'penalty' AND ch.due_date + c.penalty_grace_days < @today::date)::bigint AS overdue_contracts
FROM charges ch JOIN contracts c ON c.id = ch.contract_id
WHERE ch.status IN ('open', 'partially_paid') AND ch.amount_kop > ch.paid_kop;

-- name: ContractDebtsToday :many
-- Долг по договорам на сегодня: начисления со сроком не позже сегодня и начисляемые пени.
SELECT ch.contract_id, sum(ch.amount_kop - ch.paid_kop)::bigint AS debt_kop
FROM charges ch
WHERE ch.status IN ('open', 'partially_paid') AND ch.amount_kop > ch.paid_kop AND (ch.due_date IS NULL OR ch.due_date <= @today::date)
GROUP BY ch.contract_id;
