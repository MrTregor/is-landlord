package settings

import (
	"context"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API настроек организации и политики ПДн.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetOrganizationSettings — реквизиты и настройки (до первого сохранения — значения по умолчанию).
func (h *Handler) GetOrganizationSettings(ctx context.Context, _ api.GetOrganizationSettingsRequestObject) (
	api.GetOrganizationSettingsResponseObject, error) {
	o, err := h.svc.Get(ctx, h.svc.db.Queries())
	if err != nil {
		return nil, err
	}
	return api.GetOrganizationSettings200JSONResponse(toDTO(o)), nil
}

// UpdateOrganizationSettings — сохранить настройки (вкладки «Организация», «Налоги», «Уведомления», «ПДн»).
func (h *Handler) UpdateOrganizationSettings(ctx context.Context, req api.UpdateOrganizationSettingsRequestObject) (
	api.UpdateOrganizationSettingsResponseObject, error) {
	b := req.Body
	rate, err := decimal.NewFromString(b.TaxRatePct)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "taxRatePct", Message: "Укажите ставку числом"})
	}
	days := make([]int16, 0, len(b.ContractEndNoticeDays))
	for _, d := range b.ContractEndNoticeDays {
		days = append(days, int16(d)) //nolint:gosec // проверено схемой: 1–365
	}
	o, err := h.svc.Update(ctx, sqlc.UpsertOrganizationSettingsParams{
		FullName: b.FullName, ShortName: b.ShortName, Inn: b.Inn, Ogrnip: b.Ogrnip, Address: b.Address,
		Phone: b.Phone, Email: b.Email, BankName: b.BankName, BankBik: b.BankBik, BankAccount: b.BankAccount,
		BankCorrAccount: b.BankCorrAccount, SignatoryName: b.SignatoryName, DocumentCity: b.DocumentCity,
		TaxRegime: string(b.TaxRegime), TaxRatePct: rate, VatPayer: b.VatPayer,
		DefaultFiscalModeResidential:    string(b.DefaultFiscalModeResidential),
		DefaultFiscalModeNonResidential: string(b.DefaultFiscalModeNonResidential),
		PdnRetentionYears:               int16(b.PdnRetentionYears), //nolint:gosec // 1–10
		PrivacyPolicyVersion:            b.PrivacyPolicyVersion,
		PrivacyPolicyHtml:               b.PrivacyPolicyHtml,
		PdnConsentPurpose:               b.PdnConsentPurpose,
		RemindBeforeDueDays:             int16(b.RemindBeforeDueDays), //nolint:gosec // 0–15
		OverdueRepeatDays:               int16(b.OverdueRepeatDays),   //nolint:gosec // 1–30
		ContractEndNoticeDays:           days,
		ReadingsReminderDay:             int16(b.ReadingsReminderDay), //nolint:gosec // 1–28
		ReadingsAutoAccept:              b.ReadingsAutoAccept,
		OwnerDailyDigest:                b.OwnerDailyDigest,
		IndexationDraftDaysBefore:       int16(b.IndexationDraftDaysBefore), //nolint:gosec // 7–90
	})
	if err != nil {
		return nil, err
	}
	return api.UpdateOrganizationSettings200JSONResponse(toDTO(o)), nil
}

// GetPrivacyPolicy — текст политики с реквизитами организации (страница /privacy, окно в кабинете).
func (h *Handler) GetPrivacyPolicy(ctx context.Context, _ api.GetPrivacyPolicyRequestObject) (
	api.GetPrivacyPolicyResponseObject, error) {
	version, html, err := h.svc.PrivacyPolicy(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetPrivacyPolicy200JSONResponse{Version: version, Html: html}, nil
}

func toDTO(o sqlc.OrganizationSetting) api.OrganizationSettings {
	days := make([]int, 0, len(o.ContractEndNoticeDays))
	for _, d := range o.ContractEndNoticeDays {
		days = append(days, int(d))
	}
	return api.OrganizationSettings{
		FullName: o.FullName, ShortName: o.ShortName, Inn: o.Inn, Ogrnip: o.Ogrnip, Address: o.Address, Phone: o.Phone,
		Email: o.Email, BankName: o.BankName, BankBik: o.BankBik, BankAccount: o.BankAccount,
		BankCorrAccount: o.BankCorrAccount, SignatoryName: o.SignatoryName, DocumentCity: o.DocumentCity,
		TaxRegime: api.TaxRegime(o.TaxRegime), TaxRatePct: o.TaxRatePct.StringFixed(2), VatPayer: o.VatPayer,
		DefaultFiscalModeResidential:    api.FiscalMode(o.DefaultFiscalModeResidential),
		DefaultFiscalModeNonResidential: api.FiscalMode(o.DefaultFiscalModeNonResidential),
		PdnRetentionYears:               int(o.PdnRetentionYears),
		PrivacyPolicyVersion:            o.PrivacyPolicyVersion,
		PrivacyPolicyHtml:               o.PrivacyPolicyHtml,
		PdnConsentPurpose:               o.PdnConsentPurpose,
		RemindBeforeDueDays:             int(o.RemindBeforeDueDays),
		OverdueRepeatDays:               int(o.OverdueRepeatDays),
		ContractEndNoticeDays:           days,
		ReadingsReminderDay:             int(o.ReadingsReminderDay),
		ReadingsAutoAccept:              o.ReadingsAutoAccept,
		OwnerDailyDigest:                o.OwnerDailyDigest,
		IndexationDraftDaysBefore:       int(o.IndexationDraftDaysBefore),
		UpdatedAt:                       o.UpdatedAt,
		UpdatedBy:                       o.UpdatedBy,
	}
}
