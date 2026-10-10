package meter

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
)

var readingMax = decimal.NewFromInt(1_000_000)

// ReadingView — показание с предыдущим значением и расходом.
type ReadingView struct {
	Row         sqlc.GetReadingRow
	PrevValue   *decimal.Decimal
	PrevDate    *date.Date
	Consumption *decimal.Decimal
	High        bool
}

// Decision — результат ввода или принятия показания: показание и созданное начисление ЖКУ (если есть).
type Decision struct {
	Reading   ReadingView
	Utilities *billing.UtilitiesResult
}

func comma(d decimal.Decimal) string { return strings.Replace(d.String(), ".", ",", 1) }

// view — показание для API (05 разд. 6.2): предыдущее принятое показание раньше даты или начальное.
func (s *Service) view(ctx context.Context, q *sqlc.Queries, r sqlc.GetReadingRow) (ReadingView, error) {
	v := ReadingView{Row: r}
	m, err := q.GetMeter(ctx, r.MeterID)
	if err != nil {
		return v, err
	}
	prev, err := q.PrevAccepted(ctx, sqlc.PrevAcceptedParams{MeterID: r.MeterID, BeforeDate: r.ReadingDate, ExcludeID: r.ID})
	switch {
	case err == nil:
		v.PrevValue, v.PrevDate = &prev.Value, &prev.ReadingDate
	case db.IsNoRows(err):
		if !m.InitialDate.After(r.ReadingDate) {
			v.PrevValue, v.PrevDate = &m.InitialValue, &m.InitialDate
		}
	default:
		return v, err
	}
	if v.PrevValue != nil {
		c := r.Value.Sub(*v.PrevValue)
		v.Consumption = &c
		recent, err := q.RecentAccepted(ctx, sqlc.RecentAcceptedParams{MeterID: r.MeterID, BeforeDate: r.ReadingDate})
		if err != nil {
			return v, err
		}
		var cons []decimal.Decimal
		for i := 0; i+1 < len(recent) && len(cons) < 3; i++ {
			cons = append(cons, recent[i].Value.Sub(recent[i+1].Value))
		}
		v.High = calc.HighConsumption(c, cons)
	}
	return v, nil
}

// Reading — показание.
func (s *Service) Reading(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (ReadingView, error) {
	r, err := q.GetReading(ctx, id)
	if db.IsNoRows(err) {
		return ReadingView{}, apperr.NotFound()
	}
	if err != nil {
		return ReadingView{}, err
	}
	return s.view(ctx, q, r)
}

// ReadingFilter — фильтры экрана «Показания».
type ReadingFilter struct {
	Status      *string
	PropertyID  *uuid.UUID
	PeriodMonth *date.Date
	Limit       int32
	Offset      int32
}

// Readings — список показаний.
func (s *Service) Readings(ctx context.Context, f ReadingFilter) ([]ReadingView, int64, error) {
	q := s.db.Queries()
	rows, err := q.ListReadings(ctx, sqlc.ListReadingsParams{Status: f.Status, PropertyID: f.PropertyID, PeriodMonth: f.PeriodMonth, Lim: f.Limit, Off: f.Offset})
	if err != nil {
		return nil, 0, err
	}
	n, err := q.CountReadings(ctx, sqlc.CountReadingsParams{Status: f.Status, PropertyID: f.PropertyID, PeriodMonth: f.PeriodMonth})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ReadingView, 0, len(rows))
	for _, r := range rows {
		v, err := s.view(ctx, q, sqlc.GetReadingRow(r))
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, n, nil
}

// checkValue — проверки показания (05 разд. 6.2): формат и не меньше предыдущего принятого.
func checkValue(ctx context.Context, q *sqlc.Queries, m sqlc.Meter, value decimal.Decimal, on date.Date, exclude uuid.UUID) error {
	if value.IsNegative() || value.GreaterThanOrEqual(readingMax) || !value.Equal(value.Round(3)) {
		return apperr.Validation(apperr.FieldError{Field: "value", Message: "Показание — не больше 6 знаков до запятой и 3 после"})
	}
	prev, err := q.PrevAccepted(ctx, sqlc.PrevAcceptedParams{MeterID: m.ID, BeforeDate: on.AddDays(1), ExcludeID: exclude})
	pv, pd := m.InitialValue, m.InitialDate
	switch {
	case err == nil:
		pv, pd = prev.Value, prev.ReadingDate
	case !db.IsNoRows(err):
		return err
	}
	if value.LessThan(pv) {
		return apperr.Validation(apperr.FieldError{Field: "value", Message: fmt.Sprintf("Показание меньше предыдущего (%s от %s)", comma(pv), pd.Format())})
	}
	return nil
}

// autoUtilities — начисление ЖКУ за месяц, когда приняты показания всех счётчиков объекта (05 разд. 6.3).
func (s *Service) autoUtilities(ctx context.Context, tx *db.Tx, contractID *uuid.UUID, month date.Date) (*billing.UtilitiesResult, error) {
	if contractID == nil {
		return nil, nil
	}
	c, err := tx.Q.GetContractForUpdate(ctx, *contractID)
	if err != nil {
		return nil, err
	}
	res, err := s.billing.Utilities(ctx, tx, c, month, true)
	if err != nil || res == nil {
		return res, err
	}
	return res, s.billing.RecordCreated(ctx, tx, c, res, "приняты показания всех счётчиков")
}

// ReadingInput — показание, вносимое собственником.
type ReadingInput struct {
	MeterID     uuid.UUID
	PeriodMonth date.Date
	ReadingDate date.Date
	Value       decimal.Decimal
}

// CreateReading — показание собственника принимается сразу (06 разд. 13).
func (s *Service) CreateReading(ctx context.Context, in ReadingInput) (Decision, error) {
	in.PeriodMonth = in.PeriodMonth.FirstOfMonth()
	var id uuid.UUID
	var res *billing.UtilitiesResult
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		m, err := tx.Q.GetMeter(ctx, in.MeterID)
		if db.IsNoRows(err) {
			return apperr.Validation(apperr.FieldError{Field: "meterId", Message: "Счётчик не найден"})
		}
		if err != nil {
			return err
		}
		if m.ArchivedAt != nil {
			return apperr.Validation(apperr.FieldError{Field: "meterId", Message: "Счётчик в архиве"})
		}
		if in.ReadingDate.After(s.clock.Today()) {
			return apperr.Validation(apperr.FieldError{Field: "readingDate", Message: "Дата показания не может быть позже сегодняшней"})
		}
		exists, err := tx.Q.MonthReadingExists(ctx, sqlc.MonthReadingExistsParams{MeterID: m.ID, PeriodMonth: in.PeriodMonth, ExcludeID: uuid.Nil})
		if err != nil {
			return err
		}
		if exists {
			return apperr.Conflict(apperr.CodeConflictExists, "Показание за "+calc.MonthName(in.PeriodMonth)+" уже внесено")
		}
		if err := checkValue(ctx, tx.Q, m, in.Value, in.ReadingDate, uuid.Nil); err != nil {
			return err
		}
		var contractID *uuid.UUID
		if c, err := tx.Q.ContractForPropertyMonth(ctx, sqlc.ContractForPropertyMonthParams{PropertyID: m.PropertyID, MonthStart: in.PeriodMonth}); err == nil {
			contractID = &c.ID
		} else if !db.IsNoRows(err) {
			return err
		}
		id, err = tx.Q.InsertOwnerReading(ctx, sqlc.InsertOwnerReadingParams{
			MeterID: m.ID, ContractID: contractID, PeriodMonth: in.PeriodMonth, ReadingDate: in.ReadingDate, Value: in.Value,
			UserID: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "reading.accept", EntityType: "meter_reading", EntityID: id.String(),
			Summary: fmt.Sprintf("Внесено показание «%s» за %s: %s", TypeTitles[m.Type], calc.MonthName(in.PeriodMonth), comma(in.Value)),
			After:   map[string]any{"meterId": m.ID.String(), "periodMonth": in.PeriodMonth.String(), "value": in.Value.String()},
		}); err != nil {
			return err
		}
		res, err = s.autoUtilities(ctx, tx, contractID, in.PeriodMonth)
		return err
	})
	if err != nil {
		return Decision{}, err
	}
	v, err := s.Reading(ctx, s.db.Queries(), id)
	return Decision{Reading: v, Utilities: res}, err
}

// AcceptReading — принять показание нанимателя.
func (s *Service) AcceptReading(ctx context.Context, id uuid.UUID) (Decision, error) {
	var res *billing.UtilitiesResult
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		r, err := tx.Q.GetReadingForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if r.Status != "submitted" || r.ActID != nil {
			return apperr.Conflict(apperr.CodeConflictState, "Показание уже рассмотрено")
		}
		m, err := tx.Q.GetMeter(ctx, r.MeterID)
		if err != nil {
			return err
		}
		if err := checkValue(ctx, tx.Q, m, r.Value, r.ReadingDate, r.ID); err != nil {
			return err
		}
		if err := tx.Q.AcceptReading(ctx, sqlc.AcceptReadingParams{ID: id, DecidedBy: actorID(ctx), Now: ptr(s.clock.Now())}); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "reading.accept", EntityType: "meter_reading", EntityID: id.String(),
			Summary: fmt.Sprintf("Принято показание «%s» за %s: %s", TypeTitles[m.Type], calc.MonthName(r.PeriodMonth), comma(r.Value)),
			Before:  map[string]any{"status": r.Status}, After: map[string]any{"status": "accepted"},
		}); err != nil {
			return err
		}
		res, err = s.autoUtilities(ctx, tx, r.ContractID, r.PeriodMonth)
		return err
	})
	if err != nil {
		return Decision{}, err
	}
	v, err := s.Reading(ctx, s.db.Queries(), id)
	return Decision{Reading: v, Utilities: res}, err
}

// RejectReading — отклонить показание с причиной; нанимателю — письмо readings_rejected.
func (s *Service) RejectReading(ctx context.Context, id uuid.UUID, reason string) (ReadingView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ReadingView{}, apperr.Validation(apperr.FieldError{Field: "reason", Message: "Укажите причину"})
	}
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		r, err := tx.Q.GetReadingForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if r.Status != "submitted" || r.ActID != nil {
			return apperr.Conflict(apperr.CodeConflictState, "Показание уже рассмотрено")
		}
		m, err := tx.Q.GetMeter(ctx, r.MeterID)
		if err != nil {
			return err
		}
		if err := tx.Q.RejectReading(ctx, sqlc.RejectReadingParams{ID: id, DecidedBy: actorID(ctx), Now: ptr(s.clock.Now()), Reason: &reason}); err != nil {
			return err
		}
		if r.ContractID != nil && r.Source == "tenant" {
			if err := s.notifyRejected(ctx, tx, *r.ContractID, r, m, reason); err != nil {
				return err
			}
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "reading.reject", EntityType: "meter_reading", EntityID: id.String(),
			Summary: fmt.Sprintf("Отклонено показание «%s» за %s", TypeTitles[m.Type], calc.MonthName(r.PeriodMonth)),
			Before:  map[string]any{"status": r.Status}, After: map[string]any{"status": "rejected", "reason": reason},
		})
	})
	if err != nil {
		return ReadingView{}, err
	}
	return s.Reading(ctx, s.db.Queries(), id)
}

func (s *Service) notifyRejected(ctx context.Context, tx *db.Tx, contractID uuid.UUID, r sqlc.MeterReading, m sqlc.Meter, reason string) error {
	c, err := tx.Q.GetContract(ctx, contractID)
	if err != nil {
		return err
	}
	t, err := tx.Q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return err
	}
	p, err := tx.Q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return err
	}
	to := ""
	if t.Email != nil && t.AnonymizedAt == nil {
		to = *t.Email
	}
	greeting := "Уважаемые коллеги"
	if t.Kind != "legal" {
		greeting = names.Greeting(t.FirstName, t.MiddleName)
	}
	return s.notify.Queue(ctx, tx, notify.Email{
		Code: "readings_rejected", To: to, DedupeKey: "readings_rejected:" + r.ID.String(), RelatedType: "contract", RelatedID: &c.ID,
		Vars: map[string]string{
			"tenant.greeting": greeting, "contract.number": c.Number, "property.address": p.Address, "property.code": p.Code,
			"meter": TypeTitles[m.Type], "value": comma(r.Value), "reason": reason,
		},
	})
}
