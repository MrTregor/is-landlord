package mailing

import (
	"context"
	"fmt"

	"landlord/backend/internal/calendar"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
)

// Виды расхождений для сводки (04, справочник значений).
var discrepancyTitles = map[string]string{
	"underpayment": "Недоплата", "overpayment": "Переплата", "unidentified": "Неопознанный платёж",
}

// RunOwnerDigest — утренняя сводка собственникам (если owner_daily_digest и есть что сообщить): события сегодня
// и завтра, просрочки, новые заявки, открытые расхождения, ошибки чеков и задач (07, 10 Уведомления, раздел 3).
func (s *Service) RunOwnerDigest(ctx context.Context, args jobs.DailyArgs) (*jobs.Stats, error) {
	today := businessDate(args, s.clock.Today())
	return jobs.RunDaily(ctx, s.db, s.clock, DigestJobName, args, func(ctx context.Context, st *jobs.Stats) error {
		o, err := s.settings.Get(ctx, s.db.Queries())
		if err != nil {
			return err
		}
		if !o.OwnerDailyDigest {
			st.Inc("disabled")
			return s.summary(ctx, DigestJobName, today, st, "сводка выключена в настройках")
		}
		sections, err := s.digest(ctx, today)
		if err != nil {
			return err
		}
		empty := true
		for _, sec := range sections {
			empty = empty && len(sec.Items) == 0
		}
		if empty {
			st.Inc("empty")
			return s.summary(ctx, DigestJobName, today, st, "сообщать нечего")
		}
		block := notify.DigestBlock(sections)
		s.each(ctx, st, "сводка", "sent", func(tx *db.Tx) (bool, error) {
			n, err := s.notify.QueueOwners(ctx, tx, notify.Email{
				Code: "owner_daily_digest", Vars: map[string]string{"today": today.Format()},
				Blocks: map[string]string{"lines": block}, DedupeKey: "owner_digest:" + today.String(),
			})
			return n > 0, err
		})
		return s.summary(ctx, DigestJobName, today, st, "сводка подготовлена")
	})
}

// digest — разделы сводки.
func (s *Service) digest(ctx context.Context, today date.Date) ([]notify.Section, error) {
	q := s.db.Queries()
	link := s.notify.Link
	events := func(d date.Date) ([]notify.Item, error) {
		list, err := s.calendar.List(ctx, calendar.Filter{From: d, To: d})
		if err != nil {
			return nil, err
		}
		var items []notify.Item
		for _, e := range list {
			if e.Done {
				continue
			}
			text := e.Title
			if e.PropertyCode != nil {
				text += ", " + *e.PropertyCode
			}
			if e.AmountKop != nil {
				text += " — " + rub(*e.AmountKop)
			}
			items = append(items, notify.Item{Text: text, Link: link("/calendar")})
		}
		return items, nil
	}
	todayItems, err := events(today)
	if err != nil {
		return nil, err
	}
	tomorrowItems, err := events(today.AddDays(1))
	if err != nil {
		return nil, err
	}
	var overdue, requests, discrepancies, errorsItems []notify.Item
	over, err := q.DigestOverdue(ctx, today)
	if err != nil {
		return nil, err
	}
	for _, c := range over {
		overdue = append(overdue, notify.Item{
			Text: fmt.Sprintf("Договор № %s, %s — %s", c.Number, c.PropertyCode, rub(c.OverdueKop)),
			Link: link("/contracts/" + c.ID.String()),
		})
	}
	reqs, err := q.DigestNewRequests(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range reqs {
		requests = append(requests, notify.Item{
			Text: fmt.Sprintf("%s «%s», %s", r.Number, r.Title, r.PropertyCode), Link: link("/requests/" + r.ID.String()),
		})
	}
	disc, err := q.DigestDiscrepancies(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range disc {
		discrepancies = append(discrepancies, notify.Item{
			Text: fmt.Sprintf("%s: %d на %s", discrepancyTitles[d.Kind], d.N, rub(d.AmountKop)), Link: link("/reconciliation"),
		})
	}
	fiscalErrors, err := q.DigestFiscalErrors(ctx)
	if err != nil {
		return nil, err
	}
	if fiscalErrors > 0 {
		errorsItems = append(errorsItems, notify.Item{
			Text: fmt.Sprintf("Чеки с ошибкой или к формированию вручную: %d", fiscalErrors), Link: link("/fiscal-receipts"),
		})
	}
	queuedFiscal, err := q.DigestQueuedFiscal(ctx)
	if err != nil {
		return nil, err
	}
	if queuedFiscal > 0 {
		errorsItems = append(errorsItems, notify.Item{
			Text: fmt.Sprintf("Чеки в очереди больше часа (касса недоступна): %d", queuedFiscal), Link: link("/fiscal-receipts"),
		})
	}
	failed, err := q.DigestFailedJobs(ctx, today.AddDays(-1))
	if err != nil {
		return nil, err
	}
	for _, name := range failed {
		errorsItems = append(errorsItems, notify.Item{
			Text: "Плановая задача " + name + " завершилась с ошибкой", Link: link("/settings/service"),
		})
	}
	return []notify.Section{
		{Title: "Сегодня", Items: todayItems}, {Title: "Завтра", Items: tomorrowItems}, {Title: "Просрочки", Items: overdue},
		{Title: "Новые заявки", Items: requests}, {Title: "Расхождения", Items: discrepancies},
		{Title: "Ошибки чеков и задач", Items: errorsItems},
	}, nil
}
