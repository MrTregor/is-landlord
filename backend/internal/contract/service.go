// Package contract — договоры найма и аренды: черновик, проверки, активация, версии условий, проживающие,
// допсоглашения, акты, расторжение и ежедневная задача contracts.daily (FR-03, docs/spec/05 разд. 4).
package contract

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/settings"
	"landlord/backend/internal/tenant"
)

// Статусы договора.
const (
	StatusDraft      = "draft"
	StatusActive     = "active"
	StatusExtended   = "extended"
	StatusTerminated = "terminated"
	StatusFinished   = "finished"
)

// Вид договора (05 разд. 4.1).
const (
	BasisCh35 = "ch35"
	BasisCh34 = "ch34"
)

// Порядок оплаты ЖКУ.
const (
	UtilitiesIncluded = "included"
	UtilitiesMeters   = "meters"
	UtilitiesFixed    = "fixed"
)

// Service — договоры.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	tenants  *tenant.Service
	settings *settings.Service
	notify   *notify.Service
}

// NewService создаёт сервис договоров.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, ts *tenant.Service, st *settings.Service, nt *notify.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, tenants: ts, settings: st, notify: nt}
}

// Occupant — проживающий.
type Occupant struct {
	FullName  string
	BirthDate *date.Date
	Relation  *string
}

// Input — условия черновика (ContractInput).
type Input struct {
	PropertyID             uuid.UUID
	TenantID               uuid.UUID
	StartDate              date.Date
	EndDate                date.Date
	FirstDueDate           *date.Date
	RentKop                int64
	PaymentDay             int
	DepositKop             int64
	UtilitiesMode          string
	UtilitiesFixedKop      int64
	PenaltyEnabled         bool
	PenaltyRatePct         *decimal.Decimal
	PenaltyGraceDays       int
	PenaltyCapPct          *decimal.Decimal
	PenaltyOnUtilities     bool
	IndexationEnabled      bool
	IndexationPct          *decimal.Decimal
	IndexationPeriodMonths int
	SpecialTerms           *string
	Notes                  *string
	Occupants              []Occupant
}

var (
	pctMinPenalty = decimal.RequireFromString("0.01")
	pctMaxPenalty = decimal.NewFromInt(1)
	pctMinIndex   = decimal.RequireFromString("0.01")
	pctMaxIndex   = decimal.NewFromInt(30)
	pctMaxCap     = decimal.NewFromInt(100)
)

// LegalBasis — вид договора: жилой объект и наниматель-физлицо → ch35, иначе ch34 (05 разд. 4.1).
func LegalBasis(propertyKind, tenantKind string) string {
	if propertyKind == "residential" && tenantKind == tenant.KindIndividual {
		return BasisCh35
	}
	return BasisCh34
}

// RegistrationRequired — срок от года (end ≥ start + 1 год − 1 день), как столбец registration_required.
func RegistrationRequired(start, end date.Date) bool {
	return !end.Before(start.AddMonths(12).AddDays(-1))
}

// IsShortTerm — краткосрочный наём: ch35 со сроком менее года (ст. 683 ГК РФ).
func IsShortTerm(basis string, start, end date.Date) bool {
	return basis == BasisCh35 && end.Before(start.AddMonths(12).AddDays(-1))
}

func (in *Input) normalize() {
	in.SpecialTerms = trim(in.SpecialTerms)
	in.Notes = trim(in.Notes)
	if in.FirstDueDate == nil {
		fd := in.StartDate
		in.FirstDueDate = &fd
	}
	if !in.PenaltyEnabled {
		in.PenaltyRatePct, in.PenaltyCapPct = nil, nil
	}
	if !in.IndexationEnabled {
		in.IndexationPct = nil
	}
	for i := range in.Occupants {
		in.Occupants[i].FullName = strings.TrimSpace(in.Occupants[i].FullName)
		in.Occupants[i].Relation = trim(in.Occupants[i].Relation)
	}
}

// validate — проверки черновика (05 разд. 4.3); тексты ошибок — из спецификации.
func (in *Input) validate(basis string) error {
	var errs []apperr.FieldError
	add := func(field, msg string) { errs = append(errs, apperr.FieldError{Field: field, Message: msg}) }
	if !in.EndDate.After(in.StartDate) {
		add("endDate", "Дата окончания должна быть позже даты начала")
	} else if basis == BasisCh35 && !in.EndDate.Before(in.StartDate.AddMonths(60)) {
		add("endDate", "Договор найма заключается на срок не более пяти лет (ст. 683 ГК РФ)")
	}
	if in.RentKop <= 0 {
		add("rentKop", "Укажите размер платы")
	}
	if in.PaymentDay < 1 || in.PaymentDay > 28 {
		add("paymentDay", "День оплаты — от 1 до 28")
	}
	if fd := *in.FirstDueDate; fd.Before(in.StartDate.AddDays(-30)) || fd.After(in.StartDate.AddDays(30)) {
		add("firstDueDate", "Срок первой оплаты — не дальше 30 дней от даты начала")
	}
	if in.DepositKop < 0 {
		add("depositKop", "Сумма залога не может быть отрицательной")
	}
	if in.PenaltyEnabled {
		if r := in.PenaltyRatePct; r == nil || r.LessThan(pctMinPenalty) || r.GreaterThan(pctMaxPenalty) || !r.Equal(r.Round(4)) {
			add("penaltyRatePct", "Ставка пеней — от 0,01 до 1 % в день")
		}
		if c := in.PenaltyCapPct; c != nil && (!c.IsPositive() || c.GreaterThan(pctMaxCap) || !c.Equal(c.Round(2))) {
			add("penaltyCapPct", "Предел пеней — от 0,01 до 100 % от суммы")
		}
	}
	if in.PenaltyGraceDays < 0 || in.PenaltyGraceDays > 30 {
		add("penaltyGraceDays", "Льготных дней — от 0 до 30")
	}
	if in.IndexationEnabled {
		if p := in.IndexationPct; p == nil || p.LessThan(pctMinIndex) || p.GreaterThan(pctMaxIndex) || !p.Equal(p.Round(2)) {
			add("indexationPct", "Процент индексации — от 0,01 до 30")
		}
	}
	if in.IndexationPeriodMonths < 12 {
		add("indexationPeriodMonths", "Индексация — не чаще одного раза в 12 месяцев")
	}
	switch in.UtilitiesMode {
	case UtilitiesFixed:
		if in.UtilitiesFixedKop <= 0 {
			add("utilitiesFixedKop", "Укажите фиксированную сумму ЖКУ")
		}
	case UtilitiesMeters, UtilitiesIncluded:
		if in.UtilitiesFixedKop < 0 {
			add("utilitiesFixedKop", "Сумма не может быть отрицательной")
		}
	}
	for i, o := range in.Occupants {
		if o.FullName == "" {
			add(fmt.Sprintf("occupants[%d].fullName", i), "Укажите ФИО проживающего")
		}
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// fixedPart — фиксированная часть ЖКУ, которая хранится в условиях (для «Включены в плату» — 0).
func (in *Input) fixedPart() int64 {
	if in.UtilitiesMode == UtilitiesIncluded {
		return 0
	}
	return in.UtilitiesFixedKop
}

func (in *Input) nextIndexation() *date.Date {
	if !in.IndexationEnabled {
		return nil
	}
	d := calc.FirstIndexation(in.StartDate, in.IndexationPeriodMonths)
	return &d
}

// parties — объект и наниматель черновика с проверками доступности.
func (s *Service) parties(ctx context.Context, q *sqlc.Queries, in Input) (sqlc.Property, sqlc.Tenant, error) {
	p, err := q.GetProperty(ctx, in.PropertyID)
	if db.IsNoRows(err) {
		return p, sqlc.Tenant{}, apperr.Validation(apperr.FieldError{Field: "propertyId", Message: "Объект не найден"})
	}
	if err != nil {
		return p, sqlc.Tenant{}, err
	}
	t, err := q.GetTenant(ctx, in.TenantID)
	if db.IsNoRows(err) {
		return p, t, apperr.Validation(apperr.FieldError{Field: "tenantId", Message: "Наниматель не найден"})
	}
	if err != nil {
		return p, t, err
	}
	var errs []apperr.FieldError
	if p.ArchivedAt != nil {
		errs = append(errs, apperr.FieldError{Field: "propertyId", Message: "Объект в архиве — новый договор по нему заключить нельзя"})
	}
	if t.AnonymizedAt != nil {
		errs = append(errs, apperr.FieldError{Field: "tenantId", Message: "Наниматель обезличен"})
	}
	if len(errs) > 0 {
		return p, t, apperr.Validation(errs...)
	}
	return p, t, nil
}

func contractParams(in Input) sqlc.CreateContractParams {
	return sqlc.CreateContractParams{
		PropertyID: in.PropertyID, TenantID: in.TenantID, StartDate: in.StartDate, EndDate: in.EndDate,
		FirstDueDate: *in.FirstDueDate, DepositKop: in.DepositKop, UtilitiesMode: in.UtilitiesMode,
		PenaltyEnabled: in.PenaltyEnabled, PenaltyRatePct: in.PenaltyRatePct, PenaltyGraceDays: int16(in.PenaltyGraceDays), //nolint:gosec // 0–30
		PenaltyCapPct: in.PenaltyCapPct, PenaltyOnUtilities: in.PenaltyOnUtilities, IndexationEnabled: in.IndexationEnabled,
		IndexationPct: in.IndexationPct, IndexationPeriodMonths: int16(in.IndexationPeriodMonths), //nolint:gosec // проверено
		NextIndexationOn: in.nextIndexation(), SpecialTerms: in.SpecialTerms, Notes: in.Notes,
	}
}

// Create — новый черновик с номером ГГГГ-NNN (05 разд. 4.2) и версией условий 1.
func (s *Service) Create(ctx context.Context, in Input) (sqlc.Contract, error) {
	in.normalize()
	var c sqlc.Contract
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, t, err := s.parties(ctx, tx.Q, in)
		if err != nil {
			return err
		}
		basis := LegalBasis(p.Kind, t.Kind)
		if err := in.validate(basis); err != nil {
			return err
		}
		year := s.clock.Today().Year()
		n, err := tx.Q.NextSequenceValue(ctx, sqlc.NextSequenceValueParams{Name: "contract", Year: int32(year)}) //nolint:gosec // год
		if err != nil {
			return err
		}
		params := contractParams(in)
		params.Number = fmt.Sprintf("%d-%03d", year, n)
		params.LegalBasis = basis
		params.CreatedBy = actorID(ctx)
		params.Now = s.clock.Now()
		if c, err = tx.Q.CreateContract(ctx, params); err != nil {
			return dbError(err)
		}
		if _, err := tx.Q.InsertTerms(ctx, sqlc.InsertTermsParams{
			ContractID: c.ID, Version: 1, EffectiveFrom: in.StartDate, RentKop: in.RentKop, UtilitiesFixedKop: in.fixedPart(),
			PaymentDay: int16(in.PaymentDay), Now: s.clock.Now(), //nolint:gosec // 1–28
		}); err != nil {
			return err
		}
		if err := s.replaceOccupants(ctx, tx, c.ID, in.Occupants); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.create", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Создан черновик договора № %s, объект «%s»", c.Number, p.Code), After: snapshot(c, in.RentKop, in.PaymentDay),
		})
	})
	return c, err
}

// Update — правка черновика: перезаписывает договор и версию условий 1 (05 разд. 4.3).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (sqlc.Contract, error) {
	in.normalize()
	var c sqlc.Contract
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != StatusDraft {
			return apperr.Conflict(apperr.CodeConflictState, "Изменять все условия можно только у черновика")
		}
		p, t, err := s.parties(ctx, tx.Q, in)
		if err != nil {
			return err
		}
		basis := LegalBasis(p.Kind, t.Kind)
		if err := in.validate(basis); err != nil {
			return err
		}
		terms, err := tx.Q.ListTerms(ctx, id)
		if err != nil {
			return err
		}
		cp := contractParams(in)
		c, err = tx.Q.UpdateDraftContract(ctx, sqlc.UpdateDraftContractParams{
			ID: id, LegalBasis: basis, PropertyID: cp.PropertyID, TenantID: cp.TenantID, StartDate: cp.StartDate, EndDate: cp.EndDate,
			FirstDueDate: cp.FirstDueDate, DepositKop: cp.DepositKop, UtilitiesMode: cp.UtilitiesMode, PenaltyEnabled: cp.PenaltyEnabled,
			PenaltyRatePct: cp.PenaltyRatePct, PenaltyGraceDays: cp.PenaltyGraceDays, PenaltyCapPct: cp.PenaltyCapPct,
			PenaltyOnUtilities: cp.PenaltyOnUtilities, IndexationEnabled: cp.IndexationEnabled, IndexationPct: cp.IndexationPct,
			IndexationPeriodMonths: cp.IndexationPeriodMonths, NextIndexationOn: cp.NextIndexationOn, SpecialTerms: cp.SpecialTerms, Notes: cp.Notes,
		})
		if err != nil {
			return dbError(err)
		}
		if err := tx.Q.UpdateFirstTerms(ctx, sqlc.UpdateFirstTermsParams{
			ContractID: id, EffectiveFrom: in.StartDate, RentKop: in.RentKop, UtilitiesFixedKop: in.fixedPart(),
			PaymentDay: int16(in.PaymentDay), //nolint:gosec // 1–28
		}); err != nil {
			return err
		}
		if err := s.replaceOccupants(ctx, tx, id, in.Occupants); err != nil {
			return err
		}
		var prevRent int64
		var prevDay int
		if len(terms) > 0 {
			prevRent, prevDay = terms[0].RentKop, int(terms[0].PaymentDay)
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.update", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Изменён черновик договора № %s", c.Number),
			Before:  snapshot(before, prevRent, prevDay), After: snapshot(c, in.RentKop, in.PaymentDay),
		})
	})
	return c, err
}

// Delete — удалить черновик физически вместе с условиями и проживающими (05 разд. 4.3).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.Status != StatusDraft {
			return apperr.Conflict(apperr.CodeConflictState, "Удалить можно только черновик")
		}
		if err := tx.Q.DeleteOccupants(ctx, id); err != nil {
			return err
		}
		if err := tx.Q.DeleteTerms(ctx, id); err != nil {
			return err
		}
		if err := tx.Q.DeleteContract(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.delete_draft", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Удалён черновик договора № %s", c.Number), Before: snapshot(c, 0, 0),
		})
	})
}

// Patch — прямо изменяемые условия действующего договора (05 разд. 4.5). Поля nil не меняются.
type Patch struct {
	PenaltyEnabled         *bool
	PenaltyRatePct         **decimal.Decimal
	PenaltyGraceDays       *int
	PenaltyCapPct          **decimal.Decimal
	PenaltyOnUtilities     *bool
	IndexationEnabled      *bool
	IndexationPct          **decimal.Decimal
	IndexationPeriodMonths *int
	SpecialTerms           **string
	Notes                  **string
	RegistrationDoneOn     **date.Date
	RegistrationNumber     **string
	Reason                 *string
}

func decEqual(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Apply меняет договор; причина обязательна при изменении пеней или индексации.
func (s *Service) Apply(ctx context.Context, id uuid.UUID, pt Patch) (sqlc.Contract, error) {
	var c sqlc.Contract
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Status != StatusActive && before.Status != StatusExtended {
			return apperr.Conflict(apperr.CodeConflictState, "Изменять условия напрямую можно только у действующего договора")
		}
		n := before
		if pt.PenaltyEnabled != nil {
			n.PenaltyEnabled = *pt.PenaltyEnabled
		}
		if pt.PenaltyRatePct != nil {
			n.PenaltyRatePct = *pt.PenaltyRatePct
		}
		if pt.PenaltyGraceDays != nil {
			n.PenaltyGraceDays = int16(*pt.PenaltyGraceDays) //nolint:gosec // проверяется ниже и схемой
		}
		if pt.PenaltyCapPct != nil {
			n.PenaltyCapPct = *pt.PenaltyCapPct
		}
		if pt.PenaltyOnUtilities != nil {
			n.PenaltyOnUtilities = *pt.PenaltyOnUtilities
		}
		if pt.IndexationEnabled != nil {
			n.IndexationEnabled = *pt.IndexationEnabled
		}
		if pt.IndexationPct != nil {
			n.IndexationPct = *pt.IndexationPct
		}
		if pt.IndexationPeriodMonths != nil {
			n.IndexationPeriodMonths = int16(*pt.IndexationPeriodMonths) //nolint:gosec // ≥ 12, проверено схемой
		}
		if pt.SpecialTerms != nil {
			n.SpecialTerms = trim(*pt.SpecialTerms)
		}
		if pt.Notes != nil {
			n.Notes = trim(*pt.Notes)
		}
		if pt.RegistrationDoneOn != nil {
			n.RegistrationDoneOn = *pt.RegistrationDoneOn
		}
		if pt.RegistrationNumber != nil {
			n.RegistrationNumber = trim(*pt.RegistrationNumber)
		}
		if !n.PenaltyEnabled {
			n.PenaltyRatePct, n.PenaltyCapPct = nil, nil
		}
		if !n.IndexationEnabled {
			n.IndexationPct = nil
		}
		// Проверка тех же правил, что у черновика.
		in := Input{
			StartDate: n.StartDate, EndDate: n.EndDate, FirstDueDate: &n.FirstDueDate, RentKop: 1, PaymentDay: 1,
			UtilitiesMode: UtilitiesIncluded, PenaltyEnabled: n.PenaltyEnabled, PenaltyRatePct: n.PenaltyRatePct,
			PenaltyGraceDays: int(n.PenaltyGraceDays), PenaltyCapPct: n.PenaltyCapPct, IndexationEnabled: n.IndexationEnabled,
			IndexationPct: n.IndexationPct, IndexationPeriodMonths: int(n.IndexationPeriodMonths),
		}
		if err := in.validate(BasisCh34); err != nil {
			return err
		}
		penaltyChanged := n.PenaltyEnabled != before.PenaltyEnabled || !decEqual(n.PenaltyRatePct, before.PenaltyRatePct) ||
			n.PenaltyGraceDays != before.PenaltyGraceDays || !decEqual(n.PenaltyCapPct, before.PenaltyCapPct) ||
			n.PenaltyOnUtilities != before.PenaltyOnUtilities
		indexChanged := n.IndexationEnabled != before.IndexationEnabled || !decEqual(n.IndexationPct, before.IndexationPct) ||
			n.IndexationPeriodMonths != before.IndexationPeriodMonths
		reason := trim(pt.Reason)
		if (penaltyChanged || indexChanged) && reason == nil {
			return apperr.Validation(apperr.FieldError{Field: "reason", Message: "Укажите причину изменения условий о пенях или индексации"})
		}
		// Дата следующей индексации: включение — от даты начала с шагом периода до ближайшей будущей.
		next := before.NextIndexationOn
		if !n.IndexationEnabled {
			next = nil
		} else if indexChanged && (next == nil || n.IndexationPeriodMonths != before.IndexationPeriodMonths) {
			d := calc.FirstIndexation(n.StartDate, int(n.IndexationPeriodMonths))
			for d.Before(s.clock.Today()) {
				d = d.AddMonths(int(n.IndexationPeriodMonths))
			}
			next = &d
		}
		if n.RegistrationDoneOn != nil && n.RegistrationDoneOn.After(s.clock.Today()) {
			return apperr.Validation(apperr.FieldError{Field: "registrationDoneOn", Message: "Дата регистрации не может быть позже сегодняшней"})
		}
		c, err = tx.Q.PatchContract(ctx, sqlc.PatchContractParams{
			ID: id, PenaltyEnabled: n.PenaltyEnabled, PenaltyRatePct: n.PenaltyRatePct, PenaltyGraceDays: n.PenaltyGraceDays,
			PenaltyCapPct: n.PenaltyCapPct, PenaltyOnUtilities: n.PenaltyOnUtilities, IndexationEnabled: n.IndexationEnabled,
			IndexationPct: n.IndexationPct, IndexationPeriodMonths: n.IndexationPeriodMonths, NextIndexationOn: next,
			SpecialTerms: n.SpecialTerms, Notes: n.Notes, RegistrationDoneOn: n.RegistrationDoneOn, RegistrationNumber: n.RegistrationNumber,
		})
		if err != nil {
			return dbError(err)
		}
		if err := s.syncEvents(ctx, tx, c); err != nil {
			return err
		}
		after := snapshot(c, 0, 0)
		if reason != nil {
			after["reason"] = *reason
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.update", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Изменены условия договора № %s", c.Number), Before: snapshot(before, 0, 0), After: after,
		})
	})
	return c, err
}

// ReplaceOccupants — заменить список проживающих (черновик и действующий договор).
func (s *Service) ReplaceOccupants(ctx context.Context, id uuid.UUID, list []Occupant) error {
	for i := range list {
		list[i].FullName = strings.TrimSpace(list[i].FullName)
		list[i].Relation = trim(list[i].Relation)
		if list[i].FullName == "" {
			return apperr.Validation(apperr.FieldError{Field: fmt.Sprintf("occupants[%d].fullName", i), Message: "Укажите ФИО проживающего"})
		}
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if c.Status == StatusTerminated || c.Status == StatusFinished {
			return apperr.Conflict(apperr.CodeConflictState, "Договор закрыт — проживающих изменить нельзя")
		}
		if err := s.replaceOccupants(ctx, tx, id, list); err != nil {
			return err
		}
		// ФИО проживающих — персональные данные: в журнал пишется только их число.
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "contract.occupants", EntityType: "contract", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: изменён список проживающих (%d)", c.Number, len(list)),
			After:   map[string]any{"occupants": len(list)},
		})
	})
}

func (s *Service) replaceOccupants(ctx context.Context, tx *db.Tx, id uuid.UUID, list []Occupant) error {
	if err := tx.Q.DeleteOccupants(ctx, id); err != nil {
		return err
	}
	for _, o := range list {
		if err := tx.Q.InsertOccupant(ctx, sqlc.InsertOccupantParams{
			ContractID: id, FullName: o.FullName, BirthDate: o.BirthDate, Relation: o.Relation, Now: s.clock.Now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) lock(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Contract, error) {
	c, err := tx.Q.GetContractForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return c, apperr.NotFound()
	}
	return c, err
}

// Get — договор.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (sqlc.Contract, error) {
	c, err := s.db.Queries().GetContract(ctx, id)
	if db.IsNoRows(err) {
		return c, apperr.NotFound()
	}
	return c, err
}

// snapshot — договор для журнала (без ПДн).
func snapshot(c sqlc.Contract, rentKop int64, paymentDay int) map[string]any {
	m := map[string]any{
		"number": c.Number, "legalBasis": c.LegalBasis, "status": c.Status, "propertyId": c.PropertyID.String(),
		"tenantId": c.TenantID.String(), "startDate": c.StartDate.String(), "endDate": c.EndDate.String(),
		"firstDueDate": c.FirstDueDate.String(), "depositKop": c.DepositKop, "utilitiesMode": c.UtilitiesMode,
		"penaltyEnabled": c.PenaltyEnabled, "penaltyRatePct": decStr(c.PenaltyRatePct), "penaltyGraceDays": c.PenaltyGraceDays,
		"penaltyCapPct": decStr(c.PenaltyCapPct), "penaltyOnUtilities": c.PenaltyOnUtilities,
		"indexationEnabled": c.IndexationEnabled, "indexationPct": decStr(c.IndexationPct),
		"indexationPeriodMonths": c.IndexationPeriodMonths, "specialTerms": c.SpecialTerms, "notes": c.Notes,
		"registrationDoneOn": dateStr(c.RegistrationDoneOn), "registrationNumber": c.RegistrationNumber,
	}
	if rentKop > 0 {
		m["rentKop"] = rentKop
		m["paymentDay"] = paymentDay
	}
	return m
}

func decStr(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func dateStr(d *date.Date) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// dbError переводит нарушения ограничений таблицы contracts в понятные ошибки (последняя защита после проверок сервиса).
func dbError(err error) error {
	code, constraint, ok := db.ConstraintError(err)
	if !ok {
		return err
	}
	if code == db.CodeExclusionViolation {
		return apperr.Conflict(apperr.CodeConflictOverlap, "Сроки пересекаются с другим договором по объекту")
	}
	if code == db.CodeCheckViolation {
		return apperr.BusinessRule("Условия договора не прошли проверку (" + constraint + ")")
	}
	return err
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

func ptr[T any](v T) *T { return &v }

func actualEnd(c sqlc.Contract) date.Date {
	if c.ActualEndDate != nil {
		return *c.ActualEndDate
	}
	if c.TerminationDate != nil {
		return *c.TerminationDate
	}
	return c.EndDate
}

// TermsOf переводит версии условий в calc.Terms.
func TermsOf(rows []sqlc.ContractTerm) []calc.Terms {
	out := make([]calc.Terms, 0, len(rows))
	for _, r := range rows {
		out = append(out, calc.Terms{
			Version: int(r.Version), EffectiveFrom: r.EffectiveFrom, RentKop: money.Kop(r.RentKop),
			UtilitiesFixedKop: money.Kop(r.UtilitiesFixedKop), PaymentDay: int(r.PaymentDay),
		})
	}
	return out
}
