package billing

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

func penaltyDetails(rate decimal.Decimal, grace int, r calc.PenaltyResult) []byte {
	segs := make([]map[string]any, 0, len(r.Segments))
	for _, s := range r.Segments {
		segs = append(segs, map[string]any{"from": s.From.String(), "to": s.To.String(), "base": int64(s.Base), "days": s.Days, "amount": int64(s.Amount)})
	}
	var capKop any
	if r.CapKop != nil {
		capKop = int64(*r.CapKop)
	}
	m := map[string]any{"ratePct": rate.StringFixed(4), "graceDays": grace, "firstDay": r.FirstDay.String(), "segments": segs, "capKop": capKop}
	if !r.LastDay.IsZero() {
		m["lastDay"] = r.LastDay.String()
	}
	b, _ := json.Marshal(m)
	return b
}

// penaltyFor пересчитывает пени по одному начислению (05 разд. 7.3). Возвращает true, если пени перешли в final
// (тогда договору нужно перераспределение авансов).
func (s *Service) penaltyFor(ctx context.Context, tx *db.Tx, base sqlc.PenaltyBasesRow, today date.Date) (bool, error) {
	cur, err := tx.Q.GetChargeForUpdate(ctx, base.ID)
	if err != nil {
		return false, err
	}
	if cur.Status == "cancelled" || cur.DueDate == nil || base.RatePct == nil {
		return false, nil
	}
	rows, err := tx.Q.ChargePayments(ctx, cur.ID)
	if err != nil {
		return false, err
	}
	pays := make([]calc.Paid, 0, len(rows))
	var lastPaid date.Date
	for _, r := range rows {
		pays = append(pays, calc.Paid{On: *r.ReceivedOn, Amount: money.Kop(r.AmountKop)})
		if r.ReceivedOn.After(lastPaid) {
			lastPaid = *r.ReceivedOn
		}
	}
	res := calc.Penalty(calc.PenaltyInput{
		Amount: money.Kop(cur.AmountKop), Due: *cur.DueDate, RatePct: *base.RatePct, GraceDays: int(base.GraceDays),
		CapPct: base.CapPct, Payments: pays, Today: today,
	})
	existing, err := tx.Q.PenaltyOf(ctx, &cur.ID)
	has := err == nil
	if err != nil && !db.IsNoRows(err) {
		return false, err
	}
	if has && existing.PenaltyState != nil && *existing.PenaltyState == PenaltyManual {
		return false, nil // изменены вручную — задача не трогает
	}
	details := penaltyDetails(*base.RatePct, int(base.GraceDays), res)
	paidOff := cur.Status == "paid"
	state, due := PenaltyAccruing, (*date.Date)(nil)
	if paidOff && !lastPaid.IsZero() {
		state, due = PenaltyFinal, &lastPaid
	}
	if !has {
		if !res.Accrues || res.Total <= 0 {
			return false, nil
		}
		key := "penalty:" + cur.ID.String()
		_, err := tx.Q.InsertAutoCharge(ctx, sqlc.InsertAutoChargeParams{
			ContractID: cur.ContractID, Kind: KindPenalty, Title: fmt.Sprintf("Пени по начислению «%s»", cur.Title),
			DueDate: due, AmountKop: int64(res.Total), Status: "open", PenaltyState: &state, SourceChargeID: &cur.ID,
			CalcDetails: details, IdempotencyKey: &key, Now: s.clock.Now(),
		})
		if db.IsNoRows(err) {
			err = nil
		}
		return state == PenaltyFinal, err
	}
	if !res.Accrues || res.Total <= 0 {
		return false, s.cancel(ctx, tx, existing, "Пересчёт: просрочки нет")
	}
	wasFinal := existing.PenaltyState != nil && *existing.PenaltyState == PenaltyFinal
	if existing.AmountKop == int64(res.Total) && string(existing.CalcDetails) == string(details) && wasFinal == (state == PenaltyFinal) {
		return false, nil
	}
	if err := s.trimPaid(ctx, tx, existing, int64(res.Total), "Пересчёт пеней"); err != nil {
		return false, err
	}
	if err := tx.Q.UpdatePenalty(ctx, sqlc.UpdatePenaltyParams{
		ID: existing.ID, AmountKop: int64(res.Total), PenaltyState: &state, DueDate: due, CalcDetails: details,
	}); err != nil {
		return false, err
	}
	if _, err := tx.Q.RefreshChargePaid(ctx, existing.ID); err != nil {
		return false, err
	}
	return state == PenaltyFinal && !wasFinal, nil
}
