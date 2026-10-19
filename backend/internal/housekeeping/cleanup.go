// Package housekeeping — служебная очистка maintenance.cleanup (07 Фоновые задачи, порядок 6; NFR-Sec-01):
// истёкшие и отозванные сессии, использованные и истёкшие токены, попытки входа старше 30 дней, файлы,
// не привязанные ни к чему 24 часа.
package housekeeping

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/files"
	"landlord/backend/internal/platform/jobs"
)

// CleanupJobName — имя задачи.
const CleanupJobName = "maintenance.cleanup"

// CleanupArgs — аргументы задачи maintenance.cleanup.
type CleanupArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (CleanupArgs) Kind() string { return CleanupJobName }

// InsertOpts — уникальность по бизнес-дате.
func (CleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// CleanupDaily — расписание: 04:00 по Москве.
func CleanupDaily() jobs.Daily {
	return jobs.Daily{Name: CleanupJobName, Hour: 4, Args: func(a jobs.DailyArgs) river.JobArgs { return CleanupArgs{a} }}
}

// Service — служебная очистка.
type Service struct {
	db    *db.DB
	clock clock.Clock
	real  clock.Clock // сроки сеансов, токенов и попыток входа — по реальному времени (№ 112)
	audit *audit.Service
	store *files.Store
}

// NewService создаёт сервис.
func NewService(database *db.DB, clk, realClk clock.Clock, au *audit.Service, store *files.Store) *Service {
	return &Service{db: database, clock: clk, real: realClk, audit: au, store: store}
}

// CleanupWorker выполняет maintenance.cleanup.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[CleanupArgs]) error {
	_, err := w.Svc.RunCleanup(ctx, job.Args.DailyArgs)
	return err
}

// RunCleanup — maintenance.cleanup. Сеансы, токены и попытки входа — по реальному времени (auth выдаёт их по нему же,
// № 112); файлы без привязки — по Clock (по нему же отмечается их загрузка).
func (s *Service) RunCleanup(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
		args.BusinessDate = today
	}
	return jobs.RunDaily(ctx, s.db, s.clock, CleanupJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		now, realNow := s.clock.Now(), s.real.Now()
		q := s.db.Queries()
		n, err := q.DeleteExpiredSessions(ctx, realNow)
		if err != nil {
			return err
		}
		st.Counts["sessions"] = int(n)
		if n, err = q.DeleteExpiredAuthTokens(ctx, realNow); err != nil {
			return err
		}
		st.Counts["tokens"] = int(n)
		if n, err = q.DeleteOldLoginAttempts(ctx, realNow.Add(-30*24*time.Hour)); err != nil {
			return err
		}
		st.Counts["loginAttempts"] = int(n)
		orphans, err := q.OrphanFiles(ctx, now.Add(-24*time.Hour))
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(orphans))
		for _, f := range orphans {
			ids = append(ids, f.ID)
		}
		if err := q.MarkFilesDeleted(ctx, sqlc.MarkFilesDeletedParams{Now: &now, Ids: ids}); err != nil {
			return err
		}
		for _, f := range orphans {
			if err := s.store.Remove(f.StorageKey); err != nil {
				slog.WarnContext(ctx, "файл не удалён с диска", slog.String("key", f.StorageKey), slog.String("error", err.Error()))
			}
		}
		st.Counts["files"] = len(orphans)
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + CleanupJobName, EntityType: "job", EntityID: CleanupJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: сессий %d, токенов %d, попыток входа %d, файлов %d",
					CleanupJobName, today.Format(), st.Counts["sessions"], st.Counts["tokens"], st.Counts["loginAttempts"], st.Counts["files"]),
				After: map[string]any{"businessDate": today.String(), "stats": st.Counts},
			})
		})
	})
}
