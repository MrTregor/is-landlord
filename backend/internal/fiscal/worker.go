package fiscal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/money"
)

// activeStates — задача по чеку одна, пока она не завершена: повтор после завершения (кнопка «Повторить»,
// fiscal.reconcile) ставит новую.
var activeStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable,
	rivertype.JobStateScheduled,
}

// SendArgs — задача fiscal.send (07 Фоновые задачи).
type SendArgs struct {
	FiscalReceiptID uuid.UUID `json:"fiscalReceiptId" river:"unique"`
}

// Kind — вид задачи River.
func (SendArgs) Kind() string { return "fiscal.send" }

// InsertOpts — очередь integrations, 10 попыток; одна незавершённая задача на чек.
func (SendArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueIntegrations, MaxAttempts: jobs.DefaultMaxAttempts,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeStates}}
}

// SendWorker выполняет fiscal.send.
type SendWorker struct {
	river.WorkerDefaults[SendArgs]
	Svc *Service
}

// Work — отправка чека в кассу.
func (w *SendWorker) Work(ctx context.Context, job *river.Job[SendArgs]) error {
	return w.Svc.Send(ctx, job.Args.FiscalReceiptID)
}

// PollArgs — задача fiscal.poll. Номер попытки в аргументах не нужен: пауза между опросами считается по времени
// ожидания чека, задача откладывается (river.JobSnooze), а не ставится заново (docs/open-questions.md, M8).
type PollArgs struct {
	FiscalReceiptID uuid.UUID `json:"fiscalReceiptId" river:"unique"`
}

// Kind — вид задачи River.
func (PollArgs) Kind() string { return "fiscal.poll" }

// InsertOpts — очередь integrations; одна незавершённая задача опроса на чек (колбэк и сверка не плодят опросы).
func (PollArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueIntegrations, MaxAttempts: jobs.DefaultMaxAttempts,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeStates}}
}

// PollWorker выполняет fiscal.poll.
type PollWorker struct {
	river.WorkerDefaults[PollArgs]
	Svc *Service
}

// Work — опрос отчёта; чек ещё обрабатывается — задача откладывается.
func (w *PollWorker) Work(ctx context.Context, job *river.Job[PollArgs]) error {
	next, err := w.Svc.Poll(ctx, job.Args.FiscalReceiptID)
	if err != nil {
		return err
	}
	if next > 0 {
		return river.JobSnooze(next)
	}
	return nil
}

// ---------------------------------------------------------------- fiscal.reconcile

// ReconcileJobName — задача fiscal.reconcile (07 Фоновые задачи).
const ReconcileJobName = "fiscal.reconcile"

// ReconcileArgs — аргументы fiscal.reconcile.
type ReconcileArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (ReconcileArgs) Kind() string { return ReconcileJobName }

// InsertOpts — очередь integrations, уникальность по интервалу запуска.
func (ReconcileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueIntegrations, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// ReconcileSchedule — описание задачи: каждые 15 минут.
func ReconcileSchedule() jobs.Daily {
	return jobs.Daily{Name: ReconcileJobName, EveryMinutes: 15, Args: func(a jobs.DailyArgs) river.JobArgs { return ReconcileArgs{a} }}
}

// ReconcileWorker выполняет fiscal.reconcile.
type ReconcileWorker struct {
	river.WorkerDefaults[ReconcileArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *ReconcileWorker) Work(ctx context.Context, job *river.Job[ReconcileArgs]) error {
	_, err := w.Svc.RunReconcile(ctx, job.Args.DailyArgs)
	return err
}

// Reconcile — зависшие чеки (07): в wait дольше минуты — опрос отчёта; в queued дольше 5 минут — отправка
// (уникальность задач: если задача по чеку уже есть, новая не ставится).
func (s *Service) Reconcile(ctx context.Context, st *jobs.Stats) error {
	q := s.db.Queries()
	waiting, err := q.StaleWaitFiscal(ctx)
	if err != nil {
		return err
	}
	queued, err := q.StaleQueuedFiscal(ctx)
	if err != nil {
		return err
	}
	for _, id := range waiting {
		err := s.db.WithTx(ctx, func(tx *db.Tx) error {
			res, err := s.jobs.InsertTx(ctx, tx.Tx, PollArgs{FiscalReceiptID: id}, nil)
			if err == nil && !res.UniqueSkippedAsDuplicate {
				st.Inc("polls")
			}
			return err
		})
		if err != nil {
			st.Fail("чек "+id.String(), err)
		}
	}
	for _, id := range queued {
		err := s.db.WithTx(ctx, func(tx *db.Tx) error {
			res, err := s.jobs.InsertTx(ctx, tx.Tx, SendArgs{FiscalReceiptID: id}, nil)
			if err == nil && !res.UniqueSkippedAsDuplicate {
				st.Inc("sends")
			}
			return err
		})
		if err != nil {
			st.Fail("чек "+id.String(), err)
		}
	}
	st.Counts["waiting"] = len(waiting)
	st.Counts["queued"] = len(queued)
	return s.notifyStuck(ctx, st)
}

// notifyStuck — чек в queued дольше суток (касса недоступна): письмо собственникам owner_fiscal_error, одно на
// время ожидания (№ 63); по 54-ФЗ чек выдаётся при расчёте, собственник должен узнать о задержке.
func (s *Service) notifyStuck(ctx context.Context, st *jobs.Stats) error {
	stuck, err := s.db.Queries().StuckQueuedFiscal(ctx)
	if err != nil {
		return err
	}
	for _, r := range stuck {
		err := s.db.WithTx(ctx, func(tx *db.Tx) error {
			f, err := tx.Q.GetFiscalReceipt(ctx, r.ID)
			if err != nil {
				return err
			}
			pc, err := receiptContext(ctx, tx.Q, f)
			if err != nil {
				return err
			}
			owners, err := tx.Q.ActiveOwnerUsers(ctx)
			if err != nil {
				return err
			}
			for _, o := range owners {
				created, err := s.notify.Enqueue(ctx, tx, notify.Email{
					Code: "owner_fiscal_error", To: o.Email, RelatedType: "fiscal_receipt", RelatedID: &f.ID,
					DedupeKey: fmt.Sprintf("owner_fiscal_error:%s:queued:%d:%s", f.ID, r.QueuedSince.Unix(), o.ID),
					Vars: map[string]string{
						"amount": money.Kop(f.TotalKop).Format(), "error_text": stuckText, "contract.number": pc.ContractNumber,
						"link": s.notify.Link("/fiscal-receipts"),
					},
				})
				if err != nil {
					return err
				}
				if created {
					st.Inc("stuckLetters")
				}
			}
			return nil
		})
		if err != nil {
			st.Fail("чек "+r.ID.String(), err)
		}
	}
	return nil
}

// stuckText — текст ошибки в письме о чеке, который больше суток не удаётся отправить в кассу.
const stuckText = "Чек больше суток не удаётся отправить в кассу: касса недоступна. Отправка повторяется автоматически"

// RunReconcile — fiscal.reconcile с записью job_runs и итогом в журнале.
func (s *Service) RunReconcile(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	if args.BusinessDate.IsZero() {
		args.BusinessDate = s.clock.Today()
	}
	return jobs.RunDaily(ctx, s.db, s.clock, ReconcileJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		if err := s.Reconcile(ctx, st); err != nil {
			return err
		}
		// Запуск каждые 15 минут: в журнал — только запуски, которые что-то сделали (иначе 96 пустых записей в день).
		if st.Counts["polls"]+st.Counts["sends"]+st.Counts["stuckLetters"] == 0 && len(st.Errors) == 0 {
			return nil
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + ReconcileJobName, EntityType: "job", EntityID: ReconcileJobName,
				Summary: fmt.Sprintf("Плановая задача %s: опросов поставлено %d, отправок %d, писем о чеках в очереди %d, ошибок %d",
					ReconcileJobName, st.Counts["polls"], st.Counts["sends"], st.Counts["stuckLetters"], len(st.Errors)),
				After: map[string]any{"businessDate": args.BusinessDate.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}
