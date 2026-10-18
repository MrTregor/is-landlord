package tenant

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
)

// Обезличивание ПДн нанимателя (NFR-Sec-05, 12 Безопасность и ПДн, раздел 6): вручную и по истечении срока хранения.

// RetentionJobName — имя задачи (07 Фоновые задачи, порядок 5).
const RetentionJobName = "pdn.retention_daily"

// RetentionArgs — аргументы задачи pdn.retention_daily.
type RetentionArgs struct{ jobs.DailyArgs }

// Kind — вид задачи River.
func (RetentionArgs) Kind() string { return RetentionJobName }

// InsertOpts — уникальность по бизнес-дате.
func (RetentionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// RetentionDaily — расписание: 03:00 по Москве.
func RetentionDaily() jobs.Daily {
	return jobs.Daily{Name: RetentionJobName, Hour: 3, Args: func(a jobs.DailyArgs) river.JobArgs { return RetentionArgs{a} }}
}

// RetentionWorker выполняет pdn.retention_daily.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	Svc *Service
}

// Work — запуск задачи с записью job_runs.
func (w *RetentionWorker) Work(ctx context.Context, job *river.Job[RetentionArgs]) error {
	_, err := w.Svc.RunRetentionDaily(ctx, job.Args.DailyArgs)
	return err
}

// Anonymize — ручное обезличивание (06 разд. 8): нельзя при договорах «Черновик», «Действует», «Продлён».
func (s *Service) Anonymize(ctx context.Context, id uuid.UUID) (sqlc.Tenant, error) {
	var t sqlc.Tenant
	var keys []string
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		cur, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.AnonymizedAt != nil {
			return apperr.Conflict(apperr.CodeConflictState, "Наниматель уже обезличен")
		}
		live, err := tx.Q.TenantHasLiveContracts(ctx, id)
		if err != nil {
			return err
		}
		if live {
			return apperr.BusinessRule("Обезличить нельзя: есть договоры в статусах «Черновик», «Действует» или «Продлён»")
		}
		t, keys, err = s.anonymizeTx(ctx, tx, cur, "вручную")
		return err
	})
	if err != nil {
		return t, err
	}
	s.removeFiles(ctx, keys)
	return t, nil
}

// anonymizeTx — шаги 1–7 раздела 6 файла 12 в транзакции вызывающего; возвращает ключи файлов для удаления с диска
// после фиксации транзакции.
func (s *Service) anonymizeTx(ctx context.Context, tx *db.Tx, cur sqlc.Tenant, how string) (sqlc.Tenant, []string, error) {
	now := s.clock.Now()
	emails := []string{}
	if cur.Email != nil {
		emails = append(emails, *cur.Email)
	}
	if u, err := tx.Q.GetUserByTenantID(ctx, &cur.ID); err == nil {
		emails = append(emails, u.Email)
		hash, err := s.auth.RandomPasswordHash()
		if err != nil {
			return cur, nil, err
		}
		if _, err := tx.Q.AnonymizeTenantUser(ctx, sqlc.AnonymizeTenantUserParams{TenantID: &cur.ID, PasswordHash: &hash}); err != nil {
			return cur, nil, err
		}
		if err := tx.Q.RevokeUserSessions(ctx, sqlc.RevokeUserSessionsParams{Now: &now, UserID: u.ID}); err != nil {
			return cur, nil, err
		}
		for _, purpose := range []string{"invite", "password_reset"} {
			if err := tx.Q.InvalidateAuthTokens(ctx, sqlc.InvalidateAuthTokensParams{UserID: u.ID, Purpose: purpose, UsedAt: &now}); err != nil {
				return cur, nil, err
			}
		}
	} else if !db.IsNoRows(err) {
		return cur, nil, err
	}
	t, err := tx.Q.AnonymizeTenant(ctx, sqlc.AnonymizeTenantParams{ID: cur.ID, Now: &now})
	if err != nil {
		return cur, nil, err
	}
	if err := tx.Q.AnonymizeOccupants(ctx, cur.ID); err != nil {
		return t, nil, err
	}
	files, err := tx.Q.TenantPdnFiles(ctx, cur.ID)
	if err != nil {
		return t, nil, err
	}
	ids, keys := make([]uuid.UUID, 0, len(files)), make([]string, 0, len(files))
	for _, f := range files {
		ids, keys = append(ids, f.ID), append(keys, f.StorageKey)
	}
	if err := tx.Q.MarkFilesDeleted(ctx, sqlc.MarkFilesDeletedParams{Now: &now, Ids: ids}); err != nil {
		return t, nil, err
	}
	if err := tx.Q.ClearTenantDocuments(ctx, &cur.ID); err != nil {
		return t, nil, err
	}
	if err := tx.Q.AnonymizeEmailLog(ctx, emails); err != nil {
		return t, nil, err
	}
	return t, keys, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "tenant.anonymize", EntityType: "tenant", EntityID: cur.ID.String(),
		Summary: fmt.Sprintf("Обезличен %s (%s): удалено файлов с ПДн %d", summaryName(cur), how, len(keys)),
	})
}

// removeFiles — удаление файлов с диска после фиксации транзакции; ошибка не отменяет обезличивание
// (строки files уже помечены deleted_at).
func (s *Service) removeFiles(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := s.files.Store().Remove(k); err != nil {
			slog.WarnContext(ctx, "файл обезличенного нанимателя не удалён с диска", slog.String("key", k), slog.String("error", err.Error()))
		}
	}
}

// NoContractRetentionMonths — срок хранения ПДн нанимателя, с которым договор так и не заключён (кроме черновиков):
// месяцев от последнего изменения карточки или черновика (12 разд. 6, № 108).
const NoContractRetentionMonths = 6

// RunRetentionDaily — pdn.retention_daily: наниматели, у которых истёк срок хранения (RetentionCandidates: после
// договоров — pdn_retention_years лет, без договоров — NoContractRetentionMonths месяцев); каждый — в своей транзакции.
func (s *Service) RunRetentionDaily(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := args.BusinessDate
	if today.IsZero() {
		today = s.clock.Today()
		args.BusinessDate = today
	}
	return jobs.RunDaily(ctx, s.db, s.clock, RetentionJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		q := s.db.Queries()
		o, err := s.settings.Get(ctx, q)
		if err != nil {
			return err
		}
		ids, err := q.RetentionCandidates(ctx, sqlc.RetentionCandidatesParams{
			Years: int32(o.PdnRetentionYears), NoContractMonths: NoContractRetentionMonths, Today: today,
		})
		if err != nil {
			return err
		}
		for _, id := range ids {
			var keys []string
			err := s.db.WithTx(ctx, func(tx *db.Tx) error {
				cur, err := s.lock(ctx, tx, id)
				if err != nil || cur.AnonymizedAt != nil {
					return err
				}
				_, keys, err = s.anonymizeTx(ctx, tx, cur, "истёк срок хранения")
				return err
			})
			if err != nil {
				st.Fail("наниматель "+id.String()[:8], err)
				continue
			}
			s.removeFiles(ctx, keys)
			st.Inc("anonymized")
		}
		return s.db.WithTx(ctx, func(tx *db.Tx) error {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "job." + RetentionJobName, EntityType: "job", EntityID: RetentionJobName,
				Summary: fmt.Sprintf("Плановая задача %s за %s: обезличено нанимателей %d, ошибок %d",
					RetentionJobName, today.Format(), st.Counts["anonymized"], len(st.Errors)),
				After: map[string]any{"businessDate": today.String(), "stats": st.Counts, "errors": len(st.Errors)},
			})
		})
	})
}
