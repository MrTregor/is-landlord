-- Сроки хранения и обезличивание ПДн (NFR-Sec-05, 12 разд. 6), служебная очистка (07, maintenance.cleanup).

-- name: TenantHasLiveContracts :one
SELECT EXISTS (SELECT 1 FROM contracts WHERE tenant_id = $1 AND status IN ('draft', 'active', 'extended'))::boolean;

-- name: RetentionCandidates :many
-- Ещё не обезличенные наниматели, у которых истёк срок хранения (дата + срок раньше сегодняшнего дня):
-- с договорами (кроме черновиков), все не draft/active/extended, — years лет от последнего фактического конца договора;
-- без договоров (кроме черновиков) — no_contract_months месяцев от последнего изменения карточки или черновика
-- (цель «заключение договора» отпала, ч. 4 ст. 21 152-ФЗ; № 105, № 108).
SELECT t.id FROM tenants t LEFT JOIN contracts c ON c.tenant_id = t.id
WHERE t.anonymized_at IS NULL
GROUP BY t.id, t.updated_at
HAVING (count(c.id) FILTER (WHERE c.status <> 'draft') > 0
        AND bool_and(c.status NOT IN ('draft', 'active', 'extended'))
        AND (max(c.actual_end_date) + make_interval(years => sqlc.arg(years)::int))::date < sqlc.arg(today)::date)
    OR (count(c.id) FILTER (WHERE c.status <> 'draft') = 0
        AND ((greatest(t.updated_at, max(c.updated_at)) AT TIME ZONE 'Europe/Moscow')::date
             + make_interval(months => sqlc.arg(no_contract_months)::int))::date < sqlc.arg(today)::date)
ORDER BY t.id;

-- name: AnonymizeTenant :one
UPDATE tenants SET
  last_name = CASE WHEN kind = 'individual' THEN 'Обезличен' ELSE NULL END,
  first_name = CASE WHEN kind = 'individual' THEN 'Наниматель' ELSE NULL END,
  middle_name = CASE WHEN kind = 'individual' THEN left(id::text, 8) ELSE NULL END,
  birth_date = NULL, doc_type = NULL, doc_number_enc = NULL, doc_issued_by_enc = NULL, doc_issued_on = NULL,
  doc_division_code_enc = NULL, registration_address_enc = NULL, phone = NULL, email = NULL, note = NULL,
  signatory_name = NULL, anonymized_at = @now
WHERE id = @id
RETURNING *;

-- name: AnonymizeOccupants :exec
UPDATE contract_occupants SET full_name = 'Обезличено', birth_date = NULL, relation = NULL
WHERE contract_id IN (SELECT id FROM contracts WHERE tenant_id = $1);

-- name: AnonymizeTenantUser :one
UPDATE users SET email = 'anonymized-' || id::text || '@invalid', display_name = 'Обезличенный пользователь',
  status = 'blocked', password_hash = @password_hash, policy_accepted_version = NULL, policy_accepted_at = NULL
WHERE tenant_id = @tenant_id
RETURNING id;

-- name: TenantPdnFiles :many
-- Файлы с ПДн нанимателя: PDF документов его договоров и бланков согласия, сканы подписанных экземпляров и согласий.
SELECT f.id, f.storage_key FROM files f
WHERE f.deleted_at IS NULL AND f.id IN (
  SELECT d.file_id FROM documents d LEFT JOIN contracts c ON c.id = d.contract_id
  WHERE c.tenant_id = @tenant_id OR d.tenant_id = @tenant_id
  UNION
  SELECT d.signed_scan_file_id FROM documents d LEFT JOIN contracts c ON c.id = d.contract_id
  WHERE (c.tenant_id = @tenant_id OR d.tenant_id = @tenant_id) AND d.signed_scan_file_id IS NOT NULL
  UNION
  SELECT p.file_id FROM pdn_consents p WHERE p.tenant_id = @tenant_id AND p.file_id IS NOT NULL
);

-- name: MarkFilesDeleted :exec
UPDATE files SET deleted_at = @now WHERE id = ANY(@ids::uuid[]) AND deleted_at IS NULL;

-- name: ClearTenantDocuments :exec
-- Строки документов сохраняются (вид, номер, дата); содержимое с ПДн очищается.
UPDATE documents d SET html_snapshot = '', data_snapshot = '{}'
WHERE d.tenant_id = @tenant_id OR d.contract_id IN (SELECT c.id FROM contracts c WHERE c.tenant_id = @tenant_id);

-- name: AnonymizeEmailLog :exec
UPDATE email_log SET to_email = 'anonymized@invalid', subject = '[обезличено]', body_html = '[обезличено]', attachments = '[]'
WHERE to_email = ANY(@emails::text[]);

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < @now OR revoked_at IS NOT NULL;

-- name: DeleteExpiredAuthTokens :execrows
DELETE FROM auth_tokens WHERE expires_at < @now OR used_at IS NOT NULL;

-- name: DeleteOldLoginAttempts :execrows
DELETE FROM login_attempts WHERE at < @before;

-- name: OrphanFiles :many
-- Файлы, не привязанные ни к одной записи, загруженные раньше момента before.
SELECT f.id, f.storage_key FROM files f
WHERE f.deleted_at IS NULL AND f.created_at < @before
  AND NOT EXISTS (SELECT 1 FROM property_photos x WHERE x.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM maintenance_request_photos x WHERE x.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM expenses x WHERE x.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM pdn_consents x WHERE x.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM bank_statements x WHERE x.file_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM documents x WHERE x.file_id = f.id OR x.signed_scan_file_id = f.id);
