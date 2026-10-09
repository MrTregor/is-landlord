// Package calc — чистые функции расчётной подсистемы (docs/spec/05 Бизнес-правила и расчёты.md).
// Здесь нет обращений к БД и часам: все входные данные передаются аргументами.
package calc

import (
	"fmt"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// Terms — версия условий договора (contract_terms).
type Terms struct {
	Version           int
	EffectiveFrom     date.Date
	RentKop           money.Kop
	UtilitiesFixedKop money.Kop
	PaymentDay        int
}

// Period — часть месяца M, пересекающая срок договора (05 разд. 5.1).
type Period struct {
	Month       date.Date // первое число месяца
	Start       date.Date
	End         date.Date
	Days        int
	DaysInMonth int
	Full        bool
}

// Periods — периоды начисления платы для срока [start, end] (end — фактический конец договора).
func Periods(start, end date.Date) []Period {
	var out []Period
	for m := start.FirstOfMonth(); !m.After(end); m = m.AddMonths(1) {
		ps := date.Max(start, m)
		pe := date.Min(end, m.LastOfMonth())
		days := date.DaysInclusive(ps, pe)
		out = append(out, Period{Month: m, Start: ps, End: pe, Days: days, DaysInMonth: m.DaysInMonth(), Full: days == m.DaysInMonth()})
	}
	return out
}

// PeriodOf — период месяца month (первое число) для срока [start, end]; ok = false, если месяц вне срока.
func PeriodOf(start, end, month date.Date) (Period, bool) {
	m := month.FirstOfMonth()
	if m.After(end) || m.LastOfMonth().Before(start) {
		return Period{}, false
	}
	ps := date.Max(start, m)
	pe := date.Min(end, m.LastOfMonth())
	days := date.DaysInclusive(ps, pe)
	return Period{Month: m, Start: ps, End: pe, Days: days, DaysInMonth: m.DaysInMonth(), Full: days == m.DaysInMonth()}, true
}

// TermsFor — версия условий с наибольшим effective_from ≤ day (05 разд. 5.2). versions — в любом порядке, не пусто.
func TermsFor(versions []Terms, day date.Date) Terms {
	var best *Terms
	earliest := versions[0]
	for i, v := range versions {
		if v.EffectiveFrom.Before(earliest.EffectiveFrom) {
			earliest = v
		}
		if !v.EffectiveFrom.After(day) && (best == nil || v.EffectiveFrom.After(best.EffectiveFrom)) {
			best = &versions[i]
		}
	}
	if best == nil {
		return earliest // день раньше первой версии (версия 1 действует с даты начала)
	}
	return *best
}

// ProRate — сумма за период: полный месяц — вся сумма, иначе amount × days / daysIn(M), половина вверх.
func ProRate(amount money.Kop, p Period) money.Kop {
	if p.Full {
		return amount
	}
	return money.RoundKopHalfUp(amount.KopDecimal().Mul(decimal.NewFromInt(int64(p.Days))).Div(decimal.NewFromInt(int64(p.DaysInMonth))))
}

// DueDate — срок оплаты периода (05 разд. 5.3): для месяца, содержащего начало договора, — firstDue,
// для остальных — день оплаты месяца из версии условий.
func DueDate(p Period, contractStart, firstDue date.Date, paymentDay int) date.Date {
	if p.Month.Equal(contractStart.FirstOfMonth()) {
		return firstDue
	}
	return date.New(p.Month.Year(), p.Month.Month(), paymentDay)
}

// CreateOn — дата, с которой начисление периода должно существовать: min(первое число M, срок) − remindBefore дней.
func CreateOn(p Period, due date.Date, remindBeforeDays int) date.Date {
	return date.Min(p.Month, due).AddDays(-remindBeforeDays)
}

// Line — строка графика платежей договора.
type Line struct {
	Period
	Terms        Terms
	RentKop      money.Kop
	UtilitiesKop money.Kop // фиксированная часть ЖКУ (только при utilities_mode = fixed)
	DueDate      date.Date
	CreateOn     date.Date
}

// Schedule — график платы по договору: строки по месяцам срока [start, end].
func Schedule(start, end, firstDue date.Date, versions []Terms, fixedUtilities bool, remindBeforeDays int) []Line {
	var out []Line
	for _, p := range Periods(start, end) {
		t := TermsFor(versions, p.Start)
		due := DueDate(p, start, firstDue, t.PaymentDay)
		l := Line{Period: p, Terms: t, RentKop: ProRate(t.RentKop, p), DueDate: due, CreateOn: CreateOn(p, due, remindBeforeDays)}
		if fixedUtilities {
			l.UtilitiesKop = ProRate(t.UtilitiesFixedKop, p)
		}
		out = append(out, l)
	}
	return out
}

// DefaultPaymentDay — день оплаты по умолчанию: день даты начала, если ≤ 28, иначе 28 (05 разд. 4.3).
func DefaultPaymentDay(start date.Date) int { return min(start.Day(), 28) }

var monthsNom = [...]string{"январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
var monthsGen = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

// MonthName — «октябрь 2026».
func MonthName(m date.Date) string { return fmt.Sprintf("%s %d", monthsNom[m.Month()-1], m.Year()) }

// RentTitle — название начисления платы: «Плата за наём, октябрь 2026» (ch34 — «Арендная плата, …»);
// для неполного месяца — «Плата за наём, 15–31 марта 2026».
func RentTitle(legalBasis string, p Period) string {
	prefix := "Плата за наём"
	if legalBasis == "ch34" {
		prefix = "Арендная плата"
	}
	if p.Full {
		return prefix + ", " + MonthName(p.Month)
	}
	return fmt.Sprintf("%s, %d–%d %s %d", prefix, p.Start.Day(), p.End.Day(), monthsGen[p.Month.Month()-1], p.Month.Year())
}

// FirstIndexation — первая дата индексации (05 разд. 8.1): X = начало + период; если X — первое число, то X,
// иначе первое число следующего месяца.
func FirstIndexation(start date.Date, periodMonths int) date.Date {
	x := start.AddMonths(periodMonths)
	if x.Day() == 1 {
		return x
	}
	return x.FirstOfMonth().AddMonths(1)
}

// IndexedRent — плата после индексации на pct %, округлённая до целого рубля половиной вверх (05 разд. 8.2).
func IndexedRent(rent money.Kop, pct decimal.Decimal) money.Kop {
	rub := rent.Decimal().Mul(decimal.NewFromInt(100).Add(pct)).Div(decimal.NewFromInt(100))
	return money.Kop(rub.Round(0).IntPart() * 100)
}
