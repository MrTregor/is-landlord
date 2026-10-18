package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Запуск плановой задачи (job_runs.trigger).
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// DailyArgs — аргументы ежедневной задачи: бизнес-дата и способ запуска.
// Уникальность — по (kind, businessDate) (07 Фоновые задачи, общие правила).
// Ручной запуск передаёт RunID — заранее созданную строку job_runs (её id возвращает API), и не схлопывается
// с плановым запуском того же дня.
type DailyArgs struct {
	BusinessDate date.Date  `json:"businessDate" river:"unique"`
	Trigger      string     `json:"trigger"`
	RunID        *uuid.UUID `json:"runId,omitempty" river:"unique"`
	// Hour — час запуска ежечасной задачи (уникальность в пределах часа).
	Hour *int `json:"hour,omitempty" river:"unique"`
	// Minute — минута запуска задачи с расписанием «каждые N минут» (уникальность в пределах интервала).
	Minute *int `json:"minute,omitempty" river:"unique"`
}

// Daily — описание плановой задачи: имя (= kind River), время запуска по Москве и конструктор аргументов.
// Hourly — запуск каждый час в Minute (payments.expire_pending); EveryMinutes — каждые N минут от начала часа
// (fiscal.reconcile). Такие задачи не входят в догоняющий запуск.
type Daily struct {
	Name         string
	Hour         int
	Minute       int
	Hourly       bool
	EveryMinutes int
	// Args строит аргументы задачи на бизнес-дату.
	Args func(DailyArgs) river.JobArgs
}

// Runner — синхронный запуск ежедневной задачи (демо: «Выполнить ежедневные задачи»).
type Runner struct {
	Name string
	Run  func(ctx context.Context, args DailyArgs) (*Stats, error)
}

// DailySchedule — расписание «каждый день в HH:MM» в часовом поясе loc (River PeriodicSchedule).
type DailySchedule struct {
	Hour, Minute int
	Loc          *time.Location
}

// Next — ближайший момент запуска строго после t.
func (s DailySchedule) Next(t time.Time) time.Time {
	lt := t.In(s.Loc)
	next := time.Date(lt.Year(), lt.Month(), lt.Day(), s.Hour, s.Minute, 0, 0, s.Loc)
	if !next.After(lt) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// HourlySchedule — расписание «каждый час в :MM».
type HourlySchedule struct{ Minute int }

// Next — ближайший момент запуска строго после t.
func (s HourlySchedule) Next(t time.Time) time.Time {
	next := t.Truncate(time.Hour).Add(time.Duration(s.Minute) * time.Minute)
	if !next.After(t) {
		next = next.Add(time.Hour)
	}
	return next
}

// EverySchedule — расписание «каждые N минут» (*/N в cron): 00, N, 2N… минут каждого часа.
type EverySchedule struct{ Minutes int }

// Next — ближайший момент запуска строго после t.
func (s EverySchedule) Next(t time.Time) time.Time {
	step := time.Duration(s.Minutes) * time.Minute
	next := t.Truncate(step)
	for !next.After(t) {
		next = next.Add(step)
	}
	return next
}

// Repeating — задача не ежедневная (ежечасная или каждые N минут).
func (d Daily) Repeating() bool { return d.Hourly || d.EveryMinutes > 0 }

// PeriodicJobs — периодические задачи River для плановых задач; бизнес-дата берётся из clk в момент запуска.
func PeriodicJobs(daily []Daily, clk clock.Clock) []*river.PeriodicJob {
	out := make([]*river.PeriodicJob, 0, len(daily))
	for _, d := range daily {
		d := d
		var sched river.PeriodicSchedule = DailySchedule{Hour: d.Hour, Minute: d.Minute, Loc: clk.Location()}
		switch {
		case d.Hourly:
			sched = HourlySchedule{Minute: d.Minute}
		case d.EveryMinutes > 0:
			sched = EverySchedule{Minutes: d.EveryMinutes}
		}
		out = append(out, river.NewPeriodicJob(
			sched,
			func() (river.JobArgs, *river.InsertOpts) {
				args := DailyArgs{BusinessDate: clk.Today(), Trigger: TriggerSchedule}
				if d.Repeating() {
					now := clk.Now().In(clk.Location())
					h := now.Hour()
					args.Hour = &h
					if d.EveryMinutes > 0 {
						m := now.Minute() - now.Minute()%d.EveryMinutes
						args.Minute = &m
					}
				}
				return d.Args(args), nil
			},
			&river.PeriodicJobOpts{ID: d.Name},
		))
	}
	return out
}

// CatchUp — догоняющий запуск: для каждой задачи без успешного запуска за сегодня, время которой уже прошло,
// ставится задача немедленно (в порядке списка).
func CatchUp(ctx context.Context, database *db.DB, client *river.Client[pgx.Tx], clk clock.Clock, daily []Daily) error {
	today := clk.Today()
	now := clk.Now().In(clk.Location())
	for _, d := range daily {
		if d.Repeating() {
			continue
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), d.Hour, d.Minute, 0, 0, clk.Location())
		if now.Before(at) {
			continue
		}
		done, err := database.Queries().HasSucceededJobRun(ctx, sqlc.HasSucceededJobRunParams{JobName: d.Name, BusinessDate: today})
		if err != nil {
			return err
		}
		if done {
			continue
		}
		if _, err := client.Insert(ctx, d.Args(DailyArgs{BusinessDate: today, Trigger: TriggerSchedule}), nil); err != nil {
			return fmt.Errorf("догоняющий запуск %s: %w", d.Name, err)
		}
	}
	return nil
}

// Stats — итоги запуска плановой задачи (job_runs.stats); Errors — ошибки по отдельным договорам.
type Stats struct {
	Counts map[string]int
	Errors []string
}

// NewStats — пустые итоги.
func NewStats() *Stats { return &Stats{Counts: map[string]int{}} }

// Inc увеличивает счётчик.
func (s *Stats) Inc(key string) { s.Counts[key]++ }

// Fail добавляет ошибку по объекту обработки.
func (s *Stats) Fail(what string, err error) { s.Errors = append(s.Errors, what+": "+err.Error()) }

// JSON — {"<счётчик>": n, …, "errors": [...]}.
func (s *Stats) JSON() []byte {
	m := map[string]any{}
	for k, v := range s.Counts {
		m[k] = v
	}
	if len(s.Errors) > 0 {
		m["errors"] = s.Errors
	}
	b, _ := json.Marshal(m)
	return b
}

// RunDaily выполняет fn с записью job_runs: running → succeeded / failed (failed — при ошибке fn или ошибках в stats).
func RunDaily(ctx context.Context, database *db.DB, clk clock.Clock, name string, args DailyArgs,
	fn func(ctx context.Context, stats *Stats) error,
) (*Stats, error) {
	trigger := args.Trigger
	if trigger == "" {
		trigger = TriggerSchedule
	}
	q := database.Queries()
	var runID uuid.UUID
	if args.RunID != nil {
		runID = *args.RunID
	} else {
		var err error
		if runID, err = q.StartJobRun(ctx, sqlc.StartJobRunParams{JobName: name, BusinessDate: args.BusinessDate, Trigger: trigger, Now: clk.Now()}); err != nil {
			return nil, err
		}
	}
	stats := NewStats()
	runErr := fn(ctx, stats)
	status := "succeeded"
	var errText *string
	switch {
	case runErr != nil:
		status = "failed"
		t := runErr.Error()
		errText = &t
	case len(stats.Errors) > 0:
		status = "failed"
		t := fmt.Sprintf("ошибок: %d", len(stats.Errors))
		errText = &t
	}
	now := clk.Now()
	if err := q.FinishJobRun(ctx, sqlc.FinishJobRunParams{ID: runID, Now: &now, Status: status, Stats: stats.JSON(), ErrorText: errText}); err != nil {
		return stats, err
	}
	return stats, runErr
}
