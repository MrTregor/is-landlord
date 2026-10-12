package payment

import (
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
)

// Handler — методы API платежей, залога и расхождений (06 разд. 11, 12).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func paymentDTO(r sqlc.ListPaymentsRow) api.PaymentDTO {
	allocated := r.AllocatedKop
	advance := int64(0)
	if r.Status == "succeeded" && r.Purpose == PurposeCharges {
		advance = r.AmountKop - allocated - r.ReturnedKop
	}
	d := api.PaymentDTO{
		Id: r.ID, Contract: api.ContractRef{Id: r.ContractID, Number: r.ContractNumber},
		Tenant:   api.TenantRef{Id: r.TenantID, DisplayName: names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName)},
		Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode}, Purpose: api.PaymentPurpose(r.Purpose),
		AmountKop: r.AmountKop, AllocatedKop: allocated, AdvanceKop: advance, ReceivedOn: apiconv.FromDatePtr(r.ReceivedOn),
		Method: api.PaymentMethod(r.Method), Source: api.PaymentSource(r.Source), Status: api.PaymentStatus(r.Status),
		ReceiptNumber: r.ReceiptNumber, PayerName: r.PayerName, Comment: r.Comment, CreatedAt: r.CreatedAt,
	}
	if r.FiscalStatus != nil {
		d.FiscalStatus = apiconv.Ptr(api.FiscalStatus(*r.FiscalStatus))
	}
	return d
}

// ListPayments — GET /payments.
func (h *Handler) ListPayments(ctx context.Context, req api.ListPaymentsRequestObject) (api.ListPaymentsResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	params := sqlc.ListPaymentsParams{
		ContractID: p.ContractId, TenantID: p.TenantId, PropertyID: p.PropertyId, DateFrom: apiconv.ToDatePtr(p.From),
		DateTo: apiconv.ToDatePtr(p.To), Q: apiconv.TrimPtr(p.Q), Lim: paging.Limit(), Off: paging.Offset(),
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
	q := h.svc.db.Queries()
	rows, err := q.ListPayments(ctx, params)
	if err != nil {
		return nil, err
	}
	n, err := q.CountPayments(ctx, sqlc.CountPaymentsParams{
		ContractID: params.ContractID, TenantID: params.TenantID, PropertyID: params.PropertyID, Status: params.Status,
		Method: params.Method, Source: params.Source, DateFrom: params.DateFrom, DateTo: params.DateTo, Q: params.Q,
	})
	if err != nil {
		return nil, err
	}
	items := make([]api.PaymentDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, paymentDTO(r))
	}
	return api.ListPayments200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// detail — карточка платежа.
func (h *Handler) detail(ctx context.Context, id uuid.UUID) (api.PaymentDetail, error) {
	q := h.svc.db.Queries()
	p, err := q.GetPayment(ctx, id)
	if db.IsNoRows(err) {
		return api.PaymentDetail{}, apperr.NotFound()
	}
	if err != nil {
		return api.PaymentDetail{}, err
	}
	row, err := q.GetPaymentRow(ctx, id)
	if err != nil {
		return api.PaymentDetail{}, err
	}
	base := paymentDTO(sqlc.ListPaymentsRow(row))
	d := api.PaymentDetail{
		Id: base.Id, Contract: base.Contract, Tenant: base.Tenant, Property: base.Property, Purpose: base.Purpose,
		AmountKop: base.AmountKop, AllocatedKop: base.AllocatedKop, AdvanceKop: base.AdvanceKop, ReceivedOn: base.ReceivedOn,
		Method: base.Method, Source: base.Source, Status: base.Status, ReceiptNumber: base.ReceiptNumber,
		FiscalStatus: base.FiscalStatus, PayerName: base.PayerName, Comment: base.Comment, CreatedAt: base.CreatedAt,
		Allocations: []api.PaymentAllocationDTO{}, Refunds: []api.RefundDTO{},
		ProviderStatus: p.ProviderStatus, PaymentUrlExpiresAt: p.PaymentUrlExpiresAt, PayerInn: p.PayerInn,
		PayerAccount: p.PayerAccount, BankPurpose: p.BankPurpose, CancelledAt: p.CancelledAt, CancelReason: p.CancelReason,
	}
	allocs, err := q.PaymentAllocations(ctx, id)
	if err != nil {
		return d, err
	}
	for _, a := range allocs {
		d.Allocations = append(d.Allocations, api.PaymentAllocationDTO{
			Id: a.ID, ChargeId: a.ChargeID, ChargeTitle: a.ChargeTitle, ChargeKind: api.ChargeKind(a.ChargeKind), AmountKop: a.AmountKop,
			AllocatedAt: a.AllocatedAt, ReversedAt: a.ReversedAt, ReversedReason: a.ReversedReason,
		})
	}
	refunds, err := q.PaymentRefunds(ctx, id)
	if err != nil {
		return d, err
	}
	for _, r := range refunds {
		d.Refunds = append(d.Refunds, refundDTO(r))
	}
	doc, err := q.PaymentReceiptDocument(ctx, &id)
	switch {
	case err == nil:
		d.ReceiptDocumentId = &doc.ID
	case !db.IsNoRows(err):
		return d, err
	}
	// Чек (fiscalReceipt) — этап M8, строка выписки (bankLine) — этап M9.
	return d, nil
}

func refundDTO(r sqlc.Refund) api.RefundDTO {
	return api.RefundDTO{Id: r.ID, PaymentId: r.PaymentID, AmountKop: r.AmountKop, RefundedOn: apiconv.FromDate(r.RefundedOn),
		Method: api.RefundMethod(r.Method), Reason: r.Reason, CreatedAt: r.CreatedAt}
}

// CreatePayment — POST /payments.
func (h *Handler) CreatePayment(ctx context.Context, req api.CreatePaymentRequestObject) (api.CreatePaymentResponseObject, error) {
	b := req.Body
	id, err := h.svc.Create(ctx, CreateInput{
		ContractID: b.ContractId, Purpose: string(b.Purpose), AmountKop: b.AmountKop, ReceivedOn: apiconv.ToDate(b.ReceivedOn),
		Method: string(b.Method), ChargeIDs: b.ChargeIds, Comment: b.Comment,
	})
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, id)
	return api.CreatePayment201JSONResponse(d), err
}

// GetPayment — GET /payments/{id}.
func (h *Handler) GetPayment(ctx context.Context, req api.GetPaymentRequestObject) (api.GetPaymentResponseObject, error) {
	d, err := h.detail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetPayment200JSONResponse(d), nil
}

// CancelPayment — POST /payments/{id}/cancel.
func (h *Handler) CancelPayment(ctx context.Context, req api.CancelPaymentRequestObject) (api.CancelPaymentResponseObject, error) {
	if err := h.svc.Cancel(ctx, req.Id, req.Body.Reason); err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, req.Id)
	return api.CancelPayment200JSONResponse(d), err
}

// CreateRefund — POST /payments/{id}/refunds.
func (h *Handler) CreateRefund(ctx context.Context, req api.CreateRefundRequestObject) (api.CreateRefundResponseObject, error) {
	b := req.Body
	r, err := h.svc.Refund(ctx, req.Id, RefundInput{AmountKop: b.AmountKop, RefundedOn: apiconv.ToDate(b.RefundedOn),
		Method: string(b.Method), Reason: b.Reason})
	if err != nil {
		return nil, err
	}
	return api.CreateRefund201JSONResponse(refundDTO(r)), nil
}

// MovePaymentAdvanceToDeposit — POST /payments/{id}/move-to-deposit.
func (h *Handler) MovePaymentAdvanceToDeposit(ctx context.Context, req api.MovePaymentAdvanceToDepositRequestObject) (
	api.MovePaymentAdvanceToDepositResponseObject, error,
) {
	op, err := h.svc.MoveToDeposit(ctx, req.Id, req.Body.AmountKop, apiconv.ToDate(req.Body.OpDate))
	if err != nil {
		return nil, err
	}
	d, err := h.operation(ctx, op.ContractID, op.ID)
	return api.MovePaymentAdvanceToDeposit201JSONResponse(d), err
}

// GetPaymentReceiptPdf — GET /payments/{id}/receipt.pdf: квитанция (если задача ещё не выполнилась — формируется сейчас).
func (h *Handler) GetPaymentReceiptPdf(ctx context.Context, req api.GetPaymentReceiptPdfRequestObject) (
	api.GetPaymentReceiptPdfResponseObject, error,
) {
	doc, err := h.svc.db.Queries().PaymentReceiptDocument(ctx, &req.Id)
	if db.IsNoRows(err) {
		doc, err = h.svc.Receipt(ctx, req.Id)
	}
	if err != nil {
		return nil, err
	}
	return h.svc.documents.PDF(ctx, doc, req.Params.Download != nil && *req.Params.Download == 1)
}

// ---------------------------------------------------------------- Залог

func operationDTO(o sqlc.DepositOperationsRow, lastID uuid.UUID) api.DepositOperationDTO {
	return api.DepositOperationDTO{
		Id: o.ID, ContractId: o.ContractID, Kind: api.DepositOperationKind(o.Kind), AmountKop: o.AmountKop,
		OpDate: apiconv.FromDate(o.OpDate), Reason: o.Reason, PaymentId: o.PaymentID, FromAdvance: o.FromAdvance,
		MaintenanceRequestId: o.MaintenanceRequestID, ExpenseId: o.ExpenseID, CancelledAt: o.CancelledAt,
		CancelReason: o.CancelReason, CreatedAt: o.CreatedAt, CreatedBy: o.CreatedByName,
		CanCancel: o.CancelledAt == nil && o.ID == lastID,
	}
}

// operation — операция с залогом для ответа.
func (h *Handler) operation(ctx context.Context, contractID, id uuid.UUID) (api.DepositOperationDTO, error) {
	dep, err := h.svc.Deposit(ctx, contractID)
	if err != nil {
		return api.DepositOperationDTO{}, err
	}
	for _, o := range dep.Operations {
		if o.ID == id {
			return operationDTO(o, dep.LastID), nil
		}
	}
	return api.DepositOperationDTO{}, apperr.NotFound()
}

// GetContractDeposit — GET /contracts/{id}/deposit.
func (h *Handler) GetContractDeposit(ctx context.Context, req api.GetContractDepositRequestObject) (api.GetContractDepositResponseObject, error) {
	dep, err := h.svc.Deposit(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := api.DepositDTO{ExpectedKop: dep.ExpectedKop, ReceivedKop: dep.ReceivedKop, BalanceKop: dep.BalanceKop,
		Operations: make([]api.DepositOperationDTO, 0, len(dep.Operations))}
	for _, o := range dep.Operations {
		out.Operations = append(out.Operations, operationDTO(o, dep.LastID))
	}
	return api.GetContractDeposit200JSONResponse(out), nil
}

// CreateDepositWithholding — POST /contracts/{id}/deposit/withholdings.
func (h *Handler) CreateDepositWithholding(ctx context.Context, req api.CreateDepositWithholdingRequestObject) (
	api.CreateDepositWithholdingResponseObject, error,
) {
	b := req.Body
	op, err := h.svc.Withhold(ctx, req.Id, OpInput{AmountKop: b.AmountKop, OpDate: apiconv.ToDate(b.OpDate), Reason: b.Reason,
		MaintenanceRequestID: b.MaintenanceRequestId, ExpenseID: b.ExpenseId})
	if err != nil {
		return nil, err
	}
	d, err := h.operation(ctx, req.Id, op.ID)
	return api.CreateDepositWithholding201JSONResponse(d), err
}

// CreateDepositOffset — POST /contracts/{id}/deposit/offsets.
func (h *Handler) CreateDepositOffset(ctx context.Context, req api.CreateDepositOffsetRequestObject) (
	api.CreateDepositOffsetResponseObject, error,
) {
	b := req.Body
	op, err := h.svc.Offset(ctx, req.Id, OpInput{AmountKop: b.AmountKop, OpDate: apiconv.ToDate(b.OpDate), Reason: b.Reason,
		ChargeIDs: b.ChargeIds})
	if err != nil {
		return nil, err
	}
	d, err := h.operation(ctx, req.Id, op.ID)
	return api.CreateDepositOffset201JSONResponse(d), err
}

// CreateDepositRefund — POST /contracts/{id}/deposit/refunds.
func (h *Handler) CreateDepositRefund(ctx context.Context, req api.CreateDepositRefundRequestObject) (
	api.CreateDepositRefundResponseObject, error,
) {
	b := req.Body
	op, err := h.svc.RefundDeposit(ctx, req.Id, OpInput{AmountKop: b.AmountKop, OpDate: apiconv.ToDate(b.OpDate), Reason: b.Reason,
		Method: string(b.Method)})
	if err != nil {
		return nil, err
	}
	d, err := h.operation(ctx, req.Id, op.ID)
	return api.CreateDepositRefund201JSONResponse(d), err
}

// CancelDepositOperation — POST /deposit-operations/{id}/cancel.
func (h *Handler) CancelDepositOperation(ctx context.Context, req api.CancelDepositOperationRequestObject) (
	api.CancelDepositOperationResponseObject, error,
) {
	op, err := h.svc.CancelOperation(ctx, req.Id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	d, err := h.operation(ctx, op.ContractID, op.ID)
	return api.CancelDepositOperation200JSONResponse(d), err
}

// GetContractSettlement — GET /contracts/{id}/settlement: долг, остаток залога, аванс (05 разд. 4.9).
func (h *Handler) GetContractSettlement(ctx context.Context, req api.GetContractSettlementRequestObject) (
	api.GetContractSettlementResponseObject, error,
) {
	dep, err := h.svc.Deposit(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	charges, err := h.svc.billing.OpenChargeDTOs(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := api.ContractSettlement{OpenCharges: charges, DepositBalanceKop: dep.BalanceKop, PaymentsWithAdvance: []api.PaymentAdvance{}}
	for _, ch := range charges {
		out.DebtKop += ch.OutstandingKop
	}
	pays, err := h.svc.db.Queries().PaymentsWithAdvance(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	for _, p := range pays {
		if p.AdvanceKop <= 0 || p.ReceivedOn == nil {
			continue
		}
		out.AdvanceKop += p.AdvanceKop
		out.PaymentsWithAdvance = append(out.PaymentsWithAdvance, api.PaymentAdvance{
			PaymentId: p.ID, ReceivedOn: apiconv.FromDate(*p.ReceivedOn), AdvanceKop: p.AdvanceKop,
		})
	}
	return api.GetContractSettlement200JSONResponse(out), nil
}

// ---------------------------------------------------------------- Расхождения

func discrepancyDTO(r sqlc.ListDiscrepanciesRow) api.DiscrepancyDTO {
	d := api.DiscrepancyDTO{
		Id: r.ID, Kind: api.DiscrepancyKind(r.Kind), Status: api.DiscrepancyStatus(r.Status), PaymentId: r.PaymentID,
		ReceiptNumber: r.ReceiptNumber, AmountKop: r.AmountKop, DetectedAt: r.DetectedAt, ResolutionNote: r.ResolutionNote,
		ResolvedAt: r.ResolvedAt, ResolvedBy: r.ResolvedByName,
	}
	if r.ContractID != nil && r.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *r.ContractID, Number: *r.ContractNumber}
	}
	if r.TenantID != nil && r.TenantKind != nil {
		d.Tenant = &api.TenantRef{Id: *r.TenantID, DisplayName: names.Display(*r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName)}
	}
	if r.Resolution != nil {
		d.Resolution = apiconv.Ptr(api.DiscrepancyResolution(*r.Resolution))
	}
	return d
}

// ListDiscrepancies — GET /discrepancies (по умолчанию — открытые).
func (h *Handler) ListDiscrepancies(ctx context.Context, req api.ListDiscrepanciesRequestObject) (api.ListDiscrepanciesResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := DiscrepancyFilter{Status: apiconv.Ptr("open"), ContractID: p.ContractId, Limit: paging.Limit(), Offset: paging.Offset()}
	if p.Status != nil {
		f.Status = apiconv.Ptr(string(*p.Status))
	}
	if p.Kind != nil {
		f.Kind = apiconv.Ptr(string(*p.Kind))
	}
	rows, n, err := h.svc.Discrepancies(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]api.DiscrepancyDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, discrepancyDTO(r))
	}
	return api.ListDiscrepancies200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// ResolveDiscrepancy — POST /discrepancies/{id}/resolve.
func (h *Handler) ResolveDiscrepancy(ctx context.Context, req api.ResolveDiscrepancyRequestObject) (api.ResolveDiscrepancyResponseObject, error) {
	b := req.Body
	in := ResolveInput{Action: string(b.Action), ContractID: b.ContractId, AmountKop: b.AmountKop, Note: b.Note}
	if b.RefundedOn != nil {
		in.RefundedOn = apiconv.Ptr(date.FromTime(b.RefundedOn.Time))
	}
	if b.Method != nil {
		in.Method = apiconv.Ptr(string(*b.Method))
	}
	id, err := h.svc.Resolve(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	r, err := h.svc.db.Queries().GetDiscrepancyRow(ctx, id)
	if err != nil {
		return nil, err
	}
	return api.ResolveDiscrepancy200JSONResponse(discrepancyDTO(sqlc.ListDiscrepanciesRow(r))), nil
}
