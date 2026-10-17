package expense

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API затрат (06 разд. 13).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// DTO — затрата для API.
func DTO(r sqlc.ListExpensesRow) api.ExpenseDTO {
	d := api.ExpenseDTO{
		Id: r.ID, Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode}, Category: api.ExpenseCategory(r.Category),
		AmountKop: r.AmountKop, PaidOn: apiconv.FromDate(r.PaidOn), PeriodMonth: apiconv.FromDate(r.PeriodMonth),
		Description: r.Description, Counterparty: r.Counterparty, Recovery: api.ExpenseRecovery(r.Recovery),
		DepositTransactionId: r.DepositTransactionID, ChargeId: r.ChargeID, CreatedAt: r.CreatedAt,
	}
	if r.FileID != nil && r.FileName != nil {
		f := api.FileDTO{Id: *r.FileID, Name: *r.FileName, Url: file.URL(*r.FileID)}
		if r.FileContentType != nil {
			f.ContentType = *r.FileContentType
		}
		if r.FileSize != nil {
			f.SizeBytes = *r.FileSize
		}
		d.File = &f
	}
	if r.MaintenanceRequestID != nil && r.RequestNumber != nil {
		d.MaintenanceRequest = &api.RequestRef{Id: *r.MaintenanceRequestID, Number: *r.RequestNumber}
	}
	return d
}

// OfRequest — затраты по заявке (карточка заявки).
func OfRequest(ctx context.Context, q *sqlc.Queries, requestID uuid.UUID) ([]api.ExpenseDTO, error) {
	rows, err := q.ListExpenses(ctx, sqlc.ListExpensesParams{RequestID: &requestID, Lim: 1000})
	if err != nil {
		return nil, err
	}
	out := make([]api.ExpenseDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, DTO(r))
	}
	return out, nil
}

func (h *Handler) one(ctx context.Context, id uuid.UUID) (api.ExpenseDTO, error) {
	r, err := h.svc.db.Queries().GetExpenseRow(ctx, id)
	if db.IsNoRows(err) {
		return api.ExpenseDTO{}, apperr.NotFound()
	}
	if err != nil {
		return api.ExpenseDTO{}, err
	}
	return DTO(sqlc.ListExpensesRow(r)), nil
}

// ListExpenses — GET /expenses (с итогом по сумме).
func (h *Handler) ListExpenses(ctx context.Context, req api.ListExpensesRequestObject) (api.ListExpensesResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	params := sqlc.ListExpensesParams{
		PropertyID: p.PropertyId, DateFrom: apiconv.ToDatePtr(p.From), DateTo: apiconv.ToDatePtr(p.To),
		RequestID: p.MaintenanceRequestId, Lim: paging.Limit(), Off: paging.Offset(),
	}
	if p.Category != nil {
		params.Category = apiconv.Ptr(string(*p.Category))
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	q := h.svc.db.Queries()
	rows, err := q.ListExpenses(ctx, params)
	if err != nil {
		return nil, err
	}
	tot, err := q.CountExpenses(ctx, sqlc.CountExpensesParams{
		PropertyID: params.PropertyID, Category: params.Category, DateFrom: params.DateFrom, DateTo: params.DateTo, RequestID: params.RequestID,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.ExpenseDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, DTO(r))
	}
	return api.ListExpenses200JSONResponse{Items: items, Total: int(tot.Total), Page: paging.Page, PageSize: paging.PageSize,
		Summary: api.ExpenseSummary{AmountKop: tot.AmountKop}}, nil
}

func input(b api.ExpenseUpdateInput) Input {
	return Input{
		PropertyID: b.PropertyId, Category: string(b.Category), AmountKop: b.AmountKop, PaidOn: apiconv.ToDate(b.PaidOn),
		PeriodMonth: apiconv.ToDate(b.PeriodMonth), Description: b.Description, Counterparty: b.Counterparty, FileID: b.FileId,
		MaintenanceRequestID: b.MaintenanceRequestId,
	}
}

// CreateExpense — POST /expenses.
func (h *Handler) CreateExpense(ctx context.Context, req api.CreateExpenseRequestObject) (api.CreateExpenseResponseObject, error) {
	b := req.Body
	in := CreateInput{
		Input: input(api.ExpenseUpdateInput{
			PropertyId: b.PropertyId, Category: b.Category, AmountKop: b.AmountKop, PaidOn: b.PaidOn, PeriodMonth: b.PeriodMonth,
			Description: b.Description, Counterparty: b.Counterparty, FileId: b.FileId, MaintenanceRequestId: b.MaintenanceRequestId,
		}),
		Recovery: string(b.Recovery), ContractID: b.ContractId, ChargeDueDate: apiconv.ToDatePtr(b.ChargeDueDate),
	}
	id, err := h.svc.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	d, err := h.one(ctx, id)
	return api.CreateExpense201JSONResponse(d), err
}

// UpdateExpense — PUT /expenses/{id}.
func (h *Handler) UpdateExpense(ctx context.Context, req api.UpdateExpenseRequestObject) (api.UpdateExpenseResponseObject, error) {
	if err := h.svc.Update(ctx, req.Id, input(*req.Body)); err != nil {
		return nil, err
	}
	d, err := h.one(ctx, req.Id)
	return api.UpdateExpense200JSONResponse(d), err
}

// DeleteExpense — DELETE /expenses/{id}.
func (h *Handler) DeleteExpense(ctx context.Context, req api.DeleteExpenseRequestObject) (api.DeleteExpenseResponseObject, error) {
	if err := h.svc.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return api.DeleteExpense204Response{}, nil
}
