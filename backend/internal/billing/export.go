package billing

import (
	"context"
	"strconv"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/xlsx"
)

// exportLimit — предел строк выгрузки списка.
const exportLimit = 50000

var chargeStatusTitles = map[string]string{
	"open": "Ожидает оплаты", "partially_paid": "Оплачено частично", "paid": "Оплачено", "cancelled": "Отменено",
}

// ExportCharges — GET /charges.xlsx: те же фильтры, что у списка; колонки — как у таблицы экрана «Начисления»
// (14 разд. 5); итог по деньгам (11 разд. 8).
func (h *Handler) ExportCharges(ctx context.Context, req api.ExportChargesRequestObject) (api.ExportChargesResponseObject, error) {
	p := req.Params
	today := h.svc.clock.Today()
	params := sqlc.ListChargesParams{
		ContractID: p.ContractId, PropertyID: p.PropertyId, TenantID: p.TenantId, Kinds: []string{}, Statuses: []string{},
		Overdue: p.Overdue != nil && *p.Overdue, Today: today, PeriodFrom: apiconv.ToDatePtr(p.PeriodFrom),
		PeriodTo: apiconv.ToDatePtr(p.PeriodTo), DueFrom: apiconv.ToDatePtr(p.DueFrom), DueTo: apiconv.ToDatePtr(p.DueTo),
		Q: apiconv.TrimPtr(p.Q), Lim: exportLimit,
	}
	if p.Kind != nil {
		for _, k := range *p.Kind {
			params.Kinds = append(params.Kinds, string(k))
		}
	}
	if p.Status != nil {
		for _, s := range *p.Status {
			params.Statuses = append(params.Statuses, string(s))
		}
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	rows, err := h.svc.db.Queries().ListCharges(ctx, params)
	if err != nil {
		return nil, err
	}
	data := make([][]any, 0, len(rows))
	var amount, paid, rest money.Kop
	for _, r := range rows {
		out := money.Kop(r.AmountKop - r.PaidKop)
		if r.Status == "cancelled" {
			out = 0
		}
		status := chargeStatusTitles[r.Status]
		if r.DueDate != nil && out > 0 && r.DueDate.Before(today) {
			status += ", просрочено " + itoa(today.Sub(*r.DueDate)) + " дн."
		}
		origin := "авто"
		if r.Origin == "manual" {
			origin = "вручную"
		}
		data = append(data, []any{r.DueDate, r.Title, r.ContractNumber, r.PropertyCode,
			names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName), money.Kop(r.AmountKop), money.Kop(r.PaidKop),
			out, status, origin})
		if r.Status != "cancelled" {
			amount += money.Kop(r.AmountKop)
			paid += money.Kop(r.PaidKop)
			rest += out
		}
	}
	file, err := xlsx.Build(xlsx.Sheet{
		Name: "Начисления", Title: "Начисления", Subtitle: xlsx.Subtitle("", h.svc.clock.Now().In(h.svc.clock.Location()), ""),
		Columns: []xlsx.Column{{Title: "Срок оплаты", Width: 12, Kind: xlsx.Date}, {Title: "Начисление", Width: 36},
			{Title: "Договор", Width: 11}, {Title: "Объект", Width: 16}, {Title: "Наниматель", Width: 28},
			{Title: "Сумма", Width: 14, Kind: xlsx.Money}, {Title: "Оплачено", Width: 14, Kind: xlsx.Money},
			{Title: "Остаток", Width: 14, Kind: xlsx.Money}, {Title: "Статус", Width: 26}, {Title: "Создано", Width: 9}},
		Rows: data, Totals: []any{"Итого (без отменённых)", nil, nil, nil, nil, amount, paid, rest, nil, nil},
	})
	if err != nil {
		return nil, err
	}
	return api.ExportCharges200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(
			xlsx.FileName("Начисления на", today.Format()), file)}, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
