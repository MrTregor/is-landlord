package calc

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// Tariff — тариф на ресурс (tariffs); PropertyID = nil — для всех объектов.
type Tariff struct {
	MeterType     string
	PropertyID    *uuid.UUID
	Price         decimal.Decimal
	EffectiveFrom date.Date
}

// SelectTariff — тариф с наибольшим effective_from ≤ on: сначала по объекту, затем общий (05 разд. 6.3).
func SelectTariff(tariffs []Tariff, meterType string, propertyID uuid.UUID, on date.Date) (decimal.Decimal, bool) {
	pick := func(own bool) (decimal.Decimal, bool) {
		var best *Tariff
		for i, t := range tariffs {
			if t.MeterType != meterType || t.EffectiveFrom.After(on) {
				continue
			}
			if own != (t.PropertyID != nil && *t.PropertyID == propertyID) || (!own && t.PropertyID != nil) {
				continue
			}
			if best == nil || t.EffectiveFrom.After(best.EffectiveFrom) {
				best = &tariffs[i]
			}
		}
		if best == nil {
			return decimal.Decimal{}, false
		}
		return best.Price, true
	}
	if p, ok := pick(true); ok {
		return p, true
	}
	return pick(false)
}

// MeterLine — строка счётчика в начислении ЖКУ.
type MeterLine struct {
	MeterID     uuid.UUID
	Type        string
	Prev        decimal.Decimal
	PrevDate    date.Date
	Value       decimal.Decimal
	Consumption decimal.Decimal
	Tariff      decimal.Decimal
	Amount      money.Kop
	Missing     bool
}

// MeterAmount считает строку: расход = value − prev (при отсутствии показания — 0), сумма = RoundHalfUp(расход × тариф).
func MeterAmount(meterID uuid.UUID, meterType string, prev decimal.Decimal, prevDate date.Date, value *decimal.Decimal, tariff decimal.Decimal) MeterLine {
	l := MeterLine{MeterID: meterID, Type: meterType, Prev: prev, PrevDate: prevDate, Value: prev, Tariff: tariff}
	if value == nil {
		l.Missing = true
		return l
	}
	l.Value = *value
	l.Consumption = value.Sub(prev)
	l.Amount = money.RoundHalfUp(l.Consumption.Mul(tariff))
	return l
}

// UtilitiesTotal — итог начисления ЖКУ: сумма округлённых строк плюс фиксированная часть.
func UtilitiesTotal(lines []MeterLine, fixed money.Kop) money.Kop {
	total := fixed
	for _, l := range lines {
		total += l.Amount
	}
	return total
}

// UtilitiesDue — срок оплаты ЖКУ за месяц M: max(день оплаты месяца M+1, дата создания + 3 дня).
func UtilitiesDue(month date.Date, paymentDay int, created date.Date) date.Date {
	next := month.FirstOfMonth().AddMonths(1)
	return date.Max(date.New(next.Year(), next.Month(), paymentDay), created.AddDays(3))
}

// HighConsumption — расход больше трёх средних за последние месяцы (предупреждение, 05 разд. 6.2).
func HighConsumption(consumption decimal.Decimal, previous []decimal.Decimal) bool {
	if len(previous) == 0 {
		return false
	}
	sum := decimal.Zero
	for _, p := range previous {
		sum = sum.Add(p)
	}
	avg := sum.Div(decimal.NewFromInt(int64(len(previous))))
	return avg.IsPositive() && consumption.GreaterThan(avg.Mul(decimal.NewFromInt(3)))
}
