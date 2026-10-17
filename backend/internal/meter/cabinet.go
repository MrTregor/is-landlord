package meter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
)

// Показания нанимателя в кабинете (15 Экраны нанимателя, раздел 5; 05 разд. 6.2).

// Window — окно приёма показаний от нанимателя.
type Window struct {
	PeriodMonth, From, To date.Date
	Open                  bool
}

// WindowFor — окно месяца M: с reminderDay числа M по 5-е число M+1 включительно (05 разд. 6.2).
func WindowFor(month date.Date, reminderDay int) Window {
	month = month.FirstOfMonth()
	return Window{PeriodMonth: month, From: month.AddDays(reminderDay - 1), To: month.AddMonths(1).AddDays(4)}
}

// CurrentWindow — окно на дату: по 5-е число включительно — за прошлый месяц (открыто), дальше — за текущий
// (открыто с reminderDay).
func CurrentWindow(today date.Date, reminderDay int) Window {
	if prev := WindowFor(today.FirstOfMonth().AddMonths(-1), reminderDay); !today.After(prev.To) {
		prev.Open = true
		return prev
	}
	w := WindowFor(today, reminderDay)
	w.Open = !today.Before(w.From)
	return w
}

// monthRequested — запрашиваются ли у нанимателя показания за месяц по договору: ЖКУ по счётчикам, договор
// действует, месяц не раньше начала и не месяц окончания (за него показание берётся из акта возврата).
func monthRequested(c sqlc.Contract, month date.Date) bool {
	if c.UtilitiesMode != "meters" || (c.Status != "active" && c.Status != "extended") {
		return false
	}
	end := c.EndDate
	if c.ActualEndDate != nil {
		end = *c.ActualEndDate
	}
	return !month.Before(c.StartDate.FirstOfMonth()) && month.Before(end.FirstOfMonth())
}

// CabinetReading — месячное показание для кабинета.
type CabinetReading struct {
	ID           uuid.UUID
	PeriodMonth  date.Date
	Value        decimal.Decimal
	Status       string
	RejectReason *string
}

// CabinetMeter — счётчик объекта: предыдущее принятое показание, показание за месяц окна и история.
type CabinetMeter struct {
	Meter     sqlc.Meter
	LastValue *decimal.Decimal
	LastDate  *date.Date
	Current   *CabinetReading
	History   []CabinetReading
}

// CabinetMeters — счётчики и окно приёма.
type CabinetMeters struct {
	Meters []CabinetMeter
	Window Window
}

// TenantMeters — счётчики объекта договора нанимателя (ЖКУ по счётчикам), окно приёма и история за 12 месяцев
// по этому договору.
func (s *Service) TenantMeters(ctx context.Context, c sqlc.Contract) (CabinetMeters, error) {
	q := s.db.Queries()
	o, err := s.settings.Get(ctx, q)
	if err != nil {
		return CabinetMeters{}, err
	}
	w := CurrentWindow(s.clock.Today(), int(o.ReadingsReminderDay))
	w.Open = w.Open && monthRequested(c, w.PeriodMonth)
	out := CabinetMeters{Meters: []CabinetMeter{}, Window: w}
	if c.UtilitiesMode != "meters" {
		return out, nil
	}
	meters, err := q.ActivePropertyMeters(ctx, c.PropertyID)
	if err != nil {
		return out, err
	}
	rows, err := q.ContractMonthReadings(ctx, sqlc.ContractMonthReadingsParams{ContractID: &c.ID, FromMonth: w.PeriodMonth.AddMonths(-11)})
	if err != nil {
		return out, err
	}
	for _, m := range meters {
		cm := CabinetMeter{Meter: m, History: []CabinetReading{}}
		for _, r := range rows {
			if r.MeterID != m.ID {
				continue
			}
			v := CabinetReading{ID: r.ID, PeriodMonth: r.PeriodMonth, Value: r.Value, Status: r.Status, RejectReason: r.RejectReason}
			cm.History = append(cm.History, v)
			if cm.Current == nil && r.PeriodMonth == w.PeriodMonth {
				cm.Current = &v
			}
		}
		exclude := uuid.Nil
		if cm.Current != nil {
			exclude = cm.Current.ID
		}
		end := w.PeriodMonth.LastOfMonth()
		prev, err := q.PrevAccepted(ctx, sqlc.PrevAcceptedParams{MeterID: m.ID, BeforeDate: end.AddDays(1), ExcludeID: exclude})
		switch {
		case err == nil:
			cm.LastValue, cm.LastDate = &prev.Value, &prev.ReadingDate
		case db.IsNoRows(err):
			if !m.InitialDate.After(end) {
				cm.LastValue, cm.LastDate = &m.InitialValue, &m.InitialDate
			}
		default:
			return out, err
		}
		out.Meters = append(out.Meters, cm)
	}
	return out, nil
}

// TenantReading — значение счётчика из формы кабинета.
type TenantReading struct {
	MeterID uuid.UUID
	Value   decimal.Decimal
}

// SubmitTenantReadings — показания нанимателя за месяц по всем счётчикам объекта одной формой (05 разд. 6.2):
// только в окне приёма; счётчики с переданным или принятым показанием за месяц не передаются повторно,
// отклонённые — передаются заново. Статус submitted (письмо собственникам owner_readings_submitted) или сразу
// accepted при readings_auto_accept (с начислением ЖКУ, когда приняты все счётчики).
func (s *Service) SubmitTenantReadings(ctx context.Context, c sqlc.Contract, month date.Date, list []TenantReading) error {
	month = month.FirstOfMonth()
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := tx.Q.GetContractForUpdate(ctx, c.ID)
		if err != nil {
			return err
		}
		o, err := s.settings.Get(ctx, tx.Q)
		if err != nil {
			return err
		}
		today := s.clock.Today()
		if c.UtilitiesMode != "meters" {
			return apperr.BusinessRule("По договору показания счётчиков не передаются")
		}
		cur, w := CurrentWindow(today, int(o.ReadingsReminderDay)), WindowFor(month, int(o.ReadingsReminderDay))
		if !cur.Open || cur.PeriodMonth != month {
			return apperr.BusinessRule(fmt.Sprintf("Показания за %s принимаются с %s по %s", calc.MonthName(month), w.From.Format(), w.To.Format()))
		}
		if !monthRequested(c, month) {
			return apperr.BusinessRule(fmt.Sprintf("Показания за %s по договору не принимаются", calc.MonthName(month)))
		}
		meters, err := tx.Q.ActivePropertyMeters(ctx, c.PropertyID)
		if err != nil {
			return err
		}
		need := map[uuid.UUID]sqlc.Meter{}
		for _, m := range meters {
			exists, err := tx.Q.MonthReadingExists(ctx, sqlc.MonthReadingExistsParams{MeterID: m.ID, PeriodMonth: month, ExcludeID: uuid.Nil})
			if err != nil {
				return err
			}
			if !exists {
				need[m.ID] = m
			}
		}
		if len(need) == 0 {
			return apperr.Conflict(apperr.CodeConflictExists, "Показания за "+calc.MonthName(month)+" уже переданы")
		}
		readingDate := date.Min(today, month.LastOfMonth())
		var fields []apperr.FieldError
		seen := map[uuid.UUID]bool{}
		for i, r := range list {
			m, ok := need[r.MeterID]
			switch {
			case seen[r.MeterID]:
				fields = append(fields, apperr.FieldError{Field: fmt.Sprintf("readings[%d].meterId", i), Message: "Счётчик указан дважды"})
				continue
			case !ok:
				fields = append(fields, apperr.FieldError{Field: fmt.Sprintf("readings[%d].meterId", i),
					Message: "Счётчик не найден или показание за месяц уже передано"})
				continue
			}
			seen[r.MeterID] = true
			if err := checkValue(ctx, tx.Q, m, r.Value, readingDate, uuid.Nil, fmt.Sprintf("readings[%d].value", i)); err != nil {
				var ae *apperr.Error
				if !errors.As(err, &ae) || len(ae.Fields) == 0 {
					return err
				}
				fields = append(fields, ae.Fields...)
			}
		}
		if len(fields) == 0 && len(seen) < len(need) {
			fields = append(fields, apperr.FieldError{Field: "readings", Message: "Передайте показания всех счётчиков"})
		}
		if len(fields) > 0 {
			return apperr.Validation(fields...)
		}
		status, now := "submitted", s.clock.Now()
		var decidedAt *time.Time
		if o.ReadingsAutoAccept {
			status, decidedAt = "accepted", &now
		}
		for _, r := range list {
			m := need[r.MeterID]
			id, err := tx.Q.InsertTenantReading(ctx, sqlc.InsertTenantReadingParams{
				MeterID: m.ID, ContractID: &c.ID, PeriodMonth: month, ReadingDate: readingDate, Value: r.Value, Status: status,
				UserID: actorID(ctx), Now: now, DecidedAt: decidedAt,
			})
			if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
				return apperr.Conflict(apperr.CodeConflictExists, "Показания за "+calc.MonthName(month)+" уже переданы")
			}
			if err != nil {
				return err
			}
			if err := s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "reading.submit", EntityType: "meter_reading", EntityID: id.String(),
				Summary: fmt.Sprintf("Наниматель передал показание «%s» за %s по договору № %s: %s", TypeTitles[m.Type],
					calc.MonthName(month), c.Number, comma(r.Value)),
				After: map[string]any{"meterId": m.ID.String(), "periodMonth": month.String(), "value": r.Value.String(), "status": status},
			}); err != nil {
				return err
			}
		}
		if o.ReadingsAutoAccept {
			_, err := s.autoUtilities(ctx, tx, &c.ID, month)
			return err
		}
		return s.notifySubmitted(ctx, tx, c, month)
	})
}

// notifySubmitted — письмо собственникам owner_readings_submitted (10 Уведомления, раздел 3).
func (s *Service) notifySubmitted(ctx context.Context, tx *db.Tx, c sqlc.Contract, month date.Date) error {
	p, err := tx.Q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	t, err := tx.Q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return err
	}
	greeting := "Уважаемые коллеги"
	if t.Kind != "legal" {
		greeting = names.Greeting(t.FirstName, t.MiddleName)
	}
	_, err = s.notify.QueueOwners(ctx, tx, notify.Email{
		Code: "owner_readings_submitted", DedupeKey: fmt.Sprintf("owner_readings:%s:%s", c.ID, month.String()[:7]),
		RelatedType: "contract", RelatedID: &c.ID,
		Vars: map[string]string{
			"tenant.greeting": greeting, "contract.number": c.Number, "property.address": p.Address, "property.code": p.Code,
			"period": calc.MonthName(month), "link": s.notify.Link("/readings?status=submitted"),
		},
	})
	return err
}
