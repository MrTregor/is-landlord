-- Шаблоны документов и документы (FR-04, docs/spec/09 Документы и шаблоны.md).

-- name: ActiveTemplates :many
SELECT t.*, u.display_name AS author
FROM document_templates t LEFT JOIN users u ON u.id = t.created_by
WHERE t.is_active
ORDER BY t.kind;

-- name: ActiveTemplate :one
SELECT * FROM document_templates WHERE kind = @kind AND is_active;

-- name: TemplateVersions :many
SELECT t.id, t.version, t.created_at, u.display_name AS author
FROM document_templates t LEFT JOIN users u ON u.id = t.created_by
WHERE t.kind = @kind
ORDER BY t.version DESC;

-- name: GetTemplateVersion :one
SELECT t.*, u.display_name AS author
FROM document_templates t LEFT JOIN users u ON u.id = t.created_by
WHERE t.kind = @kind AND t.id = @id;

-- name: SystemTemplate :one
SELECT * FROM document_templates WHERE kind = @kind AND is_system ORDER BY version LIMIT 1;

-- name: LockTemplateKind :many
-- Блокировка версий вида на время сохранения новой версии.
SELECT id FROM document_templates WHERE kind = @kind FOR UPDATE;

-- name: DeactivateTemplates :exec
UPDATE document_templates SET is_active = false WHERE kind = @kind AND is_active;

-- name: InsertTemplateVersion :one
INSERT INTO document_templates (kind, name, body_html, version, is_active, is_system, created_by, created_at)
SELECT @kind, @name, @body_html, COALESCE(MAX(version), 0) + 1, true, false, sqlc.narg(created_by), @created_at
FROM document_templates WHERE kind = @kind
RETURNING *;

-- name: GetTemplate :one
SELECT * FROM document_templates WHERE id = $1;

-- name: InsertDocument :one
INSERT INTO documents (kind, number, title, contract_id, tenant_id, payment_id, amendment_id, act_id, template_id, file_id,
                       html_snapshot, data_snapshot, period_from, period_to, visible_to_tenant, created_by, created_at)
VALUES (@kind, sqlc.narg(number), @title, sqlc.narg(contract_id), sqlc.narg(tenant_id), sqlc.narg(payment_id),
        sqlc.narg(amendment_id), sqlc.narg(act_id), @template_id, @file_id, @html_snapshot, @data_snapshot,
        sqlc.narg(period_from), sqlc.narg(period_to), @visible_to_tenant, sqlc.narg(created_by), @created_at)
RETURNING *;

-- name: SupersedeDocuments :exec
-- Повторное формирование: прежние документы того же предмета получают ссылку на новый.
UPDATE documents SET superseded_by = @new_id
WHERE id <> @new_id AND superseded_by IS NULL AND kind = ANY(@kinds::text[])
  AND (sqlc.narg(contract_id)::uuid IS NULL OR contract_id = sqlc.narg(contract_id))
  AND (sqlc.narg(tenant_id)::uuid IS NULL OR tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(act_id)::uuid IS NULL OR act_id = sqlc.narg(act_id))
  AND (sqlc.narg(amendment_id)::uuid IS NULL OR amendment_id = sqlc.narg(amendment_id))
  AND (sqlc.narg(payment_id)::uuid IS NULL OR payment_id = sqlc.narg(payment_id))
  AND (sqlc.narg(period_from)::date IS NULL OR period_from = sqlc.narg(period_from))
  AND (sqlc.narg(period_to)::date IS NULL OR period_to = sqlc.narg(period_to));

-- name: GetDocument :one
SELECT * FROM documents WHERE id = $1;

-- name: ListContractDocuments :many
SELECT * FROM documents WHERE contract_id = $1 ORDER BY created_at DESC, id DESC;

-- name: ContractDocumentSince :one
-- Сформирован ли документ договора не раньше момента since (05 разд. 4.4, п. 7).
SELECT EXISTS (SELECT 1 FROM documents
               WHERE contract_id = @contract_id AND kind IN ('contract_ch35', 'contract_ch34') AND created_at >= @since);

-- name: LastAmendmentDocuments :many
SELECT DISTINCT ON (amendment_id) amendment_id, id FROM documents
WHERE amendment_id = ANY(@ids::uuid[])
ORDER BY amendment_id, created_at DESC, id DESC;

-- name: SetDocumentVisibility :one
UPDATE documents SET visible_to_tenant = @visible WHERE id = @id RETURNING *;

-- name: ShowContractDocuments :exec
-- Проект договора становится виден нанимателю при активации (docs/open-questions.md, № 43).
UPDATE documents SET visible_to_tenant = true
WHERE contract_id = @contract_id AND kind IN ('contract_ch35', 'contract_ch34') AND NOT visible_to_tenant;

-- name: ShowAmendmentDocuments :exec
-- Черновик допсоглашения становится виден нанимателю при подписании (№ 43).
UPDATE documents SET visible_to_tenant = true WHERE amendment_id = @amendment_id AND NOT visible_to_tenant;

-- name: SetDocumentSignedScan :one
UPDATE documents SET signed_scan_file_id = @file_id WHERE id = @id RETURNING *;

-- name: SetActDocument :exec
UPDATE contract_acts SET document_id = @document_id WHERE id = @id;

-- name: ReconciliationCharges :many
-- Начисления акта сверки (09 разд. 4.6): без отменённых; начисляемые пени — на дату конца периода.
SELECT id, kind, title, COALESCE(due_date, @period_to::date)::date AS on_date, amount_kop
FROM charges
WHERE contract_id = @contract_id AND status <> 'cancelled'
  AND COALESCE(due_date, @period_to::date) <= @period_to::date
ORDER BY on_date, created_at, id;

-- name: ReconciliationPayments :many
-- Платежи в счёт начислений (в том числе зачёты залога), проведённые до конца периода. Только status = succeeded:
-- отменённые и незавершённые онлайн-оплаты в акт не попадают.
SELECT id, method, received_on::date AS on_date, receipt_number, amount_kop
FROM payments
WHERE contract_id = @contract_id AND purpose = 'charges' AND status = 'succeeded' AND received_on <= @period_to::date
ORDER BY received_on, created_at, id;

-- name: ReconciliationRefunds :many
-- Возвраты переплаты по платежам договора.
SELECT r.id, r.refunded_on AS on_date, r.amount_kop
FROM refunds r JOIN payments p ON p.id = r.payment_id
WHERE p.contract_id = @contract_id AND r.refunded_on <= @period_to::date
ORDER BY r.refunded_on, r.created_at, r.id;

-- name: ReconciliationAdvanceTransfers :many
-- Переносы переплаты в обеспечительный платёж (неотменённые): уменьшают аванс так же, как возврат (04, формула аванса).
SELECT id, op_date AS on_date, amount_kop
FROM deposit_transactions
WHERE contract_id = @contract_id AND kind = 'receipt' AND from_advance AND cancelled_at IS NULL
  AND op_date <= @period_to::date
ORDER BY op_date, created_at, id;

-- name: DepositBalanceOn :one
-- Остаток обеспечительного платежа на дату: внесения минус удержания, зачёты и возвраты (без отменённых).
SELECT COALESCE(sum(CASE WHEN kind = 'receipt' THEN amount_kop ELSE -amount_kop END), 0)::bigint AS balance_kop
FROM deposit_transactions
WHERE contract_id = @contract_id AND cancelled_at IS NULL AND op_date <= @on_date::date;

-- name: ReconciliationWithholdings :many
-- Удержания из залога (неотменённые).
SELECT id, op_date AS on_date, reason, amount_kop
FROM deposit_transactions
WHERE contract_id = @contract_id AND kind = 'withholding' AND cancelled_at IS NULL AND op_date <= @period_to::date
ORDER BY op_date, created_at, id;

-- name: FirstExtension :one
-- Первое подписанное допсоглашение о продлении договора (№ 35).
SELECT effective_from, applied_at FROM contract_amendments
WHERE contract_id = @contract_id AND kind = 'extension' AND status = 'signed'
ORDER BY applied_at, number LIMIT 1;

-- name: ContractDocumentSnapshotBefore :one
-- Данные последнего документа договора, сформированного до первого продления (№ 35).
SELECT data_snapshot FROM documents
WHERE contract_id = @contract_id AND kind IN ('contract_ch35', 'contract_ch34') AND created_at < @before
ORDER BY created_at DESC, id DESC LIMIT 1;
