package billing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
)

// Имена плановых задач (07 Фоновые задачи, порядок 2 и 3).
const (
	RentJobName      = "billing.rent_daily"
	PenaltiesJobName = "billing.penalties_daily"
)

// RentArgs — аргументы billing.rent_daily.
type RentArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (RentArgs) Kind() string { return RentJobName }

// InsertOpts — уникальность по бизнес-дате и запуску.
func (RentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// PenaltiesArgs — аргументы billing.penalties_daily.
type PenaltiesArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (PenaltiesArgs) Kind() string { return PenaltiesJobName }

// InsertOpts — уникальность по бизнес-дате и запуску.
func (PenaltiesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RentDaily — описание задачи (00:10).
func RentDaily() jobs.Daily {
	return jobs.Daily{Name: RentJobName, Hour: 0, Minute: 10, Args: func(a jobs.DailyArgs) river.JobArgs { return RentArgs{a} }}
}

// PenaltiesDaily — описание задачи (00:20).
func PenaltiesDaily() jobs.Daily {
	return jobs.Daily{Name: PenaltiesJobName, Hour: 0, Minute: 20, Args: func(a jobs.DailyArgs) river.JobArgs { return PenaltiesArgs{a} }}
}

// RentWorker выполняет billing.rent_daily.
type RentWorker struct {
	river.WorkerDefaults[RentArgs]
	Svc *Service
}

// Work — запуск задачи.
func (w *RentWorker) Work(ctx context.Context, job *river.Job[RentArgs]) error {
	_, err := w.Svc.RunRentDaily(ctx, job.Args.DailyArgs)
	return err
}

// PenaltiesWorker выполняет billing.penalties_daily.
type PenaltiesWorker struct {
	river.WorkerDefaults[PenaltiesArgs]
	Svc *Service
}

// Work — запуск задачи.
func (w *PenaltiesWorker) Work(ctx context.Context, job *river.Job[PenaltiesArgs]) error {
	_, err := w.Svc.RunPenaltiesDaily(ctx, job.Args.DailyArgs)
	return err
}

// RunRentDaily — начисления платы и фиксированной части ЖКУ, срок создания которых наступил, и перераспределение
// авансов (05 разд. 5.3). Каждый договор — в отдельной транзакции; созданные начисления пишутся в журнал одной
// записью job.billing.rent_daily со списком id (12 разд. 7).
func (s *Service) RunRentDaily(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, RentJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		org, err := s.settings.Get(ctx, s.db.Queries())
		if err != nil {
			return err
		}
		// Начисление месяца может появиться раньше его начала на remind_before_due_days дней.
		contracts, err := s.db.Queries().ContractsForRent(ctx, today.AddDays(int(org.RemindBeforeDueDays)+31))
		if err != nil {
			return err
		}
		var ids []string
		for _, c := range contracts {
			var created []uuid.UUID
			err := s.db.WithTx(ctx, func(tx *db.Tx) error {
				cur, err := lockContract(ctx, tx, c.ID)
				if err != nil {
					return err
				}
				created, err = s.EnsureRent(ctx, tx, cur, today)
				return err
			})
			st.Inc("contracts")
			if err != nil {
				st.Fail("договор "+c.Number, err)
				continue
			}
			st.Counts["created"] += len(created)
			for _, id := range created {
				ids = append(ids, id.String())
			}
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + RentJobName, EntityType: "job", EntityID: RentJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: договоров %d, создано начислений %d, ошибок %d",
					RentJobName, today.Format(), st.Counts["contracts"], st.Counts["created"], len(st.Errors)),
				After: map[string]any{"businessDate": today.String(), "chargeIds": ids, "errors": len(st.Errors)},
			})
		})
	})
}

// RunPenaltiesDaily — создание и пересчёт пеней (05 разд. 7.3). Закрытие расхождений «недоплата» подключается
// вместе с платежами (этап M6).
func (s *Service) RunPenaltiesDaily(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, PenaltiesJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		bases, err := s.db.Queries().PenaltyBases(ctx, today)
		if err != nil {
			return err
		}
		for i := 0; i < len(bases); {
			contractID := bases[i].ContractID
			j := i
			for j < len(bases) && bases[j].ContractID == contractID {
				j++
			}
			group := bases[i:j]
			i = j
			err := s.db.WithTx(ctx, func(tx *db.Tx) error {
				if _, err := lockContract(ctx, tx, contractID); err != nil {
					return err
				}
				rebalance := false
				for _, b := range group {
					final, err := s.penaltyFor(ctx, tx, b, today)
					if err != nil {
						return err
					}
					rebalance = rebalance || final
					st.Inc("charges")
				}
				if rebalance {
					return s.Rebalance(ctx, tx, contractID)
				}
				return nil
			})
			if err != nil {
				st.Fail("договор "+contractID.String(), err)
			}
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + PenaltiesJobName, EntityType: "job", EntityID: PenaltiesJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: проверено начислений %d, ошибок %d",
					PenaltiesJobName, today.Format(), st.Counts["charges"], len(st.Errors)),
				After: map[string]any{"businessDate": today.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}
