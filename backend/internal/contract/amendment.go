package contract

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
)

// Виды допсоглашений (05 разд. 8.3).
const (
	AmendIndexation  = "indexation"
	AmendRentChange  = "rent_change"
	AmendExtension   = "extension"
	AmendTermsChange = "terms_change"
	AmendOther       = "other"
)

// AmendmentInput — условия допсоглашения.
type AmendmentInput struct {
	Kind                 string
	EffectiveFrom        date.Date
	NewRentKop           *int64
	NewUtilitiesFixedKop *int64
	NewPaymentDay        *int
	NewEndDate           *date.Date
	BodyText             *string
}

var amendTitles = map[string]string{
	AmendIndexation: "индексация платы", AmendRentChange: "изменение платы", AmendExtension: "продление",
	AmendTermsChange: "изменение условий", AmendOther: "иные изменения",
}

// Amendments — допсоглашения договора.
func (s *Service) Amendments(ctx context.Context, contractID uuid.UUID) ([]sqlc.ContractAmendment, error) {
	if _, err := s.Get(ctx, contractID); err != nil {
		return nil, err
	}
	return s.db.Queries().ListAmendments(ctx, contractID)
}

// normalize оставляет поля, относящиеся к виду, и проверяет их (05 разд. 8.3).
func (s *Service) checkAmendment(ctx context.Context, q *sqlc.Queries, c sqlc.Contract, in *AmendmentInput) error {
	in.BodyText = trim(in.BodyText)
	var errs []apperr.FieldError
	add := func(field, msg string) { errs = append(errs, apperr.FieldError{Field: field, Message: msg}) }
	switch in.Kind {
	case AmendIndexation, AmendRentChange:
		in.NewUtilitiesFixedKop, in.NewPaymentDay, in.NewEndDate = nil, nil, nil
		if in.NewRentKop == nil || *in.NewRentKop <= 0 {
			add("newRentKop", "Укажите новую плату")
		}
	case AmendExtension:
		in.NewRentKop, in.NewUtilitiesFixedKop, in.NewPaymentDay = nil, nil, nil
		switch {
		case in.NewEndDate == nil || !in.NewEndDate.After(c.EndDate):
			add("newEndDate", "Новая дата окончания должна быть позже текущей ("+c.EndDate.Format()+")")
		case c.LegalBasis == BasisCh35 && !in.NewEndDate.Before(c.StartDate.AddMonths(60)):
			add("newEndDate", "Договор найма заключается на срок не более пяти лет (ст. 683 ГК РФ)")
		}
	case AmendTermsChange:
		in.NewRentKop, in.NewEndDate = nil, nil
		if in.NewPaymentDay == nil && in.NewUtilitiesFixedKop == nil {
			add("newPaymentDay", "Укажите новый день оплаты или фиксированную часть ЖКУ")
		}
		if in.NewPaymentDay != nil && (*in.NewPaymentDay < 1 || *in.NewPaymentDay > 28) {
			add("newPaymentDay", "День оплаты — от 1 до 28")
		}
		if in.NewUtilitiesFixedKop != nil && *in.NewUtilitiesFixedKop < 0 {
			add("newUtilitiesFixedKop", "Сумма не может быть отрицательной")
		}
	case AmendOther:
		in.NewRentKop, in.NewUtilitiesFixedKop, in.NewPaymentDay, in.NewEndDate = nil, nil, nil, nil
		if in.BodyText == nil {
			add("bodyText", "Опишите изменения условий")
		}
	default:
		add("kind", "Неизвестный вид допсоглашения")
	}
	changesTerms := in.NewRentKop != nil || in.NewUtilitiesFixedKop != nil || in.NewPaymentDay != nil
	if changesTerms && in.EffectiveFrom.Day() != 1 {
		add("effectiveFrom", "Изменения платы, дня оплаты и фиксированной части вступают с первого числа месяца")
	}
	if in.EffectiveFrom.Before(c.StartDate) || in.EffectiveFrom.After(actualEnd(c)) && in.Kind != AmendExtension {
		add("effectiveFrom", "Дата вступления — в пределах срока договора")
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	if in.NewRentKop != nil {
		return s.checkRentIncrease(ctx, q, c, in.EffectiveFrom, *in.NewRentKop)
	}
	return nil
}

// checkRentIncrease — повышение платы не раньше чем через 12 месяцев после предыдущего повышения или начала договора.
func (s *Service) checkRentIncrease(ctx context.Context, q *sqlc.Queries, c sqlc.Contract, from date.Date, newRent int64) error {
	terms, err := q.ListTerms(ctx, c.ID)
	if err != nil {
		return err
	}
	cur := calc.TermsFor(TermsOf(terms), from)
	if newRent <= int64(cur.RentKop) {
		return nil
	}
	last, err := q.LastRentIncrease(ctx, c.ID)
	since := c.StartDate
	switch {
	case err == nil:
		since = last
	case !db.IsNoRows(err):
		return err
	}
	if from.Before(since.AddMonths(12)) {
		return apperr.BusinessRule("Плату можно повышать не чаще одного раза в 12 месяцев. Последнее повышение — с " + since.Format())
	}
	return nil
}

func (s *Service) lockAmendment(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.ContractAmendment, sqlc.Contract, error) {
	a, err := tx.Q.GetAmendmentForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return a, sqlc.Contract{}, apperr.NotFound()
	}
	if err != nil {
		return a, sqlc.Contract{}, err
	}
	c, err := s.lock(ctx, tx, a.ContractID)
	return a, c, err
}

func int16p(v *int) *int16 {
	if v == nil {
		return nil
	}
	x := int16(*v) //nolint:gosec // 1–28
	return &x
}

func amendSnapshot(a sqlc.ContractAmendment) map[string]any {
	return map[string]any{
		"number": a.Number, "kind": a.Kind, "status": a.Status, "effectiveFrom": a.EffectiveFrom.String(),
		"newRentKop": a.NewRentKop, "newUtilitiesFixedKop": a.NewUtilitiesFixedKop, "newPaymentDay": a.NewPaymentDay,
		"newEndDate": dateStr(a.NewEndDate), "bodyText": a.BodyText,
	}
}

// CreateAmendment — черновик допсоглашения к действующему договору.
func (s *Service) CreateAmendment(ctx context.Context, contractID uuid.UUID, in AmendmentInput) (sqlc.ContractAmendment, error) {
	var a sqlc.ContractAmendment
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := s.lock(ctx, tx, contractID)
		if err != nil {
			return err
		}
		if c.Status != StatusActive && c.Status != StatusExtended {
			return apperr.Conflict(apperr.CodeConflictState, "Допсоглашение оформляется к действующему договору")
		}
		if err := s.checkAmendment(ctx, tx.Q, c, &in); err != nil {
			return err
		}
		a, err = tx.Q.CreateAmendment(ctx, sqlc.CreateAmendmentParams{
			ContractID: contractID, Kind: in.Kind, EffectiveFrom: in.EffectiveFrom, NewRentKop: in.NewRentKop,
			NewUtilitiesFixedKop: in.NewUtilitiesFixedKop, NewPaymentDay: int16p(in.NewPaymentDay), NewEndDate: in.NewEndDate,
			BodyText: in.BodyText, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "amendment.create", EntityType: "amendment", EntityID: a.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: допсоглашение № %d (%s)", c.Number, a.Number, amendTitles[a.Kind]),
			After:   amendSnapshot(a),
		})
	})
	return a, err
}

// UpdateAmendment — правка черновика допсоглашения.
func (s *Service) UpdateAmendment(ctx context.Context, id uuid.UUID, in AmendmentInput) (sqlc.ContractAmendment, error) {
	var a sqlc.ContractAmendment
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, c, err := s.lockAmendment(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Изменить можно только черновик допсоглашения")
		}
		if err := s.checkAmendment(ctx, tx.Q, c, &in); err != nil {
			return err
		}
		a, err = tx.Q.UpdateAmendment(ctx, sqlc.UpdateAmendmentParams{
			ID: id, Kind: in.Kind, EffectiveFrom: in.EffectiveFrom, NewRentKop: in.NewRentKop,
			NewUtilitiesFixedKop: in.NewUtilitiesFixedKop, NewPaymentDay: int16p(in.NewPaymentDay), NewEndDate: in.NewEndDate,
			BodyText: in.BodyText,
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "amendment.create", EntityType: "amendment", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: изменён черновик допсоглашения № %d", c.Number, a.Number),
			Before:  amendSnapshot(before), After: amendSnapshot(a),
		})
	})
	return a, err
}

// SignAmendment — «Подписано»: изменения применяются в той же транзакции (05 разд. 8.3): новая версия условий,
// продление (разд. 4.6), сдвиг даты индексации, письмо нанимателю об изменении платы.
// Пересчёт уже созданных начислений (разд. 5.4) подключается на этапе M4.
func (s *Service) SignAmendment(ctx context.Context, id uuid.UUID, signedOn date.Date) (sqlc.ContractAmendment, error) {
	if signedOn.After(s.clock.Today()) {
		return sqlc.ContractAmendment{}, apperr.Validation(apperr.FieldError{Field: "signedOn", Message: "Дата подписания не может быть позже сегодняшней"})
	}
	var a sqlc.ContractAmendment
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, c, err := s.lockAmendment(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Допсоглашение уже подписано или отменено")
		}
		if c.Status != StatusActive && c.Status != StatusExtended {
			return apperr.Conflict(apperr.CodeConflictState, "Договор не действует — допсоглашение подписать нельзя")
		}
		in := AmendmentInput{
			Kind: before.Kind, EffectiveFrom: before.EffectiveFrom, NewRentKop: before.NewRentKop,
			NewUtilitiesFixedKop: before.NewUtilitiesFixedKop, NewEndDate: before.NewEndDate, BodyText: before.BodyText,
		}
		if before.NewPaymentDay != nil {
			in.NewPaymentDay = ptr(int(*before.NewPaymentDay))
		}
		if err := s.checkAmendment(ctx, tx.Q, c, &in); err != nil {
			return err
		}
		if a, err = tx.Q.SignAmendment(ctx, sqlc.SignAmendmentParams{ID: id, SignedOn: &signedOn, Now: ptr(s.clock.Now())}); err != nil {
			return err
		}
		rentChanged := false
		if a.NewRentKop != nil || a.NewUtilitiesFixedKop != nil || a.NewPaymentDay != nil {
			if rentChanged, err = s.applyTerms(ctx, tx, c, a); err != nil {
				return err
			}
		}
		if a.Kind == AmendExtension {
			if c, err = s.extend(ctx, tx, c, *a.NewEndDate); err != nil {
				return err
			}
		}
		if a.Kind == AmendIndexation {
			if err := s.advanceIndexation(ctx, tx, c); err != nil {
				return err
			}
		}
		if rentChanged {
			if err := s.queueRentChanged(ctx, tx, c, a); err != nil {
				return err
			}
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "amendment.sign", EntityType: "amendment", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: подписано допсоглашение № %d (%s) от %s", c.Number, a.Number, amendTitles[a.Kind], signedOn.Format()),
			Before:  amendSnapshot(before), After: amendSnapshot(a),
		})
	})
	return a, err
}

// applyTerms — новая версия условий с effective_from допсоглашения; возвращает, изменилась ли плата.
func (s *Service) applyTerms(ctx context.Context, tx *db.Tx, c sqlc.Contract, a sqlc.ContractAmendment) (bool, error) {
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return false, err
	}
	cur := calc.TermsFor(TermsOf(terms), a.EffectiveFrom)
	next := sqlc.InsertTermsParams{
		ContractID: c.ID, Version: int16(len(terms) + 1), EffectiveFrom: a.EffectiveFrom, //nolint:gosec // версий немного
		RentKop: int64(cur.RentKop), UtilitiesFixedKop: int64(cur.UtilitiesFixedKop), PaymentDay: int16(cur.PaymentDay), //nolint:gosec // 1–28
		AmendmentID: &a.ID, Now: s.clock.Now(),
	}
	if a.NewRentKop != nil {
		next.RentKop = *a.NewRentKop
	}
	if a.NewUtilitiesFixedKop != nil {
		next.UtilitiesFixedKop = *a.NewUtilitiesFixedKop
	}
	if a.NewPaymentDay != nil {
		next.PaymentDay = *a.NewPaymentDay
	}
	if _, err := tx.Q.InsertTerms(ctx, next); err != nil {
		if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
			return false, apperr.Conflict(apperr.CodeConflictExists, "С "+a.EffectiveFrom.Format()+" уже действуют условия другого допсоглашения")
		}
		return false, err
	}
	return next.RentKop != int64(cur.RentKop), nil
}

// extend — продление (05 разд. 4.6): проверка пересечения с последующими договорами, новая дата окончания,
// статус «Продлён», пересоздание событий окончания.
func (s *Service) extend(ctx context.Context, tx *db.Tx, c sqlc.Contract, newEnd date.Date) (sqlc.Contract, error) {
	over, err := tx.Q.OverlappingContracts(ctx, sqlc.OverlappingContractsParams{
		PropertyID: c.PropertyID, ExcludeID: c.ID, FromDate: c.EndDate.AddDays(1), ToDate: newEnd,
	})
	if err != nil {
		return c, err
	}
	if len(over) > 0 {
		o := over[0]
		end := o.StartDate
		if o.ActualEndDate != nil {
			end = *o.ActualEndDate
		}
		return c, apperr.Conflict(apperr.CodeConflictOverlap, fmt.Sprintf("Сроки пересекаются с договором %s (%s)", o.Number, period(o.StartDate, end)))
	}
	e, err := tx.Q.ExtendContract(ctx, sqlc.ExtendContractParams{ID: c.ID, EndDate: newEnd})
	if err != nil {
		return c, dbError(err)
	}
	return e, s.syncEvents(ctx, tx, e)
}

// advanceIndexation — после подписания или отмены допсоглашения об индексации: next_indexation_on += период,
// событие индексации отмечается выполненным (05 разд. 8.1, 13.1).
func (s *Service) advanceIndexation(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	if c.NextIndexationOn == nil {
		return nil
	}
	key := fmt.Sprintf("indexation:%s:%s", c.ID, c.NextIndexationOn.String())
	now := s.clock.Now()
	if err := tx.Q.SetAutoEventDone(ctx, sqlc.SetAutoEventDoneParams{AutoKey: &key, Done: true, DoneAt: &now}); err != nil {
		return err
	}
	next := c.NextIndexationOn.AddMonths(int(c.IndexationPeriodMonths))
	if err := tx.Q.SetNextIndexation(ctx, sqlc.SetNextIndexationParams{ID: c.ID, NextIndexationOn: &next}); err != nil {
		return err
	}
	c.NextIndexationOn = &next
	return s.syncEvents(ctx, tx, c)
}

// queueRentChanged — письмо нанимателю rent_changed (10 Уведомления).
func (s *Service) queueRentChanged(ctx context.Context, tx *db.Tx, c sqlc.Contract, a sqlc.ContractAmendment) error {
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
		Code: "rent_changed", To: to, DedupeKey: "rent_changed:" + a.ID.String(), RelatedType: "contract", RelatedID: &c.ID,
		Vars: map[string]string{
			"tenant.greeting": greeting, "contract.number": c.Number, "property.address": p.Address, "property.code": p.Code,
			"new_rent": money.Kop(*a.NewRentKop).Format(), "effective_from": a.EffectiveFrom.Format(),
		},
	})
}

// CancelAmendment — отменить черновик с причиной; для индексации дата следующей индексации сдвигается на период.
func (s *Service) CancelAmendment(ctx context.Context, id uuid.UUID, reason string) (sqlc.ContractAmendment, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return sqlc.ContractAmendment{}, apperr.Validation(apperr.FieldError{Field: "reason", Message: "Укажите причину отмены"})
	}
	var a sqlc.ContractAmendment
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, c, err := s.lockAmendment(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Отменить можно только черновик допсоглашения")
		}
		if a, err = tx.Q.CancelAmendment(ctx, sqlc.CancelAmendmentParams{ID: id, CancelReason: &reason}); err != nil {
			return err
		}
		if a.Kind == AmendIndexation && c.NextIndexationOn != nil && a.EffectiveFrom.Equal(*c.NextIndexationOn) {
			if err := s.advanceIndexation(ctx, tx, c); err != nil {
				return err
			}
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "amendment.cancel", EntityType: "amendment", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: отменено допсоглашение № %d", c.Number, a.Number),
			Before:  map[string]any{"status": before.Status}, After: map[string]any{"status": a.Status, "reason": reason},
		})
	})
	return a, err
}
