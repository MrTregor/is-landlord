// Package settings — реквизиты и настройки организации (FR-14.1), политика обработки ПДн.
package settings

import (
	"context"
	"net/mail"
	"strconv"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/platform/tmpl"
)

// Service — настройки организации.
type Service struct {
	db     *db.DB
	audit  *audit.Service
	clock  clock.Clock
	policy *bluemonday.Policy
}

// NewService создаёт сервис настроек.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock) *Service {
	p := bluemonday.UGCPolicy()
	p.AllowElements("table", "thead", "tbody", "tr", "th", "td", "u", "s")
	p.AllowAttrs("style").OnElements("p", "td", "th")
	p.AllowAttrs("class").Globally()
	return &Service{db: database, audit: au, clock: clk, policy: p}
}

// Defaults — настройки до первого сохранения: значения по умолчанию из 04 Модель данных.
func Defaults() sqlc.OrganizationSetting {
	return sqlc.OrganizationSetting{
		ID:                              1,
		DocumentCity:                    "г. Руза",
		TaxRegime:                       "usn_income",
		TaxRatePct:                      decimal.RequireFromString("6.00"),
		DefaultFiscalModeResidential:    "none",
		DefaultFiscalModeNonResidential: "cloud_kkt",
		PdnRetentionYears:               3,
		PrivacyPolicyVersion:            DefaultPrivacyPolicyVersion,
		PrivacyPolicyHtml:               DefaultPrivacyPolicyHTML,
		PdnConsentPurpose:               DefaultConsentPurpose,
		RemindBeforeDueDays:             3,
		OverdueRepeatDays:               7,
		ContractEndNoticeDays:           []int16{90, 30},
		ReadingsReminderDay:             20,
		OwnerDailyDigest:                true,
		IndexationDraftDaysBefore:       30,
	}
}

// Get возвращает настройки; если они ещё не сохранялись — значения по умолчанию.
func (s *Service) Get(ctx context.Context, q *sqlc.Queries) (sqlc.OrganizationSetting, error) {
	o, err := q.GetOrganizationSettings(ctx)
	if db.IsNoRows(err) {
		return Defaults(), nil
	}
	return o, err
}

// PolicyVersion — текущая версия политики обработки ПДн.
func (s *Service) PolicyVersion(ctx context.Context, q *sqlc.Queries) (string, error) {
	o, err := s.Get(ctx, q)
	return o.PrivacyPolicyVersion, err
}

// PrivacyPolicy — версия и текст политики с подставленными реквизитами организации.
func (s *Service) PrivacyPolicy(ctx context.Context) (version, html string, err error) {
	o, err := s.Get(ctx, s.db.Queries())
	if err != nil {
		return "", "", err
	}
	vars := map[string]string{
		"org.full_name":       o.FullName,
		"org.short_name":      o.ShortName,
		"org.inn":             o.Inn,
		"org.address":         o.Address,
		"org.email":           o.Email,
		"pdn_retention_years": strconv.Itoa(int(o.PdnRetentionYears)),
		"pdn_consent_purpose": o.PdnConsentPurpose,
	}
	if o.Phone != nil {
		vars["org.phone"] = *o.Phone
	}
	return o.PrivacyPolicyVersion, tmpl.Render(o.PrivacyPolicyHtml, vars, nil, tmpl.Options{Empty: "________________"}), nil
}

// Update сохраняет настройки (запись settings.update в журнал со значениями «до» и «после»).
func (s *Service) Update(ctx context.Context, in sqlc.UpsertOrganizationSettingsParams) (sqlc.OrganizationSetting, error) {
	if in.TaxRatePct.IsNegative() || in.TaxRatePct.GreaterThan(decimal.NewFromInt(30)) {
		return sqlc.OrganizationSetting{}, apperr.Validation(apperr.FieldError{Field: "taxRatePct", Message: "Ставка налога — от 0 до 30 %"})
	}
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		return sqlc.OrganizationSetting{}, apperr.Validation(apperr.FieldError{Field: "email", Message: "Неверный формат e-mail"})
	}
	if len(in.ContractEndNoticeDays) == 0 {
		return sqlc.OrganizationSetting{}, apperr.Validation(apperr.FieldError{
			Field: "contractEndNoticeDays", Message: "Укажите хотя бы один срок уведомления об окончании договора"})
	}
	in.TaxRatePct = in.TaxRatePct.Round(2)
	in.PrivacyPolicyHtml = s.policy.Sanitize(in.PrivacyPolicyHtml)
	in.UpdatedAt = s.clock.Now()
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		in.UpdatedBy = &id
	} else {
		in.UpdatedBy = (*uuid.UUID)(nil)
	}

	var out sqlc.OrganizationSetting
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetOrganizationSettings(ctx)
		var beforeVal any
		switch {
		case err == nil:
			beforeVal = snapshot(before)
		case db.IsNoRows(err):
		default:
			return err
		}
		out, err = tx.Q.UpsertOrganizationSettings(ctx, in)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionSettingsUpdate, EntityType: "settings", EntityID: "organization",
			Summary: "Изменены настройки организации", Before: beforeVal, After: snapshot(out),
		})
	})
	return out, err
}

// CurrentPolicyVersion — текущая версия политики (проверка доступа к кабинету).
func (s *Service) CurrentPolicyVersion(ctx context.Context) (string, error) {
	return s.PolicyVersion(ctx, s.db.Queries())
}

// snapshot — настройки для журнала: имена полей как в API, вместо текста политики — его длина.
func snapshot(o sqlc.OrganizationSetting) map[string]any {
	return map[string]any{
		"fullName": o.FullName, "shortName": o.ShortName, "inn": o.Inn, "ogrnip": o.Ogrnip, "address": o.Address,
		"phone": o.Phone, "email": o.Email, "bankName": o.BankName, "bankBik": o.BankBik, "bankAccount": o.BankAccount,
		"bankCorrAccount": o.BankCorrAccount, "signatoryName": o.SignatoryName, "documentCity": o.DocumentCity,
		"taxRegime": o.TaxRegime, "taxRatePct": o.TaxRatePct.StringFixed(2), "vatPayer": o.VatPayer,
		"defaultFiscalModeResidential":    o.DefaultFiscalModeResidential,
		"defaultFiscalModeNonResidential": o.DefaultFiscalModeNonResidential,
		"pdnRetentionYears":               o.PdnRetentionYears, "pdnConsentPurpose": o.PdnConsentPurpose,
		"privacyPolicyVersion": o.PrivacyPolicyVersion,
		"privacyPolicyLength":  len([]rune(o.PrivacyPolicyHtml)),
		"remindBeforeDueDays":  o.RemindBeforeDueDays, "overdueRepeatDays": o.OverdueRepeatDays,
		"contractEndNoticeDays": o.ContractEndNoticeDays, "readingsReminderDay": o.ReadingsReminderDay,
		"readingsAutoAccept": o.ReadingsAutoAccept, "ownerDailyDigest": o.OwnerDailyDigest,
		"indexationDraftDaysBefore": o.IndexationDraftDaysBefore,
	}
}
