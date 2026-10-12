package billing

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
)

// Handler — методы API начислений.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// row — строка начисления с договором, объектом и нанимателем (общая форма ListChargesRow и GetChargeRowRow).
type row = sqlc.ListChargesRow

func toDTO(r row, today date.Date) api.ChargeDTO {
	details := map[string]any{}
	_ = json.Unmarshal(r.CalcDetails, &details)
	outstanding := r.AmountKop - r.PaidKop
	if r.Status == "cancelled" {
		outstanding = 0
	}
	d := api.ChargeDTO{
		Id: r.ID, Contract: api.ContractRef{Id: r.ContractID, Number: r.ContractNumber},
		Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode},
		Tenant:   api.TenantRef{Id: r.TenantID, DisplayName: names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName)},
		Kind:     api.ChargeKind(r.Kind), Title: r.Title, PeriodMonth: apiconv.FromDatePtr(r.PeriodMonth),
		PeriodStart: apiconv.FromDatePtr(r.PeriodStart), PeriodEnd: apiconv.FromDatePtr(r.PeriodEnd), DueDate: apiconv.FromDatePtr(r.DueDate),
		AmountKop: r.AmountKop, OriginalAmountKop: r.OriginalAmountKop, PaidKop: r.PaidKop, OutstandingKop: outstanding,
		Status: api.ChargeStatus(r.Status), SourceChargeId: r.SourceChargeID, Origin: api.ChargeOrigin(r.Origin),
		CalcDetails: details, CreatedAt: r.CreatedAt,
	}
	if r.PenaltyState != nil {
		d.PenaltyState = apiconv.Ptr(api.PenaltyState(*r.PenaltyState))
	}
	if r.DueDate != nil && outstanding > 0 && r.DueDate.Before(today) && (r.Status == "open" || r.Status == "partially_paid") {
		d.IsOverdue = true
		d.OverdueDays = today.Sub(*r.DueDate)
	}
	return d
}

// DTO — начисление для ответа API.
func (s *Service) DTO(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (api.ChargeDTO, error) {
	r, err := q.GetChargeRow(ctx, id)
	if db.IsNoRows(err) {
		return api.ChargeDTO{}, apperr.NotFound()
	}
	if err != nil {
		return api.ChargeDTO{}, err
	}
	return toDTO(row(r), s.clock.Today()), nil
}

func (h *Handler) detail(ctx context.Context, id uuid.UUID) (api.ChargeDetail, error) {
	q := h.svc.db.Queries()
	base, err := h.svc.DTO(ctx, q, id)
	if err != nil {
		return api.ChargeDetail{}, err
	}
	raw, err := q.GetChargeRow(ctx, id)
	if err != nil {
		return api.ChargeDetail{}, err
	}
	allocs, err := q.ActiveAllocations(ctx, id)
	if err != nil {
		return api.ChargeDetail{}, err
	}
	adjs, err := q.ListAdjustments(ctx, id)
	if err != nil {
		return api.ChargeDetail{}, err
	}
	d := api.ChargeDetail{
		Id: base.Id, Contract: base.Contract, Property: base.Property, Tenant: base.Tenant, Kind: base.Kind, Title: base.Title,
		PeriodMonth: base.PeriodMonth, PeriodStart: base.PeriodStart, PeriodEnd: base.PeriodEnd, DueDate: base.DueDate,
		AmountKop: base.AmountKop, OriginalAmountKop: base.OriginalAmountKop, PaidKop: base.PaidKop, OutstandingKop: base.OutstandingKop,
		Status: base.Status, IsOverdue: base.IsOverdue, OverdueDays: base.OverdueDays, PenaltyState: base.PenaltyState,
		SourceChargeId: base.SourceChargeId, Origin: base.Origin, CalcDetails: base.CalcDetails, CreatedAt: base.CreatedAt,
		CancelReason: raw.CancelReason, Allocations: make([]api.ChargeAllocation, 0, len(allocs)),
		Adjustments: make([]api.ChargeAdjustmentDTO, 0, len(adjs)),
	}
	for _, a := range allocs {
		d.Allocations = append(d.Allocations, api.ChargeAllocation{
			PaymentId: a.PaymentID, ReceiptNumber: a.ReceiptNumber, ReceivedOn: apiconv.FromDatePtr(a.ReceivedOn), AmountKop: a.AmountKop,
		})
	}
	for _, a := range adjs {
		d.Adjustments = append(d.Adjustments, api.ChargeAdjustmentDTO{
			Id: a.ID, Kind: api.AdjustmentKind(a.Kind), DeltaKop: a.DeltaKop, Reason: a.Reason, CreatedAt: a.CreatedAt, CreatedBy: a.CreatedByName,
		})
	}
	if base.Kind != KindPenalty {
		pen, err := q.PenaltyOf(ctx, &id)
		switch {
		case err == nil:
			p, err := h.svc.DTO(ctx, q, pen.ID)
			if err != nil {
				return d, err
			}
			d.Penalty = &p
		case !db.IsNoRows(err):
			return d, err
		}
	}
	return d, nil
}

// OpenChargeDTOs — открытые начисления договора в порядке погашения (05 разд. 10.3), без начисляемых пеней
// (взаиморасчёт, диалог зачёта залога).
func (s *Service) OpenChargeDTOs(ctx context.Context, contractID uuid.UUID) ([]api.ChargeDTO, error) {
	today := s.clock.Today()
	rows, err := s.db.Queries().ListCharges(ctx, sqlc.ListChargesParams{
		ContractID: &contractID, Kinds: []string{}, Statuses: []string{"open", "partially_paid"}, Today: today, Lim: 1000,
	})
	if err != nil {
		return nil, err
	}
	list := make([]calc.OpenCharge, 0, len(rows))
	byID := make(map[uuid.UUID]row, len(rows))
	for i, r := range rows {
		oc := calc.OpenCharge{ID: r.ID, Kind: r.Kind, DueDate: r.DueDate, Seq: i, Outstanding: money.Kop(r.AmountKop - r.PaidKop)}
		if r.PenaltyState != nil {
			oc.PenaltyState = *r.PenaltyState
		}
		list = append(list, oc)
		byID[r.ID] = r
	}
	out := []api.ChargeDTO{}
	for _, oc := range calc.AllocationOrder(list, nil) {
		out = append(out, toDTO(byID[oc.ID], today))
	}
	return out, nil
}

// ListCharges — GET /charges.
func (h *Handler) ListCharges(ctx context.Context, req api.ListChargesRequestObject) (api.ListChargesResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	today := h.svc.clock.Today()
	params := sqlc.ListChargesParams{
		ContractID: p.ContractId, PropertyID: p.PropertyId, TenantID: p.TenantId, Kinds: []string{}, Statuses: []string{},
		Overdue: p.Overdue != nil && *p.Overdue, Today: today, PeriodFrom: apiconv.ToDatePtr(p.PeriodFrom),
		PeriodTo: apiconv.ToDatePtr(p.PeriodTo), DueFrom: apiconv.ToDatePtr(p.DueFrom), DueTo: apiconv.ToDatePtr(p.DueTo),
		Q: apiconv.TrimPtr(p.Q), Lim: paging.Limit(), Off: paging.Offset(),
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
	q := h.svc.db.Queries()
	rows, err := q.ListCharges(ctx, params)
	if err != nil {
		return nil, err
	}
	sum, err := q.CountCharges(ctx, sqlc.CountChargesParams{
		ContractID: params.ContractID, PropertyID: params.PropertyID, TenantID: params.TenantID, Kinds: params.Kinds,
		Statuses: params.Statuses, Overdue: params.Overdue, Today: today, PeriodFrom: params.PeriodFrom, PeriodTo: params.PeriodTo,
		DueFrom: params.DueFrom, DueTo: params.DueTo, Q: params.Q,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.ChargeDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDTO(r, today))
	}
	return api.ListCharges200JSONResponse{
		Items: items, Total: int(sum.Total), Page: paging.Page, PageSize: paging.PageSize,
		Summary: api.ChargeSummary{AmountKop: sum.AmountKop, PaidKop: sum.PaidKop, OutstandingKop: sum.AmountKop - sum.PaidKop},
	}, nil
}

// CreateCharge — POST /charges (ручное начисление).
func (h *Handler) CreateCharge(ctx context.Context, req api.CreateChargeRequestObject) (api.CreateChargeResponseObject, error) {
	b := req.Body
	id, err := h.svc.CreateManual(ctx, ManualInput{
		ContractID: b.ContractId, Kind: string(b.Kind), Title: b.Title, AmountKop: b.AmountKop, DueDate: apiconv.ToDate(b.DueDate), Reason: b.Reason,
	})
	if err != nil {
		return nil, err
	}
	d, err := h.svc.DTO(ctx, h.svc.db.Queries(), id)
	return api.CreateCharge201JSONResponse(d), err
}

// GetCharge — GET /charges/{id}.
func (h *Handler) GetCharge(ctx context.Context, req api.GetChargeRequestObject) (api.GetChargeResponseObject, error) {
	d, err := h.detail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetCharge200JSONResponse(d), nil
}

// AdjustCharge — POST /charges/{id}/adjust.
func (h *Handler) AdjustCharge(ctx context.Context, req api.AdjustChargeRequestObject) (api.AdjustChargeResponseObject, error) {
	if err := h.svc.Adjust(ctx, req.Id, req.Body.DeltaKop, req.Body.Reason); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.AdjustCharge200JSONResponse(d), err
}

// CancelCharge — POST /charges/{id}/cancel.
func (h *Handler) CancelCharge(ctx context.Context, req api.CancelChargeRequestObject) (api.CancelChargeResponseObject, error) {
	if err := h.svc.Cancel(ctx, req.Id, req.Body.Reason); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.CancelCharge200JSONResponse(d), err
}

// SetPenalty — POST /charges/{id}/penalty-set.
func (h *Handler) SetPenalty(ctx context.Context, req api.SetPenaltyRequestObject) (api.SetPenaltyResponseObject, error) {
	if err := h.svc.SetPenalty(ctx, req.Id, req.Body.AmountKop, req.Body.Reason, false); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.SetPenalty200JSONResponse(d), err
}

// WriteOffPenalty — POST /charges/{id}/penalty-write-off.
func (h *Handler) WriteOffPenalty(ctx context.Context, req api.WriteOffPenaltyRequestObject) (api.WriteOffPenaltyResponseObject, error) {
	if err := h.svc.SetPenalty(ctx, req.Id, 0, req.Body.Reason, true); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.WriteOffPenalty200JSONResponse(d), err
}

// CreateUtilitiesCharge — POST /contracts/{id}/utilities-charge: «Начисление ЖКУ за месяц» (05 разд. 6.3).
func (h *Handler) CreateUtilitiesCharge(ctx context.Context, req api.CreateUtilitiesChargeRequestObject) (api.CreateUtilitiesChargeResponseObject, error) {
	res, err := h.svc.CreateUtilities(ctx, req.Id, apiconv.ToDate(req.Body.PeriodMonth))
	if err != nil {
		return nil, err
	}
	d, err := h.svc.DTO(ctx, h.svc.db.Queries(), res.ChargeID)
	if err != nil {
		return nil, err
	}
	if len(res.Warnings) > 0 {
		d.CalcDetails["warnings"] = res.Warnings
	}
	return api.CreateUtilitiesCharge201JSONResponse(d), nil
}
