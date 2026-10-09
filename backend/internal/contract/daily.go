package contract

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
)

// DailyJobName — имя задачи (07 Фоновые задачи, порядок 1).
const DailyJobName = "contracts.daily"

// DailyArgs — аргументы задачи contracts.daily.
type DailyArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (DailyArgs) Kind() string { return DailyJobName }

// InsertOpts — уникальность по бизнес-дате.
func (DailyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Daily — описание ежедневной задачи для расписания (00:05 по Москве).
func Daily() jobs.Daily {
	return jobs.Daily{Name: DailyJobName, Hour: 0, Minute: 5, Args: func(a jobs.DailyArgs) river.JobArgs { return DailyArgs{a} }}
}

// DailyWorker выполняет contracts.daily.
type DailyWorker struct {
	river.WorkerDefaults[DailyArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *DailyWorker) Work(ctx context.Context, job *river.Job[DailyArgs]) error {
	_, err := w.Svc.RunDaily(ctx, job.Args.DailyArgs)
	return err
}

// RunDaily — contracts.daily на бизнес-дату (05 разд. 4.8, 07): истёкшие договоры → «Завершён», объекты:
// «Забронирован» → «Сдан» в день начала, «Сдан» → «Свободен» на следующий день после окончания или расторжения.
// Каждый договор — в своей транзакции; ошибка по договору пишется в stats и не останавливает остальные.
func (s *Service) RunDaily(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, DailyJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		q := s.db.Queries()
		toFinish, err := q.ContractsToFinish(ctx, today)
		if err != nil {
			return err
		}
		for _, c := range toFinish {
			s.each(ctx, st, c.Number, "finished", func(tx *db.Tx) error {
				cur, err := s.lock(ctx, tx, c.ID)
				if err != nil || (cur.Status != StatusActive && cur.Status != StatusExtended) || !cur.EndDate.Before(today) {
					return err
				}
				_, err = s.finish(ctx, tx, cur)
				return err
			})
		}
		ended, err := q.ContractsEndedBy(ctx, today)
		if err != nil {
			return err
		}
		for _, c := range ended {
			s.each(ctx, st, c.Number, "released", func(tx *db.Tx) error {
				if _, err := tx.Q.GetPropertyForUpdate(ctx, c.PropertyID); err != nil {
					return err
				}
				return s.release(ctx, tx, c)
			})
		}
		starting, err := q.ContractsStartingBy(ctx, today)
		if err != nil {
			return err
		}
		for _, c := range starting {
			s.each(ctx, st, c.Number, "rented", func(tx *db.Tx) error {
				if _, err := tx.Q.GetPropertyForUpdate(ctx, c.PropertyID); err != nil {
					return err
				}
				return s.setRented(ctx, tx, c)
			})
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + DailyJobName, EntityType: "job", EntityID: DailyJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: завершено договоров %d, освобождено объектов %d, сдано объектов %d, ошибок %d",
					DailyJobName, today.Format(), st.Counts["finished"], st.Counts["released"], st.Counts["rented"], len(st.Errors)),
				After: map[string]any{"businessDate": today.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}

func (s *Service) each(ctx context.Context, st *jobs.Stats, number, counter string, fn func(tx *db.Tx) error) {
	if err := s.db.WithTx(ctx, fn); err != nil {
		st.Fail("договор "+number, err)
		return
	}
	st.Inc(counter)
}
