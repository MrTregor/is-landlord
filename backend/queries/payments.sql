-- Платежи (FR-06.1), распределения, возвраты переплаты, обеспечительный платёж (FR-05.2), расхождения (FR-07.3).

-- name: InsertPayment :one
INSERT INTO payments (contract_id, purpose, amount_kop, received_on, method, source, status, requested_charge_ids,
                      receipt_number, comment, created_by, created_at, updated_at)
VALUES (@contract_id, @purpose, @amount_kop, @received_on, @method, @source, 'succeeded', @requested_charge_ids,
        @receipt_number, sqlc.narg(comment), sqlc.narg(created_by), @now, @now)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = $1;

-- name: GetPaymentForUpdate :one
SELECT * FROM payments WHERE id = $1 FOR UPDATE;

-- name: CancelPayment :exec
UPDATE payments SET status = 'cancelled', cancelled_at = @now, cancelled_by = sqlc.narg(cancelled_by), cancel_reason = @reason
WHERE id = @id;

-- name: PaymentAdvance :one
-- Аванс платежа (примечание к схеме 04): сумма − активные распределения − возвраты − переносы в залог.
SELECT (p.amount_kop
   - COALESCE((SELECT sum(amount_kop) FROM payment_allocations a WHERE a.payment_id = p.id AND a.reversed_at IS NULL), 0)
   - COALESCE((SELECT sum(amount_kop) FROM refunds r WHERE r.payment_id = p.id), 0)
   - COALESCE((SELECT sum(amount_kop) FROM deposit_transactions d
               WHERE d.payment_id = p.id AND d.kind = 'receipt' AND d.from_advance AND d.cancelled_at IS NULL), 0))::bigint
FROM payments p WHERE p.id = $1;

-- name: ListPayments :many
SELECT p.*, c.number AS contract_number, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       COALESCE((SELECT sum(amount_kop) FROM payment_allocations a WHERE a.payment_id = p.id AND a.reversed_at IS NULL), 0)::bigint AS allocated_kop,
       (COALESCE((SELECT sum(amount_kop) FROM refunds r WHERE r.payment_id = p.id), 0)
        + COALESCE((SELECT sum(amount_kop) FROM deposit_transactions d
                    WHERE d.payment_id = p.id AND d.kind = 'receipt' AND d.from_advance AND d.cancelled_at IS NULL), 0))::bigint AS returned_kop,
       f.status AS fiscal_status
FROM payments p
JOIN contracts c ON c.id = p.contract_id
JOIN properties pr ON pr.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN fiscal_receipts f ON f.payment_id = p.id
WHERE (sqlc.narg(contract_id)::uuid IS NULL OR p.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status))
  AND (sqlc.narg(method)::text IS NULL OR p.method = sqlc.narg(method))
  AND (sqlc.narg(source)::text IS NULL OR p.source = sqlc.narg(source))
  AND (sqlc.narg(date_from)::date IS NULL OR p.received_on >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR p.received_on <= sqlc.narg(date_to))
  AND (sqlc.narg(q)::text IS NULL OR p.receipt_number ILIKE '%' || sqlc.narg(q) || '%' OR p.payer_name ILIKE '%' || sqlc.narg(q) || '%'
       OR c.number ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY
  CASE WHEN @sort::text = 'receivedOn' THEN p.received_on END ASC NULLS LAST,
  CASE WHEN @sort::text = 'amountKop' THEN p.amount_kop END ASC,
  CASE WHEN @sort::text = '-amountKop' THEN p.amount_kop END DESC,
  CASE WHEN @sort::text = 'createdAt' THEN p.created_at END ASC,
  CASE WHEN @sort::text = '-createdAt' THEN p.created_at END DESC,
  p.received_on DESC NULLS FIRST, p.created_at DESC
LIMIT @lim OFFSET @off;

-- name: GetPaymentRow :one
SELECT p.*, c.number AS contract_number, pr.id AS property_id, pr.code AS property_code,
       t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name,
       COALESCE((SELECT sum(amount_kop) FROM payment_allocations a WHERE a.payment_id = p.id AND a.reversed_at IS NULL), 0)::bigint AS allocated_kop,
       (COALESCE((SELECT sum(amount_kop) FROM refunds r WHERE r.payment_id = p.id), 0)
        + COALESCE((SELECT sum(amount_kop) FROM deposit_transactions d
                    WHERE d.payment_id = p.id AND d.kind = 'receipt' AND d.from_advance AND d.cancelled_at IS NULL), 0))::bigint AS returned_kop,
       f.status AS fiscal_status
FROM payments p
JOIN contracts c ON c.id = p.contract_id
JOIN properties pr ON pr.id = c.property_id
JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN fiscal_receipts f ON f.payment_id = p.id
WHERE p.id = $1;

-- name: CountPayments :one
SELECT count(*)::bigint
FROM payments p
JOIN contracts c ON c.id = p.contract_id
JOIN tenants t ON t.id = c.tenant_id
WHERE (sqlc.narg(contract_id)::uuid IS NULL OR p.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR c.tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(property_id)::uuid IS NULL OR c.property_id = sqlc.narg(property_id))
  AND (sqlc.narg(status)::text IS NULL OR p.status = sqlc.narg(status))
  AND (sqlc.narg(method)::text IS NULL OR p.method = sqlc.narg(method))
  AND (sqlc.narg(source)::text IS NULL OR p.source = sqlc.narg(source))
  AND (sqlc.narg(date_from)::date IS NULL OR p.received_on >= sqlc.narg(date_from))
  AND (sqlc.narg(date_to)::date IS NULL OR p.received_on <= sqlc.narg(date_to))
  AND (sqlc.narg(q)::text IS NULL OR p.receipt_number ILIKE '%' || sqlc.narg(q) || '%' OR p.payer_name ILIKE '%' || sqlc.narg(q) || '%'
       OR c.number ILIKE '%' || sqlc.narg(q) || '%'
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%');

-- name: PaymentAllocations :many
-- Распределения платежа (активные и отменённые) с начислениями.
SELECT pa.*, ch.title AS charge_title, ch.kind AS charge_kind, ch.period_month, ch.period_start, ch.period_end, ch.due_date
FROM payment_allocations pa JOIN charges ch ON ch.id = pa.charge_id
WHERE pa.payment_id = $1
ORDER BY pa.allocated_at, pa.id;

-- name: ReversePaymentAllocations :many
-- Отмена всех активных распределений платежа; возвращает затронутые начисления.
UPDATE payment_allocations SET reversed_at = @now, reversed_reason = @reason
WHERE payment_id = @payment_id AND reversed_at IS NULL
RETURNING charge_id;

-- name: InsertRefund :one
INSERT INTO refunds (payment_id, amount_kop, refunded_on, method, reason, created_by, created_at)
VALUES (@payment_id, @amount_kop, @refunded_on, @method, @reason, sqlc.narg(created_by), @now)
RETURNING *;

-- name: PaymentRefunds :many
SELECT * FROM refunds WHERE payment_id = $1 ORDER BY refunded_on, created_at;

-- name: PaymentHasActiveFiscal :one
-- Чек по платежу в статусе done, queued или wait запрещает отмену (05 разд. 10.5).
SELECT EXISTS (SELECT 1 FROM fiscal_receipts WHERE payment_id = $1 AND status IN ('done', 'queued', 'wait'));

-- name: PaymentFiscalReceipt :one
SELECT * FROM fiscal_receipts WHERE payment_id = $1;

-- name: PaymentReceiptDocument :one
-- Действующая квитанция платежа.
SELECT * FROM documents WHERE payment_id = $1 AND kind = 'payment_receipt' AND superseded_by IS NULL
ORDER BY created_at DESC LIMIT 1;

-- name: PaymentBankLine :one
SELECT * FROM bank_statement_lines WHERE payment_id = $1;

-- name: ReturnBankLineToUnmatched :exec
-- Отмена платежа из выписки: строка снова «не опознана» (05 разд. 10.5).
UPDATE bank_statement_lines SET status = 'unmatched', payment_id = NULL, contract_id = NULL, match_method = NULL WHERE id = $1;

-- name: ReopenUnidentified :exec
-- Расхождение «неопознанный» по строке выписки: создаётся или открывается заново.
INSERT INTO discrepancies (kind, bank_statement_line_id, amount_kop, detected_at)
VALUES ('unidentified', @line_id, @amount_kop, @now)
ON CONFLICT (bank_statement_line_id) WHERE kind = 'unidentified'
DO UPDATE SET status = 'open', resolution = NULL, resolution_note = NULL, resolved_by = NULL, resolved_at = NULL,
              contract_id = NULL, payment_id = NULL, detected_at = EXCLUDED.detected_at;

-- name: ClearPaymentBankLink :exec
UPDATE payments SET bank_statement_line_id = NULL WHERE id = $1;

-- name: MainDebtDueBy :one
-- Недоплата (05 разд. 10.7): остаток начислений основного долга со сроком не позже даты.
SELECT COALESCE(sum(amount_kop - paid_kop), 0)::bigint
FROM charges
WHERE contract_id = @contract_id AND status IN ('open', 'partially_paid') AND kind <> 'penalty' AND due_date <= @on_date::date;

-- Обеспечительный платёж

-- name: InsertDepositTx :one
INSERT INTO deposit_transactions (contract_id, kind, amount_kop, op_date, reason, payment_id, from_advance,
                                  maintenance_request_id, expense_id, created_by, created_at)
VALUES (@contract_id, @kind, @amount_kop, @op_date, sqlc.narg(reason), sqlc.narg(payment_id), @from_advance,
        sqlc.narg(maintenance_request_id), sqlc.narg(expense_id), sqlc.narg(created_by), @now)
RETURNING *;

-- name: DepositOperations :many
SELECT d.*, u.display_name AS created_by_name
FROM deposit_transactions d LEFT JOIN users u ON u.id = d.created_by
WHERE d.contract_id = $1
ORDER BY d.op_date, d.created_at, d.id;

-- name: DepositTotals :one
-- Внесено и остаток залога (без отменённых операций).
SELECT COALESCE(sum(amount_kop) FILTER (WHERE kind = 'receipt'), 0)::bigint AS received_kop,
       COALESCE(sum(CASE WHEN kind = 'receipt' THEN amount_kop ELSE -amount_kop END), 0)::bigint AS balance_kop
FROM deposit_transactions
WHERE contract_id = $1 AND cancelled_at IS NULL;

-- name: LastDepositOperation :one
-- Последняя неотменённая операция по договору (отменить можно только её, 05 разд. 9.5).
SELECT * FROM deposit_transactions WHERE contract_id = $1 AND cancelled_at IS NULL
ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: GetDepositTxForUpdate :one
SELECT * FROM deposit_transactions WHERE id = $1 FOR UPDATE;

-- name: CancelDepositTx :exec
UPDATE deposit_transactions SET cancelled_at = @now, cancel_reason = @reason WHERE id = @id;

-- name: DepositReceiptOfPayment :one
-- Внесение залога, созданное платежом purpose = deposit.
SELECT * FROM deposit_transactions WHERE payment_id = $1 AND kind = 'receipt' AND NOT from_advance AND cancelled_at IS NULL;

-- name: DepositOffsetOfPayment :one
SELECT * FROM deposit_transactions WHERE payment_id = $1 AND kind = 'offset' AND cancelled_at IS NULL;

-- name: PaymentHasReturns :one
-- По платежу оформлен возврат переплаты или перенос переплаты в залог.
SELECT (EXISTS (SELECT 1 FROM refunds r WHERE r.payment_id = @payment_id::uuid)
    OR EXISTS (SELECT 1 FROM deposit_transactions d WHERE d.payment_id = @payment_id::uuid AND d.from_advance AND d.cancelled_at IS NULL))::boolean AS has_returns;

-- Расхождения

-- name: InsertOverpayment :exec
-- Расхождение «переплата» (одно на платёж, 05 разд. 10.6); повтор ничего не делает.
INSERT INTO discrepancies (kind, contract_id, payment_id, amount_kop, detected_at)
VALUES ('overpayment', @contract_id, @payment_id, @amount_kop, @now)
ON CONFLICT (payment_id) WHERE kind = 'overpayment' DO NOTHING;

-- name: OpenOverpayments :many
-- Открытые расхождения «переплата» договора со статусом платежа (аванс — запросом PaymentAdvance).
SELECT d.id, d.payment_id, d.amount_kop, p.status AS payment_status
FROM discrepancies d JOIN payments p ON p.id = d.payment_id
WHERE d.contract_id = @contract_id AND d.kind = 'overpayment' AND d.status = 'open'
ORDER BY d.detected_at
FOR UPDATE OF d;

-- name: SetDiscrepancyAmount :exec
UPDATE discrepancies SET amount_kop = @amount_kop WHERE id = @id;

-- name: ResolveDiscrepancy :one
UPDATE discrepancies SET status = 'resolved', resolution = @resolution::text, resolution_note = sqlc.narg(note),
  resolved_by = sqlc.narg(resolved_by), resolved_at = @now::timestamptz
WHERE id = @id
RETURNING *;

-- name: OpenUnderpayment :one
SELECT * FROM discrepancies WHERE contract_id = $1 AND kind = 'underpayment' AND status = 'open' FOR UPDATE;

-- name: InsertUnderpayment :exec
INSERT INTO discrepancies (kind, contract_id, payment_id, amount_kop, detected_at)
VALUES ('underpayment', @contract_id, sqlc.narg(payment_id), @amount_kop, @now);

-- name: SetUnderpayment :exec
UPDATE discrepancies SET amount_kop = @amount_kop, payment_id = sqlc.narg(payment_id) WHERE id = @id;

-- name: OpenUnderpaymentsAll :many
-- Открытые недоплаты для billing.penalties_daily: остаток долга со сроком не позже даты обнаружения.
SELECT d.id, d.contract_id,
  COALESCE((SELECT sum(ch.amount_kop - ch.paid_kop) FROM charges ch
            WHERE ch.contract_id = d.contract_id AND ch.status IN ('open', 'partially_paid') AND ch.kind <> 'penalty'
              AND ch.due_date <= (d.detected_at AT TIME ZONE 'Europe/Moscow')::date), 0)::bigint AS debt_kop
FROM discrepancies d
WHERE d.kind = 'underpayment' AND d.status = 'open';

-- name: GetDiscrepancyForUpdate :one
SELECT * FROM discrepancies WHERE id = $1 FOR UPDATE;

-- name: ListDiscrepancies :many
SELECT d.*, c.number AS contract_number, t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name,
       t.middle_name, t.org_name, p.receipt_number, u.display_name AS resolved_by_name
FROM discrepancies d
LEFT JOIN contracts c ON c.id = d.contract_id
LEFT JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN payments p ON p.id = d.payment_id
LEFT JOIN users u ON u.id = d.resolved_by
WHERE (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR d.kind = sqlc.narg(kind))
  AND (sqlc.narg(contract_id)::uuid IS NULL OR d.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(payment_id)::uuid IS NULL OR d.payment_id = sqlc.narg(payment_id))
ORDER BY d.detected_at DESC, d.id DESC
LIMIT @lim OFFSET @off;

-- name: CountDiscrepancies :one
SELECT count(*)::bigint FROM discrepancies d
WHERE (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR d.kind = sqlc.narg(kind))
  AND (sqlc.narg(contract_id)::uuid IS NULL OR d.contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(payment_id)::uuid IS NULL OR d.payment_id = sqlc.narg(payment_id));

-- name: GetDiscrepancyRow :one
SELECT d.*, c.number AS contract_number, t.id AS tenant_id, t.kind AS tenant_kind, t.last_name, t.first_name,
       t.middle_name, t.org_name, p.receipt_number, u.display_name AS resolved_by_name
FROM discrepancies d
LEFT JOIN contracts c ON c.id = d.contract_id
LEFT JOIN tenants t ON t.id = c.tenant_id
LEFT JOIN payments p ON p.id = d.payment_id
LEFT JOIN users u ON u.id = d.resolved_by
WHERE d.id = $1;
