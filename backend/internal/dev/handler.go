// Package dev — служебные методы демонстрационного режима (06 разд. 17, 07 «Порядок выполнения на защите»): дата
// системы и синхронный запуск ежедневных задач. Вне APP_ENV=demo (или без DEMO_CLOCK_ENABLED для даты) — 404.
package dev

import (
	"context"
	"fmt"

	"landlord/backend/internal/api"
	"landlord/backend/internal/audit"
	"landlord/backend/internal/jobrun"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
)

// Handler — методы /dev/*.
type Handler struct {
	demo    bool
	db      *db.DB
	clock   clock.Clock
	audit   *audit.Service
	runners []jobs.Runner
}

// NewHandler создаёт обработчик; demo — APP_ENV=demo.
func NewHandler(demo bool, database *db.DB, clk clock.Clock, au *audit.Service, runners []jobs.Runner) *Handler {
	return &Handler{demo: demo, db: database, clock: clk, audit: au, runners: runners}
}

// offset — сдвинутые часы (только demo с DEMO_CLOCK_ENABLED).
func (h *Handler) offset() (*clock.Offset, error) {
	c, ok := h.clock.(*clock.Offset)
	if !h.demo || !ok {
		return nil, apperr.NotFound()
	}
	return c, nil
}

func (h *Handler) state(c *clock.Offset) api.DevClock {
	return api.DevClock{Now: c.Now(), Today: apiconv.FromDate(c.Today()), OffsetSeconds: c.Seconds()}
}

// GetDevClock — GET /dev/clock.
func (h *Handler) GetDevClock(context.Context, api.GetDevClockRequestObject) (api.GetDevClockResponseObject, error) {
	c, err := h.offset()
	if err != nil {
		return nil, err
	}
	return api.GetDevClock200JSONResponse(h.state(c)), nil
}

func (h *Handler) set(ctx context.Context, c *clock.Offset, seconds int64, summary string) error {
	err := h.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.SetDemoClockOffset(ctx, seconds); err != nil {
			return err
		}
		return h.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "settings.update", EntityType: "demo_clock", EntityID: "1", Summary: summary,
			After: map[string]any{"offsetSeconds": seconds},
		})
	})
	if err == nil {
		c.Apply(seconds)
	}
	return err
}

// SetDevClock — PUT /dev/clock: «сегодня» становится указанной датой, время суток сохраняется.
func (h *Handler) SetDevClock(ctx context.Context, req api.SetDevClockRequestObject) (api.SetDevClockResponseObject, error) {
	c, err := h.offset()
	if err != nil {
		return nil, err
	}
	today := apiconv.ToDate(req.Body.Today)
	s := clock.OffsetFor(c.RealNow(), today, c.Location())
	if err := h.set(ctx, c, s, "Дата системы (демо) изменена на "+today.Format()); err != nil {
		return nil, err
	}
	return api.SetDevClock200JSONResponse(h.state(c)), nil
}

// ResetDevClock — DELETE /dev/clock.
func (h *Handler) ResetDevClock(ctx context.Context, _ api.ResetDevClockRequestObject) (api.ResetDevClockResponseObject, error) {
	c, err := h.offset()
	if err != nil {
		return nil, err
	}
	if err := h.set(ctx, c, 0, "Дата системы (демо) сброшена на реальную"); err != nil {
		return nil, err
	}
	return api.ResetDevClock200JSONResponse(h.state(c)), nil
}

// RunDailyJobs — POST /dev/run-daily: ежедневные задачи за «сегодня» по порядку 07, синхронно; ошибка одной задачи
// не останавливает следующие (её итог — в строке job_runs).
func (h *Handler) RunDailyJobs(ctx context.Context, _ api.RunDailyJobsRequestObject) (api.RunDailyJobsResponseObject, error) {
	if !h.demo {
		return nil, apperr.NotFound()
	}
	today := h.clock.Today()
	out := api.RunDailyResult{Today: apiconv.FromDate(today), Jobs: []api.JobRunDTO{}}
	q := h.db.Queries()
	for _, r := range h.runners {
		id, err := q.StartJobRun(ctx, sqlc.StartJobRunParams{JobName: r.Name, BusinessDate: today, Trigger: jobs.TriggerManual, Now: h.clock.Now()})
		if err != nil {
			return nil, err
		}
		_, _ = r.Run(ctx, jobs.DailyArgs{BusinessDate: today, Trigger: jobs.TriggerManual, RunID: &id})
		run, err := q.GetJobRun(ctx, id)
		if err != nil {
			return nil, err
		}
		out.Jobs = append(out.Jobs, jobrun.RunDTO(run))
	}
	err := h.db.WithTx(ctx, func(tx *db.Tx) error {
		return h.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "settings.update", EntityType: "job", EntityID: "run-daily",
			Summary: fmt.Sprintf("Выполнены ежедневные задачи за %s (демо)", today.Format()),
		})
	})
	if err != nil {
		return nil, err
	}
	return api.RunDailyJobs200JSONResponse(out), nil
}
