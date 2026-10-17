-- Наниматели и согласия (FR-02).

-- name: CreateTenant :one
INSERT INTO tenants (id, kind, last_name, first_name, middle_name, birth_date, doc_type, doc_number_enc, doc_issued_by_enc,
  doc_issued_on, doc_division_code_enc, registration_address_enc, org_name, inn, kpp, ogrn, legal_address, signatory_name,
  signatory_position, signatory_basis, phone, email, note, created_at, updated_at)
VALUES (@id, @kind, sqlc.narg(last_name), sqlc.narg(first_name), sqlc.narg(middle_name), sqlc.narg(birth_date),
  sqlc.narg(doc_type), sqlc.narg(doc_number_enc), sqlc.narg(doc_issued_by_enc), sqlc.narg(doc_issued_on),
  sqlc.narg(doc_division_code_enc), sqlc.narg(registration_address_enc), sqlc.narg(org_name), sqlc.narg(inn),
  sqlc.narg(kpp), sqlc.narg(ogrn), sqlc.narg(legal_address), sqlc.narg(signatory_name), sqlc.narg(signatory_position),
  sqlc.narg(signatory_basis), sqlc.narg(phone), sqlc.narg(email), sqlc.narg(note), @now, @now)
RETURNING *;

-- name: UpdateTenant :one
UPDATE tenants SET last_name = sqlc.narg(last_name), first_name = sqlc.narg(first_name), middle_name = sqlc.narg(middle_name),
  birth_date = sqlc.narg(birth_date), doc_type = sqlc.narg(doc_type), doc_number_enc = sqlc.narg(doc_number_enc),
  doc_issued_by_enc = sqlc.narg(doc_issued_by_enc), doc_issued_on = sqlc.narg(doc_issued_on),
  doc_division_code_enc = sqlc.narg(doc_division_code_enc), registration_address_enc = sqlc.narg(registration_address_enc),
  org_name = sqlc.narg(org_name), inn = sqlc.narg(inn), kpp = sqlc.narg(kpp), ogrn = sqlc.narg(ogrn),
  legal_address = sqlc.narg(legal_address), signatory_name = sqlc.narg(signatory_name),
  signatory_position = sqlc.narg(signatory_position), signatory_basis = sqlc.narg(signatory_basis),
  phone = sqlc.narg(phone), email = sqlc.narg(email), note = sqlc.narg(note)
WHERE id = @id
RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id = $1;

-- name: GetTenantForUpdate :one
SELECT * FROM tenants WHERE id = $1 FOR UPDATE;

-- name: ListTenants :many
SELECT t.*,
  (SELECT count(*) FROM contracts c WHERE c.tenant_id = t.id AND c.status IN ('active', 'extended'))::int AS active_contracts,
  u.status AS user_status
FROM tenants t LEFT JOIN users u ON u.tenant_id = t.id
WHERE (sqlc.narg(kind)::text IS NULL OR t.kind = sqlc.narg(kind))
  AND ((@anonymized::boolean AND t.anonymized_at IS NOT NULL) OR (NOT @anonymized::boolean AND t.anonymized_at IS NULL))
  AND (sqlc.narg(has_active)::boolean IS NULL OR sqlc.narg(has_active)::boolean =
       EXISTS (SELECT 1 FROM contracts c WHERE c.tenant_id = t.id AND c.status IN ('active', 'extended')))
  AND (sqlc.narg(q)::text IS NULL
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%' OR t.inn LIKE sqlc.narg(q) || '%'
       OR t.phone LIKE '%' || sqlc.narg(q_digits)::text || '%' OR t.email ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY lower(COALESCE(t.org_name, t.last_name)), lower(t.first_name)
LIMIT @lim OFFSET @off;

-- name: CountTenants :one
SELECT count(*) FROM tenants t
WHERE (sqlc.narg(kind)::text IS NULL OR t.kind = sqlc.narg(kind))
  AND ((@anonymized::boolean AND t.anonymized_at IS NOT NULL) OR (NOT @anonymized::boolean AND t.anonymized_at IS NULL))
  AND (sqlc.narg(has_active)::boolean IS NULL OR sqlc.narg(has_active)::boolean =
       EXISTS (SELECT 1 FROM contracts c WHERE c.tenant_id = t.id AND c.status IN ('active', 'extended')))
  AND (sqlc.narg(q)::text IS NULL
       OR concat_ws(' ', t.last_name, t.first_name, t.middle_name) ILIKE '%' || sqlc.narg(q) || '%'
       OR t.org_name ILIKE '%' || sqlc.narg(q) || '%' OR t.inn LIKE sqlc.narg(q) || '%'
       OR t.phone LIKE '%' || sqlc.narg(q_digits)::text || '%' OR t.email ILIKE '%' || sqlc.narg(q) || '%');

-- name: ListTenantContracts :many
SELECT c.id, c.number, c.status, c.start_date, c.end_date, p.code AS property_code
FROM contracts c JOIN properties p ON p.id = c.property_id
WHERE c.tenant_id = $1
ORDER BY c.start_date DESC;

-- name: GetUserByTenantID :one
SELECT * FROM users WHERE tenant_id = $1;

-- name: InsertConsent :one
INSERT INTO pdn_consents (tenant_id, purpose, document_id, given_on, method, file_id, recorded_by, ip, created_at)
VALUES (@tenant_id, @purpose, sqlc.narg(document_id), @given_on, @method, sqlc.narg(file_id), sqlc.narg(recorded_by),
        sqlc.narg(ip), @now)
RETURNING *;

-- name: ListConsents :many
SELECT * FROM pdn_consents WHERE tenant_id = $1 ORDER BY given_on DESC, created_at DESC;

-- name: GetConsent :one
SELECT * FROM pdn_consents WHERE id = $1 AND tenant_id = $2;

-- name: RevokeConsent :one
UPDATE pdn_consents SET revoked_on = $3, revoke_note = sqlc.narg(revoke_note)
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: SetTenantPhone :one
-- Телефон нанимателя из профиля кабинета (15 разд. 8).
UPDATE tenants SET phone = sqlc.narg(phone) WHERE id = @id RETURNING *;
