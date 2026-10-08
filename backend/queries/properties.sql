-- Объекты (FR-01).

-- name: CreateProperty :one
INSERT INTO properties (code, kind, address, area_sqm, floor, rooms, cadastral_number, ownership_basis, furnished,
                        description, expected_rent_kop, fiscal_mode, status, created_at, updated_at)
VALUES (@code, @kind, @address, @area_sqm, sqlc.narg(floor), sqlc.narg(rooms), sqlc.narg(cadastral_number),
        sqlc.narg(ownership_basis), @furnished, sqlc.narg(description), @expected_rent_kop, @fiscal_mode, @status, @now, @now)
RETURNING *;

-- name: UpdateProperty :one
UPDATE properties
SET code = @code, kind = @kind, address = @address, area_sqm = @area_sqm, floor = sqlc.narg(floor), rooms = sqlc.narg(rooms),
    cadastral_number = sqlc.narg(cadastral_number), ownership_basis = sqlc.narg(ownership_basis), furnished = @furnished,
    description = sqlc.narg(description), expected_rent_kop = @expected_rent_kop, fiscal_mode = @fiscal_mode
WHERE id = @id
RETURNING *;

-- name: GetProperty :one
SELECT * FROM properties WHERE id = $1;

-- name: GetPropertyForUpdate :one
SELECT * FROM properties WHERE id = $1 FOR UPDATE;

-- name: SetPropertyStatus :exec
UPDATE properties SET status = $2 WHERE id = $1;

-- name: SetPropertyArchived :one
UPDATE properties SET archived_at = sqlc.narg(archived_at) WHERE id = @id RETURNING *;

-- name: ListProperties :many
SELECT * FROM properties
WHERE (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
  AND ((@archived::boolean AND archived_at IS NOT NULL) OR (NOT @archived::boolean AND archived_at IS NULL))
  AND (sqlc.narg(q)::text IS NULL OR code ILIKE '%' || sqlc.narg(q) || '%' OR address ILIKE '%' || sqlc.narg(q) || '%')
ORDER BY
  CASE WHEN @sort::text = 'status' THEN status END ASC,
  CASE WHEN @sort::text = '-status' THEN status END DESC,
  CASE WHEN @sort::text = '-code' THEN code END DESC,
  code ASC
LIMIT @lim OFFSET @off;

-- name: CountProperties :one
SELECT count(*) FROM properties
WHERE (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
  AND ((@archived::boolean AND archived_at IS NOT NULL) OR (NOT @archived::boolean AND archived_at IS NULL))
  AND (sqlc.narg(q)::text IS NULL OR code ILIKE '%' || sqlc.narg(q) || '%' OR address ILIKE '%' || sqlc.narg(q) || '%');

-- name: CountOpenContractsByProperty :one
SELECT count(*) FROM contracts WHERE property_id = $1 AND status IN ('draft', 'active', 'extended');

-- name: HasActiveContractOn :one
-- Есть ли на дату действующий (не черновой и не завершённый раньше) договор по объекту.
SELECT EXISTS (
  SELECT 1 FROM contracts
  WHERE property_id = @property_id AND status IN ('active', 'extended', 'terminated', 'finished')
    AND start_date <= @on_date::date AND actual_end_date >= @on_date::date
)::boolean;

-- name: CurrentContracts :many
-- Текущие договоры объектов (действует на дату): номер, наниматель, срок.
SELECT c.id, c.number, c.property_id, c.end_date, t.kind AS tenant_kind, t.last_name, t.first_name, t.middle_name, t.org_name
FROM contracts c JOIN tenants t ON t.id = c.tenant_id
WHERE c.property_id = ANY(@property_ids::uuid[]) AND c.status IN ('active', 'extended')
  AND c.start_date <= @on_date::date AND c.actual_end_date >= @on_date::date;

-- name: CurrentStatusRow :one
SELECT * FROM property_status_history WHERE property_id = $1 AND valid_to IS NULL;

-- name: InsertStatusHistory :exec
INSERT INTO property_status_history (property_id, status, valid_from, contract_id, reason, created_by, created_at)
VALUES (@property_id, @status, @valid_from, sqlc.narg(contract_id), sqlc.narg(reason), sqlc.narg(created_by), @now);

-- name: CloseStatusHistory :exec
UPDATE property_status_history SET valid_to = $2 WHERE id = $1;

-- name: ReplaceStatusHistory :exec
UPDATE property_status_history SET status = @status, reason = sqlc.narg(reason), created_by = sqlc.narg(created_by) WHERE id = @id;

-- name: ListStatusHistory :many
SELECT h.*, c.number AS contract_number
FROM property_status_history h LEFT JOIN contracts c ON c.id = h.contract_id
WHERE h.property_id = $1
ORDER BY h.valid_from DESC;

-- name: ListPropertyPhotos :many
SELECT pp.file_id, pp.sort, pp.caption FROM property_photos pp
JOIN files f ON f.id = pp.file_id AND f.deleted_at IS NULL
WHERE pp.property_id = $1 ORDER BY pp.sort, pp.file_id;

-- name: AddPropertyPhoto :one
INSERT INTO property_photos (property_id, file_id, sort, caption)
VALUES (@property_id, @file_id, COALESCE((SELECT max(sort) + 1 FROM property_photos WHERE property_id = @property_id), 0), sqlc.narg(caption))
RETURNING *;

-- name: SetPropertyPhotoSort :exec
UPDATE property_photos SET sort = $3 WHERE property_id = $1 AND file_id = $2;

-- name: DeletePropertyPhoto :execrows
DELETE FROM property_photos WHERE property_id = $1 AND file_id = $2;

-- name: ListInventory :many
SELECT * FROM property_inventory_items WHERE property_id = $1 ORDER BY sort, name;

-- name: DeleteInventory :exec
DELETE FROM property_inventory_items WHERE property_id = $1;

-- name: InsertInventoryItem :one
INSERT INTO property_inventory_items (id, property_id, name, quantity, condition, note, sort)
VALUES (@id, @property_id, @name, @quantity, @condition, sqlc.narg(note), @sort)
RETURNING *;
