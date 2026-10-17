package cabinet

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/tenant"
)

// profile — профиль нанимателя (15 разд. 8): имя и e-mail учётной записи, телефон нанимателя, ознакомление с
// политикой, действующее отдельное согласие и контакты организации.
func (h *Handler) profile(ctx context.Context) (api.CabinetProfile, error) {
	tid, err := tenantID(ctx)
	if err != nil {
		return api.CabinetProfile{}, err
	}
	q := h.db.Queries()
	u, err := q.GetUserByID(ctx, reqctx.ActorFrom(ctx).UserID)
	if err != nil {
		return api.CabinetProfile{}, err
	}
	t, err := q.GetTenant(ctx, tid)
	if err != nil {
		return api.CabinetProfile{}, err
	}
	o, err := h.settings.Get(ctx, q)
	if err != nil {
		return api.CabinetProfile{}, err
	}
	out := api.CabinetProfile{
		DisplayName: u.DisplayName, Email: u.Email, Phone: t.Phone, PolicyAcceptedVersion: u.PolicyAcceptedVersion,
		PolicyAcceptedAt: u.PolicyAcceptedAt, ConsentPurpose: o.PdnConsentPurpose, ConsentAvailable: t.Kind == tenant.KindIndividual,
		Organization: api.OrganizationContacts{ShortName: o.ShortName, Phone: o.Phone, Email: o.Email},
	}
	c, err := h.tenants.ActiveConsent(ctx, q, tid)
	if err != nil {
		return out, err
	}
	if c != nil {
		out.Consent = &api.CabinetConsent{GivenOn: apiconv.FromDate(c.GivenOn), Method: api.ConsentMethod(c.Method)}
	}
	return out, nil
}

// GetCabinetProfile — GET /cabinet/profile.
func (h *Handler) GetCabinetProfile(ctx context.Context, _ api.GetCabinetProfileRequestObject) (api.GetCabinetProfileResponseObject, error) {
	p, err := h.profile(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetCabinetProfile200JSONResponse(p), nil
}

// UpdateCabinetProfile — PATCH /cabinet/profile: изменить телефон.
func (h *Handler) UpdateCabinetProfile(ctx context.Context, req api.UpdateCabinetProfileRequestObject) (api.UpdateCabinetProfileResponseObject, error) {
	tid, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.tenants.SetPhone(ctx, tid, req.Body.Phone); err != nil {
		return nil, err
	}
	p, err := h.profile(ctx)
	if err != nil {
		return nil, err
	}
	return api.UpdateCabinetProfile200JSONResponse(p), nil
}

// GiveCabinetConsent — POST /cabinet/consent: отдельное электронное согласие (12 разд. 2).
func (h *Handler) GiveCabinetConsent(ctx context.Context, req api.GiveCabinetConsentRequestObject) (api.GiveCabinetConsentResponseObject, error) {
	if !req.Body.Agree {
		return nil, apperr.Validation(apperr.FieldError{Field: "agree", Message: "Отметьте «Даю согласие»"})
	}
	tid, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.tenants.GiveElectronicConsent(ctx, tid); err != nil {
		return nil, err
	}
	return api.GiveCabinetConsent204Response{}, nil
}

// RevokeCabinetConsent — POST /cabinet/consent/revoke: отзыв согласия.
func (h *Handler) RevokeCabinetConsent(ctx context.Context, _ api.RevokeCabinetConsentRequestObject) (api.RevokeCabinetConsentResponseObject, error) {
	tid, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.tenants.RevokeOwnConsent(ctx, tid); err != nil {
		return nil, err
	}
	return api.RevokeCabinetConsent204Response{}, nil
}
