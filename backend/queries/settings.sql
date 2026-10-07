-- Настройки организации (FR-14.1).

-- name: GetOrganizationSettings :one
SELECT * FROM organization_settings WHERE id = 1;

-- name: UpsertOrganizationSettings :one
INSERT INTO organization_settings (id, full_name, short_name, inn, ogrnip, address, phone, email, bank_name, bank_bik,
  bank_account, bank_corr_account, signatory_name, document_city, tax_regime, tax_rate_pct, vat_payer,
  default_fiscal_mode_residential, default_fiscal_mode_non_residential, pdn_retention_years, privacy_policy_version,
  privacy_policy_html, pdn_consent_purpose, remind_before_due_days, overdue_repeat_days, contract_end_notice_days,
  readings_reminder_day, readings_auto_accept, owner_daily_digest, indexation_draft_days_before, updated_at, updated_by)
VALUES (1, @full_name, @short_name, @inn, sqlc.narg(ogrnip), @address, sqlc.narg(phone), @email, sqlc.narg(bank_name),
  sqlc.narg(bank_bik), sqlc.narg(bank_account), sqlc.narg(bank_corr_account), @signatory_name, @document_city,
  @tax_regime, @tax_rate_pct, @vat_payer, @default_fiscal_mode_residential, @default_fiscal_mode_non_residential,
  @pdn_retention_years, @privacy_policy_version, @privacy_policy_html, @pdn_consent_purpose, @remind_before_due_days,
  @overdue_repeat_days, @contract_end_notice_days, @readings_reminder_day, @readings_auto_accept, @owner_daily_digest,
  @indexation_draft_days_before, @updated_at, sqlc.narg(updated_by))
ON CONFLICT (id) DO UPDATE SET
  full_name = EXCLUDED.full_name, short_name = EXCLUDED.short_name, inn = EXCLUDED.inn, ogrnip = EXCLUDED.ogrnip,
  address = EXCLUDED.address, phone = EXCLUDED.phone, email = EXCLUDED.email, bank_name = EXCLUDED.bank_name,
  bank_bik = EXCLUDED.bank_bik, bank_account = EXCLUDED.bank_account, bank_corr_account = EXCLUDED.bank_corr_account,
  signatory_name = EXCLUDED.signatory_name, document_city = EXCLUDED.document_city, tax_regime = EXCLUDED.tax_regime,
  tax_rate_pct = EXCLUDED.tax_rate_pct, vat_payer = EXCLUDED.vat_payer,
  default_fiscal_mode_residential = EXCLUDED.default_fiscal_mode_residential,
  default_fiscal_mode_non_residential = EXCLUDED.default_fiscal_mode_non_residential,
  pdn_retention_years = EXCLUDED.pdn_retention_years, privacy_policy_version = EXCLUDED.privacy_policy_version,
  privacy_policy_html = EXCLUDED.privacy_policy_html, pdn_consent_purpose = EXCLUDED.pdn_consent_purpose,
  remind_before_due_days = EXCLUDED.remind_before_due_days, overdue_repeat_days = EXCLUDED.overdue_repeat_days,
  contract_end_notice_days = EXCLUDED.contract_end_notice_days, readings_reminder_day = EXCLUDED.readings_reminder_day,
  readings_auto_accept = EXCLUDED.readings_auto_accept, owner_daily_digest = EXCLUDED.owner_daily_digest,
  indexation_draft_days_before = EXCLUDED.indexation_draft_days_before,
  updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by
RETURNING *;
