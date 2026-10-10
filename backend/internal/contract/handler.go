package contract

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
)

// Handler — методы API договоров, допсоглашений и актов.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func parseDec(field string, s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: field, Message: "Введите число"})
	}
	return &d, nil
}

func decOut(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func fromAPI(b api.ContractInput) (Input, error) {
	in := Input{
		PropertyID: b.PropertyId, TenantID: b.TenantId, StartDate: apiconv.ToDate(b.StartDate), EndDate: apiconv.ToDate(b.EndDate),
		FirstDueDate: apiconv.ToDatePtr(b.FirstDueDate), RentKop: b.RentKop, PaymentDay: b.PaymentDay, DepositKop: b.DepositKop,
		UtilitiesMode: string(b.UtilitiesMode), UtilitiesFixedKop: b.UtilitiesFixedKop, PenaltyEnabled: b.PenaltyEnabled,
		PenaltyGraceDays: b.PenaltyGraceDays, PenaltyOnUtilities: b.PenaltyOnUtilities, IndexationEnabled: b.IndexationEnabled,
		IndexationPeriodMonths: b.IndexationPeriodMonths, SpecialTerms: b.SpecialTerms, Notes: b.Notes,
	}
	var err error
	if in.PenaltyRatePct, err = parseDec("penaltyRatePct", b.PenaltyRatePct); err != nil {
		return in, err
	}
	if in.PenaltyCapPct, err = parseDec("penaltyCapPct", b.PenaltyCapPct); err != nil {
		return in, err
	}
	if in.IndexationPct, err = parseDec("indexationPct", b.IndexationPct); err != nil {
		return in, err
	}
	in.Occupants = occupants(b.Occupants)
	return in, nil
}

func occupants(list []api.OccupantInput) []Occupant {
	out := make([]Occupant, 0, len(list))
	for _, o := range list {
		out = append(out, Occupant{FullName: o.FullName, BirthDate: apiconv.ToDatePtr(o.BirthDate), Relation: o.Relation})
	}
	return out
}

func termsDTO(t sqlc.ContractTerm) api.ContractTermsDTO {
	return api.ContractTermsDTO{
		Version: int(t.Version), EffectiveFrom: apiconv.FromDate(t.EffectiveFrom), RentKop: t.RentKop,
		UtilitiesFixedKop: t.UtilitiesFixedKop, PaymentDay: int(t.PaymentDay), AmendmentId: t.AmendmentID,
	}
}

func boolOf(b *bool) bool { return b != nil && *b }

func (h *Handler) detailDTO(ctx context.Context, c sqlc.Contract) (api.ContractDetail, error) {
	d, err := h.svc.Detail(ctx, c)
	if err != nil {
		return api.ContractDetail{}, err
	}
	t := d.Tenant
	dto := api.ContractDetail{
		Id: c.ID, Number: c.Number, LegalBasis: api.LegalBasis(c.LegalBasis), Status: api.ContractStatus(c.Status),
		ConcludedOn: apiconv.FromDatePtr(c.ConcludedOn), StartDate: apiconv.FromDate(c.StartDate), EndDate: apiconv.FromDate(c.EndDate),
		TerminationDate: apiconv.FromDatePtr(c.TerminationDate), TerminationReason: c.TerminationReason,
		ActualEndDate: apiconv.FromDate(actualEnd(c)), RegistrationRequired: boolOf(c.RegistrationRequired),
		IsShortTerm: boolOf(c.IsShortTerm), RegistrationDoneOn: apiconv.FromDatePtr(c.RegistrationDoneOn),
		RegistrationNumber: c.RegistrationNumber, FirstDueDate: apiconv.FromDate(c.FirstDueDate), DepositKop: c.DepositKop,
		UtilitiesMode: api.UtilitiesMode(c.UtilitiesMode), PenaltyEnabled: c.PenaltyEnabled, PenaltyRatePct: decOut(c.PenaltyRatePct),
		PenaltyGraceDays: int(c.PenaltyGraceDays), PenaltyCapPct: decOut(c.PenaltyCapPct), PenaltyOnUtilities: c.PenaltyOnUtilities,
		IndexationEnabled: c.IndexationEnabled, IndexationPct: decOut(c.IndexationPct), IndexationPeriodMonths: int(c.IndexationPeriodMonths),
		NextIndexationOn: apiconv.FromDatePtr(c.NextIndexationOn), SpecialTerms: c.SpecialTerms, Notes: c.Notes,
		ActivatedAt: c.ActivatedAt, ClosedAt: c.ClosedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		Property: api.ContractPropertyInfo{Id: d.Property.ID, Code: d.Property.Code, Kind: api.PropertyKind(d.Property.Kind), Address: d.Property.Address},
		Tenant: api.ContractTenantInfo{
			Id: t.ID, Kind: api.TenantKind(t.Kind), DisplayName: names.Display(t.Kind, t.LastName, t.FirstName, t.MiddleName, t.OrgName),
			Email: t.Email, Phone: t.Phone,
		},
		CurrentTerms: termsDTO(h.svc.CurrentTerms(c, d.Terms)), TermsHistory: make([]api.ContractTermsDTO, 0, len(d.Terms)),
		Occupants: make([]api.OccupantDTO, 0, len(d.Occupants)), Warnings: d.Warnings, ActivationBlockers: d.Blockers.All(),
		// Залог (внесено, остаток) появляется вместе с платежами на этапе M6.
		Balances: api.ContractBalances{
			DebtKop: d.Balances.DebtKop, OverdueKop: d.Balances.OverdueKop, PenaltiesAccruingKop: d.Balances.PenaltiesAccruingKop,
			AdvanceKop: d.Balances.AdvanceKop, DepositExpectedKop: c.DepositKop,
		},
	}
	if c.TerminationInitiator != nil {
		dto.TerminationInitiator = apiconv.Ptr(api.TerminationInitiator(*c.TerminationInitiator))
	}
	for _, tr := range d.Terms {
		dto.TermsHistory = append(dto.TermsHistory, termsDTO(tr))
	}
	for _, o := range d.Occupants {
		dto.Occupants = append(dto.Occupants, api.OccupantDTO{Id: o.ID, FullName: o.FullName, BirthDate: apiconv.FromDatePtr(o.BirthDate), Relation: o.Relation})
	}
	if dto.Warnings == nil {
		dto.Warnings = []string{}
	}
	if dto.ActivationBlockers == nil {
		dto.ActivationBlockers = []string{}
	}
	dto.CanActivate = c.Status == StatusDraft && d.Blockers.empty()
	if d.NextDue != nil {
		dto.NextDue = &api.NextDue{Date: apiconv.FromDate(d.NextDue.Date), AmountKop: int64(d.NextDue.AmountKop)}
	}
	return dto, nil
}

// ListContracts — GET /contracts.
func (h *Handler) ListContracts(ctx context.Context, req api.ListContractsRequestObject) (api.ListContractsResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := ListFilter{PropertyID: p.PropertyId, TenantID: p.TenantId, EndingWithinDays: p.EndingWithinDays, Q: apiconv.TrimPtr(p.Q),
		Limit: paging.Limit(), Offset: paging.Offset()}
	if p.Status != nil {
		for _, s := range *p.Status {
			f.Statuses = append(f.Statuses, string(s))
		}
	}
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	rows, total, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	debts, err := h.svc.Debts(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]api.ContractListItem, 0, len(rows))
	for _, r := range rows {
		end := r.EndDate
		if r.ActualEndDate != nil {
			end = *r.ActualEndDate
		}
		items = append(items, api.ContractListItem{
			Id: r.ID, Number: r.Number, LegalBasis: api.LegalBasis(r.LegalBasis), IsShortTerm: boolOf(r.IsShortTerm),
			Status: api.ContractStatus(r.Status), Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode},
			Tenant:    api.TenantRef{Id: r.TenantID, DisplayName: names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName)},
			StartDate: apiconv.FromDate(r.StartDate), EndDate: apiconv.FromDate(r.EndDate), ActualEndDate: apiconv.FromDate(end),
			RentKop: r.RentKop, DebtKop: debts[r.ID],
		})
	}
	return api.ListContracts200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// CreateContract — POST /contracts.
func (h *Handler) CreateContract(ctx context.Context, req api.CreateContractRequestObject) (api.CreateContractResponseObject, error) {
	in, err := fromAPI(*req.Body)
	if err != nil {
		return nil, err
	}
	c, err := h.svc.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.CreateContract201JSONResponse(dto), err
}

// GetContract — GET /contracts/{id}.
func (h *Handler) GetContract(ctx context.Context, req api.GetContractRequestObject) (api.GetContractResponseObject, error) {
	c, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.GetContract200JSONResponse(dto), err
}

// UpdateContract — PUT /contracts/{id} (черновик).
func (h *Handler) UpdateContract(ctx context.Context, req api.UpdateContractRequestObject) (api.UpdateContractResponseObject, error) {
	in, err := fromAPI(*req.Body)
	if err != nil {
		return nil, err
	}
	c, err := h.svc.Update(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.UpdateContract200JSONResponse(dto), err
}

// PatchContract — PATCH /contracts/{id} (действующий договор).
func (h *Handler) PatchContract(ctx context.Context, req api.PatchContractRequestObject) (api.PatchContractResponseObject, error) {
	b := req.Body
	pt := Patch{
		PenaltyEnabled: b.PenaltyEnabled, PenaltyGraceDays: b.PenaltyGraceDays, PenaltyOnUtilities: b.PenaltyOnUtilities,
		IndexationEnabled: b.IndexationEnabled, IndexationPeriodMonths: b.IndexationPeriodMonths, Reason: b.Reason,
	}
	// Пени, индексация и регистрация меняются блоками: если передан признак блока (penaltyEnabled, indexationEnabled,
	// registrationNumber), остальные поля блока берутся как есть, null — очистка. Тексты очищаются пустой строкой.
	dec := func(field string, v *string) (**decimal.Decimal, error) {
		d, err := parseDec(field, v)
		return &d, err
	}
	var err error
	if b.PenaltyEnabled != nil {
		if pt.PenaltyRatePct, err = dec("penaltyRatePct", b.PenaltyRatePct); err != nil {
			return nil, err
		}
		if pt.PenaltyCapPct, err = dec("penaltyCapPct", b.PenaltyCapPct); err != nil {
			return nil, err
		}
	}
	if b.IndexationEnabled != nil {
		if pt.IndexationPct, err = dec("indexationPct", b.IndexationPct); err != nil {
			return nil, err
		}
	}
	setStr := func(v *string) **string {
		if v == nil {
			return nil
		}
		return &v
	}
	pt.SpecialTerms, pt.Notes, pt.RegistrationNumber = setStr(b.SpecialTerms), setStr(b.Notes), setStr(b.RegistrationNumber)
	if b.RegistrationNumber != nil || b.RegistrationDoneOn != nil {
		d := apiconv.ToDatePtr(b.RegistrationDoneOn)
		pt.RegistrationDoneOn = &d
	}
	c, err := h.svc.Apply(ctx, req.Id, pt)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.PatchContract200JSONResponse(dto), err
}

// DeleteContract — DELETE /contracts/{id} (черновик).
func (h *Handler) DeleteContract(ctx context.Context, req api.DeleteContractRequestObject) (api.DeleteContractResponseObject, error) {
	if err := h.svc.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return api.DeleteContract204Response{}, nil
}

// ActivateContract — POST /contracts/{id}/activate.
func (h *Handler) ActivateContract(ctx context.Context, req api.ActivateContractRequestObject) (api.ActivateContractResponseObject, error) {
	c, err := h.svc.Activate(ctx, req.Id, apiconv.ToDate(req.Body.ConcludedOn), req.Body.InviteTenant)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.ActivateContract200JSONResponse(dto), err
}

// TerminateContract — POST /contracts/{id}/terminate.
func (h *Handler) TerminateContract(ctx context.Context, req api.TerminateContractRequestObject) (api.TerminateContractResponseObject, error) {
	b := req.Body
	c, err := h.svc.Terminate(ctx, req.Id, apiconv.ToDate(b.TerminationDate), string(b.Initiator), b.Reason)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.TerminateContract200JSONResponse(dto), err
}

// UpdateContractOccupants — PUT /contracts/{id}/occupants.
func (h *Handler) UpdateContractOccupants(ctx context.Context, req api.UpdateContractOccupantsRequestObject) (api.UpdateContractOccupantsResponseObject, error) {
	if err := h.svc.ReplaceOccupants(ctx, req.Id, occupants(req.Body.Occupants)); err != nil {
		return nil, err
	}
	c, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	dto, err := h.detailDTO(ctx, c)
	return api.UpdateContractOccupants200JSONResponse(dto), err
}

// GetContractTimeline — GET /contracts/{id}/timeline.
func (h *Handler) GetContractTimeline(ctx context.Context, req api.GetContractTimelineRequestObject) (api.GetContractTimelineResponseObject, error) {
	if _, err := h.svc.Get(ctx, req.Id); err != nil {
		return nil, err
	}
	paging := apiconv.NewPaging(req.Params.Page, req.Params.PageSize)
	q := h.svc.db.Queries()
	rows, err := q.ContractTimeline(ctx, sqlc.ContractTimelineParams{ContractID: req.Id.String(), Lim: paging.Limit(), Off: paging.Offset()})
	if err != nil {
		return nil, err
	}
	total, err := q.CountContractTimeline(ctx, req.Id.String())
	if err != nil {
		return nil, err
	}
	items := make([]api.AuditEntryDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, audit.ToDTO(sqlc.ListAuditRow(r)))
	}
	return api.GetContractTimeline200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// ---------------------------------------------------------------- допсоглашения

func amendmentDTO(a sqlc.ContractAmendment) api.AmendmentDTO {
	d := api.AmendmentDTO{
		Id: a.ID, ContractId: a.ContractID, Number: int(a.Number), Kind: api.AmendmentKind(a.Kind), Status: api.AmendmentStatus(a.Status),
		EffectiveFrom: apiconv.FromDate(a.EffectiveFrom), NewRentKop: a.NewRentKop, NewUtilitiesFixedKop: a.NewUtilitiesFixedKop,
		NewEndDate: apiconv.FromDatePtr(a.NewEndDate), BodyText: a.BodyText, SignedOn: apiconv.FromDatePtr(a.SignedOn),
		CancelReason: a.CancelReason, AutoCreated: a.AutoCreated, AppliedAt: a.AppliedAt, CreatedAt: a.CreatedAt,
	}
	if a.NewPaymentDay != nil {
		d.NewPaymentDay = apiconv.Ptr(int(*a.NewPaymentDay))
	}
	return d
}

func amendmentIn(b api.AmendmentInput) AmendmentInput {
	return AmendmentInput{
		Kind: string(b.Kind), EffectiveFrom: apiconv.ToDate(b.EffectiveFrom), NewRentKop: b.NewRentKop,
		NewUtilitiesFixedKop: b.NewUtilitiesFixedKop, NewPaymentDay: b.NewPaymentDay, NewEndDate: apiconv.ToDatePtr(b.NewEndDate),
		BodyText: b.BodyText,
	}
}

// ListAmendments — GET /contracts/{id}/amendments.
func (h *Handler) ListAmendments(ctx context.Context, req api.ListAmendmentsRequestObject) (api.ListAmendmentsResponseObject, error) {
	rows, err := h.svc.Amendments(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(api.ListAmendments200JSONResponse, 0, len(rows))
	for _, a := range rows {
		out = append(out, amendmentDTO(a))
	}
	return out, nil
}

// CreateAmendment — POST /contracts/{id}/amendments.
func (h *Handler) CreateAmendment(ctx context.Context, req api.CreateAmendmentRequestObject) (api.CreateAmendmentResponseObject, error) {
	a, err := h.svc.CreateAmendment(ctx, req.Id, amendmentIn(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.CreateAmendment201JSONResponse(amendmentDTO(a)), nil
}

// UpdateAmendment — PUT /amendments/{id}.
func (h *Handler) UpdateAmendment(ctx context.Context, req api.UpdateAmendmentRequestObject) (api.UpdateAmendmentResponseObject, error) {
	a, err := h.svc.UpdateAmendment(ctx, req.Id, amendmentIn(*req.Body))
	if err != nil {
		return nil, err
	}
	return api.UpdateAmendment200JSONResponse(amendmentDTO(a)), nil
}

// SignAmendment — POST /amendments/{id}/sign.
func (h *Handler) SignAmendment(ctx context.Context, req api.SignAmendmentRequestObject) (api.SignAmendmentResponseObject, error) {
	a, err := h.svc.SignAmendment(ctx, req.Id, apiconv.ToDate(req.Body.SignedOn))
	if err != nil {
		return nil, err
	}
	return api.SignAmendment200JSONResponse(amendmentDTO(a)), nil
}

// CancelAmendment — POST /amendments/{id}/cancel.
func (h *Handler) CancelAmendment(ctx context.Context, req api.CancelAmendmentRequestObject) (api.CancelAmendmentResponseObject, error) {
	a, err := h.svc.CancelAmendment(ctx, req.Id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return api.CancelAmendment200JSONResponse(amendmentDTO(a)), nil
}

// ---------------------------------------------------------------- акты

func actDTO(v ActView) api.ActDTO {
	a := v.Act
	d := api.ActDTO{
		Id: a.ID, ContractId: a.ContractID, Kind: api.ActKind(a.Kind), ActDate: apiconv.FromDate(a.ActDate),
		Inventory: make([]api.ActInventoryItem, 0, len(v.Inventory)), KeysCount: int(a.KeysCount), ConditionNotes: a.ConditionNotes,
		Damages: a.Damages, Signed: a.Signed, SignedOn: apiconv.FromDatePtr(a.SignedOn), DocumentId: a.DocumentID,
		Readings: make([]api.ActReadingDTO, 0, len(v.Meters)), CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	for _, it := range v.Inventory {
		d.Inventory = append(d.Inventory, api.ActInventoryItem{Name: it.Name, Quantity: it.Quantity, Condition: it.Condition, Note: it.Note})
	}
	for _, m := range v.Meters {
		r := api.ActReadingDTO{
			MeterId: m.Meter.ID, MeterType: api.MeterType(m.Meter.Type), SerialNumber: m.Meter.SerialNumber,
			Value: decOut(m.Value), PreviousValue: decOut(m.PreviousVal), PreviousDate: apiconv.FromDatePtr(m.PreviousDate),
		}
		d.Readings = append(d.Readings, r)
	}
	return d
}

// ListActs — GET /contracts/{id}/acts.
func (h *Handler) ListActs(ctx context.Context, req api.ListActsRequestObject) (api.ListActsResponseObject, error) {
	views, err := h.svc.Acts(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(api.ListActs200JSONResponse, 0, len(views))
	for _, v := range views {
		out = append(out, actDTO(v))
	}
	return out, nil
}

// CreateAct — POST /contracts/{id}/acts (акт возврата).
func (h *Handler) CreateAct(ctx context.Context, req api.CreateActRequestObject) (api.CreateActResponseObject, error) {
	v, err := h.svc.CreateReturnAct(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.CreateAct201JSONResponse(actDTO(v)), nil
}

// UpdateAct — PUT /acts/{id}.
func (h *Handler) UpdateAct(ctx context.Context, req api.UpdateActRequestObject) (api.UpdateActResponseObject, error) {
	b := req.Body
	in := ActInput{
		ActDate: apiconv.ToDate(b.ActDate), KeysCount: b.KeysCount, ConditionNotes: b.ConditionNotes, Damages: b.Damages,
		Inventory: make([]InventoryItem, 0, len(b.Inventory)),
	}
	for _, it := range b.Inventory {
		in.Inventory = append(in.Inventory, InventoryItem{Name: it.Name, Quantity: it.Quantity, Condition: it.Condition, Note: it.Note})
	}
	for i, r := range b.Readings {
		v, err := decimal.NewFromString(r.Value)
		if err != nil {
			return nil, apperr.Validation(apperr.FieldError{Field: "readings[" + strconv.Itoa(i) + "].value", Message: "Введите число"})
		}
		in.Readings = append(in.Readings, ActReading{MeterID: r.MeterId, Value: v})
	}
	v, err := h.svc.UpdateAct(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	return api.UpdateAct200JSONResponse(actDTO(v)), nil
}

// SignAct — POST /acts/{id}/sign.
func (h *Handler) SignAct(ctx context.Context, req api.SignActRequestObject) (api.SignActResponseObject, error) {
	v, err := h.svc.SignAct(ctx, req.Id, apiconv.ToDate(req.Body.SignedOn))
	if err != nil {
		return nil, err
	}
	return api.SignAct200JSONResponse(actDTO(v)), nil
}
