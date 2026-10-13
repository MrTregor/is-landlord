-- Личный кабинет нанимателя (FR-13, 06 разд. 15): только договоры нанимателя пользователя.

-- name: TenantContracts :many
-- Договоры нанимателя (без черновиков): сначала действующие, затем по дате начала, новые первыми.
SELECT c.id, c.number, c.legal_basis, c.status, c.start_date, c.end_date, c.termination_date, c.deposit_kop,
       p.code AS property_code, p.address AS property_address, p.fiscal_mode
FROM contracts c JOIN properties p ON p.id = c.property_id
WHERE c.tenant_id = $1 AND c.status <> 'draft'
ORDER BY (c.status IN ('active', 'extended')) DESC, c.start_date DESC, c.number DESC;

-- name: CabinetPayments :many
-- Платежи договора, видимые нанимателю: проведённые и незавершённые онлайн (06 разд. 15).
SELECT p.*, f.ofd_receipt_url, f.npd_receipt_url, f.status AS fiscal_status
FROM payments p LEFT JOIN fiscal_receipts f ON f.payment_id = p.id
WHERE p.contract_id = $1 AND p.status IN ('succeeded', 'pending')
ORDER BY COALESCE(p.received_on, p.created_at::date) DESC, p.created_at DESC;
