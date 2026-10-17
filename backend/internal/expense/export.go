package expense

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/xlsx"
)

var (
	categoryTitles = map[string]string{
		"repair": "Ремонт", "maintenance": "Содержание", "utilities_owner": "ЖКУ за счёт собственника", "tax": "Налоги и сборы",
		"insurance": "Страхование", "furniture_appliances": "Мебель и техника", "commission": "Комиссии", "other": "Прочее",
	}
	recoveryTitles = map[string]string{RecoveryNone: "Нет", RecoveryDeposit: "Из залога", RecoveryCharge: "Начисление"}
)

// ExportExpenses — GET /expenses.xlsx: те же фильтры, что у списка; колонки — как у таблицы экрана «Затраты»
// (14 разд. 10); итог по сумме (11 разд. 8).
func (h *Handler) ExportExpenses(ctx context.Context, req api.ExportExpensesRequestObject) (api.ExportExpensesResponseObject, error) {
	p := req.Params
	params := sqlc.ListExpensesParams{
		PropertyID: p.PropertyId, DateFrom: apiconv.ToDatePtr(p.From), DateTo: apiconv.ToDatePtr(p.To),
		RequestID: p.MaintenanceRequestId, Lim: 50000,
	}
	if p.Category != nil {
		params.Category = apiconv.Ptr(string(*p.Category))
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	rows, err := h.svc.db.Queries().ListExpenses(ctx, params)
	if err != nil {
		return nil, err
	}
	data := make([][]any, 0, len(rows))
	var total money.Kop
	for _, r := range rows {
		data = append(data, []any{r.PaidOn, r.PropertyCode, categoryTitles[r.Category], r.Description, deref(r.Counterparty),
			money.Kop(r.AmountKop), r.PeriodMonth, deref(r.RequestNumber), deref(r.FileName), recoveryTitles[r.Recovery]})
		total += money.Kop(r.AmountKop)
	}
	now := h.svc.clock.Now().In(h.svc.clock.Location())
	file, err := xlsx.Build(xlsx.Sheet{
		Name: "Затраты", Title: "Затраты", Subtitle: xlsx.Subtitle(periodOf(p), now, ""),
		Columns: []xlsx.Column{{Title: "Дата оплаты", Width: 12, Kind: xlsx.Date}, {Title: "Объект", Width: 16},
			{Title: "Категория", Width: 20}, {Title: "Описание", Width: 36}, {Title: "Контрагент", Width: 22},
			{Title: "Сумма", Width: 14, Kind: xlsx.Money}, {Title: "Месяц отнесения", Width: 12, Kind: xlsx.Date},
			{Title: "Заявка", Width: 13}, {Title: "Документ", Width: 20}, {Title: "Возмещение", Width: 12}},
		Rows: data, Totals: []any{"Итого", nil, nil, nil, nil, total, nil, nil, nil, nil},
	})
	if err != nil {
		return nil, err
	}
	return api.ExportExpenses200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(
			xlsx.FileName("Затраты на", h.svc.clock.Today().Format()), file)}, nil
}

func periodOf(p api.ExportExpensesParams) string {
	if p.From != nil && p.To != nil {
		return xlsx.Period(apiconv.ToDate(*p.From), apiconv.ToDate(*p.To))
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
