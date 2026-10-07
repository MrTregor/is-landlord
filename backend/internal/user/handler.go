package user

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/platform/apiconv"
)

// Handler — методы API пользователей.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListUsers — список пользователей.
func (h *Handler) ListUsers(ctx context.Context, req api.ListUsersRequestObject) (api.ListUsersResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := Filter{Q: apiconv.TrimPtr(p.Q), Limit: paging.Limit(), Offset: paging.Offset()}
	if p.Role != nil {
		f.Role = apiconv.Ptr(string(*p.Role))
	}
	if p.Status != nil {
		f.Status = apiconv.Ptr(string(*p.Status))
	}
	rows, total, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]api.UserDTO, 0, len(rows))
	for _, u := range rows {
		items = append(items, auth.UserDTO(u))
	}
	return api.ListUsers200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// CreateUser — добавить собственника и отправить приглашение.
func (h *Handler) CreateUser(ctx context.Context, req api.CreateUserRequestObject) (api.CreateUserResponseObject, error) {
	u, _, err := h.svc.CreateOwner(ctx, string(req.Body.Email), req.Body.DisplayName)
	if err != nil {
		return nil, err
	}
	return api.CreateUser201JSONResponse(auth.UserDTO(u)), nil
}

// UpdateUser — изменить имя или статус.
func (h *Handler) UpdateUser(ctx context.Context, req api.UpdateUserRequestObject) (api.UpdateUserResponseObject, error) {
	var status *string
	if req.Body.Status != nil {
		status = apiconv.Ptr(string(*req.Body.Status))
	}
	u, err := h.svc.Update(ctx, req.Id, req.Body.DisplayName, status)
	if err != nil {
		return nil, err
	}
	return api.UpdateUser200JSONResponse(auth.UserDTO(u)), nil
}

// ResendInvite — отправить приглашение повторно.
func (h *Handler) ResendInvite(ctx context.Context, req api.ResendInviteRequestObject) (api.ResendInviteResponseObject, error) {
	if err := h.svc.ResendInvite(ctx, req.Id); err != nil {
		return nil, err
	}
	return api.ResendInvite204Response{}, nil
}
