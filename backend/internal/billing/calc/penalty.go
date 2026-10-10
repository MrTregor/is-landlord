package calc

import (
	"sort"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// Paid — распределение платежа на начисление: дата поступления платежа и сумма.
type Paid struct {
	On     date.Date
	Amount money.Kop
}

// PenaltyInput — данные для расчёта пеней по одному начислению (05 разд. 7.2).
type PenaltyInput struct {
	Amount    money.Kop
	Due       date.Date
	RatePct   decimal.Decimal // % в день
	GraceDays int
	CapPct    *decimal.Decimal
	Payments  []Paid
	Today     date.Date
}

// Segment — отрезок дней с одинаковой базой.
type Segment struct {
	From, To date.Date
	Base     money.Kop
	Days     int
	Amount   money.Kop // для показа; итог считается по несокращённой сумме
}

// PenaltyResult — итог расчёта пеней.
type PenaltyResult struct {
	FirstDay date.Date
	LastDay  date.Date // последний день просрочки, вошедший в расчёт
	Segments []Segment
	Total    money.Kop
	CapKop   *money.Kop
	Accrues  bool // есть хотя бы один день просрочки с положительной базой
}

// FirstOverdueDay — первый день просрочки S = срок + льгота + 1.
func FirstOverdueDay(due date.Date, graceDays int) date.Date { return due.AddDays(graceDays + 1) }

// Penalty — пени по начислению (05 разд. 7.2): для каждого дня d от S до T − 1 база = A − Σ платежей,
// поступивших раньше d; при базе ≤ 0 расчёт останавливается; пени = RoundHalfUp(Σ база × r / 100), не больше предела.
// Расчёт детерминированный — каждый раз заново от начала.
func Penalty(in PenaltyInput) PenaltyResult {
	pays := append([]Paid(nil), in.Payments...)
	sort.Slice(pays, func(i, j int) bool { return pays[i].On.Before(pays[j].On) })
	res := PenaltyResult{FirstDay: FirstOverdueDay(in.Due, in.GraceDays)}
	last := in.Today.AddDays(-1)
	baseOn := func(d date.Date) money.Kop {
		b := in.Amount
		for _, p := range pays {
			if p.On.Before(d) {
				b -= p.Amount
			}
		}
		return b
	}
	sumKopDays := decimal.Zero // Σ base(d) в копейках
	var cur *Segment
	for d := res.FirstDay; !d.After(last); d = d.AddDays(1) {
		b := baseOn(d)
		if b <= 0 {
			break
		}
		res.Accrues = true
		res.LastDay = d
		sumKopDays = sumKopDays.Add(decimal.NewFromInt(int64(b)))
		if cur == nil || cur.Base != b {
			res.Segments = append(res.Segments, Segment{From: d, To: d, Base: b})
			cur = &res.Segments[len(res.Segments)-1]
		}
		cur.To = d
		cur.Days++
	}
	hundred := decimal.NewFromInt(100)
	for i := range res.Segments {
		s := &res.Segments[i]
		s.Amount = money.RoundKopHalfUp(decimal.NewFromInt(int64(s.Base) * int64(s.Days)).Mul(in.RatePct).Div(hundred))
	}
	res.Total = money.RoundKopHalfUp(sumKopDays.Mul(in.RatePct).Div(hundred))
	if in.CapPct != nil {
		capKop := money.RoundKopHalfUp(in.Amount.KopDecimal().Mul(*in.CapPct).Div(hundred))
		res.CapKop = &capKop
		if res.Total > capKop {
			res.Total = capKop
		}
	}
	return res
}
