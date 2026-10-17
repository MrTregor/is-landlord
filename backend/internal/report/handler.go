package report

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/xlsx"
)

// Handler — методы API отчётов (06 разд. 14), сводки (06 разд. 5) и доходности объекта.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetDashboard — GET /dashboard.
func (h *Handler) GetDashboard(ctx context.Context, _ api.GetDashboardRequestObject) (api.GetDashboardResponseObject, error) {
	d, err := h.svc.Dashboard(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetDashboard200JSONResponse(d), nil
}

// GetPropertyProfitability — GET /properties/{id}/profitability.
func (h *Handler) GetPropertyProfitability(ctx context.Context, req api.GetPropertyProfitabilityRequestObject) (
	api.GetPropertyProfitabilityResponseObject, error,
) {
	d, err := h.svc.Profitability(ctx, req.Id, apiconv.ToDate(req.Params.From), apiconv.ToDate(req.Params.To))
	if err != nil {
		return nil, err
	}
	return api.GetPropertyProfitability200JSONResponse(d), nil
}

// GetIncomeExpenseReport — GET /reports/income-expense.
func (h *Handler) GetIncomeExpenseReport(ctx context.Context, req api.GetIncomeExpenseReportRequestObject) (
	api.GetIncomeExpenseReportResponseObject, error,
) {
	p := req.Params
	r, err := h.svc.IncomeExpense(ctx, apiconv.ToDate(p.From), apiconv.ToDate(p.To), p.PropertyId)
	if err != nil {
		return nil, err
	}
	return api.GetIncomeExpenseReport200JSONResponse(r), nil
}

// ExportIncomeExpenseReport — GET /reports/income-expense.xlsx.
func (h *Handler) ExportIncomeExpenseReport(ctx context.Context, req api.ExportIncomeExpenseReportRequestObject) (
	api.ExportIncomeExpenseReportResponseObject, error,
) {
	p := req.Params
	r, err := h.svc.IncomeExpense(ctx, apiconv.ToDate(p.From), apiconv.ToDate(p.To), p.PropertyId)
	if err != nil {
		return nil, err
	}
	data, name, err := h.svc.IncomeExpenseXLSX(ctx, r)
	if err != nil {
		return nil, err
	}
	return api.ExportIncomeExpenseReport200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(name, data)}, nil
}

// GetOccupancyReport — GET /reports/occupancy.
func (h *Handler) GetOccupancyReport(ctx context.Context, req api.GetOccupancyReportRequestObject) (
	api.GetOccupancyReportResponseObject, error,
) {
	r, err := h.svc.Occupancy(ctx, apiconv.ToDate(req.Params.From), apiconv.ToDate(req.Params.To))
	if err != nil {
		return nil, err
	}
	return api.GetOccupancyReport200JSONResponse(r), nil
}

// ExportOccupancyReport — GET /reports/occupancy.xlsx.
func (h *Handler) ExportOccupancyReport(ctx context.Context, req api.ExportOccupancyReportRequestObject) (
	api.ExportOccupancyReportResponseObject, error,
) {
	r, err := h.svc.Occupancy(ctx, apiconv.ToDate(req.Params.From), apiconv.ToDate(req.Params.To))
	if err != nil {
		return nil, err
	}
	data, name, err := h.svc.OccupancyXLSX(ctx, r)
	if err != nil {
		return nil, err
	}
	return api.ExportOccupancyReport200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(name, data)}, nil
}

// GetDebtsReport — GET /reports/debts.
func (h *Handler) GetDebtsReport(ctx context.Context, req api.GetDebtsReportRequestObject) (api.GetDebtsReportResponseObject, error) {
	p := req.Params
	r, err := h.svc.Debts(ctx, apiconv.ToDatePtr(p.AsOf), apiconv.ToDatePtr(p.From), apiconv.ToDatePtr(p.To))
	if err != nil {
		return nil, err
	}
	return api.GetDebtsReport200JSONResponse(r), nil
}

// ExportDebtsReport — GET /reports/debts.xlsx.
func (h *Handler) ExportDebtsReport(ctx context.Context, req api.ExportDebtsReportRequestObject) (api.ExportDebtsReportResponseObject, error) {
	p := req.Params
	r, err := h.svc.Debts(ctx, apiconv.ToDatePtr(p.AsOf), apiconv.ToDatePtr(p.From), apiconv.ToDatePtr(p.To))
	if err != nil {
		return nil, err
	}
	data, name, err := h.svc.DebtsXLSX(ctx, r)
	if err != nil {
		return nil, err
	}
	return api.ExportDebtsReport200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(name, data)}, nil
}

// GetTaxBaseReport — GET /reports/tax-base.
func (h *Handler) GetTaxBaseReport(ctx context.Context, req api.GetTaxBaseReportRequestObject) (api.GetTaxBaseReportResponseObject, error) {
	p := req.Params
	r, err := h.svc.TaxBase(ctx, p.Year, apiconv.ToDatePtr(p.From), apiconv.ToDatePtr(p.To))
	if err != nil {
		return nil, err
	}
	return api.GetTaxBaseReport200JSONResponse(r), nil
}

// ExportTaxBaseReport — GET /reports/tax-base.xlsx.
func (h *Handler) ExportTaxBaseReport(ctx context.Context, req api.ExportTaxBaseReportRequestObject) (
	api.ExportTaxBaseReportResponseObject, error,
) {
	p := req.Params
	r, err := h.svc.TaxBase(ctx, p.Year, apiconv.ToDatePtr(p.From), apiconv.ToDatePtr(p.To))
	if err != nil {
		return nil, err
	}
	data, name, err := h.svc.TaxBaseXLSX(ctx, r)
	if err != nil {
		return nil, err
	}
	return api.ExportTaxBaseReport200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(name, data)}, nil
}

// GetIncomeBookReport — GET /reports/income-book.
func (h *Handler) GetIncomeBookReport(ctx context.Context, req api.GetIncomeBookReportRequestObject) (
	api.GetIncomeBookReportResponseObject, error,
) {
	r, err := h.svc.IncomeBook(ctx, req.Params.Year)
	if err != nil {
		return nil, err
	}
	return api.GetIncomeBookReport200JSONResponse(r), nil
}

// ExportIncomeBookReport — GET /reports/income-book.xlsx.
func (h *Handler) ExportIncomeBookReport(ctx context.Context, req api.ExportIncomeBookReportRequestObject) (
	api.ExportIncomeBookReportResponseObject, error,
) {
	r, err := h.svc.IncomeBook(ctx, req.Params.Year)
	if err != nil {
		return nil, err
	}
	data, name, err := h.svc.IncomeBookXLSX(ctx, r)
	if err != nil {
		return nil, err
	}
	return api.ExportIncomeBookReport200ApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse: xlsx.Response(name, data)}, nil
}
