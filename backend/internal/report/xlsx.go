package report

import (
	"context"
	"fmt"
	"strings"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/xlsx"
)

var propertyStatusTitles = map[api.PropertyStatus]string{
	"free": "Свободен", "reserved": "Забронирован", "rented": "Сдан", "repair": "На ремонте",
}

var contractStatusTitles = map[api.ContractStatus]string{
	"draft": "Черновик", "active": "Действует", "extended": "Продлён", "terminated": "Расторгнут", "finished": "Завершён",
}

func k(v int64) money.Kop { return money.Kop(v) }

func dt(d api.IncomeExpenseMonthRow) date.Date { return apiconv.ToDate(d.Month) }

// subtitle — строка 2 листа: период, время формирования, организация.
func (s *Service) subtitle(ctx context.Context, period string) string {
	org := ""
	if o, err := s.settings.Get(ctx, s.db.Queries()); err == nil {
		org = o.ShortName
	}
	return xlsx.Subtitle(period, s.clock.Now().In(s.clock.Location()), org)
}

func money0(c xlsx.Column) xlsx.Column { c.Kind = xlsx.Money; return c }

func moneyCols(titles ...string) []xlsx.Column {
	out := make([]xlsx.Column, 0, len(titles))
	for _, t := range titles {
		out = append(out, money0(xlsx.Column{Title: t, Width: 15}))
	}
	return out
}

// IncomeExpenseXLSX — «Доходы и расходы по объектам»: листы «По объектам» и «По месяцам».
func (s *Service) IncomeExpenseXLSX(ctx context.Context, r api.IncomeExpenseReport) ([]byte, string, error) {
	from, to := apiconv.ToDate(r.From), apiconv.ToDate(r.To)
	sub := s.subtitle(ctx, xlsx.Period(from, to))
	cols := append([]xlsx.Column{{Title: "Объект", Width: 18}}, moneyCols("Начислено", "Плата за наём", "Компенсация ЖКУ", "Пени",
		"Возмещение ущерба", "Прочее", "Авансы", "Удержания из залога", "Возвраты", "Перенос переплаты в залог",
		"Поступления, всего", "Расходы", "Финансовый результат")...)
	cols = append(cols, xlsx.Column{Title: "Рентабельность, %", Width: 14, Kind: xlsx.Pct},
		xlsx.Column{Title: "Заполняемость, %", Width: 14, Kind: xlsx.Pct})
	line := func(name string, t api.IncomeExpenseTotals) []any {
		return []any{name, k(t.ChargedKop), k(t.RentKop), k(t.UtilitiesKop), k(t.PenaltyKop), k(t.DamageKop), k(t.OtherKop),
			k(t.AdvanceKop), k(t.DepositWithheldKop), k(t.RefundsKop), k(t.ToDepositKop), k(t.IncomeKop), k(t.ExpensesKop),
			k(t.ResultKop), t.MarginPct, t.OccupancyPct}
	}
	rows := make([][]any, 0, len(r.Rows))
	for _, x := range r.Rows {
		rows = append(rows, line(x.PropertyCode, api.IncomeExpenseTotals{ChargedKop: x.ChargedKop, RentKop: x.RentKop,
			UtilitiesKop: x.UtilitiesKop, PenaltyKop: x.PenaltyKop, DamageKop: x.DamageKop, OtherKop: x.OtherKop, AdvanceKop: x.AdvanceKop,
			DepositWithheldKop: x.DepositWithheldKop, RefundsKop: x.RefundsKop, ToDepositKop: x.ToDepositKop, IncomeKop: x.IncomeKop,
			ExpensesKop: x.ExpensesKop, ResultKop: x.ResultKop, MarginPct: x.MarginPct, OccupancyPct: x.OccupancyPct}))
	}
	months := make([][]any, 0, len(r.ByMonth))
	var mi, me, mr int64
	for _, m := range r.ByMonth {
		months = append(months, []any{dt(m), m.PropertyCode, k(m.IncomeKop), k(m.ExpensesKop), k(m.ResultKop)})
		mi, me, mr = mi+m.IncomeKop, me+m.ExpensesKop, mr+m.ResultKop
	}
	data, err := xlsx.Build(
		xlsx.Sheet{Name: "По объектам", Title: "Доходы и расходы по объектам", Subtitle: sub, Columns: cols, Rows: rows,
			Totals: line("Итого", r.Totals)},
		xlsx.Sheet{Name: "По месяцам", Title: "Доходы и расходы по месяцам", Subtitle: sub,
			Columns: append([]xlsx.Column{{Title: "Месяц", Width: 12, Kind: xlsx.Date}, {Title: "Объект", Width: 18}},
				moneyCols("Поступления", "Расходы", "Результат")...),
			Rows: months, Totals: []any{"Итого", nil, k(mi), k(me), k(mr)}},
	)
	return data, xlsx.FileName("Доходы и расходы", from.Format()+"–"+to.Format()), err
}

// OccupancyXLSX — «Заполняемость и простои»: листы «Заполняемость» и «Периоды простоя».
func (s *Service) OccupancyXLSX(ctx context.Context, r api.OccupancyReport) ([]byte, string, error) {
	from, to := apiconv.ToDate(r.From), apiconv.ToDate(r.To)
	sub := s.subtitle(ctx, xlsx.Period(from, to))
	rows := make([][]any, 0, len(r.Rows))
	var expected int64
	for _, x := range r.Rows {
		rows = append(rows, []any{x.PropertyCode, x.PeriodDays, x.RentedDays, x.FreeDays, x.ReservedDays, x.RepairDays, x.OccupancyPct,
			k(x.ExpectedRentKop), k(x.LostIncomeKop)})
		expected += x.ExpectedRentKop
	}
	t := r.Totals
	idle := make([][]any, 0, len(r.IdlePeriods))
	var lost int64
	for _, p := range r.IdlePeriods {
		idle = append(idle, []any{p.PropertyCode, propertyStatusTitles[p.Status], apiconv.ToDate(p.From), apiconv.ToDate(p.To), p.Days,
			k(p.LostIncomeKop)})
		lost += p.LostIncomeKop
	}
	data, err := xlsx.Build(
		xlsx.Sheet{Name: "Заполняемость", Title: "Заполняемость и простои", Subtitle: sub,
			Columns: []xlsx.Column{{Title: "Объект", Width: 18}, {Title: "Дней в периоде", Width: 11, Kind: xlsx.Int},
				{Title: "Сдан", Width: 9, Kind: xlsx.Int}, {Title: "Свободен", Width: 10, Kind: xlsx.Int},
				{Title: "Забронирован", Width: 13, Kind: xlsx.Int}, {Title: "На ремонте", Width: 11, Kind: xlsx.Int},
				{Title: "Заполняемость, %", Width: 14, Kind: xlsx.Pct}, {Title: "Ожидаемая плата в месяц", Width: 16, Kind: xlsx.Money},
				{Title: "Недополученный доход", Width: 16, Kind: xlsx.Money}},
			Rows: rows, Totals: []any{"Итого", t.PeriodDays, t.RentedDays, t.FreeDays, t.ReservedDays, t.RepairDays, t.OccupancyPct,
				k(expected), k(t.LostIncomeKop)}},
		xlsx.Sheet{Name: "Периоды простоя", Title: "Периоды простоя", Subtitle: sub,
			Columns: []xlsx.Column{{Title: "Объект", Width: 18}, {Title: "Статус", Width: 14}, {Title: "С", Width: 12, Kind: xlsx.Date},
				{Title: "По", Width: 12, Kind: xlsx.Date}, {Title: "Дней", Width: 8, Kind: xlsx.Int},
				{Title: "Недополученный доход", Width: 16, Kind: xlsx.Money}},
			Rows: idle, Totals: []any{"Итого", nil, nil, nil, nil, k(lost)}},
	)
	return data, xlsx.FileName("Заполняемость", from.Format()+"–"+to.Format()), err
}

// DebtsXLSX — «Задолженность по нанимателям»: листы «Задолженность» и «Просрочки».
func (s *Service) DebtsXLSX(ctx context.Context, r api.DebtsReport) ([]byte, string, error) {
	on := apiconv.ToDate(r.AsOf)
	sub := s.subtitle(ctx, "На дату: "+on.Format())
	rows := make([][]any, 0, len(r.Rows))
	for _, x := range r.Rows {
		var last any
		if x.LastPaymentOn != nil {
			last = apiconv.ToDate(*x.LastPaymentOn)
		}
		rows = append(rows, []any{x.Tenant.DisplayName, x.Contract.Number, x.Property.Code, contractStatusTitles[x.ContractStatus],
			k(x.NotDueKop), k(x.Days1to30Kop), k(x.Days31to60Kop), k(x.Days61to90Kop), k(x.Over90Kop), k(x.PenaltiesKop),
			k(x.TotalDebtKop), k(x.DepositBalanceKop), k(x.AdvanceKop), last})
	}
	t := r.Totals
	o := r.Overdue
	cases := make([][]any, 0, len(o.Rows))
	for _, c := range o.Rows {
		var paid any = "не погашено"
		if c.PaidOffOn != nil {
			paid = apiconv.ToDate(*c.PaidOffOn)
		}
		cases = append(cases, []any{c.Contract.Number, c.Property.Code, c.ChargeTitle, apiconv.ToDate(c.DueDate), paid, c.OverdueDays})
	}
	avg := "—"
	if o.AvgDays != nil {
		avg = strings.ReplaceAll(*o.AvgDays, ".", ",")
	}
	data, err := xlsx.Build(
		xlsx.Sheet{Name: "Задолженность", Title: "Задолженность по нанимателям", Subtitle: sub,
			Columns: append(append([]xlsx.Column{{Title: "Наниматель", Width: 28}, {Title: "Договор", Width: 11}, {Title: "Объект", Width: 16},
				{Title: "Статус договора", Width: 13}}, moneyCols("Срок не наступил", "1–30 дн.", "31–60 дн.", "61–90 дн.",
				"Более 90 дн.", "Пени", "Итого долг", "Остаток залога", "Аванс")...),
				xlsx.Column{Title: "Последний платёж", Width: 13, Kind: xlsx.Date}),
			Rows: rows, Totals: []any{"Итого", nil, nil, nil, k(t.NotDueKop), k(t.Days1to30Kop), k(t.Days31to60Kop), k(t.Days61to90Kop),
				k(t.Over90Kop), k(t.PenaltiesKop), k(t.TotalDebtKop), k(t.DepositBalanceKop), k(t.AdvanceKop), nil}},
		xlsx.Sheet{Name: "Просрочки", Title: "Случаи просрочки",
			Subtitle: s.subtitle(ctx, xlsx.Period(apiconv.ToDate(o.From), apiconv.ToDate(o.To))),
			Notes:    []string{fmt.Sprintf("Случаев просрочки: %d, средняя длительность: %s дн.", o.Count, avg)},
			Columns: []xlsx.Column{{Title: "Договор", Width: 11}, {Title: "Объект", Width: 16}, {Title: "Начисление", Width: 34},
				{Title: "Срок оплаты", Width: 12, Kind: xlsx.Date}, {Title: "Дата полного погашения", Width: 14, Kind: xlsx.Date},
				{Title: "Дней просрочки", Width: 11, Kind: xlsx.Int}},
			Rows: cases, Totals: []any{fmt.Sprintf("Итого: %d", o.Count), nil, nil, nil, nil, nil}},
	)
	return data, xlsx.FileName("Задолженность на", on.Format()), err
}

// TaxBaseXLSX — «Налоговая база»: листы «Сводка» и «Операции».
func (s *Service) TaxBaseXLSX(ctx context.Context, r api.TaxBaseReport) ([]byte, string, error) {
	from, to := apiconv.ToDate(r.From), apiconv.ToDate(r.To)
	sub := s.subtitle(ctx, xlsx.Period(from, to))
	notes := []string{r.RegimeTitle,
		"Справочно: внесено обеспечительных платежей " + money.Kop(r.DepositsReceivedKop).Format() +
			", возвращено обеспечительных платежей " + money.Kop(r.DepositsRefundedKop).Format() + " (в доход не входят)"}
	if r.Note != nil {
		notes = append(notes, *r.Note)
	}
	cols := append([]xlsx.Column{{Title: "Период", Width: 14}}, moneyCols("Плата за наём", "Компенсация ЖКУ", "Пени",
		"Возмещение ущерба", "Прочее", "Авансы", "Удержания из залога", "Возвраты переплат", "Перенос переплаты в залог",
		"Доходы, всего")...)
	regime := string(r.TaxRegime)
	switch regime {
	case "usn_income", "usn_income_expense":
		if regime == "usn_income_expense" {
			cols = append(cols, moneyCols("Расходы")...)
		}
		cols = append(cols, moneyCols("Налоговая база")...)
		cols = append(cols, xlsx.Column{Title: "Ставка, %", Width: 9, Kind: xlsx.Pct})
		cols = append(cols, moneyCols("Налог исчисленный")...)
	case "npd":
		cols = append(cols, moneyCols("Доходы от физлиц", "Доходы от юрлиц", "Налог 4 %", "Налог 6 %")...)
	}
	rows := make([][]any, 0, len(r.Periods))
	bold := map[int]bool{}
	for i, p := range r.Periods {
		label := p.Label
		if p.Cumulative {
			label = "Нарастающим итогом: " + p.Label
			bold[i] = true
		}
		row := []any{label, k(p.RentKop), k(p.UtilitiesKop), k(p.PenaltyKop), k(p.DamageKop), k(p.OtherKop), k(p.AdvanceKop),
			k(p.DepositWithheldKop), k(p.RefundsKop), k(p.ToDepositKop), k(p.IncomeKop)}
		switch regime {
		case "usn_income", "usn_income_expense":
			if regime == "usn_income_expense" {
				row = append(row, kp(p.ExpensesKop))
			}
			rate := r.TaxRatePct
			row = append(row, k(p.TaxBaseKop), &rate, kp(p.TaxKop))
		case "npd":
			row = append(row, kp(p.IncomeIndividualsKop), kp(p.IncomeLegalKop), kp(p.TaxIndividualsKop), kp(p.TaxLegalKop))
		}
		rows = append(rows, row)
	}
	ops := make([][]any, 0, len(r.Operations))
	var total int64
	for _, o := range r.Operations {
		ops = append(ops, []any{apiconv.ToDate(o.Date), o.Document, o.Contract.Number, o.Tenant.DisplayName, o.Property.Code,
			KindTitles[string(o.Kind)], k(o.AmountKop)})
		total += o.AmountKop
	}
	name := "Налоговая база " + from.Format() + "–" + to.Format()
	if r.Year != nil {
		name = fmt.Sprintf("Налоговая база %d", *r.Year)
	}
	data, err := xlsx.Build(
		xlsx.Sheet{Name: "Сводка", Title: "Налоговая база", Subtitle: sub, Notes: notes, Columns: cols, Rows: rows, Bold: bold},
		xlsx.Sheet{Name: "Операции", Title: "Доходные операции", Subtitle: sub,
			Columns: []xlsx.Column{{Title: "Дата", Width: 12, Kind: xlsx.Date}, {Title: "Документ", Width: 32}, {Title: "Договор", Width: 11},
				{Title: "Наниматель", Width: 28}, {Title: "Объект", Width: 16}, {Title: "Вид", Width: 22}, {Title: "Сумма", Width: 15, Kind: xlsx.Money}},
			Rows: ops, Totals: []any{"Итого", nil, nil, nil, nil, nil, k(total)}},
	)
	return data, xlsx.FileName(name), err
}

func kp(v *int64) any {
	if v == nil {
		return nil
	}
	return money.Kop(*v)
}

// IncomeBookXLSX — «Доходы для книги учёта»: лист «Раздел I».
func (s *Service) IncomeBookXLSX(ctx context.Context, r api.IncomeBookReport) ([]byte, string, error) {
	notes := []string{r.OrgName + ", ИНН " + r.Inn, fmt.Sprintf("Год: %d · Объект налогообложения: %s", r.Year, r.TaxObject)}
	withExp := false
	for _, x := range r.Rows {
		withExp = withExp || x.ExpensesKop != nil
	}
	cols := []xlsx.Column{{Title: "№ п/п", Width: 7, Kind: xlsx.Int}, {Title: "Дата и номер первичного документа", Width: 32},
		{Title: "Содержание операции", Width: 60}, {Title: "Доходы, учитываемые при исчислении налоговой базы", Width: 18, Kind: xlsx.Money}}
	if withExp {
		cols = append(cols, xlsx.Column{Title: "Расходы, учитываемые при исчислении налоговой базы", Width: 18, Kind: xlsx.Money})
	}
	rows := make([][]any, 0, len(r.Rows))
	bold := map[int]bool{}
	for i, x := range r.Rows {
		var no any
		if x.No != nil {
			no = *x.No
		}
		row := []any{no, deref(x.Document), x.Description, k(x.IncomeKop)}
		if withExp {
			row = append(row, kp(x.ExpensesKop))
		}
		if x.RowType != api.Operation {
			bold[i] = true
		}
		rows = append(rows, row)
	}
	data, err := xlsx.Build(xlsx.Sheet{Name: "Раздел I", Title: "Доходы и расходы для книги учёта (раздел I)",
		Subtitle: s.subtitle(ctx, fmt.Sprintf("Год: %d", r.Year)), Notes: notes, Columns: cols, Rows: rows, Bold: bold})
	return data, xlsx.FileName(fmt.Sprintf("Книга учёта доходов %d", r.Year)), err
}
