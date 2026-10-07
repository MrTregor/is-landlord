package audit

import (
	"context"
	"encoding/json"
	"time"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API журнала действий (FR-12.2).
type Handler struct {
	svc   *Service
	clock clock.Clock
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service, clk clock.Clock) *Handler { return &Handler{svc: svc, clock: clk} }

// ListAudit — записи журнала с фильтрами, новые сверху.
func (h *Handler) ListAudit(ctx context.Context, req api.ListAuditRequestObject) (api.ListAuditResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := Filter{
		ActorUserID: p.ActorUserId,
		EntityType:  apiconv.TrimPtr(p.EntityType),
		EntityID:    apiconv.TrimPtr(p.EntityId),
		Action:      apiconv.TrimPtr(p.Action),
		Q:           apiconv.TrimPtr(p.Q),
		Limit:       paging.Limit(),
		Offset:      paging.Offset(),
	}
	loc := h.clock.Location()
	if p.From != nil {
		f.From = apiconv.Ptr(apiconv.DayStart(apiconv.ToDate(*p.From), loc))
	}
	if p.To != nil {
		f.To = apiconv.Ptr(apiconv.DayStart(apiconv.ToDate(*p.To).AddDays(1), loc))
	}
	rows, total, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]api.AuditEntryDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, ToDTO(r))
	}
	return api.ListAudit200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// VerifyAudit — проверка цепочки хешей.
func (h *Handler) VerifyAudit(ctx context.Context, _ api.VerifyAuditRequestObject) (api.VerifyAuditResponseObject, error) {
	res, err := h.svc.Verify(ctx)
	if err != nil {
		return nil, err
	}
	return api.VerifyAudit200JSONResponse{Ok: res.OK, Checked: res.Checked, BrokenAtId: res.BrokenAtID}, nil
}

// ToDTO — запись журнала для API.
func ToDTO(r sqlc.ListAuditRow) api.AuditEntryDTO {
	dto := api.AuditEntryDTO{
		Id:          r.ID,
		OccurredAt:  r.OccurredAt.UTC().Truncate(time.Microsecond),
		ActorUserId: r.ActorUserID,
		ActorName:   r.ActorName,
		ActorRole:   api.AuditActorRole(r.ActorRole),
		Action:      r.Action,
		ActionTitle: ActionTitle(r.Action),
		EntityType:  r.EntityType,
		EntityId:    r.EntityID,
		Summary:     r.Summary,
		Before:      jsonObject(r.Before),
		After:       jsonObject(r.After),
	}
	if r.Ip != nil {
		dto.Ip = apiconv.Ptr(r.Ip.String())
	}
	return dto
}

func jsonObject(raw []byte) *map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return &m
}
