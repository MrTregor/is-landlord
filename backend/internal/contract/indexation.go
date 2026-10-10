package contract

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/money"
)

// IndexationJobName — имя задачи (07 Фоновые задачи, порядок 4).
const IndexationJobName = "billing.indexation_daily"

// IndexationArgs — аргументы billing.indexation_daily.
type IndexationArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (IndexationArgs) Kind() string { return IndexationJobName }

// InsertOpts — уникальность по бизнес-дате и запуску.
func (IndexationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// IndexationDaily — описание задачи (00:30).
func IndexationDaily() jobs.Daily {
	return jobs.Daily{Name: IndexationJobName, Hour: 0, Minute: 30, Args: func(a jobs.DailyArgs) river.JobArgs { return IndexationArgs{a} }}
}

// IndexationWorker выполняет billing.indexation_daily.
type IndexationWorker struct {
	river.WorkerDefaults[IndexationArgs]
	Svc *Service
}

// Work — запуск задачи.
func (w *IndexationWorker) Work(ctx context.Context, job *river.Job[IndexationArgs]) error {
	_, err := w.Svc.RunIndexationDaily(ctx, job.Args.DailyArgs)
	return err
}

// RunIndexationDaily — черновики допсоглашений об индексации (05 разд. 8.2): за indexation_draft_days_before дней до
// next_indexation_on, новая плата — текущая × (1 + pct/100) с округлением до рубля; письмо собственникам.
func (s *Service) RunIndexationDaily(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, IndexationJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		org, err := s.settings.Get(ctx, s.db.Queries())
		if err != nil {
			return err
		}
		list, err := s.db.Queries().ContractsForIndexation(ctx, sqlc.ContractsForIndexationParams{DaysBefore: int32(org.IndexationDraftDaysBefore), Today: today})
		if err != nil {
			return err
		}
		for _, c := range list {
			created := false
			err := s.db.WithTx(ctx, func(tx *db.Tx) error {
				var err error
				created, err = s.indexationDraft(ctx, tx, c.ID)
				return err
			})
			if err != nil {
				st.Fail("договор "+c.Number, err)
				continue
			}
			if created {
				st.Inc("created")
			}
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + IndexationJobName, EntityType: "job", EntityID: IndexationJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: черновиков индексации %d, ошибок %d",
					IndexationJobName, today.Format(), st.Counts["created"], len(st.Errors)),
				After: map[string]any{"businessDate": today.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}

func (s *Service) indexationDraft(ctx context.Context, tx *db.Tx, id uuid.UUID) (bool, error) {
	c, err := s.lock(ctx, tx, id)
	if err != nil || c.NextIndexationOn == nil || c.IndexationPct == nil {
		return false, err
	}
	on := *c.NextIndexationOn
	exists, err := tx.Q.HasIndexationAmendment(ctx, sqlc.HasIndexationAmendmentParams{ContractID: c.ID, EffectiveFrom: on})
	if err != nil || exists {
		return false, err
	}
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return false, err
	}
	cur := calc.TermsFor(billing.TermsOf(terms), on.AddDays(-1))
	newRent := calc.IndexedRent(cur.RentKop, *c.IndexationPct)
	nr := int64(newRent)
	a, err := tx.Q.CreateAmendment(ctx, sqlc.CreateAmendmentParams{
		ContractID: c.ID, Kind: AmendIndexation, EffectiveFrom: on, NewRentKop: &nr, AutoCreated: true, Now: s.clock.Now(),
	})
	if err != nil {
		return false, err
	}
	owners, err := tx.Q.ActiveOwnerUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range owners {
		if err := s.notify.Queue(ctx, tx, notify.Email{
			Code: "owner_indexation_due", To: u.Email, DedupeKey: fmt.Sprintf("owner_indexation:%s:%s", a.ID, u.ID),
			RelatedType: "contract", RelatedID: &c.ID,
			Vars: map[string]string{
				"contract.number": c.Number, "effective_from": on.Format(), "current_rent": cur.RentKop.Format(),
				"new_rent": money.Kop(nr).Format(), "link": s.notify.Link("/contracts/" + c.ID.String() + "?tab=amendments"),
			},
		}); err != nil {
			return false, err
		}
	}
	return true, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "amendment.create", EntityType: "amendment", EntityID: a.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: черновик допсоглашения об индексации с %s, плата %s → %s",
			c.Number, on.Format(), cur.RentKop.Format(), money.Kop(nr).Format()),
		After: amendSnapshot(a),
	})
}
