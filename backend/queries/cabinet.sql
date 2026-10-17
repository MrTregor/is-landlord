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

-- name: CabinetDocuments :many
-- Документы договора, видимые нанимателю (15 разд. 7): договор, акты, допсоглашения, акты сверки — без заменённых.
SELECT * FROM documents
WHERE contract_id = $1 AND visible_to_tenant AND superseded_by IS NULL
  AND kind NOT IN ('payment_receipt', 'pdn_consent')
ORDER BY created_at DESC, id DESC;

-- name: ActiveConsent :one
-- Действующее (не отозванное) согласие нанимателя на обработку ПДн.
SELECT * FROM pdn_consents WHERE tenant_id = $1 AND revoked_on IS NULL ORDER BY given_on DESC, created_at DESC LIMIT 1;
