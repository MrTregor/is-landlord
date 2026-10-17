package report

import (
	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// Статусы объекта.
const (
	StatusFree     = "free"
	StatusReserved = "reserved"
	StatusRented   = "rented"
	StatusRepair   = "repair"
)

// Interval — интервал истории статусов объекта; To — последний день включительно (nil — по сей день).
type Interval struct {
	Status string
	From   date.Date
	To     *date.Date
}

// MonthOccupancy — дни месяца по статусам.
type MonthOccupancy struct {
	Month  date.Date
	Rented int
	Idle   int
}

// IdleSpan — период простоя, обрезанный периодом отчёта.
type IdleSpan struct {
	Status   string
	From, To date.Date
	Days     int
	Lost     money.Kop
}

// Occupancy — простой и заполняемость за период (05 разд. 2.2).
type Occupancy struct {
	PeriodDays                     int
	Rented, Free, Reserved, Repair int
	Lost                           money.Kop
	ByMonth                        []MonthOccupancy
	Idle                           []IdleSpan
}

// IdleDays — дни простоя (свободен, забронирован, на ремонте).
func (o Occupancy) IdleDays() int { return o.Free + o.Reserved + o.Repair }

// Pct — заполняемость, % = сдан / (сдан + простой) × 100, до 0,1; nil — дней нет.
func (o Occupancy) Pct() *string { return PctOf(int64(o.Rented), int64(o.Rented+o.IdleDays())) }

// PctOf — num / den × 100 с округлением до 0,1 половиной вверх; nil при den = 0.
func PctOf(num, den int64) *string {
	if den == 0 {
		return nil
	}
	s := decimal.NewFromInt(num).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(den)).Round(1).StringFixed(1)
	return &s
}

// lostFor — недополученный доход за дни простоя месяца: RoundHalfUp(ожидаемая плата × дни / дней в месяце).
func lostFor(expected money.Kop, days, daysInMonth int) money.Kop {
	if days == 0 || expected == 0 {
		return 0
	}
	return money.RoundKopHalfUp(expected.KopDecimal().Mul(decimal.NewFromInt(int64(days))).Div(decimal.NewFromInt(int64(daysInMonth))))
}

// OccupancyOf — дни по статусам, заполняемость и недополученный доход за период [from, to] (уже обрезанный датой
// первой записи истории и сегодняшним днём): недополученный доход = Σ по месяцам RoundHalfUp(плата × простой(M) /
// дней в M) (05 разд. 2.2).
func OccupancyOf(history []Interval, from, to date.Date, expected money.Kop) Occupancy {
	o := Occupancy{}
	if to.Before(from) {
		return o
	}
	o.PeriodDays = date.DaysInclusive(from, to)
	months := map[date.Date]*MonthOccupancy{}
	var order []date.Date
	for m := from.FirstOfMonth(); !m.After(to); m = m.AddMonths(1) {
		mo := &MonthOccupancy{Month: m}
		months[m] = mo
		order = append(order, m)
	}
	idleByMonth := map[date.Date]int{}
	for _, iv := range history {
		start := date.Max(iv.From, from)
		end := to
		if iv.To != nil {
			end = date.Min(*iv.To, to)
		}
		if end.Before(start) {
			continue
		}
		span := IdleSpan{Status: iv.Status, From: start, To: end, Days: date.DaysInclusive(start, end)}
		// Дни интервала по месяцам.
		for m := start.FirstOfMonth(); !m.After(end); m = m.AddMonths(1) {
			a, b := date.Max(start, m), date.Min(end, m.LastOfMonth())
			days := date.DaysInclusive(a, b)
			mo := months[m]
			if iv.Status == StatusRented {
				mo.Rented += days
			} else {
				mo.Idle += days
				idleByMonth[m] += days
				span.Lost += lostFor(expected, days, m.DaysInMonth())
			}
		}
		switch iv.Status {
		case StatusRented:
			o.Rented += span.Days
		case StatusFree:
			o.Free += span.Days
		case StatusReserved:
			o.Reserved += span.Days
		case StatusRepair:
			o.Repair += span.Days
		}
		if iv.Status != StatusRented {
			o.Idle = append(o.Idle, span)
		}
	}
	for _, m := range order {
		o.ByMonth = append(o.ByMonth, *months[m])
		o.Lost += lostFor(expected, idleByMonth[m], m.DaysInMonth())
	}
	return o
}
