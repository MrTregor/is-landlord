package tenant

import (
	"context"
	"strings"
	"unicode"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"landlord/backend/internal/api"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
)

// Handler — методы API нанимателей.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func email(e *openapi_types.Email) *string {
	if e == nil {
		return nil
	}
	s := string(*e)
	return &s
}

func docType(d *api.DocType) *string {
	if d == nil {
		return nil
	}
	s := string(*d)
	return &s
}

func fromAPI(b api.TenantInput) Input {
	return Input{
		Kind: string(b.Kind), LastName: b.LastName, FirstName: b.FirstName, MiddleName: b.MiddleName,
		BirthDate: apiconv.ToDatePtr(b.BirthDate), DocType: docType(b.DocType), DocNumber: b.DocNumber,
		DocIssuedBy: b.DocIssuedBy, DocDivisionCode: b.DocDivisionCode, RegAddress: b.RegistrationAddress,
		DocIssuedOn: apiconv.ToDatePtr(b.DocIssuedOn), OrgName: b.OrgName, Inn: b.Inn, Kpp: b.Kpp, Ogrn: b.Ogrn,
		LegalAddress: b.LegalAddress, SignatoryName: b.SignatoryName, SignatoryPosition: b.SignatoryPosition,
		SignatoryBasis: b.SignatoryBasis, Phone: b.Phone, Email: email(b.Email), Note: b.Note,
	}
}

// DisplayName — «Фамилия И. О.» или наименование.
func DisplayName(t sqlc.Tenant) string {
	return names.Display(t.Kind, t.LastName, t.FirstName, t.MiddleName, t.OrgName)
}

func consentDTO(c sqlc.PdnConsent) api.PdnConsentDTO {
	d := api.PdnConsentDTO{
		Id: c.ID, Purpose: c.Purpose, GivenOn: apiconv.FromDate(c.GivenOn), Method: api.ConsentMethod(c.Method),
		FileId: c.FileID, DocumentId: c.DocumentID, RevokedOn: apiconv.FromDatePtr(c.RevokedOn), RevokeNote: c.RevokeNote,
		CreatedAt: c.CreatedAt,
	}
	if c.FileID != nil {
		d.FileUrl = apiconv.Ptr(file.URL(*c.FileID))
	}
	return d
}

func (h *Handler) detail(ctx context.Context, t sqlc.Tenant) (api.TenantDetail, error) {
	q := h.svc.db.Queries()
	m, err := h.svc.Mask(t)
	if err != nil {
		return api.TenantDetail{}, err
	}
	consents, err := q.ListConsents(ctx, t.ID)
	if err != nil {
		return api.TenantDetail{}, err
	}
	contracts, err := q.ListTenantContracts(ctx, t.ID)
	if err != nil {
		return api.TenantDetail{}, err
	}
	d := api.TenantDetail{
		Id: t.ID, Kind: api.TenantKind(t.Kind), DisplayName: DisplayName(t), LastName: t.LastName, FirstName: t.FirstName,
		MiddleName: t.MiddleName, BirthDate: apiconv.FromDatePtr(t.BirthDate), DocIssuedOn: apiconv.FromDatePtr(t.DocIssuedOn),
		OrgName: t.OrgName, Inn: t.Inn, Kpp: t.Kpp, Ogrn: t.Ogrn, LegalAddress: t.LegalAddress, SignatoryName: t.SignatoryName,
		SignatoryPosition: t.SignatoryPosition, SignatoryBasis: t.SignatoryBasis, Phone: t.Phone, Email: t.Email, Note: t.Note,
		DocNumberMasked: m.DocNumber, RegistrationAddressMasked: m.RegAddress, HasDocument: m.HasDocument,
		AnonymizedAt: t.AnonymizedAt, Consents: []api.PdnConsentDTO{}, Contracts: []api.TenantContractItem{},
	}
	if rows, err := q.DebtByTenant(ctx, []uuid.UUID{t.ID}); err != nil {
		return api.TenantDetail{}, err
	} else if len(rows) > 0 {
		d.DebtKop = rows[0].DebtKop
	}
	if t.DocType != nil {
		d.DocType = apiconv.Ptr(api.DocType(*t.DocType))
	}
	for _, c := range consents {
		d.Consents = append(d.Consents, consentDTO(c))
	}
	for _, c := range contracts {
		d.Contracts = append(d.Contracts, api.TenantContractItem{
			Id: c.ID, Number: c.Number, Status: api.ContractStatus(c.Status), PropertyCode: c.PropertyCode,
			StartDate: apiconv.FromDate(c.StartDate), EndDate: apiconv.FromDate(c.EndDate),
		})
	}
	u, err := q.GetUserByTenantID(ctx, &t.ID)
	switch {
	case err == nil:
		d.User = &api.TenantUser{Id: u.ID, Status: api.UserStatus(u.Status), LastLoginAt: u.LastLoginAt}
	case db.IsNoRows(err):
	default:
		return d, err
	}
	return d, nil
}

func digits(s string) *string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() < 3 {
		return nil
	}
	v := b.String()
	return &v
}

// ListTenants — список нанимателей.
func (h *Handler) ListTenants(ctx context.Context, req api.ListTenantsRequestObject) (api.ListTenantsResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	q := apiconv.TrimPtr(p.Q)
	var qDigits *string
	if q != nil {
		qDigits = digits(*q)
	}
	params := sqlc.ListTenantsParams{
		Anonymized: p.Anonymized != nil && *p.Anonymized, HasActive: p.HasActiveContract, Q: q, QDigits: qDigits,
		Lim: paging.Limit(), Off: paging.Offset(),
	}
	if p.Kind != nil {
		params.Kind = apiconv.Ptr(string(*p.Kind))
	}
	rows, err := h.svc.db.Queries().ListTenants(ctx, params)
	if err != nil {
		return nil, err
	}
	total, err := h.svc.db.Queries().CountTenants(ctx, sqlc.CountTenantsParams{
		Kind: params.Kind, Anonymized: params.Anonymized, HasActive: params.HasActive, Q: q, QDigits: qDigits,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	debtRows, err := h.svc.db.Queries().DebtByTenant(ctx, ids)
	if err != nil {
		return nil, err
	}
	debts := map[uuid.UUID]int64{}
	for _, d := range debtRows {
		debts[d.ID] = d.DebtKop
	}
	items := make([]api.TenantListItem, 0, len(rows))
	for _, r := range rows {
		status := api.CabinetStatusNone
		if r.UserStatus != nil {
			status = api.CabinetStatus(*r.UserStatus)
		}
		items = append(items, api.TenantListItem{
			Id: r.ID, Kind: api.TenantKind(r.Kind),
			DisplayName: names.Display(r.Kind, r.LastName, r.FirstName, r.MiddleName, r.OrgName),
			Phone:       r.Phone, Email: r.Email, ActiveContracts: int(r.ActiveContracts), DebtKop: debts[r.ID],
			CabinetStatus: status, Anonymized: r.AnonymizedAt != nil,
		})
	}
	return api.ListTenants200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// CreateTenant — добавить нанимателя.
func (h *Handler) CreateTenant(ctx context.Context, req api.CreateTenantRequestObject) (api.CreateTenantResponseObject, error) {
	b := req.Body
	in := fromAPI(api.TenantInput{
		Kind: b.Kind, LastName: b.LastName, FirstName: b.FirstName, MiddleName: b.MiddleName, BirthDate: b.BirthDate,
		DocType: b.DocType, DocNumber: b.DocNumber, DocIssuedBy: b.DocIssuedBy, DocIssuedOn: b.DocIssuedOn,
		DocDivisionCode: b.DocDivisionCode, RegistrationAddress: b.RegistrationAddress, OrgName: b.OrgName, Inn: b.Inn,
		Kpp: b.Kpp, Ogrn: b.Ogrn, LegalAddress: b.LegalAddress, SignatoryName: b.SignatoryName,
		SignatoryPosition: b.SignatoryPosition, SignatoryBasis: b.SignatoryBasis, Phone: b.Phone, Email: b.Email, Note: b.Note,
	})
	var consent *ConsentOnCreate
	if b.Consent != nil {
		consent = &ConsentOnCreate{GivenOn: apiconv.ToDate(b.Consent.GivenOn), FileID: b.Consent.FileId}
	}
	t, err := h.svc.Create(ctx, in, consent)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, t)
	if err != nil {
		return nil, err
	}
	return api.CreateTenant201JSONResponse(d), nil
}

// GetTenant — карточка нанимателя.
func (h *Handler) GetTenant(ctx context.Context, req api.GetTenantRequestObject) (api.GetTenantResponseObject, error) {
	t, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, t)
	if err != nil {
		return nil, err
	}
	return api.GetTenant200JSONResponse(d), nil
}

// UpdateTenant — изменить нанимателя.
func (h *Handler) UpdateTenant(ctx context.Context, req api.UpdateTenantRequestObject) (api.UpdateTenantResponseObject, error) {
	t, err := h.svc.Update(ctx, req.Id, fromAPI(*req.Body))
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, t)
	if err != nil {
		return nil, err
	}
	return api.UpdateTenant200JSONResponse(d), nil
}

// RevealTenantPdn — полные паспортные данные и адрес регистрации (запись tenant.pdn_view).
func (h *Handler) RevealTenantPdn(ctx context.Context, req api.RevealTenantPdnRequestObject) (api.RevealTenantPdnResponseObject, error) {
	p, err := h.svc.Reveal(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.RevealTenantPdn200JSONResponse{
		DocNumber: p.DocNumber, DocIssuedBy: p.DocIssuedBy, DocDivisionCode: p.DivisionCode, RegistrationAddress: p.RegAddress,
	}, nil
}

// RecordConsent — отметка о получении подписанного согласия.
func (h *Handler) RecordConsent(ctx context.Context, req api.RecordConsentRequestObject) (api.RecordConsentResponseObject, error) {
	c, err := h.svc.RecordConsent(ctx, req.Id, apiconv.ToDate(req.Body.GivenOn), req.Body.FileId, req.Body.DocumentId)
	if err != nil {
		return nil, err
	}
	return api.RecordConsent201JSONResponse(consentDTO(c)), nil
}

// RevokeConsent — отзыв согласия.
func (h *Handler) RevokeConsent(ctx context.Context, req api.RevokeConsentRequestObject) (api.RevokeConsentResponseObject, error) {
	c, err := h.svc.RevokeConsent(ctx, req.Id, req.ConsentId, apiconv.ToDate(req.Body.RevokedOn), req.Body.Note)
	if err != nil {
		return nil, err
	}
	return api.RevokeConsent200JSONResponse(consentDTO(c)), nil
}

// InviteTenant — пригласить в личный кабинет.
func (h *Handler) InviteTenant(ctx context.Context, req api.InviteTenantRequestObject) (api.InviteTenantResponseObject, error) {
	u, err := h.svc.Invite(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.InviteTenant201JSONResponse(auth.UserDTO(u)), nil
}

// AnonymizeTenant — POST /tenants/{id}/anonymize (NFR-Sec-05, 12 разд. 6).
func (h *Handler) AnonymizeTenant(ctx context.Context, req api.AnonymizeTenantRequestObject) (api.AnonymizeTenantResponseObject, error) {
	if !req.Body.Confirm {
		return nil, apperr.Validation(apperr.FieldError{Field: "confirm", Message: "Подтвердите обезличивание"})
	}
	t, err := h.svc.Anonymize(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, t)
	if err != nil {
		return nil, err
	}
	return api.AnonymizeTenant200JSONResponse(d), nil
}
