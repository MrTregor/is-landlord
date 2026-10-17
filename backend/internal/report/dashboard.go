package report

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/calendar"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
)

// Dashboard — сводка (06 разд. 5, 14 разд. 1): поступления за месяц и прошлый месяц, задолженность и просрочка,
// заполняемость (сданные объекты из неархивных), счётчики «Требуют внимания», поступления за 12 месяцев, объекты
// с текущими договорами и долгом, ближайшие события на 7 дней и просрочки (docs/open-questions.md, № 93).
func (s *Service) Dashboard(ctx context.Context) (api.Dashboard, error) {
	today := s.clock.Today()
	q := s.db.Queries()
	month := today.FirstOfMonth()
	start := month.AddMonths(-11)
	ops, err := LoadOps(ctx, q, start, today, nil)
	if err != nil {
		return api.Dashboard{}, err
	}
	byMonth := map[date.Date]money.Kop{}
	for _, op := range ops {
		byMonth[op.Date.FirstOfMonth()] += op.Amount
	}
	out := api.Dashboard{Today: apiconv.FromDate(today), IncomeMonthKop: int64(byMonth[month]),
		IncomePrevMonthKop: int64(byMonth[month.AddMonths(-1)]), IncomeByMonth: []api.DashboardIncomeMonth{},
		Properties: []api.DashboardProperty{}, Upcoming: []api.CalendarEventDTO{}}
	for m := start; !m.After(month); m = m.AddMonths(1) {
		out.IncomeByMonth = append(out.IncomeByMonth, api.DashboardIncomeMonth{Month: apiconv.FromDate(m), AmountKop: int64(byMonth[m])})
	}
	debts, err := q.DashboardDebts(ctx, today)
	if err != nil {
		return out, err
	}
	out.DebtTotalKop, out.OverdueKop, out.OverdueContracts = debts.DebtKop, debts.OverdueKop, int(debts.OverdueContracts)
	cnt, err := q.DashboardCounters(ctx)
	if err != nil {
		return out, err
	}
	out.OpenRequests, out.OpenDiscrepancies = int(cnt.NewRequests), int(cnt.OpenDiscrepancies)
	out.FiscalProblems, out.PendingReadings = int(cnt.FiscalProblems), int(cnt.PendingReadings)
	out.OccupancyPct = "0.0"
	if p := PctOf(cnt.PropertiesRented, cnt.PropertiesTotal); p != nil {
		out.OccupancyPct = *p
	}

	props, err := q.ReportProperties(ctx, sqlcProps(today))
	if err != nil {
		return out, err
	}
	ids := make([]uuid.UUID, 0, len(props))
	for _, p := range props {
		if p.ArchivedAt == nil {
			ids = append(ids, p.ID)
		}
	}
	current, err := q.CurrentContracts(ctx, sqlcCurrent(ids, today))
	if err != nil {
		return out, err
	}
	debtRows, err := q.ContractDebtsToday(ctx, today)
	if err != nil {
		return out, err
	}
	debtOf := map[uuid.UUID]int64{}
	for _, d := range debtRows {
		debtOf[d.ContractID] = d.DebtKop
	}
	byProp := map[uuid.UUID]int{}
	for i, c := range current {
		byProp[c.PropertyID] = i
	}
	for _, p := range props {
		if p.ArchivedAt != nil {
			continue
		}
		row := api.DashboardProperty{Id: p.ID, Code: p.Code, Status: api.PropertyStatus(p.Status)}
		if i, ok := byProp[p.ID]; ok {
			c := current[i]
			row.ContractId, row.ContractNumber = &c.ID, &c.Number
			row.EndDate = apiconv.FromDatePtr(&c.EndDate)
			row.TenantName = apiconv.Ptr(names.Display(c.TenantKind, c.LastName, c.FirstName, c.MiddleName, c.OrgName))
			row.DebtKop = debtOf[c.ID]
		}
		out.Properties = append(out.Properties, row)
	}

	// Ближайшие события: 7 дней вперёд и просрочки (за последние 60 дней), без выполненных.
	events, err := s.calendar.List(ctx, calendar.Filter{From: today.AddDays(-60), To: today.AddDays(7)})
	if err != nil {
		return out, err
	}
	for _, e := range events {
		if e.Done || (e.Date.Before(today) && e.Kind != calendar.KindOverdue) {
			continue
		}
		out.Upcoming = append(out.Upcoming, calendar.ToDTO(e))
	}
	return out, nil
}

func sqlcProps(today date.Date) sqlc.ReportPropertiesParams {
	return sqlc.ReportPropertiesParams{DateFrom: today}
}

func sqlcCurrent(ids []uuid.UUID, today date.Date) sqlc.CurrentContractsParams {
	return sqlc.CurrentContractsParams{PropertyIds: ids, OnDate: today}
}
