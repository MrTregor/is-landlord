// Package calendar — календарь событий (FR-09, docs/spec/05 разд. 13): хранимые автоматические и ручные события,
// вычисляемые сроки оплаты.
package calendar

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/contract"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/reqctx"
)

// MaxRangeDays — наибольший запрашиваемый диапазон (06 разд. 13).
const MaxRangeDays = 92

// Вычисляемые виды событий.
const (
	KindPaymentDue = "payment_due"
	KindOverdue    = "overdue"
)

var manualKinds = map[string]bool{"showing": true, "repair": true, "custom": true}

// Service — календарь.
type Service struct {
	db    *db.DB
	audit *audit.Service
	clock clock.Clock
}

// NewService создаёт сервис календаря.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock) *Service {
	return &Service{db: database, audit: au, clock: clk}
}

// Event — событие календаря (хранимое или вычисляемое).
type Event struct {
	ID             *uuid.UUID
	Kind           string
	Title          string
	Date           date.Date
	StartTime      *string
	EndTime        *string
	PropertyID     *uuid.UUID
	PropertyCode   *string
	ContractID     *uuid.UUID
	ContractNumber *string
	Source         string
	Done           bool
	Notes          *string
	AmountKop      *int64
	Forecast       bool
}

// Filter — запрос событий.
type Filter struct {
	From, To   date.Date
	PropertyID *uuid.UUID
	Kinds      []string
}

func (f Filter) wants(kind string) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// List — хранимые события и вычисляемые сроки оплаты в диапазоне (не более 92 дней).
func (s *Service) List(ctx context.Context, f Filter) ([]Event, error) {
	if f.To.Before(f.From) {
		return nil, apperr.Validation(apperr.FieldError{Field: "to", Message: "Дата окончания периода раньше даты начала"})
	}
	if date.DaysInclusive(f.From, f.To) > MaxRangeDays {
		return nil, apperr.Validation(apperr.FieldError{Field: "to", Message: fmt.Sprintf("Период — не более %d дней", MaxRangeDays)})
	}
	q := s.db.Queries()
	stored := []string{}
	for _, k := range f.Kinds {
		if k != KindPaymentDue && k != KindOverdue {
			stored = append(stored, k)
		}
	}
	var out []Event
	if len(f.Kinds) == 0 || len(stored) > 0 {
		rows, err := q.ListCalendarEvents(ctx, sqlc.ListCalendarEventsParams{FromDate: f.From, ToDate: f.To, PropertyID: f.PropertyID, Kinds: stored})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, fromRow(sqlc.CalendarEvent{
				ID: r.ID, Kind: r.Kind, Title: r.Title, StartsOn: r.StartsOn, StartTime: r.StartTime, EndTime: r.EndTime,
				PropertyID: r.PropertyID, ContractID: r.ContractID, Source: r.Source, Done: r.Done, Notes: r.Notes,
			}, r.PropertyCode, r.ContractNumber))
		}
	}
	if f.wants(KindPaymentDue) || f.wants(KindOverdue) {
		due, err := s.fromCharges(ctx, q, f)
		if err != nil {
			return nil, err
		}
		out = append(out, due...)
	}
	if f.wants(KindPaymentDue) {
		due, err := s.paymentDue(ctx, q, f)
		if err != nil {
			return nil, err
		}
		out = append(out, due...)
	}
	return out, nil
}

// fromCharges — «Срок оплаты» по начислениям с сроком в диапазоне и «Просрочка» — начисления основного долга с
// прошедшим сроком и долгом (05 разд. 13.2).
func (s *Service) fromCharges(ctx context.Context, q *sqlc.Queries, f Filter) ([]Event, error) {
	rows, err := q.ChargesInRange(ctx, sqlc.ChargesInRangeParams{FromDate: f.From, ToDate: f.To, PropertyID: f.PropertyID})
	if err != nil {
		return nil, err
	}
	today := s.clock.Today()
	var out []Event
	for _, r := range rows {
		id, number, pid, code := r.ContractID, r.ContractNumber, r.PropertyID, r.PropertyCode
		outstanding := r.AmountKop - r.PaidKop
		ev := Event{Date: *r.DueDate, PropertyID: &pid, PropertyCode: &code, ContractID: &id, ContractNumber: &number,
			Source: "computed", Done: outstanding <= 0}
		if f.wants(KindPaymentDue) {
			e := ev
			amount := r.AmountKop
			e.Kind, e.AmountKop = KindPaymentDue, &amount
			e.Title = fmt.Sprintf("Срок оплаты: %s, договор %s", r.Title, r.ContractNumber)
			out = append(out, e)
		}
		if f.wants(KindOverdue) && r.Kind != "penalty" && outstanding > 0 && r.DueDate.Before(today) {
			e := ev
			e.Kind, e.AmountKop = KindOverdue, &outstanding
			e.Title = fmt.Sprintf("Просрочка: %s, договор %s — %s", r.Title, r.ContractNumber, money.Kop(outstanding).Format())
			out = append(out, e)
		}
	}
	return out, nil
}

// paymentDue — прогноз сроков оплаты по графику договоров для будущих месяцев, начисления за которые ещё не созданы
// (05 разд. 13.2): сумма — по текущей версии условий.
func (s *Service) paymentDue(ctx context.Context, q *sqlc.Queries, f Filter) ([]Event, error) {
	contracts, err := q.ScheduleContracts(ctx, sqlc.ScheduleContractsParams{FromDate: f.From, ToDate: f.To, PropertyID: f.PropertyID})
	if err != nil {
		return nil, err
	}
	if len(contracts) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(contracts))
	for _, c := range contracts {
		ids = append(ids, c.ID)
	}
	terms, err := q.ListTermsForContracts(ctx, ids)
	if err != nil {
		return nil, err
	}
	charged := map[string]bool{}
	months, err := q.ContractMonthsWithRent(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range months {
		charged[m.ContractID.String()+m.PeriodMonth.String()] = true
	}
	today := s.clock.Today()
	byContract := map[uuid.UUID][]sqlc.ContractTerm{}
	for _, t := range terms {
		byContract[t.ContractID] = append(byContract[t.ContractID], t)
	}
	var out []Event
	for _, c := range contracts {
		ts := byContract[c.ID]
		if len(ts) == 0 {
			continue
		}
		end := c.EndDate
		if c.ActualEndDate != nil {
			end = *c.ActualEndDate
		}
		for _, l := range calc.Schedule(c.StartDate, end, c.FirstDueDate, billing.TermsOf(ts), c.UtilitiesMode == contract.UtilitiesFixed, 0) {
			if l.DueDate.Before(f.From) || l.DueDate.After(f.To) || l.DueDate.Before(today) || charged[c.ID.String()+l.Month.String()] {
				continue
			}
			amount := int64(l.RentKop + l.UtilitiesKop)
			id, number, pid, code := c.ID, c.Number, c.PropertyID, c.PropertyCode
			out = append(out, Event{
				Kind: KindPaymentDue, Title: fmt.Sprintf("Срок оплаты по договору %s: %s (прогноз)", c.Number, money.Kop(amount).Format()),
				Date: l.DueDate, PropertyID: &pid, PropertyCode: &code, ContractID: &id, ContractNumber: &number,
				Source: "computed", AmountKop: &amount, Forecast: true,
			})
		}
	}
	return out, nil
}

func fromRow(e sqlc.CalendarEvent, propertyCode, contractNumber *string) Event {
	id := e.ID
	return Event{
		ID: &id, Kind: e.Kind, Title: e.Title, Date: e.StartsOn, StartTime: timeOut(e.StartTime), EndTime: timeOut(e.EndTime),
		PropertyID: e.PropertyID, PropertyCode: propertyCode, ContractID: e.ContractID, ContractNumber: contractNumber,
		Source: e.Source, Done: e.Done, Notes: e.Notes,
	}
}

// Get — хранимое событие.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Event, error) {
	r, err := s.db.Queries().GetCalendarEvent(ctx, id)
	if db.IsNoRows(err) {
		return Event{}, apperr.NotFound()
	}
	if err != nil {
		return Event{}, err
	}
	return fromRow(sqlc.CalendarEvent{
		ID: r.ID, Kind: r.Kind, Title: r.Title, StartsOn: r.StartsOn, StartTime: r.StartTime, EndTime: r.EndTime,
		PropertyID: r.PropertyID, ContractID: r.ContractID, Source: r.Source, Done: r.Done, Notes: r.Notes,
	}, r.PropertyCode, r.ContractNumber), nil
}

func timeOut(t pgtype.Time) *string {
	if !t.Valid {
		return nil
	}
	m := t.Microseconds / 60_000_000
	s := fmt.Sprintf("%02d:%02d", m/60, m%60)
	return &s
}

func timeIn(s *string) (pgtype.Time, error) {
	if s == nil || *s == "" {
		return pgtype.Time{}, nil
	}
	parts := strings.Split(*s, ":")
	if len(parts) != 2 {
		return pgtype.Time{}, fmt.Errorf("время %q", *s)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return pgtype.Time{}, fmt.Errorf("время %q", *s)
	}
	return pgtype.Time{Microseconds: int64(h*60+m) * 60_000_000, Valid: true}, nil
}

// Input — ручное событие.
type Input struct {
	Kind       string
	Title      string
	StartsOn   date.Date
	StartTime  *string
	EndTime    *string
	PropertyID *uuid.UUID
	ContractID *uuid.UUID
	Notes      *string
}

type checked struct {
	start, end pgtype.Time
	propertyID *uuid.UUID
	contractID *uuid.UUID
}

func (s *Service) check(ctx context.Context, q *sqlc.Queries, in *Input) (checked, error) {
	var c checked
	var errs []apperr.FieldError
	in.Title = strings.TrimSpace(in.Title)
	in.Notes = trim(in.Notes)
	if !manualKinds[in.Kind] {
		errs = append(errs, apperr.FieldError{Field: "kind", Message: "Вид — показ, плановый ремонт или прочее"})
	}
	if in.Title == "" {
		errs = append(errs, apperr.FieldError{Field: "title", Message: "Укажите название"})
	}
	var err error
	if c.start, err = timeIn(in.StartTime); err != nil {
		errs = append(errs, apperr.FieldError{Field: "startTime", Message: "Время — ЧЧ:ММ"})
	}
	if c.end, err = timeIn(in.EndTime); err != nil {
		errs = append(errs, apperr.FieldError{Field: "endTime", Message: "Время — ЧЧ:ММ"})
	}
	if c.end.Valid && (!c.start.Valid || c.end.Microseconds <= c.start.Microseconds) {
		errs = append(errs, apperr.FieldError{Field: "endTime", Message: "Время окончания — позже времени начала"})
	}
	c.propertyID, c.contractID = in.PropertyID, in.ContractID
	if c.contractID != nil {
		ct, err := q.GetContract(ctx, *c.contractID)
		switch {
		case db.IsNoRows(err):
			errs = append(errs, apperr.FieldError{Field: "contractId", Message: "Договор не найден"})
		case err != nil:
			return c, err
		case c.propertyID == nil:
			c.propertyID = &ct.PropertyID
		case *c.propertyID != ct.PropertyID:
			errs = append(errs, apperr.FieldError{Field: "contractId", Message: "Договор относится к другому объекту"})
		}
	}
	if c.propertyID != nil {
		if _, err := q.GetProperty(ctx, *c.propertyID); db.IsNoRows(err) {
			errs = append(errs, apperr.FieldError{Field: "propertyId", Message: "Объект не найден"})
		} else if err != nil {
			return c, err
		}
	}
	if len(errs) > 0 {
		return c, apperr.Validation(errs...)
	}
	return c, nil
}

func snapshot(in Input) map[string]any {
	return map[string]any{"kind": in.Kind, "title": in.Title, "startsOn": in.StartsOn.String(), "startTime": in.StartTime, "endTime": in.EndTime}
}

// Create — ручное событие (показ, плановый ремонт, прочее).
func (s *Service) Create(ctx context.Context, in Input) (Event, error) {
	var id uuid.UUID
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := s.check(ctx, tx.Q, &in)
		if err != nil {
			return err
		}
		id, err = tx.Q.CreateManualEvent(ctx, sqlc.CreateManualEventParams{
			Kind: in.Kind, Title: in.Title, StartsOn: in.StartsOn, StartTime: c.start, EndTime: c.end,
			PropertyID: c.propertyID, ContractID: c.contractID, Notes: in.Notes, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "calendar.create", EntityType: "calendar_event", EntityID: id.String(),
			Summary: fmt.Sprintf("Добавлено событие «%s» на %s", in.Title, in.StartsOn.Format()), After: snapshot(in),
		})
	})
	if err != nil {
		return Event{}, err
	}
	return s.Get(ctx, id)
}

// Update — ручное событие меняется целиком; у автоматического меняются только заметки.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, onlyNotes bool) (Event, error) {
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		e, err := tx.Q.GetCalendarEventForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if e.Source == "auto" || onlyNotes {
			if err := tx.Q.SetEventNotes(ctx, sqlc.SetEventNotesParams{ID: id, Notes: trim(in.Notes)}); err != nil {
				return err
			}
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "calendar.update", EntityType: "calendar_event", EntityID: id.String(),
				Summary: fmt.Sprintf("Изменены заметки события «%s»", e.Title),
			})
		}
		c, err := s.check(ctx, tx.Q, &in)
		if err != nil {
			return err
		}
		if err := tx.Q.UpdateManualEvent(ctx, sqlc.UpdateManualEventParams{
			ID: id, Kind: in.Kind, Title: in.Title, StartsOn: in.StartsOn, StartTime: c.start, EndTime: c.end,
			PropertyID: c.propertyID, ContractID: c.contractID, Notes: in.Notes,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "calendar.update", EntityType: "calendar_event", EntityID: id.String(),
			Summary: fmt.Sprintf("Изменено событие «%s»", in.Title),
			Before:  map[string]any{"kind": e.Kind, "title": e.Title, "startsOn": e.StartsOn.String()}, After: snapshot(in),
		})
	})
	if err != nil {
		return Event{}, err
	}
	return s.Get(ctx, id)
}

// Delete — удалить ручное событие.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		e, err := tx.Q.GetCalendarEventForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if e.Source == "auto" {
			return apperr.BusinessRule("Автоматическое событие удалить нельзя — его можно отметить выполненным")
		}
		if err := tx.Q.DeleteCalendarEvent(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "calendar.delete", EntityType: "calendar_event", EntityID: id.String(),
			Summary: fmt.Sprintf("Удалено событие «%s» на %s", e.Title, e.StartsOn.Format()),
		})
	})
}

// SetDone — отметка «Выполнено» хранимого события.
func (s *Service) SetDone(ctx context.Context, id uuid.UUID, done bool) (Event, error) {
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		e, err := tx.Q.GetCalendarEventForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		var at = s.clock.Now()
		params := sqlc.SetEventDoneParams{ID: id, Done: done}
		if done {
			params.DoneAt = &at
		}
		if err := tx.Q.SetEventDone(ctx, params); err != nil {
			return err
		}
		state := "отмечено выполненным"
		if !done {
			state = "снята отметка о выполнении"
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "calendar.update", EntityType: "calendar_event", EntityID: id.String(),
			Summary: fmt.Sprintf("Событие «%s»: %s", e.Title, state),
			Before:  map[string]any{"done": e.Done}, After: map[string]any{"done": done},
		})
	})
	if err != nil {
		return Event{}, err
	}
	return s.Get(ctx, id)
}

func trim(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}
