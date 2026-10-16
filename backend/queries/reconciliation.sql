-- Сверка (FR-07.2, FR-07.3): выписки банка, строки выписок, сопоставление с договорами.

-- name: LockStatements :exec
-- Загрузки выписок обрабатываются по одной: отпечатки строк сравниваются с уже сохранёнными.
SELECT pg_advisory_xact_lock(hashtext('bank_statements'));

-- name: InsertBankStatement :one
INSERT INTO bank_statements (file_id, format, account_number, date_from, date_to, lines_total, lines_incoming,
                             uploaded_by, uploaded_at)
VALUES (@file_id, @format, sqlc.narg(account_number), sqlc.narg(date_from), sqlc.narg(date_to), @lines_total,
        @lines_incoming, sqlc.narg(uploaded_by), @now)
RETURNING *;

-- name: SetBankStatementStats :exec
UPDATE bank_statements SET lines_matched = @matched, lines_unmatched = @unmatched, lines_duplicate = @duplicates
WHERE id = @id;

-- name: FingerprintExists :one
SELECT EXISTS (SELECT 1 FROM bank_statement_lines WHERE fingerprint = $1 AND status <> 'duplicate');

-- name: InsertBankLine :one
INSERT INTO bank_statement_lines (statement_id, line_no, doc_number, doc_date, received_on, amount_kop, payer_name,
                                  payer_inn, payer_account, purpose, fingerprint, status, match_method, contract_id)
VALUES (@statement_id, @line_no, sqlc.narg(doc_number), sqlc.narg(doc_date), @received_on, @amount_kop,
        sqlc.narg(payer_name), sqlc.narg(payer_inn), sqlc.narg(payer_account), sqlc.narg(purpose), @fingerprint,
        @status, sqlc.narg(match_method), sqlc.narg(contract_id))
RETURNING *;

-- name: GetBankLine :one
SELECT * FROM bank_statement_lines WHERE id = $1;

-- name: GetBankLineForUpdate :one
SELECT * FROM bank_statement_lines WHERE id = $1 FOR UPDATE;

-- name: SetBankLineMatched :exec
-- Строка привязана к платежу: сопоставлена автоматически, «тот же платёж», «другой платёж» или вручную.
UPDATE bank_statement_lines SET status = @status, match_method = @match_method, contract_id = @contract_id,
  payment_id = @payment_id
WHERE id = @id;

-- name: SetBankLineClosed :exec
-- Неопознанная строка: «возвращён плательщику» (refunded) или «не относится к аренде» (ignored).
UPDATE bank_statement_lines SET status = @status, note = sqlc.narg(note) WHERE id = @id;

-- name: SetPaymentBankData :one
-- Данные плательщика из строки выписки (05 разд. 15.1, шаги 5 и 6).
UPDATE payments SET bank_statement_line_id = @line_id, payer_name = sqlc.narg(payer_name),
  payer_inn = sqlc.narg(payer_inn), payer_account = sqlc.narg(payer_account), bank_purpose = sqlc.narg(bank_purpose)
WHERE id = @id
RETURNING *;

-- name: StatementLines :many
SELECT l.*, c.number AS contract_number
FROM bank_statement_lines l LEFT JOIN contracts c ON c.id = l.contract_id
WHERE l.statement_id = @statement_id AND (sqlc.narg(status)::text IS NULL OR l.status = sqlc.narg(status))
ORDER BY l.line_no;

-- name: BankLineRow :one
SELECT l.*, c.number AS contract_number
FROM bank_statement_lines l LEFT JOIN contracts c ON c.id = l.contract_id
WHERE l.id = $1;

-- name: GetBankStatementRow :one
SELECT s.*, f.original_name AS file_name, u.display_name AS uploaded_by_name
FROM bank_statements s JOIN files f ON f.id = s.file_id LEFT JOIN users u ON u.id = s.uploaded_by
WHERE s.id = $1;

-- name: ListBankStatements :many
SELECT s.*, f.original_name AS file_name, u.display_name AS uploaded_by_name
FROM bank_statements s JOIN files f ON f.id = s.file_id LEFT JOIN users u ON u.id = s.uploaded_by
ORDER BY s.uploaded_at DESC, s.id DESC
LIMIT @lim OFFSET @off;

-- name: CountBankStatements :one
SELECT count(*)::bigint FROM bank_statements;

-- name: MatchContracts :many
-- Договоры (кроме черновиков) с данными нанимателя для сопоставления строк выписки (05 разд. 15.1).
SELECT c.id, c.number, c.status, c.start_date, c.actual_end_date, c.tenant_id, t.kind AS tenant_kind,
       t.inn AS tenant_inn, t.last_name, t.first_name, t.middle_name
FROM contracts c JOIN tenants t ON t.id = c.tenant_id
WHERE c.status <> 'draft' AND t.anonymized_at IS NULL
ORDER BY c.start_date, c.id;

-- name: DuplicateCandidates :many
-- Возможный дубль (05 разд. 15.1, шаг 5): проведённый ручной или онлайн-платёж договора с той же суммой,
-- датой поступления ±3 дня и без привязки к выписке.
SELECT * FROM payments
WHERE contract_id = @contract_id AND status = 'succeeded' AND amount_kop = @amount_kop
  AND source IN ('manual', 'online') AND bank_statement_line_id IS NULL
  AND received_on >= @date_from::date AND received_on <= @date_to::date
ORDER BY received_on, created_at;

-- name: UnidentifiedOfLine :one
SELECT * FROM discrepancies WHERE bank_statement_line_id = $1 AND kind = 'unidentified' FOR UPDATE;

-- name: LinkUnidentified :one
-- Неопознанный платёж привязан к договору (05 разд. 15.2).
UPDATE discrepancies SET status = 'resolved', resolution = 'linked', resolution_note = sqlc.narg(note),
  resolved_by = sqlc.narg(resolved_by), resolved_at = @now::timestamptz, contract_id = @contract_id, payment_id = @payment_id
WHERE id = @id
RETURNING *;
