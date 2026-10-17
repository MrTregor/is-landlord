package payment

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/xlsx"
)

var (
	paymentStatusTitles = map[string]string{"pending": "Ожидает оплаты", "succeeded": "Проведён", "failed": "Не прошёл", "cancelled": "Отменён"}
	purposeTitles       = map[string]string{PurposeCharges: "Начисления", PurposeDeposit: "Залог"}
	methodTitles        = map[string]string{"online": "Онлайн (карта, СБП)", MethodBankTransfer: "Перевод на счёт", MethodCash: "Наличные",
		MethodDepositOffset: "Зачёт из залога"}
	fiscalTitles = map[string]string{"queued": "В очереди", "wait": "Обрабатывается", "done": "Сформирован", "fail": "Ошибка",
		"manual_required": "Нужно сформировать вручную"}
)

// ExportPayments — GET /payments.xlsx: те же фильтры, что у списка; колонки — как у таблицы экрана «Платежи»
// (14 разд. 6.1); итог по деньгам — по проведённым платежам (11 разд. 8).
func (h *Handler) ExportPayments(ctx context.Context, req api.ExportPaymentsRequestObject) (api.ExportPaymentsResponseObject, error) {
	p := req.Params
	params := sqlc.ListPaymentsParams{
		ContractID: p.ContractId, TenantID: p.TenantId, PropertyID: p.PropertyId, DateFrom: apiconv.ToDatePtr(p.From),
		DateTo: apiconv.ToDatePtr(p.To), Q: apiconv.TrimPtr(p.Q), Lim: 50000,
	}
	if p.Status != nil {
		params.Status = apiconv.Ptr(string(*p.Status))
	}
	if p.Method != nil {
		params.Method = apiconv.Ptr(string(*p.Method))
	}
	if p.Source != nil {
		params.Source = apiconv.Ptr(string(*p.Source))
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	rows, err := h.svc.db.Queries().ListPayments(ctx, params)
	if err != nil {
		return nil, err
	}
	data := make([][]any, 0, len(rows))
	var amount, allocated, advance money.Kop
	for _, r := range rows {
		d := paymentDTO(r)
		fiscal := ""
		if r.FiscalStatus != nil {
			fiscal = fiscalTitles[*r.FiscalStatus]
		}
		data = append(data, []any{r.ReceivedOn, valStr(r.ReceiptNumber), r.ContractNumber,
			names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName), r.PropertyCode, purposeTitles[r.Purpose],
			money.Kop(d.AmountKop), money.Kop(d.AllocatedKop), money.Kop(d.AdvanceKop), methodTitles[r.Method],
			paymentStatusTitles[r.Status], fiscal})
		if r.Status == "succeeded" {
			amount += money.Kop(d.AmountKop)
			allocated += money.Kop(d.AllocatedKop)
			advance += money.Kop(d.AdvanceKop)
		}
	}
	now := h.svc.clock.Now().In(h.svc.clock.Location())
	file, err := xlsx.Build(xlsx.Sheet{
		Name: "Платежи", Title: "Платежи", Subtitle: xlsx.Subtitle("", now, ""),
		Columns: []xlsx.Column{{Title: "Дата поступления", Width: 12, Kind: xlsx.Date}, {Title: "Квитанция", Width: 15},
			{Title: "Договор", Width: 11}, {Title: "Наниматель", Width: 28}, {Title: "Объект", Width: 16}, {Title: "Назначение", Width: 12},
			{Title: "Сумма", Width: 14, Kind: xlsx.Money}, {Title: "Распределено", Width: 14, Kind: xlsx.Money},
			{Title: "Аванс", Width: 14, Kind: xlsx.Money}, {Title: "Способ", Width: 18}, {Title: "Статус", Width: 14}, {Title: "Чек", Width: 18}},
		Rows: data, Totals: []any{"Итого (проведённые)", nil, nil, nil, nil, nil, amount, allocated, advance, nil, nil, nil},
	})
	if err != nil {
		return nil, err
	}
	return api.ExportPayments200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(
			xlsx.FileName("Платежи на", h.svc.clock.Today().Format()), file)}, nil
}
