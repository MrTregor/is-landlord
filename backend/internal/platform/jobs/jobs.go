// Package jobs — очередь фоновых задач River на PostgreSQL (docs/spec/07 Фоновые задачи.md).
package jobs

import (
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"landlord/backend/internal/platform/clock"
)

// Очереди и число воркеров (07 Фоновые задачи).
const (
	QueueDefault      = river.QueueDefault
	QueueEmail        = "email"
	QueueIntegrations = "integrations"
)

// DefaultMaxAttempts — число попыток по умолчанию.
const DefaultMaxAttempts = 10

// Registry — набор воркеров процесса worker.
type Registry struct {
	workers  *river.Workers
	count    int
	periodic []*river.PeriodicJob
	// Daily — ежедневные плановые задачи в порядке выполнения (07 Фоновые задачи).
	Daily []Daily
}

// NewRegistry создаёт пустой набор воркеров.
func NewRegistry() *Registry { return &Registry{workers: river.NewWorkers()} }

// Add регистрирует воркер задачи вида T.
func Add[T river.JobArgs](r *Registry, w river.Worker[T]) {
	river.AddWorker(r.workers, w)
	r.count++
}

// AddDaily регистрирует ежедневную задачу (её воркер добавляется отдельно через Add).
func (r *Registry) AddDaily(d Daily, clk clock.Clock) {
	r.Daily = append(r.Daily, d)
	r.periodic = append(r.periodic, PeriodicJobs([]Daily{d}, clk)...)
}

// Len — число зарегистрированных воркеров.
func (r *Registry) Len() int { return r.count }

// NewInsertClient — клиент только для постановки задач (server, cli): river.InsertTx в транзакции действия.
func NewInsertClient(pool *pgxpool.Pool, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:      log,
		MaxAttempts: DefaultMaxAttempts,
	})
}

// NewWorkerClient — клиент, выполняющий задачи (процесс worker).
func NewWorkerClient(pool *pgxpool.Pool, reg *Registry, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:      log,
		MaxAttempts: DefaultMaxAttempts,
		Queues: map[string]river.QueueConfig{
			QueueDefault:      {MaxWorkers: 4},
			QueueEmail:        {MaxWorkers: 2},
			QueueIntegrations: {MaxWorkers: 2},
		},
		Workers:      reg.workers,
		PeriodicJobs: reg.periodic,
	})
}
