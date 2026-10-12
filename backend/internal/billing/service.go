// Package billing — начисления (FR-05): плата по графику, фиксированная часть и ЖКУ, пени, корректировки и отмены,
// пересчёты при допсоглашениях и расторжении, перераспределение авансов. Формулы — в пакете calc.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/settings"
)

// Виды начислений.
const (
	KindRent      = "rent"
	KindUtilities = "utilities"
	KindPenalty   = "penalty"
	KindDamage    = "damage"
	KindOther     = "other"
)

// Состояния пеней.
const (
	PenaltyAccruing = "accruing"
	PenaltyFinal    = "final"
	PenaltyManual   = "manual"
)

// Причины автоматических действий (05 разд. 4.7).
const reasonTermination = "Расторжение договора"

// Service — начисления.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	settings *settings.Service
}

// NewService создаёт сервис начислений.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, st *settings.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, settings: st}
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

// ActualEnd — фактический конец договора (дата расторжения или окончания).
func ActualEnd(c sqlc.Contract) date.Date {
	if c.ActualEndDate != nil {
		return *c.ActualEndDate
	}
	if c.TerminationDate != nil {
		return *c.TerminationDate
	}
	return c.EndDate
}

func monthKey(m date.Date) string { return m.String()[:7] }

func rentDetails(l calc.Line, rent money.Kop) []byte {
	b, _ := json.Marshal(map[string]any{
		"rent": int64(rent), "days": l.Days, "daysInMonth": l.DaysInMonth, "periodStart": l.Start.String(),
		"periodEnd": l.End.String(), "termsVersion": l.Terms.Version,
	})
	return b
}

// EnsureRent создаёт начисления платы (и фиксированной части ЖКУ при utilities_mode = fixed), срок создания которых
// наступил (05 разд. 5.3). Идемпотентно: повтор по тому же ключу ничего не создаёт. Возвращает id созданных.
func (s *Service) EnsureRent(ctx context.Context, tx *db.Tx, c sqlc.Contract, today date.Date) ([]uuid.UUID, error) {
	if c.Status == "draft" {
		return nil, nil
	}
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil || len(terms) == 0 {
		return nil, err
	}
	org, err := s.settings.Get(ctx, tx.Q)
	if err != nil {
		return nil, err
	}
	var created []uuid.UUID
	now := s.clock.Now()
	for _, l := range calc.Schedule(c.StartDate, ActualEnd(c), c.FirstDueDate, TermsOf(terms), c.UtilitiesMode == "fixed", int(org.RemindBeforeDueDays)) {
		if today.Before(l.CreateOn) {
			break
		}
		month, start, end, due := l.Month, l.Start, l.End, l.DueDate
		key := fmt.Sprintf("rent:%s:%s", c.ID, monthKey(month))
		id, err := tx.Q.InsertAutoCharge(ctx, sqlc.InsertAutoChargeParams{
			ContractID: c.ID, Kind: KindRent, Title: calc.RentTitle(c.LegalBasis, l.Period), PeriodMonth: &month,
			PeriodStart: &start, PeriodEnd: &end, DueDate: &due, AmountKop: int64(l.RentKop), Status: "open",
			CalcDetails: rentDetails(l, l.Terms.RentKop), IdempotencyKey: &key, Now: now,
		})
		switch {
		case err == nil:
			created = append(created, id)
		case !db.IsNoRows(err):
			return created, err
		}
		if c.UtilitiesMode == "fixed" && l.UtilitiesKop > 0 {
			fkey := fmt.Sprintf("fixed-util:%s:%s", c.ID, monthKey(month))
			id, err := tx.Q.InsertAutoCharge(ctx, sqlc.InsertAutoChargeParams{
				ContractID: c.ID, Kind: KindUtilities, Title: utilitiesTitle(l.Period), PeriodMonth: &month,
				PeriodStart: &start, PeriodEnd: &end, DueDate: &due, AmountKop: int64(l.UtilitiesKop), Status: "open",
				CalcDetails: rentDetails(l, l.Terms.UtilitiesFixedKop), IdempotencyKey: &fkey, Now: now,
			})
			switch {
			case err == nil:
				created = append(created, id)
			case !db.IsNoRows(err):
				return created, err
			}
		}
	}
	if len(created) > 0 {
		if err := s.Rebalance(ctx, tx, c.ID); err != nil {
			return created, err
		}
	}
	return created, nil
}

// utilitiesTitle — «Компенсация ЖКУ, октябрь 2026» (для неполного месяца — с датами).
func utilitiesTitle(p calc.Period) string {
	t := calc.RentTitle("ch35", p)
	return "Компенсация ЖКУ" + t[len("Плата за наём"):]
}

// periodOfCharge — период начисления платы (месяц, даты) для пересчёта.
func periodOfCharge(ch sqlc.Charge, end date.Date) calc.Period {
	m := *ch.PeriodMonth
	start := m
	if ch.PeriodStart != nil {
		start = *ch.PeriodStart
	}
	p := calc.Period{Month: m, Start: start, End: end, DaysInMonth: m.DaysInMonth()}
	p.Days = date.DaysInclusive(start, end)
	p.Full = p.Days == p.DaysInMonth
	return p
}

// isScheduled — начисление платы или фиксированной части ЖКУ по графику.
func isScheduled(ch sqlc.Charge) bool {
	if ch.Origin != "auto" || ch.PeriodMonth == nil || ch.IdempotencyKey == nil {
		return false
	}
	k := *ch.IdempotencyKey
	return len(k) > 5 && (k[:5] == "rent:" || (len(k) > 11 && k[:11] == "fixed-util:"))
}

func isFixed(ch sqlc.Charge) bool { return ch.Kind == KindUtilities }

// ApplyTerms — пересчёт начислений платы и фиксированной части с месяца from по новой версии условий (05 разд. 5.4):
// корректировка terms_change на разницу; если оплачено больше новой суммы — отмена распределений на излишек.
func (s *Service) ApplyTerms(ctx context.Context, tx *db.Tx, c sqlc.Contract, from date.Date, reason string) error {
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return err
	}
	versions := TermsOf(terms)
	charges, err := tx.Q.ContractChargesForUpdate(ctx, c.ID)
	if err != nil {
		return err
	}
	for _, ch := range charges {
		if !isScheduled(ch) || ch.PeriodMonth.Before(from) {
			continue
		}
		end := *ch.PeriodEnd
		p := periodOfCharge(ch, end)
		t := calc.TermsFor(versions, p.Start)
		base := t.RentKop
		if isFixed(ch) {
			base = t.UtilitiesFixedKop
		}
		amount := calc.ProRate(base, p)
		if err := s.setAmount(ctx, tx, ch, int64(amount), &end, "terms_change", reason, map[string]any{
			"rent": int64(base), "days": p.Days, "daysInMonth": p.DaysInMonth, "periodStart": p.Start.String(),
			"periodEnd": end.String(), "termsVersion": t.Version,
		}); err != nil {
			return err
		}
	}
	return s.Rebalance(ctx, tx, c.ID)
}

// setAmount меняет сумму начисления с корректировкой; при оплате больше новой суммы излишек снимается.
func (s *Service) setAmount(ctx context.Context, tx *db.Tx, ch sqlc.Charge, amount int64, end *date.Date, kind, reason string,
	details map[string]any,
) error {
	delta := amount - ch.AmountKop
	raw, _ := json.Marshal(details)
	if delta == 0 && (end == nil || ch.PeriodEnd == nil || end.Equal(*ch.PeriodEnd)) {
		return nil
	}
	// Сначала снимается излишек оплаты: paid_kop не может превышать сумму (ограничение charges).
	if err := s.trimPaid(ctx, tx, ch, amount, reason); err != nil {
		return err
	}
	if err := tx.Q.SetChargeAmount(ctx, sqlc.SetChargeAmountParams{ID: ch.ID, AmountKop: amount, PeriodEnd: end, CalcDetails: raw}); err != nil {
		return err
	}
	if delta != 0 {
		if err := tx.Q.InsertAdjustment(ctx, sqlc.InsertAdjustmentParams{
			ChargeID: ch.ID, Kind: kind, DeltaKop: delta, Reason: reason, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		}); err != nil {
			return err
		}
	}
	_, err := tx.Q.RefreshChargePaid(ctx, ch.ID)
	return err
}

// trimPaid снимает распределения сверх новой суммы и обновляет paid_kop до изменения суммы.
func (s *Service) trimPaid(ctx context.Context, tx *db.Tx, ch sqlc.Charge, amount int64, reason string) error {
	if ch.PaidKop <= amount {
		return nil
	}
	if err := s.reverse(ctx, tx, ch.ID, ch.PaidKop-amount, reason); err != nil {
		return err
	}
	_, err := tx.Q.RefreshChargePaid(ctx, ch.ID)
	return err
}

// reverse отменяет распределения на начисление на сумму excess, начиная с последних; частично снимаемое
// распределение отменяется целиком, а остаток распределяется заново тем же платежом.
func (s *Service) reverse(ctx context.Context, tx *db.Tx, chargeID uuid.UUID, excess int64, reason string) error {
	allocs, err := tx.Q.ActiveAllocations(ctx, chargeID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, a := range allocs {
		if excess <= 0 {
			break
		}
		if err := tx.Q.ReverseAllocation(ctx, sqlc.ReverseAllocationParams{ID: a.ID, Now: &now, Reason: &reason}); err != nil {
			return err
		}
		if a.AmountKop > excess {
			if err := tx.Q.InsertAllocation(ctx, sqlc.InsertAllocationParams{
				PaymentID: a.PaymentID, ChargeID: chargeID, AmountKop: a.AmountKop - excess, Now: now,
			}); err != nil {
				return err
			}
			break
		}
		excess -= a.AmountKop
	}
	return nil
}

// cancel — отмена начисления: распределения снимаются (деньги — аванс), ключ автоначисления обнуляется.
func (s *Service) cancel(ctx context.Context, tx *db.Tx, ch sqlc.Charge, reason string) error {
	if ch.PaidKop > 0 {
		if err := s.reverse(ctx, tx, ch.ID, ch.PaidKop, reason); err != nil {
			return err
		}
	}
	return tx.Q.CancelCharge(ctx, sqlc.CancelChargeParams{ID: ch.ID, Now: ptr(s.clock.Now()), CancelledBy: actorID(ctx), Reason: &reason})
}

// Terminate — начисления при расторжении (05 разд. 4.7, п. 2–3): месяцы после месяца последнего дня отменяются,
// месяц последнего дня пересчитывается как неполный (корректировка termination), затем перераспределение авансов.
func (s *Service) Terminate(ctx context.Context, tx *db.Tx, c sqlc.Contract) error {
	last := ActualEnd(c)
	lastMonth := last.FirstOfMonth()
	terms, err := tx.Q.ListTerms(ctx, c.ID)
	if err != nil {
		return err
	}
	versions := TermsOf(terms)
	charges, err := tx.Q.ContractChargesForUpdate(ctx, c.ID)
	if err != nil {
		return err
	}
	for _, ch := range charges {
		if !isScheduled(ch) {
			continue
		}
		switch {
		case ch.PeriodMonth.After(lastMonth):
			if err := s.cancel(ctx, tx, ch, reasonTermination); err != nil {
				return err
			}
		case ch.PeriodMonth.Equal(lastMonth) && ch.PeriodEnd != nil && ch.PeriodEnd.After(last):
			p := periodOfCharge(ch, last)
			t := calc.TermsFor(versions, p.Start)
			base := t.RentKop
			if isFixed(ch) {
				base = t.UtilitiesFixedKop
			}
			amount := calc.ProRate(base, p)
			if err := s.setAmount(ctx, tx, ch, int64(amount), &last, "termination", reasonTermination, map[string]any{
				"rent": int64(base), "days": p.Days, "daysInMonth": p.DaysInMonth, "periodStart": p.Start.String(),
				"periodEnd": last.String(), "termsVersion": t.Version,
			}); err != nil {
				return err
			}
		}
	}
	if err := s.Rebalance(ctx, tx, c.ID); err != nil {
		return err
	}
	// 05 разд. 4.7, п. 6: при остатке аванса — расхождение «переплата».
	pays, err := tx.Q.PaymentsWithAdvance(ctx, c.ID)
	if err != nil {
		return err
	}
	for _, p := range pays {
		if err := s.FlagOverpayment(ctx, tx, c.ID, p.ID, money.Kop(p.AdvanceKop)); err != nil {
			return err
		}
	}
	return nil
}

// Rebalance — перераспределение авансов договора (05 разд. 10.4): платежи «за начисления» с авансом по возрастанию
// даты поступления распределяются по порядку раздела 10.3 (шаги 2–5). Существующие распределения не переставляются.
// Идемпотентна. В конце — сумма открытых расхождений «переплата» приводится к авансу платежа (05 разд. 10.6).
func (s *Service) Rebalance(ctx context.Context, tx *db.Tx, contractID uuid.UUID) error {
	pays, err := tx.Q.PaymentsWithAdvance(ctx, contractID)
	if err != nil {
		return err
	}
	var open []sqlc.Charge
	loaded := false
	now := s.clock.Now()
	for _, p := range pays {
		if p.AdvanceKop <= 0 {
			continue
		}
		if !loaded {
			if open, err = tx.Q.OpenChargesForAllocation(ctx, contractID); err != nil {
				return err
			}
			loaded = true
		}
		if _, err := s.allocate(ctx, tx, p.ID, money.Kop(p.AdvanceKop), open, nil, now); err != nil {
			return err
		}
	}
	return s.SyncOverpayments(ctx, tx, contractID)
}

// OpenCharges — открытые начисления договора для распределения (строки блокируются).
func (s *Service) OpenCharges(ctx context.Context, tx *db.Tx, contractID uuid.UUID) ([]sqlc.Charge, error) {
	return tx.Q.OpenChargesForAllocation(ctx, contractID)
}

// Allocate распределяет avail платежа paymentID по открытым начислениям open (05 разд. 10.3): сначала requested,
// затем по общему порядку. Суммы оплаты в open обновляются. Возвращает нераспределённый остаток (аванс).
func (s *Service) Allocate(ctx context.Context, tx *db.Tx, paymentID uuid.UUID, avail money.Kop, open []sqlc.Charge,
	requested []uuid.UUID,
) (money.Kop, error) {
	return s.allocate(ctx, tx, paymentID, avail, open, requested, s.clock.Now())
}

func (s *Service) allocate(ctx context.Context, tx *db.Tx, paymentID uuid.UUID, avail money.Kop, open []sqlc.Charge,
	requested []uuid.UUID, now time.Time,
) (money.Kop, error) {
	list := make([]calc.OpenCharge, 0, len(open))
	idx := make(map[uuid.UUID]int, len(open))
	for i, ch := range open {
		oc := calc.OpenCharge{ID: ch.ID, Kind: ch.Kind, DueDate: ch.DueDate, Seq: i, Outstanding: money.Kop(ch.AmountKop - ch.PaidKop)}
		if ch.PenaltyState != nil {
			oc.PenaltyState = *ch.PenaltyState
		}
		list = append(list, oc)
		idx[ch.ID] = i
	}
	allocs, rest := calc.Allocate(avail, list, requested)
	for _, a := range allocs {
		if err := tx.Q.InsertAllocation(ctx, sqlc.InsertAllocationParams{
			PaymentID: paymentID, ChargeID: a.ChargeID, AmountKop: int64(a.Amount), Now: now,
		}); err != nil {
			return rest, err
		}
		open[idx[a.ChargeID]].PaidKop += int64(a.Amount)
		if _, err := tx.Q.RefreshChargePaid(ctx, a.ChargeID); err != nil {
			return rest, err
		}
	}
	return rest, nil
}

// FlagOverpayment — расхождение «переплата» по платежу с остатком аванса (одно на платёж, 05 разд. 10.6).
func (s *Service) FlagOverpayment(ctx context.Context, tx *db.Tx, contractID, paymentID uuid.UUID, advance money.Kop) error {
	if advance <= 0 {
		return nil
	}
	return tx.Q.InsertOverpayment(ctx, sqlc.InsertOverpaymentParams{
		ContractID: &contractID, PaymentID: &paymentID, AmountKop: int64(advance), Now: s.clock.Now(),
	})
}

// SyncOverpayments — открытые расхождения «переплата» договора: аванс израсходован (или платёж отменён) —
// закрыть auto_closed, изменился — сумма расхождения = аванс (05 разд. 10.6).
func (s *Service) SyncOverpayments(ctx context.Context, tx *db.Tx, contractID uuid.UUID) error {
	list, err := tx.Q.OpenOverpayments(ctx, &contractID)
	if err != nil {
		return err
	}
	for _, d := range list {
		var advance int64
		if d.PaymentStatus == "succeeded" {
			if advance, err = tx.Q.PaymentAdvance(ctx, *d.PaymentID); err != nil {
				return err
			}
		}
		switch {
		case advance <= 0:
			if _, err := tx.Q.ResolveDiscrepancy(ctx, sqlc.ResolveDiscrepancyParams{
				ID: d.ID, Resolution: "auto_closed", Note: ptr("Аванс израсходован"), Now: s.clock.Now(),
			}); err != nil {
				return err
			}
		case advance != d.AmountKop:
			if err := tx.Q.SetDiscrepancyAmount(ctx, sqlc.SetDiscrepancyAmountParams{ID: d.ID, AmountKop: advance}); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckUnderpayment — расхождение «недоплата» после проведения платежа (05 разд. 10.7): долг по начислениям
// основного долга со сроком не позже даты поступления; есть — создать или обновить открытое, нет — закрыть.
func (s *Service) CheckUnderpayment(ctx context.Context, tx *db.Tx, contractID, paymentID uuid.UUID, receivedOn date.Date) error {
	overdue, err := tx.Q.MainDebtDueBy(ctx, sqlc.MainDebtDueByParams{ContractID: contractID, OnDate: receivedOn})
	if err != nil {
		return err
	}
	cur, err := tx.Q.OpenUnderpayment(ctx, &contractID)
	exists := err == nil
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	switch {
	case overdue > 0 && exists:
		return tx.Q.SetUnderpayment(ctx, sqlc.SetUnderpaymentParams{ID: cur.ID, AmountKop: overdue, PaymentID: &paymentID})
	case overdue > 0:
		return tx.Q.InsertUnderpayment(ctx, sqlc.InsertUnderpaymentParams{
			ContractID: &contractID, PaymentID: &paymentID, AmountKop: overdue, Now: s.clock.Now(),
		})
	case exists:
		_, err := tx.Q.ResolveDiscrepancy(ctx, sqlc.ResolveDiscrepancyParams{
			ID: cur.ID, Resolution: "auto_closed", Note: ptr("Долг погашен"), Now: s.clock.Now(),
		})
		return err
	}
	return nil
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// lockContract — договор для изменения начислений (распределение блокирует строку договора).
func lockContract(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Contract, error) {
	c, err := tx.Q.GetContractForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return c, apperr.NotFound()
	}
	return c, err
}
