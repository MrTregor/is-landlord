// Package cabinet — личный кабинет нанимателя (FR-13, docs/spec/06 API.md, раздел 15): сводка «К оплате», начисления,
// платежи, онлайн-оплата и квитанции. Наниматель видит только свои договоры; чужой ресурс — 404.
package cabinet

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/document"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
)

// Handler — методы кабинета.
type Handler struct {
	db        *db.DB
	clock     clock.Clock
	payments  *payment.Service
	documents *document.Service
}

// NewHandler создаёт обработчик.
func NewHandler(database *db.DB, clk clock.Clock, ps *payment.Service, docs *document.Service) *Handler {
	return &Handler{db: database, clock: clk, payments: ps, documents: docs}
}

func tenantID(ctx context.Context) (uuid.UUID, error) {
	a := reqctx.ActorFrom(ctx)
	if a == nil || a.TenantID == nil {
		return uuid.Nil, apperr.NotFound()
	}
	return *a.TenantID, nil
}

// contract — договор нанимателя по id (чужой — 404).
func (h *Handler) contract(ctx context.Context, id uuid.UUID) (sqlc.Contract, error) {
	return payment.TenantContract(ctx, h.db.Queries(), id)
}

// GetCabinetSummary — GET /cabinet/summary (15 разд. 2): по умолчанию — действующий договор, иначе последний.
func (h *Handler) GetCabinetSummary(ctx context.Context, req api.GetCabinetSummaryRequestObject) (api.GetCabinetSummaryResponseObject, error) {
	tid, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	q := h.db.Queries()
	rows, err := q.TenantContracts(ctx, tid)
	if err != nil {
		return nil, err
	}
	out := api.CabinetSummary{Contracts: make([]api.CabinetContractItem, 0, len(rows)), Lines: []api.CabinetChargeLine{}}
	for _, r := range rows {
		out.Contracts = append(out.Contracts, api.CabinetContractItem{
			Id: r.ID, Number: r.Number, LegalBasis: api.LegalBasis(r.LegalBasis), Status: api.ContractStatus(r.Status),
			PropertyCode: r.PropertyCode, PropertyAddress: r.PropertyAddress, StartDate: apiconv.FromDate(r.StartDate),
			EndDate: apiconv.FromDate(r.EndDate),
		})
	}
	i := 0
	if req.Params.ContractId != nil {
		if i = slices.IndexFunc(rows, func(r sqlc.TenantContractsRow) bool { return r.ID == *req.Params.ContractId }); i < 0 {
			return nil, apperr.NotFound()
		}
	}
	if len(rows) == 0 {
		return api.GetCabinetSummary200JSONResponse(out), nil
	}
	cur := rows[i]
	out.ContractId = &cur.ID
	today := h.clock.Today()
	charges, err := q.ListCharges(ctx, sqlc.ListChargesParams{
		ContractID: &cur.ID, Kinds: []string{}, Statuses: []string{"open", "partially_paid"}, Today: today, Lim: 1000,
	})
	if err != nil {
		return nil, err
	}
	var next *date.Date
	for _, ch := range charges {
		rest := ch.AmountKop - ch.PaidKop
		if ch.PenaltyState != nil && *ch.PenaltyState == "accruing" {
			out.PenaltiesAccruingKop += ch.AmountKop
			continue
		}
		if rest <= 0 {
			continue
		}
		overdue := ch.DueDate != nil && ch.DueDate.Before(today)
		out.Lines = append(out.Lines, api.CabinetChargeLine{ChargeId: ch.ID, Title: ch.Title, OutstandingKop: rest,
			DueDate: apiconv.FromDatePtr(ch.DueDate), IsOverdue: overdue})
		out.DueTotalKop += rest
		out.Overdue = out.Overdue || overdue
		if ch.DueDate != nil && (next == nil || ch.DueDate.Before(*next)) {
			next = ch.DueDate
		}
	}
	out.NextDueDate = apiconv.FromDatePtr(next)
	dep, err := q.DepositTotals(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	out.DepositExpectedKop, out.DepositBalanceKop = cur.DepositKop, dep.BalanceKop
	out.DepositToPayKop = max(0, cur.DepositKop-dep.ReceivedKop)
	pays, err := q.PaymentsWithAdvance(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range pays {
		out.AdvanceKop += max(0, p.AdvanceKop)
	}
	pending, err := q.ActivePendingPayment(ctx, sqlc.ActivePendingPaymentParams{ContractID: cur.ID, Now: apiconv.Ptr(h.clock.Now())})
	switch {
	case err == nil:
		out.PendingPayment = &api.CabinetPendingPayment{Id: pending.ID, PaymentUrl: *pending.PaymentUrl, AmountKop: pending.AmountKop}
	case !db.IsNoRows(err):
		return nil, err
	}
	return api.GetCabinetSummary200JSONResponse(out), nil
}

// penaltyText — «за 8 дн. по ставке 0,1 % в день» (06 разд. 15).
func penaltyText(details []byte) *string {
	var d struct {
		RatePct  string `json:"ratePct"`
		Segments []struct {
			Days int `json:"days"`
		} `json:"segments"`
	}
	if json.Unmarshal(details, &d) != nil || d.RatePct == "" {
		return nil
	}
	days := 0
	for _, s := range d.Segments {
		days += s.Days
	}
	s := fmt.Sprintf("за %d дн. по ставке %s %% в день", days, ratePct(d.RatePct))
	return &s
}

// ratePct — «0.1000» → «0,1».
func ratePct(s string) string {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return s
	}
	return strings.Replace(d.String(), ".", ",", 1)
}

// ListCabinetCharges — GET /cabinet/charges (15 разд. 4).
func (h *Handler) ListCabinetCharges(ctx context.Context, req api.ListCabinetChargesRequestObject) (api.ListCabinetChargesResponseObject, error) {
	c, err := h.contract(ctx, req.Params.ContractId)
	if err != nil {
		return nil, err
	}
	today := h.clock.Today()
	rows, err := h.db.Queries().ListCharges(ctx, sqlc.ListChargesParams{
		ContractID: &c.ID, Kinds: []string{}, Statuses: []string{}, Today: today, Sort: "-dueDate", Lim: 1000,
	})
	if err != nil {
		return nil, err
	}
	out := make(api.ListCabinetCharges200JSONResponse, 0, len(rows))
	for _, r := range rows {
		rest := r.AmountKop - r.PaidKop
		if r.Status == "cancelled" {
			continue
		}
		d := api.CabinetChargeDTO{
			Id: r.ID, Kind: api.ChargeKind(r.Kind), Title: r.Title, PeriodMonth: apiconv.FromDatePtr(r.PeriodMonth),
			PeriodStart: apiconv.FromDatePtr(r.PeriodStart), PeriodEnd: apiconv.FromDatePtr(r.PeriodEnd),
			DueDate: apiconv.FromDatePtr(r.DueDate), AmountKop: r.AmountKop, PaidKop: r.PaidKop, OutstandingKop: rest,
			Status: api.ChargeStatus(r.Status),
		}
		if r.PenaltyState != nil {
			d.PenaltyState = apiconv.Ptr(api.PenaltyState(*r.PenaltyState))
			d.PenaltyText = penaltyText(r.CalcDetails)
		} else {
			details := map[string]any{}
			_ = json.Unmarshal(r.CalcDetails, &details)
			d.CalcDetails = &details
		}
		if r.DueDate != nil && rest > 0 && r.DueDate.Before(today) {
			d.IsOverdue, d.OverdueDays = true, today.Sub(*r.DueDate)
		}
		out = append(out, d)
	}
	return out, nil
}

// receiptURL — квитанция в кабинете.
func receiptURL(id uuid.UUID) string {
	return "/api/v1/cabinet/payments/" + id.String() + "/receipt.pdf"
}

// ListCabinetPayments — GET /cabinet/payments (15 разд. 4): проведённые и незавершённые.
func (h *Handler) ListCabinetPayments(ctx context.Context, req api.ListCabinetPaymentsRequestObject) (api.ListCabinetPaymentsResponseObject, error) {
	c, err := h.contract(ctx, req.Params.ContractId)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.Queries().CabinetPayments(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	out := make(api.ListCabinetPayments200JSONResponse, 0, len(rows))
	for _, r := range rows {
		d := api.CabinetPaymentDTO{
			Id: r.ID, Purpose: api.PaymentPurpose(r.Purpose), AmountKop: r.AmountKop, ReceivedOn: apiconv.FromDatePtr(r.ReceivedOn),
			CreatedAt: r.CreatedAt, Method: api.PaymentMethod(r.Method), Status: api.PaymentStatus(r.Status),
			ReceiptNumber: r.ReceiptNumber,
		}
		if r.Status == "succeeded" {
			d.ReceiptUrl = apiconv.Ptr(receiptURL(r.ID))
		}
		if r.FiscalStatus != nil && *r.FiscalStatus == "done" {
			d.OfdReceiptUrl, d.NpdReceiptUrl = r.OfdReceiptUrl, r.NpdReceiptUrl
		}
		out = append(out, d)
	}
	return out, nil
}

// CreateCabinetPayment — POST /cabinet/payments: онлайн-оплата (08 разд. 1.2).
func (h *Handler) CreateCabinetPayment(ctx context.Context, req api.CreateCabinetPaymentRequestObject) (
	api.CreateCabinetPaymentResponseObject, error,
) {
	b := req.Body
	r, err := h.payments.StartOnline(ctx, payment.OnlineInput{ContractID: b.ContractId, Purpose: string(b.Purpose), ChargeIDs: b.ChargeIds})
	if err != nil {
		return nil, err
	}
	return api.CreateCabinetPayment201JSONResponse{PaymentId: r.PaymentID, PaymentUrl: r.PaymentURL}, nil
}

// tenantPayment — платёж договора нанимателя (чужой — 404).
func (h *Handler) tenantPayment(ctx context.Context, id uuid.UUID) (sqlc.Payment, error) {
	p, err := h.db.Queries().GetPayment(ctx, id)
	if db.IsNoRows(err) {
		return p, apperr.NotFound()
	}
	if err != nil {
		return p, err
	}
	if _, err := h.contract(ctx, p.ContractID); err != nil {
		return p, err
	}
	return p, nil
}

// GetCabinetPayment — GET /cabinet/payments/{id}: статус для страницы возврата из оплаты (15 разд. 3).
func (h *Handler) GetCabinetPayment(ctx context.Context, req api.GetCabinetPaymentRequestObject) (api.GetCabinetPaymentResponseObject, error) {
	p, err := h.tenantPayment(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetCabinetPayment200JSONResponse{Id: p.ID, Status: api.PaymentStatus(p.Status), AmountKop: p.AmountKop,
		ReceiptNumber: p.ReceiptNumber, ContractId: p.ContractID, Purpose: api.PaymentPurpose(p.Purpose), ChargeIds: p.RequestedChargeIds}, nil
}

// GetCabinetPaymentReceiptPdf — GET /cabinet/payments/{id}/receipt.pdf.
func (h *Handler) GetCabinetPaymentReceiptPdf(ctx context.Context, req api.GetCabinetPaymentReceiptPdfRequestObject) (
	api.GetCabinetPaymentReceiptPdfResponseObject, error,
) {
	p, err := h.tenantPayment(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if p.Status != "succeeded" {
		return nil, apperr.NotFound()
	}
	doc, err := h.db.Queries().PaymentReceiptDocument(ctx, &p.ID)
	if db.IsNoRows(err) {
		doc, err = h.payments.Receipt(ctx, p.ID)
	}
	if err != nil {
		return nil, err
	}
	if !doc.VisibleToTenant {
		return nil, apperr.NotFound()
	}
	return h.documents.PDF(ctx, doc, req.Params.Download != nil && *req.Params.Download == 1)
}
