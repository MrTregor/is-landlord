package mailing

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
)

// Имена плановых задач (07 Фоновые задачи, порядок 7–10).
const (
	DigestJobName    = "notify.owner_digest"
	RemindersJobName = "notify.payment_reminders"
	ContractEndName  = "notify.contract_end"
	ReadingsJobName  = "notify.readings_reminder"
)

func businessDate(args jobs.DailyArgs, today date.Date) date.Date {
	if args.BusinessDate.IsZero() {
		return today
	}
	return args.BusinessDate
}

// contractMail — адрес нанимателя и общие переменные письма по договору (10 Уведомления, раздел 1).
// Наниматель обезличен или без e-mail — пустой адрес (письмо skipped).
func contractMail(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (string, map[string]string, error) {
	c, err := q.ContractMailInfo(ctx, id)
	if err != nil {
		return "", nil, err
	}
	to := ""
	if c.Email != nil && c.AnonymizedAt == nil {
		to = *c.Email
	}
	greeting := "Уважаемые коллеги"
	if c.TenantKind != "legal" {
		greeting = names.Greeting(c.FirstName, c.MiddleName)
	}
	return to, map[string]string{
		"tenant.greeting": greeting, "contract.number": c.Number, "property.address": c.PropertyAddress,
		"property.code": c.PropertyCode,
	}, nil
}

// each — действие по договору в своей транзакции; ошибка пишется в stats и не останавливает остальные (07).
func (s *Service) each(ctx context.Context, st *jobs.Stats, what, counter string, fn func(tx *db.Tx) (bool, error)) {
	var created bool
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		created, err = fn(tx)
		return err
	})
	switch {
	case err != nil:
		st.Fail(what, err)
	case created:
		st.Inc(counter)
	default:
		st.Inc("already_sent")
	}
}

// summary — итог задачи в журнал действий (job.<name>).
func (s *Service) summary(ctx context.Context, name string, today date.Date, st *jobs.Stats, text string) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "job." + name, EntityType: "job", EntityID: name,
			Summary: fmt.Sprintf("Плановая задача %s за %s: %s, ошибок %d", name, today.Format(), text, len(st.Errors)),
			After:   map[string]any{"businessDate": today.String(), "stats": st.Counts, "errors": len(st.Errors)},
		})
	})
}

func rub(k int64) string { return money.Kop(k).Format() }

// ---------------------------------------------------------------- notify.payment_reminders

// chargeRow — начисление для таблицы письма.
type chargeRow struct {
	title        string
	due          date.Date
	outstanding  int64
	firstOverdue date.Date
}

// RunPaymentReminders — напоминания об оплате (за remind_before_due_days дней до срока, одно письмо на договор и
// срок) и о просрочке (в первый день просрочки и затем каждые overdue_repeat_days дней, одно письмо на договор
// в день) — 10 Уведомления, раздел 2.
func (s *Service) RunPaymentReminders(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := businessDate(args, s.clock.Today())
	return jobs.RunDaily(ctx, s.db, s.clock, RemindersJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		q := s.db.Queries()
		o, err := s.settings.Get(ctx, q)
		if err != nil {
			return err
		}
		due := today.AddDays(int(o.RemindBeforeDueDays))
		dueRows, err := q.DueMainCharges(ctx, due)
		if err != nil {
			return err
		}
		upcoming := map[uuid.UUID][]chargeRow{}
		var order []uuid.UUID
		for _, r := range dueRows {
			if _, ok := upcoming[r.ContractID]; !ok {
				order = append(order, r.ContractID)
			}
			upcoming[r.ContractID] = append(upcoming[r.ContractID], chargeRow{title: r.Title, due: r.DueDate, outstanding: r.OutstandingKop})
		}
		for _, id := range order {
			rows := upcoming[id]
			s.each(ctx, st, "договор "+id.String(), "reminders", func(tx *db.Tx) (bool, error) {
				return s.reminder(ctx, tx, id, due, rows)
			})
		}

		overRows, err := q.OverdueMainCharges(ctx, today)
		if err != nil {
			return err
		}
		overdue := map[uuid.UUID][]chargeRow{}
		order = order[:0]
		for _, r := range overRows {
			if _, ok := overdue[r.ContractID]; !ok {
				order = append(order, r.ContractID)
			}
			overdue[r.ContractID] = append(overdue[r.ContractID], chargeRow{
				title: r.Title, due: r.DueDate, outstanding: r.OutstandingKop, firstOverdue: r.FirstOverdue,
			})
		}
		repeat := max(int(o.OverdueRepeatDays), 1)
		for _, id := range order {
			rows := overdue[id]
			if !OverdueDay(firstDays(rows), today, repeat) {
				continue
			}
			s.each(ctx, st, "договор "+id.String(), "overdue", func(tx *db.Tx) (bool, error) {
				return s.overdueLetter(ctx, tx, id, today, rows)
			})
		}
		return s.summary(ctx, RemindersJobName, today, st, fmt.Sprintf("напоминаний %d, писем о просрочке %d",
			st.Counts["reminders"], st.Counts["overdue"]))
	})
}

func firstDays(rows []chargeRow) []date.Date {
	out := make([]date.Date, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.firstOverdue)
	}
	return out
}

// OverdueDay — день письма о просрочке (docs/open-questions.md, № 77): первый день просрочки любого начисления
// договора или каждые repeat дней от первого дня просрочки самого раннего из них.
func OverdueDay(first []date.Date, today date.Date, repeat int) bool {
	if len(first) == 0 {
		return false
	}
	earliest := first[0]
	for _, f := range first {
		if f.Equal(today) {
			return true
		}
		if f.Before(earliest) {
			earliest = f
		}
	}
	d := today.Sub(earliest)
	return d >= 0 && d%repeat == 0
}

func lines(rows []chargeRow) (string, int64) {
	var total int64
	list := make([]notify.Line, 0, len(rows))
	for _, r := range rows {
		total += r.outstanding
		list = append(list, notify.Line{Title: r.title, Due: r.due.Format(), Amount: rub(r.outstanding)})
	}
	return notify.LinesTable(list), total
}

func (s *Service) reminder(ctx context.Context, tx *db.Tx, contractID uuid.UUID, due date.Date, rows []chargeRow) (bool, error) {
	to, vars, err := contractMail(ctx, tx.Q, contractID)
	if err != nil {
		return false, err
	}
	block, total := lines(rows)
	vars["due_date"], vars["total"] = due.Format(), rub(total)
	return s.notify.Enqueue(ctx, tx, notify.Email{
		Code: "payment_reminder", To: to, Vars: vars, Blocks: map[string]string{"lines": block},
		DedupeKey:   "payment_reminder:" + contractID.String() + ":" + due.String(),
		RelatedType: "contract", RelatedID: &contractID,
	})
}

func (s *Service) overdueLetter(ctx context.Context, tx *db.Tx, contractID uuid.UUID, today date.Date, rows []chargeRow) (bool, error) {
	to, vars, err := contractMail(ctx, tx.Q, contractID)
	if err != nil {
		return false, err
	}
	penalties, err := tx.Q.AccruingPenaltiesOf(ctx, contractID)
	if err != nil {
		return false, err
	}
	block, total := lines(rows)
	vars["overdue_total"], vars["penalties_accruing"] = rub(total), rub(penalties)
	return s.notify.Enqueue(ctx, tx, notify.Email{
		Code: "payment_overdue", To: to, Vars: vars, Blocks: map[string]string{"lines": block},
		DedupeKey:   "payment_overdue:" + contractID.String() + ":" + today.String(),
		RelatedType: "contract", RelatedID: &contractID,
	})
}

// ---------------------------------------------------------------- notify.contract_end

// NoticeFor — за сколько дней (N из contract_end_notice_days) отправить письмо договору, у которого до окончания
// daysLeft дней: наименьшее N ≥ daysLeft, если договор уже действовал за N дней до окончания (docs/open-questions.md,
// № 78). ok = false — письма нет.
func NoticeFor(notice []int16, daysLeft int, start, end date.Date) (int, bool) {
	ns := make([]int, 0, len(notice))
	for _, n := range notice {
		ns = append(ns, int(n))
	}
	sort.Ints(ns)
	for _, n := range ns {
		if daysLeft <= n {
			return n, !start.After(end.AddDays(-n))
		}
	}
	return 0, false
}

// RunContractEnd — письма нанимателю за N дней до окончания договора (10 Уведомления, раздел 2).
func (s *Service) RunContractEnd(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := businessDate(args, s.clock.Today())
	return jobs.RunDaily(ctx, s.db, s.clock, ContractEndName, args, func(ctx context.Context, st *jobs.Stats) error {
		q := s.db.Queries()
		o, err := s.settings.Get(ctx, q)
		if err != nil {
			return err
		}
		maxN := 0
		for _, n := range o.ContractEndNoticeDays {
			maxN = max(maxN, int(n))
		}
		list, err := q.EndingContracts(ctx, sqlc.EndingContractsParams{Today: today, Until: today.AddDays(maxN)})
		if err != nil {
			return err
		}
		for _, c := range list {
			daysLeft := c.EndDate.Sub(today)
			n, ok := NoticeFor(o.ContractEndNoticeDays, daysLeft, c.StartDate, c.EndDate)
			if !ok {
				continue
			}
			s.each(ctx, st, "договор "+c.Number, "letters", func(tx *db.Tx) (bool, error) {
				to, vars, err := contractMail(ctx, tx.Q, c.ID)
				if err != nil {
					return false, err
				}
				vars["end_date"], vars["days_left"] = c.EndDate.Format(), fmt.Sprint(daysLeft)
				return s.notify.Enqueue(ctx, tx, notify.Email{
					Code: "contract_ending", To: to, Vars: vars,
					DedupeKey:   fmt.Sprintf("contract_ending:%s:%d", c.ID, n),
					RelatedType: "contract", RelatedID: &c.ID,
				})
			})
		}
		return s.summary(ctx, ContractEndName, today, st, fmt.Sprintf("писем об окончании договора %d", st.Counts["letters"]))
	})
}

// ---------------------------------------------------------------- notify.readings_reminder

// RunReadingsReminder — с readings_reminder_day каждого месяца — напоминание передать показания за месяц
// (договоры с ЖКУ по счётчикам), одно письмо на договор и месяц; не отправляется, если показания за месяц по всем
// счётчикам уже переданы или приняты (№ 79); окно приёма — по 5-е число следующего месяца
// (05 разд. 6.2).
func (s *Service) RunReadingsReminder(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := businessDate(args, s.clock.Today())
	return jobs.RunDaily(ctx, s.db, s.clock, ReadingsJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		q := s.db.Queries()
		o, err := s.settings.Get(ctx, q)
		if err != nil {
			return err
		}
		if today.Day() < int(o.ReadingsReminderDay) {
			return s.summary(ctx, ReadingsJobName, today, st, "день напоминания ещё не наступил")
		}
		month := today.FirstOfMonth()
		windowTo := month.AddMonths(1).AddDays(4)
		list, err := q.MeterContracts(ctx, sqlc.MeterContractsParams{Today: today, Month: month})
		if err != nil {
			return err
		}
		for _, c := range list {
			s.each(ctx, st, "договор "+c.Number, "letters", func(tx *db.Tx) (bool, error) {
				to, vars, err := contractMail(ctx, tx.Q, c.ID)
				if err != nil {
					return false, err
				}
				vars["period"], vars["window_to"] = calc.MonthName(month), windowTo.Format()
				return s.notify.Enqueue(ctx, tx, notify.Email{
					Code: "readings_reminder", To: to, Vars: vars,
					DedupeKey:   fmt.Sprintf("readings_reminder:%s:%s", c.ID, month.String()[:7]),
					RelatedType: "contract", RelatedID: &c.ID,
				})
			})
		}
		return s.summary(ctx, ReadingsJobName, today, st, fmt.Sprintf("напоминаний о показаниях %d", st.Counts["letters"]))
	})
}
