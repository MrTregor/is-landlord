package report

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/calendar"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/settings"
)

// Service — построение отчётов и сводки по данным на момент запроса.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Service
	calendar *calendar.Service
}

// NewService создаёт сервис отчётов.
func NewService(database *db.DB, clk clock.Clock, st *settings.Service, cal *calendar.Service) *Service {
	return &Service{db: database, clock: clk, settings: st, calendar: cal}
}

func checkPeriod(from, to date.Date) error {
	if to.Before(from) {
		return apperr.Validation(apperr.FieldError{Field: "to", Message: "Дата окончания периода раньше даты начала"})
	}
	return nil
}

func kindsDTO(k Kinds) api.IncomeByKinds {
	return api.IncomeByKinds{RentKop: int64(k.Rent), UtilitiesKop: int64(k.Utilities), PenaltyKop: int64(k.Penalty),
		DamageKop: int64(k.Damage), OtherKop: int64(k.Other), AdvanceKop: int64(k.Advance), DepositWithheldKop: int64(k.Withheld),
		RefundsKop: int64(k.Refunds), ToDepositKop: int64(k.ToDeposit), IncomeKop: int64(k.Total())}
}

// occupancies — простой и заполняемость объектов за период; период объекта обрезается датой первой записи истории
// и сегодняшним днём (05 разд. 2.2).
func (s *Service) occupancies(ctx context.Context, q *sqlc.Queries, props []sqlc.ReportPropertiesRow, from, to date.Date,
	propertyID *uuid.UUID,
) (map[uuid.UUID]Occupancy, error) {
	hist, err := q.StatusHistoryInRange(ctx, sqlc.StatusHistoryInRangeParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return nil, err
	}
	firsts, err := q.FirstStatusDates(ctx)
	if err != nil {
		return nil, err
	}
	first := map[uuid.UUID]date.Date{}
	for _, f := range firsts {
		first[f.PropertyID] = f.FirstFrom
	}
	byProp := map[uuid.UUID][]Interval{}
	for _, h := range hist {
		byProp[h.PropertyID] = append(byProp[h.PropertyID], Interval{Status: h.Status, From: h.ValidFrom, To: h.ValidTo})
	}
	today := s.clock.Today()
	out := map[uuid.UUID]Occupancy{}
	for _, p := range props {
		f, t := from, date.Min(to, today)
		if fd, ok := first[p.ID]; ok {
			f = date.Max(f, fd)
		} else {
			out[p.ID] = Occupancy{}
			continue
		}
		out[p.ID] = OccupancyOf(byProp[p.ID], f, t, money.Kop(p.ExpectedRentKop))
	}
	return out, nil
}

// ---------------------------------------------------------------- Доходы и расходы по объектам (11 разд. 2)

// IncomeExpense — отчёт «Доходы и расходы по объектам».
func (s *Service) IncomeExpense(ctx context.Context, from, to date.Date, propertyID *uuid.UUID) (api.IncomeExpenseReport, error) {
	out := api.IncomeExpenseReport{From: apiconv.FromDate(from), To: apiconv.FromDate(to), Rows: []api.IncomeExpenseRow{},
		ByMonth: []api.IncomeExpenseMonthRow{}}
	if err := checkPeriod(from, to); err != nil {
		return out, err
	}
	q := s.db.Queries()
	props, err := q.ReportProperties(ctx, sqlc.ReportPropertiesParams{PropertyID: propertyID, DateFrom: from})
	if err != nil {
		return out, err
	}
	ops, err := LoadOps(ctx, q, from, to, propertyID)
	if err != nil {
		return out, err
	}
	exps, err := q.ReportExpenses(ctx, sqlc.ReportExpensesParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return out, err
	}
	charged, err := q.ChargedByProperty(ctx, sqlc.ChargedByPropertyParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return out, err
	}
	occ, err := s.occupancies(ctx, q, props, from, to, propertyID)
	if err != nil {
		return out, err
	}
	chargedBy := map[uuid.UUID]int64{}
	for _, c := range charged {
		chargedBy[c.PropertyID] = c.ChargedKop
	}
	expBy := map[uuid.UUID]money.Kop{}
	monthExp := map[uuid.UUID]map[date.Date]money.Kop{}
	for _, e := range exps {
		expBy[e.PropertyID] += money.Kop(e.AmountKop)
		if monthExp[e.PropertyID] == nil {
			monthExp[e.PropertyID] = map[date.Date]money.Kop{}
		}
		monthExp[e.PropertyID][e.PaidOn.FirstOfMonth()] += money.Kop(e.AmountKop)
	}
	monthInc := map[uuid.UUID]map[date.Date]money.Kop{}
	for _, op := range ops {
		if monthInc[op.PropertyID] == nil {
			monthInc[op.PropertyID] = map[date.Date]money.Kop{}
		}
		monthInc[op.PropertyID][op.Date.FirstOfMonth()] += op.Amount
	}
	var tot Kinds
	var totExp money.Kop
	var totCharged int64
	rented, idle := 0, 0
	for _, p := range props {
		k := KindsOf(ops, func(op Op) bool { return op.PropertyID == p.ID })
		o := occ[p.ID]
		row := incomeExpenseRow(k, money.Kop(chargedBy[p.ID]), expBy[p.ID], o.Pct())
		out.Rows = append(out.Rows, api.IncomeExpenseRow{
			PropertyId: p.ID, PropertyCode: p.Code, RentKop: row.RentKop, UtilitiesKop: row.UtilitiesKop, PenaltyKop: row.PenaltyKop,
			DamageKop: row.DamageKop, OtherKop: row.OtherKop, AdvanceKop: row.AdvanceKop, DepositWithheldKop: row.DepositWithheldKop,
			RefundsKop: row.RefundsKop, ToDepositKop: row.ToDepositKop, IncomeKop: row.IncomeKop, ChargedKop: row.ChargedKop,
			ExpensesKop: row.ExpensesKop, ResultKop: row.ResultKop, MarginPct: row.MarginPct, OccupancyPct: row.OccupancyPct,
		})
		tot = tot.Plus(k)
		totExp += expBy[p.ID]
		totCharged += chargedBy[p.ID]
		rented += o.Rented
		idle += o.IdleDays()
		for m := from.FirstOfMonth(); !m.After(to); m = m.AddMonths(1) {
			inc, ex := monthInc[p.ID][m], monthExp[p.ID][m]
			if inc == 0 && ex == 0 {
				continue
			}
			out.ByMonth = append(out.ByMonth, api.IncomeExpenseMonthRow{Month: apiconv.FromDate(m), PropertyId: p.ID,
				PropertyCode: p.Code, IncomeKop: int64(inc), ExpensesKop: int64(ex), ResultKop: int64(inc - ex)})
		}
	}
	sort.SliceStable(out.ByMonth, func(i, j int) bool { return out.ByMonth[i].Month.Before(out.ByMonth[j].Month.Time) })
	out.Totals = incomeExpenseRow(tot, money.Kop(totCharged), totExp, PctOf(int64(rented), int64(rented+idle)))
	return out, nil
}

func incomeExpenseRow(k Kinds, charged, expenses money.Kop, occupancy *string) api.IncomeExpenseTotals {
	b := kindsDTO(k)
	result := k.Total() - expenses
	return api.IncomeExpenseTotals{RentKop: b.RentKop, UtilitiesKop: b.UtilitiesKop, PenaltyKop: b.PenaltyKop, DamageKop: b.DamageKop,
		OtherKop: b.OtherKop, AdvanceKop: b.AdvanceKop, DepositWithheldKop: b.DepositWithheldKop, RefundsKop: b.RefundsKop,
		ToDepositKop: b.ToDepositKop, IncomeKop: b.IncomeKop, ChargedKop: int64(charged), ExpensesKop: int64(expenses),
		ResultKop: int64(result), MarginPct: PctOf(int64(result), int64(k.Total())), OccupancyPct: occupancy}
}

// Profitability — доходность объекта за период (05 разд. 2.3).
func (s *Service) Profitability(ctx context.Context, propertyID uuid.UUID, from, to date.Date) (api.PropertyProfitability, error) {
	out := api.PropertyProfitability{ByMonth: []api.ProfitabilityMonth{}}
	if err := checkPeriod(from, to); err != nil {
		return out, err
	}
	q := s.db.Queries()
	if _, err := q.GetProperty(ctx, propertyID); err != nil {
		if db.IsNoRows(err) {
			return out, apperr.NotFound()
		}
		return out, err
	}
	rep, err := s.IncomeExpense(ctx, from, to, &propertyID)
	if err != nil {
		return out, err
	}
	props, err := q.ReportProperties(ctx, sqlc.ReportPropertiesParams{PropertyID: &propertyID, DateFrom: date.New(1900, time.January, 1)})
	if err != nil {
		return out, err
	}
	occ, err := s.occupancies(ctx, q, props, from, to, &propertyID)
	if err != nil {
		return out, err
	}
	o := occ[propertyID]
	t := rep.Totals
	out = api.PropertyProfitability{IncomeKop: t.IncomeKop, ExpensesKop: t.ExpensesKop, ResultKop: t.ResultKop, MarginPct: t.MarginPct,
		RentedDays: o.Rented, IdleDays: o.IdleDays(), IdleByStatus: api.IdleByStatus{Free: o.Free, Reserved: o.Reserved, Repair: o.Repair},
		OccupancyPct: o.Pct(), LostIncomeKop: int64(o.Lost), ByMonth: []api.ProfitabilityMonth{}}
	monthDays := map[date.Date]MonthOccupancy{}
	for _, m := range o.ByMonth {
		monthDays[m.Month] = m
	}
	monthMoney := map[date.Date]api.IncomeExpenseMonthRow{}
	for _, m := range rep.ByMonth {
		monthMoney[apiconv.ToDate(m.Month)] = m
	}
	for m := from.FirstOfMonth(); !m.After(to); m = m.AddMonths(1) {
		mm, md := monthMoney[m], monthDays[m]
		out.ByMonth = append(out.ByMonth, api.ProfitabilityMonth{Month: apiconv.FromDate(m), IncomeKop: mm.IncomeKop,
			ExpensesKop: mm.ExpensesKop, RentedDays: md.Rented, IdleDays: md.Idle})
	}
	return out, nil
}

// ---------------------------------------------------------------- Заполняемость и простои (11 разд. 3)

// Occupancy — отчёт «Заполняемость и простои».
func (s *Service) Occupancy(ctx context.Context, from, to date.Date) (api.OccupancyReport, error) {
	out := api.OccupancyReport{From: apiconv.FromDate(from), To: apiconv.FromDate(to), Rows: []api.OccupancyRow{},
		IdlePeriods: []api.IdlePeriod{}}
	if err := checkPeriod(from, to); err != nil {
		return out, err
	}
	q := s.db.Queries()
	props, err := q.ReportProperties(ctx, sqlc.ReportPropertiesParams{DateFrom: from})
	if err != nil {
		return out, err
	}
	occ, err := s.occupancies(ctx, q, props, from, to, nil)
	if err != nil {
		return out, err
	}
	var t api.OccupancyTotals
	for _, p := range props {
		o := occ[p.ID]
		out.Rows = append(out.Rows, api.OccupancyRow{PropertyId: p.ID, PropertyCode: p.Code, PeriodDays: o.PeriodDays,
			RentedDays: o.Rented, FreeDays: o.Free, ReservedDays: o.Reserved, RepairDays: o.Repair, OccupancyPct: o.Pct(),
			ExpectedRentKop: p.ExpectedRentKop, LostIncomeKop: int64(o.Lost)})
		t.PeriodDays += o.PeriodDays
		t.RentedDays += o.Rented
		t.FreeDays += o.Free
		t.ReservedDays += o.Reserved
		t.RepairDays += o.Repair
		t.LostIncomeKop += int64(o.Lost)
		for _, sp := range o.Idle {
			out.IdlePeriods = append(out.IdlePeriods, api.IdlePeriod{PropertyId: p.ID, PropertyCode: p.Code,
				Status: api.PropertyStatus(sp.Status), From: apiconv.FromDate(sp.From), To: apiconv.FromDate(sp.To), Days: sp.Days,
				LostIncomeKop: int64(sp.Lost)})
		}
	}
	t.OccupancyPct = PctOf(int64(t.RentedDays), int64(t.RentedDays+t.FreeDays+t.ReservedDays+t.RepairDays))
	out.Totals = t
	return out, nil
}

// ---------------------------------------------------------------- Задолженность (11 разд. 4)

// Debts — задолженность по нанимателям на дату и случаи просрочки за период (по умолчанию — 12 месяцев до asOf).
func (s *Service) Debts(ctx context.Context, asOf *date.Date, from, to *date.Date) (api.DebtsReport, error) {
	on := s.clock.Today()
	if asOf != nil {
		on = *asOf
	}
	pf, pt := on.AddMonths(-12).AddDays(1), on
	if from != nil {
		pf = *from
	}
	if to != nil {
		pt = *to
	}
	out := api.DebtsReport{AsOf: apiconv.FromDate(on), Rows: []api.DebtRow{}}
	if err := checkPeriod(pf, pt); err != nil {
		return out, err
	}
	q := s.db.Queries()
	charges, err := q.DebtCharges(ctx, on)
	if err != nil {
		return out, err
	}
	amounts := map[uuid.UUID]*api.DebtAmounts{}
	for _, ch := range charges {
		rest := ch.AmountKop - ch.PaidKop
		if rest <= 0 {
			continue
		}
		a := amounts[ch.ContractID]
		if a == nil {
			a = &api.DebtAmounts{}
			amounts[ch.ContractID] = a
		}
		a.TotalDebtKop += rest
		if ch.Kind == "penalty" {
			a.PenaltiesKop += rest
			continue
		}
		age := 0
		if ch.DueDate != nil {
			age = on.Sub(*ch.DueDate)
		}
		switch {
		case age <= 0:
			a.NotDueKop += rest
		case age <= 30:
			a.Days1to30Kop += rest
		case age <= 60:
			a.Days31to60Kop += rest
		case age <= 90:
			a.Days61to90Kop += rest
		default:
			a.Over90Kop += rest
		}
	}
	contracts, err := q.DebtContracts(ctx, on)
	if err != nil {
		return out, err
	}
	var tot api.DebtAmounts
	for _, c := range contracts {
		a := amounts[c.ID]
		if a == nil {
			a = &api.DebtAmounts{}
		}
		a.DepositBalanceKop, a.AdvanceKop = c.DepositKop, max(c.AdvanceKop, 0)
		if a.TotalDebtKop <= 0 && a.AdvanceKop <= 0 {
			continue
		}
		p := party{c.TenantKind, c.LastName, c.FirstName, c.MiddleName, c.OrgName}
		row := api.DebtRow{
			Tenant: api.TenantRef{Id: c.TenantID, DisplayName: p.display()}, Contract: api.ContractRef{Id: c.ID, Number: c.Number},
			Property: api.PropertyRef{Id: c.PropertyID, Code: c.PropertyCode}, ContractStatus: api.ContractStatus(c.Status),
			NotDueKop: a.NotDueKop, Days1to30Kop: a.Days1to30Kop, Days31to60Kop: a.Days31to60Kop, Days61to90Kop: a.Days61to90Kop,
			Over90Kop: a.Over90Kop, PenaltiesKop: a.PenaltiesKop, TotalDebtKop: a.TotalDebtKop, DepositBalanceKop: a.DepositBalanceKop,
			AdvanceKop: a.AdvanceKop,
		}
		if !c.LastPaymentOn.IsZero() {
			row.LastPaymentOn = apiconv.FromDatePtr(&c.LastPaymentOn)
		}
		out.Rows = append(out.Rows, row)
		tot.NotDueKop += a.NotDueKop
		tot.Days1to30Kop += a.Days1to30Kop
		tot.Days31to60Kop += a.Days31to60Kop
		tot.Days61to90Kop += a.Days61to90Kop
		tot.Over90Kop += a.Over90Kop
		tot.PenaltiesKop += a.PenaltiesKop
		tot.TotalDebtKop += a.TotalDebtKop
		tot.DepositBalanceKop += a.DepositBalanceKop
		tot.AdvanceKop += a.AdvanceKop
	}
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].TotalDebtKop > out.Rows[j].TotalDebtKop })
	out.Totals = tot
	out.Overdue, err = s.overdue(ctx, q, on, pf, pt)
	return out, err
}

// overdue — случаи просрочки (11 разд. 4, лист «Просрочки»): начисление платы или ЖКУ со сроком в периоде, не
// погашенное полностью на конец дня срок + льгота (распределения от платежей, поступивших не позже этой даты).
func (s *Service) overdue(ctx context.Context, q *sqlc.Queries, asOf, from, to date.Date) (api.OverdueSummary, error) {
	out := api.OverdueSummary{From: apiconv.FromDate(from), To: apiconv.FromDate(to), Rows: []api.OverdueCase{}}
	cands, err := q.OverdueCandidates(ctx, sqlc.OverdueCandidatesParams{DateFrom: from, DateTo: to})
	if err != nil {
		return out, err
	}
	ids := make([]uuid.UUID, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.ID)
	}
	pays, err := q.ChargePaymentsByIDs(ctx, ids)
	if err != nil {
		return out, err
	}
	byCharge := map[uuid.UUID][]sqlc.ChargePaymentsByIDsRow{}
	for _, p := range pays {
		byCharge[p.ChargeID] = append(byCharge[p.ChargeID], p)
	}
	total := 0
	for _, c := range cands {
		limit := c.DueDate.AddDays(int(c.PenaltyGraceDays))
		if !limit.Before(asOf) {
			continue // срок с льготой ещё не прошёл
		}
		paidOff := PaidOffOn(c.AmountKop, byCharge[c.ID], asOf)
		if paidOff != nil && !paidOff.After(limit) {
			continue // погашено вовремя
		}
		end := asOf
		if paidOff != nil {
			end = *paidOff
		}
		days := end.Sub(c.DueDate)
		total += days
		out.Rows = append(out.Rows, api.OverdueCase{
			Contract: api.ContractRef{Id: c.ContractID, Number: c.ContractNumber}, Property: api.PropertyRef{Id: c.PropertyID, Code: c.PropertyCode},
			ChargeId: c.ID, ChargeKind: api.ChargeKind(c.Kind), ChargeTitle: c.Title, PeriodMonth: apiconv.FromDatePtr(c.PeriodMonth),
			DueDate: apiconv.FromDate(c.DueDate), PaidOffOn: apiconv.FromDatePtr(paidOff), OverdueDays: days,
		})
	}
	out.Count = len(out.Rows)
	if out.Count > 0 {
		avg := decimal.NewFromInt(int64(total)).Div(decimal.NewFromInt(int64(out.Count))).Round(1).StringFixed(1)
		out.AvgDays = &avg
	}
	return out, nil
}

// PaidOffOn — дата полного погашения начисления суммой amount (по датам поступления платежей, не позже asOf);
// nil — не погашено.
func PaidOffOn(amount int64, pays []sqlc.ChargePaymentsByIDsRow, asOf date.Date) *date.Date {
	var sum int64
	for _, p := range pays {
		if p.ReceivedOn.After(asOf) {
			break
		}
		sum += p.AmountKop
		if sum >= amount {
			d := p.ReceivedOn
			return &d
		}
	}
	return nil
}

// ---------------------------------------------------------------- Налоговая база (11 разд. 5)

// RegimeTitles — режимы налогообложения для шапки отчёта.
var RegimeTitles = map[string]string{
	"usn_income": "УСН «доходы»", "usn_income_expense": "УСН «доходы минус расходы»", "npd": "НПД",
	"psn": "ПСН", "osn": "ОСН",
}

type taxPeriod struct {
	label      string
	cumulative bool
	from, to   date.Date
}

// taxPeriods — I–IV кварталы и нарастающий итог (I квартал, полугодие, 9 месяцев, год).
func taxPeriods(year int) []taxPeriod {
	q := func(n int) (date.Date, date.Date) {
		f := date.New(year, time.Month(3*n-2), 1)
		return f, f.AddMonths(3).AddDays(-1)
	}
	var out []taxPeriod
	for i, label := range []string{"I квартал", "II квартал", "III квартал", "IV квартал"} {
		f, t := q(i + 1)
		out = append(out, taxPeriod{label: label, from: f, to: t})
	}
	start := date.New(year, time.January, 1)
	for i, label := range []string{"I квартал", "полугодие", "9 месяцев", "год"} {
		_, t := q(i + 1)
		out = append(out, taxPeriod{label: label, cumulative: true, from: start, to: t})
	}
	return out
}

// TaxBase — налоговая база за год (или за период from–to — одной строкой).
func (s *Service) TaxBase(ctx context.Context, year *int, from, to *date.Date) (api.TaxBaseReport, error) {
	var periods []taxPeriod
	var f, t date.Date
	switch {
	case year != nil:
		periods = taxPeriods(*year)
		f, t = date.New(*year, time.January, 1), date.New(*year, time.December, 31)
	case from != nil && to != nil:
		f, t = *from, *to
		periods = []taxPeriod{{label: "Период", from: f, to: t}}
	default:
		return api.TaxBaseReport{}, apperr.Validation(apperr.FieldError{Field: "year", Message: "Укажите год или период"})
	}
	if err := checkPeriod(f, t); err != nil {
		return api.TaxBaseReport{}, err
	}
	q := s.db.Queries()
	org, err := s.settings.Get(ctx, q)
	if err != nil {
		return api.TaxBaseReport{}, err
	}
	ops, err := LoadOps(ctx, q, f, t, nil)
	if err != nil {
		return api.TaxBaseReport{}, err
	}
	exps, err := q.ReportExpenses(ctx, sqlc.ReportExpensesParams{DateFrom: f, DateTo: t})
	if err != nil {
		return api.TaxBaseReport{}, err
	}
	deps, err := q.IncomeDepositOps(ctx, sqlc.IncomeDepositOpsParams{DateFrom: f, DateTo: t})
	if err != nil {
		return api.TaxBaseReport{}, err
	}
	regime, rate := org.TaxRegime, org.TaxRatePct
	out := api.TaxBaseReport{From: apiconv.FromDate(f), To: apiconv.FromDate(t), Year: year, TaxRegime: api.TaxRegime(regime),
		TaxRatePct: rate.StringFixed(2), RegimeTitle: regimeTitle(regime, rate), Periods: []api.TaxPeriodRow{},
		Operations: []api.TaxOperation{}}
	if regime == "psn" || regime == "osn" {
		out.Note = apiconv.Ptr("Налог по этому режиму не рассчитывается")
	}
	for _, d := range deps {
		switch {
		case d.Kind == "receipt" && !d.FromAdvance:
			out.DepositsReceivedKop += d.AmountKop
		case d.Kind == "refund":
			out.DepositsRefundedKop += d.AmountKop
		}
	}
	for _, p := range periods {
		k := KindsOf(ops, func(op Op) bool { return !op.Date.Before(p.from) && !op.Date.After(p.to) })
		var expenses money.Kop
		for _, e := range exps {
			if !e.PaidOn.Before(p.from) && !e.PaidOn.After(p.to) {
				expenses += money.Kop(e.AmountKop)
			}
		}
		out.Periods = append(out.Periods, taxRow(p, k, regime, rate, expenses))
	}
	for _, op := range ops {
		out.Operations = append(out.Operations, api.TaxOperation{Date: apiconv.FromDate(op.Date), Document: op.Document,
			Contract: api.ContractRef{Id: op.ContractID, Number: op.ContractNumber}, Tenant: api.TenantRef{Id: op.TenantID, DisplayName: op.TenantName},
			Property: api.PropertyRef{Id: op.PropertyID, Code: op.PropertyCode}, Kind: api.IncomeKind(op.Kind), AmountKop: int64(op.Amount)})
	}
	return out, nil
}

func regimeTitle(regime string, rate decimal.Decimal) string {
	t := "Режим: " + RegimeTitles[regime]
	switch regime {
	case "usn_income", "usn_income_expense":
		t += ", ставка " + strings.ReplaceAll(rate.String(), ".", ",") + " %"
	case "npd":
		t += ", ставки 4 % и 6 %"
	}
	return t
}

func taxRow(p taxPeriod, k Kinds, regime string, rate decimal.Decimal, expenses money.Kop) api.TaxPeriodRow {
	b := kindsDTO(k)
	res := calc.Tax(regime, rate, k.Income(), expenses)
	row := api.TaxPeriodRow{Label: p.label, Cumulative: p.cumulative, From: apiconv.FromDate(p.from), To: apiconv.FromDate(p.to),
		RentKop: b.RentKop, UtilitiesKop: b.UtilitiesKop, PenaltyKop: b.PenaltyKop, DamageKop: b.DamageKop, OtherKop: b.OtherKop,
		AdvanceKop: b.AdvanceKop, DepositWithheldKop: b.DepositWithheldKop, RefundsKop: b.RefundsKop, ToDepositKop: b.ToDepositKop,
		IncomeKop: b.IncomeKop, TaxBaseKop: int64(max(res.Base, 0))}
	if res.Tax != nil {
		row.TaxKop = apiconv.Ptr(int64(*res.Tax))
	}
	switch regime {
	case "usn_income_expense":
		row.ExpensesKop = apiconv.Ptr(int64(expenses))
	case "npd":
		ind := calc.Tax("usn_income", decimal.NewFromInt(4), calc.Income{Total: k.FromIndividuals}, 0)
		leg := calc.Tax("usn_income", decimal.NewFromInt(6), calc.Income{Total: k.FromLegal}, 0)
		row.IncomeIndividualsKop, row.IncomeLegalKop = apiconv.Ptr(int64(k.FromIndividuals)), apiconv.Ptr(int64(k.FromLegal))
		row.TaxIndividualsKop, row.TaxLegalKop = apiconv.Ptr(int64(*ind.Tax)), apiconv.Ptr(int64(*leg.Tax))
	}
	return row
}

// ---------------------------------------------------------------- Доходы для книги учёта (11 разд. 6)

// IncomeBook — доходы (и расходы при УСН «доходы минус расходы») за год по кварталам для раздела I КУДиР.
func (s *Service) IncomeBook(ctx context.Context, year int) (api.IncomeBookReport, error) {
	q := s.db.Queries()
	org, err := s.settings.Get(ctx, q)
	if err != nil {
		return api.IncomeBookReport{}, err
	}
	f, t := date.New(year, time.January, 1), date.New(year, time.December, 31)
	ops, err := LoadOps(ctx, q, f, t, nil)
	if err != nil {
		return api.IncomeBookReport{}, err
	}
	withExpenses := org.TaxRegime == "usn_income_expense"
	var exps []sqlc.ReportExpensesRow
	if withExpenses {
		if exps, err = q.ReportExpenses(ctx, sqlc.ReportExpensesParams{DateFrom: f, DateTo: t}); err != nil {
			return api.IncomeBookReport{}, err
		}
	}
	taxObject := "Доходы"
	if withExpenses {
		taxObject = "Доходы, уменьшенные на величину расходов"
	}
	out := api.IncomeBookReport{Year: year, OrgName: org.FullName, Inn: org.Inn, TaxObject: taxObject, Rows: []api.IncomeBookRow{}}
	type line struct {
		on      date.Date
		doc     string
		descr   string
		income  money.Kop
		expense money.Kop
	}
	var all []line
	for _, op := range ops {
		doc := op.Date.Format() + ", " + op.Document
		if op.Kind != calc.IncomeWithholding && op.Kind != calc.IncomeRefund && op.Kind != calc.IncomeToDeposit {
			doc = op.Date.Format() + ", квитанция " + op.Document
		}
		all = append(all, line{on: op.Date, doc: doc, descr: op.Description(), income: op.Amount})
	}
	for _, e := range exps {
		all = append(all, line{on: e.PaidOn, doc: e.PaidOn.Format() + ", расход", descr: "Расход: " + e.Description, expense: money.Kop(e.AmountKop)})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].on.Before(all[j].on) })
	quarters := []string{"I квартал", "II квартал", "III квартал", "IV квартал"}
	no := 0
	var yearInc, yearExp money.Kop
	for qn := 0; qn < 4; qn++ {
		var qi, qe money.Kop
		for _, l := range all {
			if int(l.on.Month()-1)/3 != qn {
				continue
			}
			no++
			row := api.IncomeBookRow{RowType: api.Operation, No: apiconv.Ptr(no), Date: apiconv.FromDatePtr(&l.on), Document: apiconv.Ptr(l.doc),
				Description: l.descr, IncomeKop: int64(l.income)}
			if withExpenses {
				row.ExpensesKop = apiconv.Ptr(int64(l.expense))
			}
			out.Rows = append(out.Rows, row)
			qi += l.income
			qe += l.expense
		}
		total := api.IncomeBookRow{RowType: api.QuarterTotal, Description: "Итого за " + quarters[qn], IncomeKop: int64(qi)}
		if withExpenses {
			total.ExpensesKop = apiconv.Ptr(int64(qe))
		}
		out.Rows = append(out.Rows, total)
		yearInc += qi
		yearExp += qe
	}
	total := api.IncomeBookRow{RowType: api.YearTotal, Description: fmt.Sprintf("Итого за %d год", year), IncomeKop: int64(yearInc)}
	if withExpenses {
		total.ExpensesKop = apiconv.Ptr(int64(yearExp))
	}
	out.Rows = append(out.Rows, total)
	return out, nil
}

// YearToDate — поступления и заполняемость объекта с начала года по сегодня (показатели карточки объекта).
func (s *Service) YearToDate(ctx context.Context, propertyID uuid.UUID) (int64, *string, error) {
	today := s.clock.Today()
	p, err := s.Profitability(ctx, propertyID, date.New(today.Year(), time.January, 1), today)
	if err != nil {
		return 0, nil, err
	}
	return p.IncomeKop, p.OccupancyPct, nil
}
