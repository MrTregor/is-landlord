-- Письма (FR-10).

-- name: GetEmailTemplate :one
SELECT * FROM email_templates WHERE code = $1;

-- name: InsertEmailLog :one
INSERT INTO email_log (template_code, to_email, subject, body_html, attachments, status, dedupe_key,
                       related_entity_type, related_entity_id, created_at)
VALUES (@template_code, @to_email, @subject, @body_html, @attachments, @status, @dedupe_key,
        sqlc.narg(related_entity_type), sqlc.narg(related_entity_id), @created_at)
ON CONFLICT (dedupe_key) DO NOTHING
RETURNING id;

-- name: GetEmailLog :one
SELECT * FROM email_log WHERE id = $1;

-- name: MarkEmailSent :exec
UPDATE email_log SET status = 'sent', attempts = attempts + 1, last_error = NULL, sent_at = $2 WHERE id = $1;

-- name: MarkEmailAttemptFailed :exec
UPDATE email_log SET status = $2, attempts = attempts + 1, last_error = $3 WHERE id = $1;
