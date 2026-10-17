package maintenance

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/expense"
	"landlord/backend/internal/file"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API заявок собственника (06 разд. 13) и кабинета нанимателя (06 разд. 15).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func listItem(r sqlc.ListRequestsRow) api.MaintenanceRequestListItem {
	d := api.MaintenanceRequestListItem{
		Id: r.ID, Number: r.Number, CreatedAt: r.CreatedAt, Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode},
		Title: r.Title, Category: api.RequestCategory(r.Category), Priority: api.RequestPriority(r.Priority),
		Status: api.RequestStatus(r.Status), Contractor: r.Contractor, PlannedDate: apiconv.FromDatePtr(r.PlannedDate),
		AuthorRole: api.UserRole(r.AuthorRole),
	}
	if r.ContractID != nil && r.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *r.ContractID, Number: *r.ContractNumber}
	}
	return d
}

func photos(ctx context.Context, q *sqlc.Queries, id uuid.UUID) ([]api.FileDTO, error) {
	list, err := q.RequestPhotos(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]api.FileDTO, 0, len(list))
	for _, f := range list {
		out = append(out, file.DTO(f))
	}
	return out, nil
}

func events(ctx context.Context, q *sqlc.Queries, id uuid.UUID) ([]api.RequestEventDTO, error) {
	list, err := q.RequestEvents(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]api.RequestEventDTO, 0, len(list))
	for _, e := range list {
		out = append(out, eventDTO(e))
	}
	return out, nil
}

func eventDTO(e sqlc.RequestEventsRow) api.RequestEventDTO {
	d := api.RequestEventDTO{Id: e.ID, Kind: api.RequestEventKind(e.Kind), Text: e.Text, AuthorName: e.AuthorName, CreatedAt: e.CreatedAt}
	if e.FromStatus != nil {
		d.FromStatus = apiconv.Ptr(api.RequestStatus(*e.FromStatus))
	}
	if e.ToStatus != nil {
		d.ToStatus = apiconv.Ptr(api.RequestStatus(*e.ToStatus))
	}
	if e.AuthorRole != nil {
		d.AuthorRole = apiconv.Ptr(api.UserRole(*e.AuthorRole))
	}
	return d
}

// eventOf — событие ленты с автором для ответа.
func (h *Handler) eventOf(ctx context.Context, ev sqlc.MaintenanceRequestEvent) (api.RequestEventDTO, error) {
	list, err := h.svc.db.Queries().RequestEvents(ctx, ev.RequestID)
	if err != nil {
		return api.RequestEventDTO{}, err
	}
	for _, e := range list {
		if e.ID == ev.ID {
			return eventDTO(e), nil
		}
	}
	return api.RequestEventDTO{}, apperr.NotFound()
}

func (h *Handler) row(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (sqlc.ListRequestsRow, error) {
	r, err := q.GetRequestRow(ctx, id)
	if db.IsNoRows(err) {
		return sqlc.ListRequestsRow{}, apperr.NotFound()
	}
	return sqlc.ListRequestsRow(r), err
}

// detail — карточка заявки собственника.
func (h *Handler) detail(ctx context.Context, id uuid.UUID) (api.MaintenanceRequestDetail, error) {
	q := h.svc.db.Queries()
	r, err := h.row(ctx, q, id)
	if err != nil {
		return api.MaintenanceRequestDetail{}, err
	}
	b := listItem(r)
	d := api.MaintenanceRequestDetail{
		Id: b.Id, Number: b.Number, CreatedAt: b.CreatedAt, Property: b.Property, Contract: b.Contract, Title: b.Title,
		Category: b.Category, Priority: b.Priority, Status: b.Status, Contractor: b.Contractor, PlannedDate: b.PlannedDate,
		AuthorRole: b.AuthorRole, Description: r.Description, ResultNote: r.ResultNote, TenantFault: r.TenantFault,
		DoneAt: r.DoneAt, ClosedAt: r.ClosedAt, UpdatedAt: r.UpdatedAt,
	}
	if d.Photos, err = photos(ctx, q, id); err != nil {
		return d, err
	}
	if d.Events, err = events(ctx, q, id); err != nil {
		return d, err
	}
	d.Expenses, err = expense.OfRequest(ctx, q, id)
	return d, err
}

// ListMaintenanceRequests — GET /maintenance-requests.
func (h *Handler) ListMaintenanceRequests(ctx context.Context, req api.ListMaintenanceRequestsRequestObject) (
	api.ListMaintenanceRequestsResponseObject, error,
) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	params := sqlc.ListRequestsParams{Statuses: []string{}, PropertyID: p.PropertyId, Q: apiconv.TrimPtr(p.Q),
		Lim: paging.Limit(), Off: paging.Offset()}
	if p.Status != nil {
		for _, s := range *p.Status {
			params.Statuses = append(params.Statuses, string(s))
		}
	}
	if p.Category != nil {
		params.Category = apiconv.Ptr(string(*p.Category))
	}
	if p.Priority != nil {
		params.Priority = apiconv.Ptr(string(*p.Priority))
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	q := h.svc.db.Queries()
	rows, err := q.ListRequests(ctx, params)
	if err != nil {
		return nil, err
	}
	n, err := q.CountRequests(ctx, sqlc.CountRequestsParams{Statuses: params.Statuses, PropertyID: params.PropertyID,
		Category: params.Category, Priority: params.Priority, Q: params.Q})
	if err != nil {
		return nil, err
	}
	items := make([]api.MaintenanceRequestListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, listItem(r))
	}
	return api.ListMaintenanceRequests200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// CreateMaintenanceRequest — POST /maintenance-requests.
func (h *Handler) CreateMaintenanceRequest(ctx context.Context, req api.CreateMaintenanceRequestRequestObject) (
	api.CreateMaintenanceRequestResponseObject, error,
) {
	b := req.Body
	id, err := h.svc.Create(ctx, CreateInput{PropertyID: b.PropertyId, ContractID: b.ContractId, Category: string(b.Category),
		Title: b.Title, Description: b.Description, Priority: string(b.Priority), PhotoIDs: b.PhotoFileIds})
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, id)
	return api.CreateMaintenanceRequest201JSONResponse(d), err
}

// GetMaintenanceRequest — GET /maintenance-requests/{id}.
func (h *Handler) GetMaintenanceRequest(ctx context.Context, req api.GetMaintenanceRequestRequestObject) (
	api.GetMaintenanceRequestResponseObject, error,
) {
	d, err := h.detail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetMaintenanceRequest200JSONResponse(d), nil
}

// UpdateMaintenanceRequest — PATCH /maintenance-requests/{id}.
func (h *Handler) UpdateMaintenanceRequest(ctx context.Context, req api.UpdateMaintenanceRequestRequestObject) (
	api.UpdateMaintenanceRequestResponseObject, error,
) {
	b := req.Body
	in := PatchInput{Contractor: b.Contractor, PlannedDate: apiconv.ToDatePtr(b.PlannedDate), TenantFault: b.TenantFault}
	if b.Priority != nil {
		in.Priority = apiconv.Ptr(string(*b.Priority))
	}
	if err := h.svc.Update(ctx, req.Id, in); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.UpdateMaintenanceRequest200JSONResponse(d), err
}

// TransitionMaintenanceRequest — POST /maintenance-requests/{id}/transition.
func (h *Handler) TransitionMaintenanceRequest(ctx context.Context, req api.TransitionMaintenanceRequestRequestObject) (
	api.TransitionMaintenanceRequestResponseObject, error,
) {
	b := req.Body
	if err := h.svc.Transition(ctx, req.Id, TransitionInput{To: string(b.ToStatus), Comment: b.Comment, ResultNote: b.ResultNote}); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.TransitionMaintenanceRequest200JSONResponse(d), err
}

// CommentMaintenanceRequest — POST /maintenance-requests/{id}/comments.
func (h *Handler) CommentMaintenanceRequest(ctx context.Context, req api.CommentMaintenanceRequestRequestObject) (
	api.CommentMaintenanceRequestResponseObject, error,
) {
	ev, err := h.svc.Comment(ctx, req.Id, req.Body.Text, false)
	if err != nil {
		return nil, err
	}
	d, err := h.eventOf(ctx, ev)
	return api.CommentMaintenanceRequest201JSONResponse(d), err
}

// ---------------------------------------------------------------- Кабинет нанимателя

// cabinetDetail — заявка своего договора без внутренних полей (исполнитель); чужая — 404.
func (h *Handler) cabinetDetail(ctx context.Context, id uuid.UUID) (api.CabinetRequestDetail, error) {
	q := h.svc.db.Queries()
	r, err := h.row(ctx, q, id)
	if err != nil {
		return api.CabinetRequestDetail{}, err
	}
	if err := tenantOwns(ctx, q, sqlc.MaintenanceRequest{ContractID: r.ContractID}); err != nil {
		return api.CabinetRequestDetail{}, err
	}
	d := api.CabinetRequestDetail{Id: r.ID, Number: r.Number, Title: r.Title, Category: api.RequestCategory(r.Category),
		Status: api.RequestStatus(r.Status), CreatedAt: r.CreatedAt, Description: r.Description,
		PlannedDate: apiconv.FromDatePtr(r.PlannedDate)}
	if d.Photos, err = photos(ctx, q, id); err != nil {
		return d, err
	}
	d.Events, err = events(ctx, q, id)
	return d, err
}

// ListCabinetRequests — GET /cabinet/requests?contractId=: заявки своего договора.
func (h *Handler) ListCabinetRequests(ctx context.Context, req api.ListCabinetRequestsRequestObject) (
	api.ListCabinetRequestsResponseObject, error,
) {
	q := h.svc.db.Queries()
	c, err := payment.TenantContract(ctx, q, req.Params.ContractId)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListRequests(ctx, sqlc.ListRequestsParams{Statuses: []string{}, ContractID: &c.ID, Lim: 1000})
	if err != nil {
		return nil, err
	}
	out := make(api.ListCabinetRequests200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.CabinetRequestListItem{Id: r.ID, Number: r.Number, Title: r.Title,
			Category: api.RequestCategory(r.Category), Status: api.RequestStatus(r.Status), CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// CreateCabinetRequest — POST /cabinet/requests.
func (h *Handler) CreateCabinetRequest(ctx context.Context, req api.CreateCabinetRequestRequestObject) (
	api.CreateCabinetRequestResponseObject, error,
) {
	b := req.Body
	id, err := h.svc.CreateByTenant(ctx, CreateInput{ContractID: &b.ContractId, Category: string(b.Category), Title: b.Title,
		Description: b.Description, PhotoIDs: b.PhotoFileIds})
	if err != nil {
		return nil, err
	}
	d, err := h.cabinetDetail(ctx, id)
	return api.CreateCabinetRequest201JSONResponse(d), err
}

// GetCabinetRequest — GET /cabinet/requests/{id}.
func (h *Handler) GetCabinetRequest(ctx context.Context, req api.GetCabinetRequestRequestObject) (api.GetCabinetRequestResponseObject, error) {
	d, err := h.cabinetDetail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetCabinetRequest200JSONResponse(d), nil
}

// CommentCabinetRequest — POST /cabinet/requests/{id}/comments.
func (h *Handler) CommentCabinetRequest(ctx context.Context, req api.CommentCabinetRequestRequestObject) (
	api.CommentCabinetRequestResponseObject, error,
) {
	ev, err := h.svc.Comment(ctx, req.Id, req.Body.Text, true)
	if err != nil {
		return nil, err
	}
	d, err := h.eventOf(ctx, ev)
	return api.CommentCabinetRequest201JSONResponse(d), err
}

// ConfirmCabinetRequest — POST /cabinet/requests/{id}/confirm: «Подтверждаю выполнение» (done → closed).
func (h *Handler) ConfirmCabinetRequest(ctx context.Context, req api.ConfirmCabinetRequestRequestObject) (
	api.ConfirmCabinetRequestResponseObject, error,
) {
	if err := h.svc.TransitionByTenant(ctx, req.Id, TransitionInput{To: StatusClosed}); err != nil {
		return nil, err
	}
	d, err := h.cabinetDetail(ctx, req.Id)
	return api.ConfirmCabinetRequest200JSONResponse(d), err
}

// ReopenCabinetRequest — POST /cabinet/requests/{id}/reopen: «Не устранено» (done → in_progress).
func (h *Handler) ReopenCabinetRequest(ctx context.Context, req api.ReopenCabinetRequestRequestObject) (
	api.ReopenCabinetRequestResponseObject, error,
) {
	if err := h.svc.TransitionByTenant(ctx, req.Id, TransitionInput{To: StatusInProgress, Comment: &req.Body.Comment}); err != nil {
		return nil, err
	}
	d, err := h.cabinetDetail(ctx, req.Id)
	return api.ReopenCabinetRequest200JSONResponse(d), err
}
