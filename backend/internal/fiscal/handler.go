package fiscal

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API чеков (06 разд. 11) и колбэк кассы (06 разд. 16).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func dto(r sqlc.ListFiscalReceiptsRow) api.FiscalReceiptDTO {
	d := api.FiscalReceiptDTO{
		Id: r.ID, PaymentId: r.PaymentID, RefundId: r.RefundID, ReceiptNumber: r.ReceiptNumber,
		Operation: api.FiscalOperation(r.Operation), Provider: api.FiscalProvider(r.Provider), Status: api.FiscalStatus(r.Status),
		TotalKop: r.TotalKop, FiscalReceiptNumber: intPtr(r.FiscalReceiptNumber), ShiftNumber: intPtr(r.ShiftNumber),
		ReceiptDatetime: r.ReceiptDatetime, FnNumber: r.FnNumber, FiscalDocumentNumber: r.FiscalDocumentNumber,
		FiscalDocumentAttribute: r.FiscalDocumentAttribute, OfdReceiptUrl: r.OfdReceiptUrl, NpdReceiptUrl: r.NpdReceiptUrl,
		ErrorCode: r.ErrorCode, ErrorText: r.ErrorText, Attempts: int(r.Attempts), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.ContractID != nil && r.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *r.ContractID, Number: *r.ContractNumber}
	}
	return d
}

// one — чек по id или по платежу (nil — нет).
func one(ctx context.Context, q *sqlc.Queries, id, paymentID *uuid.UUID) (*api.FiscalReceiptDTO, error) {
	rows, err := q.ListFiscalReceipts(ctx, sqlc.ListFiscalReceiptsParams{ID: id, PaymentID: paymentID, Lim: 1})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	d := dto(rows[0])
	return &d, nil
}

// PaymentReceipt — чек прихода по платежу для карточки платежа (nil — чека нет).
func PaymentReceipt(ctx context.Context, q *sqlc.Queries, paymentID uuid.UUID) (*api.FiscalReceiptDTO, error) {
	return one(ctx, q, nil, &paymentID)
}

func (h *Handler) get(ctx context.Context, id uuid.UUID) (api.FiscalReceiptDTO, error) {
	d, err := one(ctx, h.svc.db.Queries(), &id, nil)
	if err != nil {
		return api.FiscalReceiptDTO{}, err
	}
	if d == nil {
		return api.FiscalReceiptDTO{}, apperr.NotFound()
	}
	return *d, nil
}

// ListFiscalReceipts — GET /fiscal-receipts.
func (h *Handler) ListFiscalReceipts(ctx context.Context, req api.ListFiscalReceiptsRequestObject) (api.ListFiscalReceiptsResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	params := sqlc.ListFiscalReceiptsParams{DateFrom: apiconv.ToDatePtr(p.From), DateTo: apiconv.ToDatePtr(p.To),
		Lim: paging.Limit(), Off: paging.Offset()}
	if p.Status != nil {
		params.Status = apiconv.Ptr(string(*p.Status))
	}
	q := h.svc.db.Queries()
	rows, err := q.ListFiscalReceipts(ctx, params)
	if err != nil {
		return nil, err
	}
	n, err := q.CountFiscalReceipts(ctx, sqlc.CountFiscalReceiptsParams{Status: params.Status, DateFrom: params.DateFrom, DateTo: params.DateTo})
	if err != nil {
		return nil, err
	}
	items := make([]api.FiscalReceiptDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, dto(r))
	}
	return api.ListFiscalReceipts200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// RetryFiscalReceipt — POST /fiscal-receipts/{id}/retry.
func (h *Handler) RetryFiscalReceipt(ctx context.Context, req api.RetryFiscalReceiptRequestObject) (api.RetryFiscalReceiptResponseObject, error) {
	if err := h.svc.Retry(ctx, req.Id); err != nil {
		return nil, err
	}
	d, err := h.get(ctx, req.Id)
	return api.RetryFiscalReceipt200JSONResponse(d), err
}

// SetManualFiscalReceipt — POST /fiscal-receipts/{id}/manual.
func (h *Handler) SetManualFiscalReceipt(ctx context.Context, req api.SetManualFiscalReceiptRequestObject) (api.SetManualFiscalReceiptResponseObject, error) {
	if err := h.svc.Manual(ctx, req.Id, req.Body.NpdReceiptUrl); err != nil {
		return nil, err
	}
	d, err := h.get(ctx, req.Id)
	return api.SetManualFiscalReceipt200JSONResponse(d), err
}

// AtolCallback — POST /integrations/atol/callback: ответ 200 после сохранения события (08 разд. 3.4).
func (h *Handler) AtolCallback(ctx context.Context, req api.AtolCallbackRequestObject) (api.AtolCallbackResponseObject, error) {
	if req.Body == nil {
		return api.AtolCallback200Response{}, nil
	}
	if err := h.svc.Callback(ctx, map[string]any(*req.Body)); err != nil {
		return nil, err
	}
	return api.AtolCallback200Response{}, nil
}
