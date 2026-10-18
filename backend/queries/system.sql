-- Служебные запросы.

-- name: GetDemoClockOffset :one
-- Сдвиг даты системы в демонстрационном режиме (03 Архитектура, «Время и часовой пояс»).
SELECT offset_seconds FROM demo_clock WHERE id = 1;

-- name: LastJobRun :one
-- Последний запуск плановой задачи.
SELECT * FROM job_runs WHERE job_name = $1 ORDER BY started_at DESC LIMIT 1;

-- name: GetOrganizationShortName :one
-- Краткое наименование организации для страницы входа (GET /public/app-info).
SELECT short_name FROM organization_settings WHERE id = 1;

-- name: SetDemoClockOffset :exec
INSERT INTO demo_clock (id, offset_seconds) VALUES (1, @offset_seconds)
ON CONFLICT (id) DO UPDATE SET offset_seconds = EXCLUDED.offset_seconds;

-- name: GetJobRun :one
SELECT * FROM job_runs WHERE id = $1;
