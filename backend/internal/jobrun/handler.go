// Package jobrun — экран «Настройки → Служебное»: плановые задачи, ручной запуск, история, резервные копии
// (06 разд. 4, 07).
package jobrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"landlord/backend/internal/api"
	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
)

// Job — плановая задача из таблицы 07: название и расписание (cron, Europe/Moscow).
type Job struct {
	Name     string
	Title    string
	Schedule string
}

// Catalog — все плановые задачи в порядке выполнения (07 Фоновые задачи).
var Catalog = []Job{
	{"contracts.daily", "Договоры: завершение и статусы объектов", "5 0 * * *"},
	{"billing.rent_daily", "Начисления платы", "10 0 * * *"},
	{"billing.penalties_daily", "Пени", "20 0 * * *"},
	{"billing.indexation_daily", "Черновики индексации", "30 0 * * *"},
	{"pdn.retention_daily", "Обезличивание по сроку хранения", "0 3 * * *"},
	{"maintenance.cleanup", "Очистка сессий, токенов и файлов", "0 4 * * *"},
	{"notify.owner_digest", "Утренняя сводка собственнику", "0 8 * * *"},
	{"notify.payment_reminders", "Напоминания об оплате и просрочке", "0 9 * * *"},
	{"notify.contract_end", "Письма об окончании договора", "5 9 * * *"},
	{"notify.readings_reminder", "Напоминание передать показания", "10 9 * * *"},
	{"fiscal.reconcile", "Сверка чеков", "*/15 * * * *"},
	{"payments.expire_pending", "Просроченные онлайн-платежи", "15 * * * *"},
}

// Handler — методы API плановых задач.
type Handler struct {
	db      *db.DB
	jobs    *river.Client[pgx.Tx]
	clock   clock.Clock
	audit   *audit.Service
	daily   map[string]jobs.Daily
	titles  map[string]Job
	backups string
}

// NewHandler создаёт обработчик; daily — реализованные ежедневные задачи.
// backupsDir — каталог резервных копий (BACKUPS_DIR), только чтение.
func NewHandler(database *db.DB, client *river.Client[pgx.Tx], clk clock.Clock, au *audit.Service, daily []jobs.Daily,
	backupsDir string,
) *Handler {
	h := &Handler{db: database, jobs: client, clock: clk, audit: au, daily: map[string]jobs.Daily{}, titles: map[string]Job{},
		backups: backupsDir}
	for _, d := range daily {
		h.daily[d.Name] = d
	}
	for _, j := range Catalog {
		h.titles[j.Name] = j
	}
	return h
}

// RunDTO — запуск задачи для API.
func RunDTO(r sqlc.JobRun) api.JobRunDTO {
	stats := map[string]any{}
	_ = json.Unmarshal(r.Stats, &stats)
	return api.JobRunDTO{
		Id: r.ID, JobName: api.JobName(r.JobName), BusinessDate: apiconv.FromDate(r.BusinessDate), Trigger: api.JobTrigger(r.Trigger),
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Status: api.JobRunStatus(r.Status), Stats: stats, ErrorText: r.ErrorText,
	}
}

// ListJobs — GET /settings/jobs.
func (h *Handler) ListJobs(ctx context.Context, _ api.ListJobsRequestObject) (api.ListJobsResponseObject, error) {
	last, err := h.db.Queries().LastJobRuns(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]sqlc.JobRun{}
	for _, r := range last {
		byName[r.JobName] = r
	}
	out := make(api.ListJobs200JSONResponse, 0, len(Catalog))
	for _, j := range Catalog {
		d := api.JobDTO{Name: api.JobName(j.Name), Title: j.Title, Schedule: j.Schedule}
		if r, ok := byName[j.Name]; ok {
			dto := RunDTO(r)
			d.LastRun = &dto
		}
		out = append(out, d)
	}
	return out, nil
}

// RunJob — POST /settings/jobs/{name}/run: строка job_runs создаётся сразу (её id — в ответе), задачу выполняет worker.
func (h *Handler) RunJob(ctx context.Context, req api.RunJobRequestObject) (api.RunJobResponseObject, error) {
	name := string(req.Name)
	d, ok := h.daily[name]
	if !ok {
		return nil, apperr.NotFound()
	}
	today := h.clock.Today()
	var runID [16]byte
	err := h.db.WithTx(ctx, func(tx *db.Tx) error {
		id, err := tx.Q.StartJobRun(ctx, sqlc.StartJobRunParams{JobName: name, BusinessDate: today, Trigger: jobs.TriggerManual, Now: h.clock.Now()})
		if err != nil {
			return err
		}
		runID = id
		if _, err := h.jobs.InsertTx(ctx, tx.Tx, d.Args(jobs.DailyArgs{BusinessDate: today, Trigger: jobs.TriggerManual, RunID: &id}), nil); err != nil {
			return err
		}
		return h.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "settings.update", EntityType: "job", EntityID: name,
			Summary: fmt.Sprintf("Запуск плановой задачи «%s» вручную", h.titles[name].Title),
		})
	})
	if err != nil {
		return nil, err
	}
	return api.RunJob202JSONResponse{JobRunId: runID}, nil
}

// ListJobRuns — GET /settings/jobs/runs.
func (h *Handler) ListJobRuns(ctx context.Context, req api.ListJobRunsRequestObject) (api.ListJobRunsResponseObject, error) {
	paging := apiconv.NewPaging(req.Params.Page, req.Params.PageSize)
	var name *string
	if req.Params.Name != nil {
		name = apiconv.Ptr(string(*req.Params.Name))
	}
	q := h.db.Queries()
	rows, err := q.ListJobRuns(ctx, sqlc.ListJobRunsParams{JobName: name, Lim: paging.Limit(), Off: paging.Offset()})
	if err != nil {
		return nil, err
	}
	total, err := q.CountJobRuns(ctx, name)
	if err != nil {
		return nil, err
	}
	items := make([]api.JobRunDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, RunDTO(r))
	}
	return api.ListJobRuns200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// ListBackups — GET /settings/backups: файлы каталога резервных копий, новые первыми (только чтение).
// Каталога нет — пустой список.
func (h *Handler) ListBackups(context.Context, api.ListBackupsRequestObject) (api.ListBackupsResponseObject, error) {
	out := api.ListBackups200JSONResponse{}
	entries, err := os.ReadDir(h.backups)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, api.BackupFile{FileName: e.Name(), SizeBytes: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
