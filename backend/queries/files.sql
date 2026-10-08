-- Файлы (06 API, раздел 6).

-- name: InsertFile :one
INSERT INTO files (storage_key, original_name, content_type, size_bytes, sha256, category, uploaded_by, created_at)
VALUES (@storage_key, @original_name, @content_type, @size_bytes, @sha256, @category, sqlc.narg(uploaded_by), @created_at)
RETURNING *;

-- name: GetFile :one
SELECT * FROM files WHERE id = $1 AND deleted_at IS NULL;

-- name: TenantCanReadFile :one
-- Наниматель видит свои загрузки, фото своих заявок, видимые документы своих договоров (и свои согласия)
-- и фото объектов своих договоров (12 Безопасность и ПДн, раздел 3).
SELECT (
  EXISTS (SELECT 1 FROM files f WHERE f.id = @file_id AND f.uploaded_by = @user_id)
  OR EXISTS (SELECT 1 FROM maintenance_request_photos p
             JOIN maintenance_requests r ON r.id = p.request_id
             JOIN contracts c ON c.id = r.contract_id
             WHERE p.file_id = @file_id AND c.tenant_id = @tenant_id)
  OR EXISTS (SELECT 1 FROM documents d
             LEFT JOIN contracts c ON c.id = d.contract_id
             WHERE (d.file_id = @file_id OR d.signed_scan_file_id = @file_id) AND d.visible_to_tenant
               AND (c.tenant_id = @tenant_id OR d.tenant_id = @tenant_id))
  OR EXISTS (SELECT 1 FROM property_photos pp
             JOIN contracts c ON c.property_id = pp.property_id AND c.status <> 'draft'
             WHERE pp.file_id = @file_id AND c.tenant_id = @tenant_id)
)::boolean AS allowed;
