-- Первая миграция: полная схема из docs/spec/04 Модель данных.md (перенесена без изменений).
-- Добавлены: разметка goose для функций, триггеры updated_at, COMMENT ON, раздел Down.
-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN NEW.updated_at := now(); RETURN NEW; END $$;
-- +goose StatementEnd

------------------------------------------------------------------
-- Файлы
------------------------------------------------------------------
CREATE TABLE files (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  storage_key   text NOT NULL UNIQUE,            -- 2026/10/<uuid>.<ext> относительно FILES_DIR
  original_name text NOT NULL,
  content_type  text NOT NULL,
  size_bytes    bigint NOT NULL CHECK (size_bytes > 0),
  sha256        bytea NOT NULL,
  category      text NOT NULL CHECK (category IN ('property_photo','request_photo','expense_doc','consent_scan',
                  'document_pdf','signed_scan','bank_statement','export','other')),
  uploaded_by   uuid NULL,                       -- FK добавляется после users
  created_at    timestamptz NOT NULL DEFAULT now(),
  deleted_at    timestamptz NULL
);

------------------------------------------------------------------
-- Наниматели и согласия
------------------------------------------------------------------
CREATE TABLE tenants (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  kind                    text NOT NULL CHECK (kind IN ('individual','legal')),
  -- физлицо
  last_name               text NULL,
  first_name              text NULL,
  middle_name             text NULL,
  birth_date              date NULL,
  doc_type                text NULL CHECK (doc_type IN ('passport_rf','foreign_passport','other')),
  doc_number_enc          bytea NULL,           -- серия и номер, AES-256-GCM
  doc_issued_by_enc       bytea NULL,
  doc_issued_on           date NULL,
  doc_division_code_enc   bytea NULL,
  registration_address_enc bytea NULL,
  -- юрлицо
  org_name                text NULL,
  inn                     text NULL CHECK (inn IS NULL OR inn ~ '^\d{10}(\d{2})?$'),
  kpp                     text NULL CHECK (kpp IS NULL OR kpp ~ '^\d{9}$'),
  ogrn                    text NULL CHECK (ogrn IS NULL OR ogrn ~ '^\d{13}(\d{2})?$'),
  legal_address           text NULL,
  signatory_name          text NULL,
  signatory_position      text NULL,
  signatory_basis         text NULL,            -- «Устава», «доверенности № … от …»
  -- общее
  phone                   text NULL CHECK (phone IS NULL OR phone ~ '^\+7\d{10}$'),
  email                   citext NULL,
  note                    text NULL,
  anonymized_at           timestamptz NULL,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CHECK (anonymized_at IS NOT NULL OR kind <> 'individual' OR (last_name IS NOT NULL AND first_name IS NOT NULL)),
  CHECK (anonymized_at IS NOT NULL OR kind <> 'legal' OR (org_name IS NOT NULL AND inn IS NOT NULL))
);
CREATE INDEX tenants_name_idx ON tenants (lower(last_name), lower(first_name));
CREATE INDEX tenants_org_idx ON tenants (lower(org_name));

------------------------------------------------------------------
-- Пользователи, сессии, токены
------------------------------------------------------------------
CREATE TABLE users (
  id                   uuid PRIMARY KEY DEFAULT uuidv7(),
  email                citext NOT NULL UNIQUE,
  password_hash        text NULL,                -- NULL пока приглашение не принято
  role                 text NOT NULL CHECK (role IN ('owner','tenant')),
  status               text NOT NULL CHECK (status IN ('invited','active','blocked')),
  tenant_id            uuid NULL UNIQUE REFERENCES tenants(id),
  display_name         text NOT NULL,
  failed_login_count   int NOT NULL DEFAULT 0,
  locked_until         timestamptz NULL,
  last_login_at        timestamptz NULL,
  password_changed_at  timestamptz NULL,
  policy_accepted_version text NULL,            -- ознакомление с политикой ПДн (ч. 2 ст. 18.1), не согласие
  policy_accepted_at   timestamptz NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CHECK ((role = 'tenant') = (tenant_id IS NOT NULL)),
  CHECK (status = 'invited' OR password_hash IS NOT NULL)
);
ALTER TABLE files ADD CONSTRAINT files_uploaded_by_fk FOREIGN KEY (uploaded_by) REFERENCES users(id);

CREATE TABLE sessions (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id       uuid NOT NULL REFERENCES users(id),
  token_hash    bytea NOT NULL UNIQUE,           -- sha256 от значения cookie
  csrf_token    text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,            -- абсолютный срок
  revoked_at    timestamptz NULL,
  ip            inet NULL,
  user_agent    text NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE auth_tokens (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id     uuid NOT NULL REFERENCES users(id),
  purpose     text NOT NULL CHECK (purpose IN ('invite','password_reset')),
  token_hash  bytea NOT NULL UNIQUE,
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE login_attempts (
  id       uuid PRIMARY KEY DEFAULT uuidv7(),
  email    citext NOT NULL,
  ip       inet NULL,
  success  boolean NOT NULL,
  at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_email_idx ON login_attempts (email, at DESC);
CREATE INDEX login_attempts_ip_idx ON login_attempts (ip, at DESC);

-- Отдельное согласие на ПДн (ст. 9 152-ФЗ) — только для цели вне договора (organization_settings.pdn_consent_purpose).
-- Для заключения и исполнения договора согласие не нужно (п. 5 ч. 1 ст. 6), его отсутствие ничего не блокирует.
CREATE TABLE pdn_consents (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id       uuid NOT NULL REFERENCES tenants(id),
  purpose         text NOT NULL,                   -- текст цели из настроек на момент получения
  document_id     uuid NULL,                       -- бланк pdn_consent, по которому получено (FK добавляется после documents)
  given_on        date NOT NULL,
  method          text NOT NULL CHECK (method IN ('paper','electronic')),
  file_id         uuid NULL REFERENCES files(id),   -- скан подписанного бланка
  recorded_by     uuid NULL REFERENCES users(id),   -- собственник (paper) или сам наниматель (electronic)
  ip              inet NULL,
  revoked_on      date NULL,
  revoke_note     text NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pdn_consents_tenant_idx ON pdn_consents (tenant_id);

------------------------------------------------------------------
-- Настройки организации (одна строка)
------------------------------------------------------------------
CREATE TABLE organization_settings (
  id                                  smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  full_name                           text NOT NULL,   -- «Индивидуальный предприниматель Гордейчик Александр Васильевич»
  short_name                          text NOT NULL,   -- «ИП Гордейчик А.В.»
  inn                                 text NOT NULL CHECK (inn ~ '^\d{10}(\d{2})?$'),
  ogrnip                              text NULL CHECK (ogrnip IS NULL OR ogrnip ~ '^\d{15}$'),
  address                             text NOT NULL,
  phone                               text NULL,
  email                               citext NOT NULL,
  bank_name                           text NULL,
  bank_bik                            text NULL CHECK (bank_bik IS NULL OR bank_bik ~ '^\d{9}$'),
  bank_account                        text NULL CHECK (bank_account IS NULL OR bank_account ~ '^\d{20}$'),
  bank_corr_account                   text NULL CHECK (bank_corr_account IS NULL OR bank_corr_account ~ '^\d{20}$'),
  signatory_name                      text NOT NULL,   -- «А.В. Гордейчик»
  document_city                       text NOT NULL DEFAULT 'г. Руза',  -- место составления документов
  tax_regime                          text NOT NULL DEFAULT 'usn_income'
                                       CHECK (tax_regime IN ('usn_income','usn_income_expense','npd','psn','osn')),
  tax_rate_pct                        numeric(5,2) NOT NULL DEFAULT 6.00 CHECK (tax_rate_pct >= 0 AND tax_rate_pct <= 30),
  vat_payer                           boolean NOT NULL DEFAULT false,
  default_fiscal_mode_residential     text NOT NULL DEFAULT 'none' CHECK (default_fiscal_mode_residential IN ('none','cloud_kkt','npd_manual')),
  default_fiscal_mode_non_residential text NOT NULL DEFAULT 'cloud_kkt' CHECK (default_fiscal_mode_non_residential IN ('none','cloud_kkt','npd_manual')),
  pdn_retention_years                 smallint NOT NULL DEFAULT 3 CHECK (pdn_retention_years BETWEEN 1 AND 10),
  privacy_policy_version              text NOT NULL DEFAULT '1.0',
  privacy_policy_html                 text NOT NULL,
  pdn_consent_purpose                 text NOT NULL DEFAULT 'направление сообщений о свободных для найма объектах после окончания договора',
  remind_before_due_days              smallint NOT NULL DEFAULT 3 CHECK (remind_before_due_days BETWEEN 0 AND 15),
  overdue_repeat_days                 smallint NOT NULL DEFAULT 7 CHECK (overdue_repeat_days BETWEEN 1 AND 30),
  contract_end_notice_days            smallint[] NOT NULL DEFAULT '{90,30}',
  readings_reminder_day               smallint NOT NULL DEFAULT 20 CHECK (readings_reminder_day BETWEEN 1 AND 28),
  readings_auto_accept                boolean NOT NULL DEFAULT false,
  owner_daily_digest                  boolean NOT NULL DEFAULT true,
  indexation_draft_days_before        smallint NOT NULL DEFAULT 30 CHECK (indexation_draft_days_before BETWEEN 7 AND 90),
  updated_at                          timestamptz NOT NULL DEFAULT now(),
  updated_by                          uuid NULL REFERENCES users(id)
);

------------------------------------------------------------------
-- Объекты
------------------------------------------------------------------
CREATE TABLE properties (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  code              text NOT NULL UNIQUE,          -- «Студия 1»
  kind              text NOT NULL CHECK (kind IN ('residential','non_residential')),
  address           text NOT NULL,                 -- полный адрес с номером квартиры/помещения
  area_sqm          numeric(7,2) NOT NULL CHECK (area_sqm > 0),
  floor             smallint NULL,
  rooms             smallint NULL CHECK (rooms IS NULL OR rooms >= 0),  -- 0 = студия
  cadastral_number  text NULL CHECK (cadastral_number IS NULL OR cadastral_number ~ '^\d{2}:\d{2}:\d{6,7}:\d+$'),
  ownership_basis   text NULL,                     -- «Выписка из ЕГРН от 12.03.2019 № …» — подставляется в договор
  furnished         boolean NOT NULL DEFAULT false,
  description       text NULL,
  expected_rent_kop bigint NOT NULL DEFAULT 0 CHECK (expected_rent_kop >= 0),  -- ожидаемая плата для расчёта недополученного дохода
  fiscal_mode       text NOT NULL CHECK (fiscal_mode IN ('none','cloud_kkt','npd_manual')),
  status            text NOT NULL DEFAULT 'free' CHECK (status IN ('free','reserved','rented','repair')),
  archived_at       timestamptz NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE property_status_history (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  property_id  uuid NOT NULL REFERENCES properties(id),
  status       text NOT NULL CHECK (status IN ('free','reserved','rented','repair')),
  valid_from   date NOT NULL,
  valid_to     date NULL,                          -- последний день включительно; NULL = текущий
  contract_id  uuid NULL,                          -- FK после contracts
  reason       text NULL,
  created_by   uuid NULL REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (valid_to IS NULL OR valid_to >= valid_from),
  EXCLUDE USING gist (property_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&)
);
CREATE UNIQUE INDEX psh_current_uq ON property_status_history (property_id) WHERE valid_to IS NULL;

CREATE TABLE property_photos (
  property_id uuid NOT NULL REFERENCES properties(id),
  file_id     uuid NOT NULL REFERENCES files(id),
  sort        smallint NOT NULL DEFAULT 0,
  caption     text NULL,
  PRIMARY KEY (property_id, file_id)
);

CREATE TABLE property_inventory_items (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  property_id  uuid NOT NULL REFERENCES properties(id),
  name         text NOT NULL,                      -- «Холодильник Atlant ХМ-4021»
  quantity     int NOT NULL DEFAULT 1 CHECK (quantity > 0),
  condition    text NOT NULL DEFAULT 'исправно',
  note         text NULL,
  sort         int NOT NULL DEFAULT 0
);

CREATE TABLE meters (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  property_id      uuid NOT NULL REFERENCES properties(id),
  type             text NOT NULL CHECK (type IN ('cold_water','hot_water','electricity','electricity_day','electricity_night','gas','heating')),
  serial_number    text NULL,
  initial_value    numeric(12,3) NOT NULL DEFAULT 0 CHECK (initial_value >= 0),
  initial_date     date NOT NULL,
  verification_due date NULL,                      -- дата следующей поверки → событие календаря
  note             text NULL,
  archived_at      timestamptz NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tariffs (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  meter_type      text NOT NULL CHECK (meter_type IN ('cold_water','hot_water','electricity','electricity_day','electricity_night','gas','heating')),
  property_id     uuid NULL REFERENCES properties(id),  -- NULL = для всех объектов
  price           numeric(12,4) NOT NULL CHECK (price > 0),  -- руб. за единицу (м³, кВт·ч, Гкал)
  effective_from  date NOT NULL,
  note            text NULL,
  created_by      uuid NULL REFERENCES users(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (meter_type, property_id, effective_from)
);

------------------------------------------------------------------
-- Договоры
------------------------------------------------------------------
CREATE TABLE contracts (
  id                        uuid PRIMARY KEY DEFAULT uuidv7(),
  number                    text NOT NULL UNIQUE,      -- «2026-007»
  legal_basis               text NOT NULL CHECK (legal_basis IN ('ch35','ch34')),
  property_id               uuid NOT NULL REFERENCES properties(id),
  tenant_id                 uuid NOT NULL REFERENCES tenants(id),
  status                    text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','extended','terminated','finished')),
  concluded_on              date NULL,                 -- дата подписания
  start_date                date NOT NULL,
  end_date                  date NOT NULL,             -- последний день срока включительно
  termination_date          date NULL,                 -- последний день пользования при расторжении
  termination_reason        text NULL,
  termination_initiator     text NULL CHECK (termination_initiator IN ('landlord','tenant','mutual')),
  actual_end_date           date GENERATED ALWAYS AS (COALESCE(termination_date, end_date)) STORED,
  registration_required     boolean GENERATED ALWAYS AS (end_date >= (start_date + interval '1 year' - interval '1 day')::date) STORED,
  is_short_term             boolean GENERATED ALWAYS AS (legal_basis = 'ch35' AND end_date < (start_date + interval '1 year' - interval '1 day')::date) STORED,  -- краткосрочный наём, ст. 683 ГК РФ
  registration_done_on      date NULL,
  registration_number       text NULL,
  first_due_date            date NOT NULL,             -- срок оплаты первого периода
  deposit_kop               bigint NOT NULL DEFAULT 0 CHECK (deposit_kop >= 0),
  utilities_mode            text NOT NULL CHECK (utilities_mode IN ('included','meters','fixed')),
  penalty_enabled           boolean NOT NULL DEFAULT false,
  penalty_rate_pct          numeric(6,4) NULL CHECK (penalty_rate_pct IS NULL OR (penalty_rate_pct > 0 AND penalty_rate_pct <= 1)),  -- % в день
  penalty_grace_days        smallint NOT NULL DEFAULT 0 CHECK (penalty_grace_days BETWEEN 0 AND 30),
  penalty_cap_pct           numeric(6,2) NULL CHECK (penalty_cap_pct IS NULL OR (penalty_cap_pct > 0 AND penalty_cap_pct <= 100)),
  penalty_on_utilities      boolean NOT NULL DEFAULT false,
  indexation_enabled        boolean NOT NULL DEFAULT false,
  indexation_pct            numeric(5,2) NULL CHECK (indexation_pct IS NULL OR (indexation_pct > 0 AND indexation_pct <= 30)),
  indexation_period_months  smallint NOT NULL DEFAULT 12 CHECK (indexation_period_months >= 12),
  next_indexation_on        date NULL,
  special_terms             text NULL,                 -- особые условия, попадают в текст договора
  notes                     text NULL,                 -- внутренние заметки, в документы не попадают
  activated_at              timestamptz NULL,
  activated_by              uuid NULL REFERENCES users(id),
  closed_at                 timestamptz NULL,
  created_by                uuid NULL REFERENCES users(id),
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now(),
  CHECK (end_date > start_date),
  CHECK (legal_basis <> 'ch35' OR end_date < (start_date + interval '5 years')::date),
  CHECK (termination_date IS NULL OR (termination_date >= start_date AND termination_date <= end_date)),
  CHECK (first_due_date >= start_date - 30 AND first_due_date <= start_date + 30),
  CHECK (NOT penalty_enabled OR penalty_rate_pct IS NOT NULL),
  CHECK (NOT indexation_enabled OR (indexation_pct IS NOT NULL AND next_indexation_on IS NOT NULL)),
  EXCLUDE USING gist (property_id WITH =, daterange(start_date, actual_end_date, '[]') WITH &&) WHERE (status <> 'draft')
);
CREATE INDEX contracts_tenant_idx ON contracts (tenant_id);
CREATE INDEX contracts_status_idx ON contracts (status);
ALTER TABLE property_status_history ADD CONSTRAINT psh_contract_fk FOREIGN KEY (contract_id) REFERENCES contracts(id);

CREATE TABLE contract_amendments (
  id                       uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id              uuid NOT NULL REFERENCES contracts(id),
  number                   smallint NOT NULL,          -- 1, 2, … в рамках договора
  kind                     text NOT NULL CHECK (kind IN ('indexation','rent_change','extension','terms_change','other')),
  status                   text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','signed','cancelled')),
  effective_from           date NOT NULL,
  new_rent_kop             bigint NULL CHECK (new_rent_kop IS NULL OR new_rent_kop > 0),
  new_utilities_fixed_kop  bigint NULL CHECK (new_utilities_fixed_kop IS NULL OR new_utilities_fixed_kop >= 0),
  new_payment_day          smallint NULL CHECK (new_payment_day IS NULL OR new_payment_day BETWEEN 1 AND 28),
  new_end_date             date NULL,
  body_text                text NULL,                  -- иные изменения условий (свободный текст)
  signed_on                date NULL,
  cancel_reason            text NULL,
  auto_created             boolean NOT NULL DEFAULT false,
  applied_at               timestamptz NULL,           -- когда изменения применены к договору
  created_by               uuid NULL REFERENCES users(id),
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  UNIQUE (contract_id, number),
  CHECK (status <> 'signed' OR signed_on IS NOT NULL),
  CHECK (kind NOT IN ('indexation','rent_change') OR new_rent_kop IS NOT NULL),
  CHECK (kind <> 'extension' OR new_end_date IS NOT NULL),
  CHECK (new_rent_kop IS NULL AND new_utilities_fixed_kop IS NULL AND new_payment_day IS NULL
         OR extract(day FROM effective_from) = 1)
);

CREATE TABLE contract_terms (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id           uuid NOT NULL REFERENCES contracts(id),
  version               smallint NOT NULL CHECK (version >= 1),
  effective_from        date NOT NULL,
  rent_kop              bigint NOT NULL CHECK (rent_kop > 0),
  utilities_fixed_kop   bigint NOT NULL DEFAULT 0 CHECK (utilities_fixed_kop >= 0),
  payment_day           smallint NOT NULL CHECK (payment_day BETWEEN 1 AND 28),
  amendment_id          uuid NULL REFERENCES contract_amendments(id),
  created_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (contract_id, version),
  UNIQUE (contract_id, effective_from),
  CHECK (version = 1 OR extract(day FROM effective_from) = 1)
);

CREATE TABLE contract_occupants (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id  uuid NOT NULL REFERENCES contracts(id),
  full_name    text NOT NULL,
  birth_date   date NULL,
  relation     text NULL,                            -- «супруга», «сын»
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE contract_acts (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id      uuid NOT NULL REFERENCES contracts(id),
  kind             text NOT NULL CHECK (kind IN ('handover','return')),
  act_date         date NOT NULL,
  inventory        jsonb NOT NULL DEFAULT '[]',      -- [{"name":…, "quantity":1, "condition":"исправно", "note":…}]
  keys_count       smallint NOT NULL DEFAULT 0 CHECK (keys_count >= 0),
  condition_notes  text NULL,                        -- общее состояние помещения
  damages          text NULL,                        -- только для return: выявленные повреждения
  signed           boolean NOT NULL DEFAULT false,
  signed_on        date NULL,
  document_id      uuid NULL,                        -- FK после documents
  created_by       uuid NULL REFERENCES users(id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (contract_id, kind),
  CHECK (NOT signed OR signed_on IS NOT NULL)
);

------------------------------------------------------------------
-- Показания счётчиков
------------------------------------------------------------------
CREATE TABLE meter_readings (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  meter_id       uuid NOT NULL REFERENCES meters(id),
  contract_id    uuid NULL REFERENCES contracts(id),
  act_id         uuid NULL REFERENCES contract_acts(id),
  period_month   date NOT NULL CHECK (extract(day FROM period_month) = 1),
  reading_date   date NOT NULL,
  value          numeric(12,3) NOT NULL CHECK (value >= 0),
  source         text NOT NULL CHECK (source IN ('owner','tenant','act_handover','act_return')),
  status         text NOT NULL CHECK (status IN ('submitted','accepted','rejected')),
  submitted_by   uuid NULL REFERENCES users(id),
  submitted_at   timestamptz NOT NULL DEFAULT now(),
  decided_by     uuid NULL REFERENCES users(id),
  decided_at     timestamptz NULL,
  reject_reason  text NULL,
  CHECK (source NOT IN ('act_handover','act_return') OR act_id IS NOT NULL)
);
CREATE UNIQUE INDEX meter_readings_month_uq ON meter_readings (meter_id, period_month)
  WHERE status <> 'rejected' AND source IN ('owner','tenant');
CREATE UNIQUE INDEX meter_readings_act_uq ON meter_readings (meter_id, act_id) WHERE act_id IS NOT NULL;

------------------------------------------------------------------
-- Начисления
------------------------------------------------------------------
CREATE TABLE charges (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id           uuid NOT NULL REFERENCES contracts(id),
  kind                  text NOT NULL CHECK (kind IN ('rent','utilities','penalty','damage','other')),
  title                 text NOT NULL,              -- «Плата за наём, октябрь 2026»
  period_month          date NULL CHECK (period_month IS NULL OR extract(day FROM period_month) = 1),
  period_start          date NULL,
  period_end            date NULL,
  due_date              date NULL,                  -- NULL только у пеней в состоянии accruing
  amount_kop            bigint NOT NULL CHECK (amount_kop >= 0),
  original_amount_kop   bigint NOT NULL CHECK (original_amount_kop >= 0),
  paid_kop              bigint NOT NULL DEFAULT 0 CHECK (paid_kop >= 0),  -- сумма активных распределений (поддерживается сервисом)
  status                text NOT NULL DEFAULT 'open' CHECK (status IN ('open','partially_paid','paid','cancelled')),
  penalty_state         text NULL CHECK (penalty_state IN ('accruing','final','manual')),
  source_charge_id      uuid NULL REFERENCES charges(id),
  calc_details          jsonb NOT NULL DEFAULT '{}',
  origin                text NOT NULL CHECK (origin IN ('auto','manual')),
  idempotency_key       text NULL UNIQUE,           -- автоначисления: 'rent:<contract>:2026-10', 'util:<contract>:2026-09',
                                                    -- 'util-final:<contract>', 'fixed-util:<contract>:2026-10', 'penalty:<charge>';
                                                    -- при отмене начисления обнуляется (NULL), чтобы его можно было создать заново
  created_by            uuid NULL REFERENCES users(id),
  cancelled_at          timestamptz NULL,
  cancelled_by          uuid NULL REFERENCES users(id),
  cancel_reason         text NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'penalty') = (source_charge_id IS NOT NULL)),
  CHECK ((kind = 'penalty') = (penalty_state IS NOT NULL)),
  CHECK (due_date IS NOT NULL OR (kind = 'penalty' AND penalty_state = 'accruing')),
  CHECK (kind NOT IN ('rent','utilities') OR period_month IS NOT NULL),
  CHECK (paid_kop <= amount_kop OR status = 'cancelled'),
  CHECK (status <> 'cancelled' OR (cancelled_at IS NOT NULL AND cancel_reason IS NOT NULL AND idempotency_key IS NULL)),
  CHECK (origin <> 'auto' OR status = 'cancelled' OR idempotency_key IS NOT NULL)
);
CREATE INDEX charges_source_idx ON charges (source_charge_id) WHERE source_charge_id IS NOT NULL;
CREATE INDEX charges_open_idx ON charges (contract_id, due_date) WHERE status IN ('open','partially_paid');

CREATE TABLE charge_adjustments (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  charge_id   uuid NOT NULL REFERENCES charges(id),
  kind        text NOT NULL CHECK (kind IN ('manual','termination','terms_change','penalty_recalc','penalty_manual_set','penalty_writeoff')),
  delta_kop   bigint NOT NULL CHECK (delta_kop <> 0),
  reason      text NOT NULL,
  created_by  uuid NULL REFERENCES users(id),
  created_at  timestamptz NOT NULL DEFAULT now()
);

------------------------------------------------------------------
-- Платежи, распределения, возвраты, залог
------------------------------------------------------------------
CREATE TABLE payments (
  id                     uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id            uuid NOT NULL REFERENCES contracts(id),
  purpose                text NOT NULL CHECK (purpose IN ('charges','deposit')),
  amount_kop             bigint NOT NULL CHECK (amount_kop > 0),
  received_on            date NULL,                 -- дата поступления (кассовый метод); NULL пока pending
  method                 text NOT NULL CHECK (method IN ('online','bank_transfer','cash','deposit_offset')),
  source                 text NOT NULL CHECK (source IN ('online','manual','bank_import','deposit_offset')),
  status                 text NOT NULL CHECK (status IN ('pending','succeeded','failed','cancelled')),
  requested_charge_ids   uuid[] NOT NULL DEFAULT '{}',  -- что выбрал плательщик (онлайн) или собственник (ручной)
  provider_payment_id    text NULL UNIQUE,          -- PaymentId Т-Банка
  provider_status        text NULL,                 -- последний Status от Т-Банка
  payment_url            text NULL,
  payment_url_expires_at timestamptz NULL,
  payer_name             text NULL,
  payer_inn              text NULL,
  payer_account          text NULL,
  bank_purpose           text NULL,                 -- назначение платежа из выписки
  bank_statement_line_id uuid NULL UNIQUE,          -- FK после bank_statement_lines
  receipt_number         text NULL UNIQUE,          -- «КВ-2026-00048», присваивается при succeeded
  comment                text NULL,
  created_by             uuid NULL REFERENCES users(id),
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  cancelled_at           timestamptz NULL,
  cancelled_by           uuid NULL REFERENCES users(id),
  cancel_reason          text NULL,
  CHECK (status <> 'succeeded' OR (received_on IS NOT NULL AND receipt_number IS NOT NULL)),
  CHECK ((source = 'online') = (method = 'online')),
  CHECK ((source = 'deposit_offset') = (method = 'deposit_offset')),
  CHECK (source <> 'deposit_offset' OR purpose = 'charges')
);
CREATE INDEX payments_contract_idx ON payments (contract_id, received_on);
CREATE INDEX payments_received_idx ON payments (received_on) WHERE status = 'succeeded';

CREATE TABLE payment_allocations (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  payment_id       uuid NOT NULL REFERENCES payments(id),
  charge_id        uuid NOT NULL REFERENCES charges(id),
  amount_kop       bigint NOT NULL CHECK (amount_kop > 0),
  allocated_at     timestamptz NOT NULL DEFAULT now(),
  reversed_at      timestamptz NULL,
  reversed_reason  text NULL
);
CREATE INDEX pa_charge_idx ON payment_allocations (charge_id) WHERE reversed_at IS NULL;
CREATE INDEX pa_payment_idx ON payment_allocations (payment_id) WHERE reversed_at IS NULL;

CREATE TABLE refunds (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  payment_id   uuid NOT NULL REFERENCES payments(id),   -- возврат переплаты (аванса) по этому платежу
  amount_kop   bigint NOT NULL CHECK (amount_kop > 0),
  refunded_on  date NOT NULL,
  method       text NOT NULL CHECK (method IN ('bank_transfer','cash')),
  reason       text NOT NULL,
  created_by   uuid NULL REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deposit_transactions (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  contract_id             uuid NOT NULL REFERENCES contracts(id),
  kind                    text NOT NULL CHECK (kind IN ('receipt','withholding','offset','refund')),
  amount_kop              bigint NOT NULL CHECK (amount_kop > 0),
  op_date                 date NOT NULL,
  reason                  text NULL,
  payment_id              uuid NULL REFERENCES payments(id),  -- receipt: платёж-источник; offset: созданный платёж deposit_offset
  from_advance            boolean NOT NULL DEFAULT false,     -- receipt: перенос из аванса (переплаты) платежа payment_id
  maintenance_request_id  uuid NULL,                          -- FK после maintenance_requests
  expense_id              uuid NULL,                          -- FK после expenses
  cancelled_at            timestamptz NULL,
  cancel_reason           text NULL,
  created_by              uuid NULL REFERENCES users(id),
  created_at              timestamptz NOT NULL DEFAULT now(),
  CHECK (kind NOT IN ('withholding','offset') OR reason IS NOT NULL),
  CHECK (kind <> 'offset' OR payment_id IS NOT NULL),
  CHECK (NOT from_advance OR (kind = 'receipt' AND payment_id IS NOT NULL))
);
CREATE INDEX deposit_tx_contract_idx ON deposit_transactions (contract_id);

------------------------------------------------------------------
-- Фискализация и входящие уведомления
------------------------------------------------------------------
CREATE TABLE fiscal_receipts (
  id                          uuid PRIMARY KEY DEFAULT uuidv7(),
  payment_id                  uuid NULL REFERENCES payments(id),
  refund_id                   uuid NULL REFERENCES refunds(id),
  operation                   text NOT NULL CHECK (operation IN ('sell','sell_refund')),
  provider                    text NOT NULL CHECK (provider IN ('atol','npd_manual')),
  status                      text NOT NULL CHECK (status IN ('queued','wait','done','fail','manual_required')),
  external_uuid               text NULL UNIQUE,     -- uuid документа в АТОЛ
  request_payload             jsonb NULL,
  last_response               jsonb NULL,
  total_kop                   bigint NOT NULL CHECK (total_kop > 0),
  fiscal_receipt_number       int NULL,
  shift_number                int NULL,
  receipt_datetime            timestamptz NULL,
  fn_number                   text NULL,
  fiscal_document_number      bigint NULL,
  fiscal_document_attribute   bigint NULL,
  ofd_receipt_url             text NULL,
  npd_receipt_url             text NULL,            -- ссылка на чек «Мой налог», вводится вручную
  error_code                  text NULL,
  error_text                  text NULL,
  attempts                    int NOT NULL DEFAULT 0,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  updated_at                  timestamptz NOT NULL DEFAULT now(),
  CHECK ((payment_id IS NOT NULL) <> (refund_id IS NOT NULL)),
  CHECK ((operation = 'sell') = (payment_id IS NOT NULL))
);
CREATE UNIQUE INDEX fiscal_payment_uq ON fiscal_receipts (payment_id) WHERE payment_id IS NOT NULL;
CREATE UNIQUE INDEX fiscal_refund_uq ON fiscal_receipts (refund_id) WHERE refund_id IS NOT NULL;

CREATE TABLE webhook_events (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  provider      text NOT NULL CHECK (provider IN ('tbank','atol')),
  event_key     text NOT NULL,                      -- tbank: PaymentId:Status; atol: uuid:status
  payload       jsonb NOT NULL,
  received_at   timestamptz NOT NULL DEFAULT now(),
  processed_at  timestamptz NULL,
  result        text NULL CHECK (result IN ('processed','ignored','error')),
  error_text    text NULL,
  UNIQUE (provider, event_key)
);

------------------------------------------------------------------
-- Выписки банка и расхождения
------------------------------------------------------------------
CREATE TABLE bank_statements (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  file_id          uuid NOT NULL REFERENCES files(id),
  format           text NOT NULL CHECK (format IN ('1c','csv')),
  account_number   text NULL,
  date_from        date NULL,
  date_to          date NULL,
  lines_total      int NOT NULL DEFAULT 0,
  lines_incoming   int NOT NULL DEFAULT 0,
  lines_matched    int NOT NULL DEFAULT 0,
  lines_unmatched  int NOT NULL DEFAULT 0,
  lines_duplicate  int NOT NULL DEFAULT 0,
  uploaded_by      uuid NULL REFERENCES users(id),
  uploaded_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE bank_statement_lines (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  statement_id   uuid NOT NULL REFERENCES bank_statements(id),
  line_no        int NOT NULL,
  doc_number     text NULL,
  doc_date       date NULL,
  received_on    date NOT NULL,
  amount_kop     bigint NOT NULL CHECK (amount_kop > 0),
  payer_name     text NULL,
  payer_inn      text NULL,
  payer_account  text NULL,
  purpose        text NULL,
  fingerprint    text NOT NULL,                     -- sha256(received_on|amount|doc_number|payer_account|purpose)
  status         text NOT NULL CHECK (status IN ('matched','possible_duplicate','unmatched','duplicate','linked_manual','refunded','ignored')),
  match_method   text NULL CHECK (match_method IN ('contract_number','payer_inn','payer_name','manual')),
  contract_id    uuid NULL REFERENCES contracts(id),
  payment_id     uuid NULL REFERENCES payments(id),
  note           text NULL,
  UNIQUE (statement_id, line_no)
);
CREATE UNIQUE INDEX bsl_fingerprint_uq ON bank_statement_lines (fingerprint) WHERE status <> 'duplicate';
ALTER TABLE payments ADD CONSTRAINT payments_bsl_fk FOREIGN KEY (bank_statement_line_id) REFERENCES bank_statement_lines(id);

CREATE TABLE discrepancies (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  kind                    text NOT NULL CHECK (kind IN ('underpayment','overpayment','unidentified')),
  status                  text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  contract_id             uuid NULL REFERENCES contracts(id),
  payment_id              uuid NULL REFERENCES payments(id),
  bank_statement_line_id  uuid NULL REFERENCES bank_statement_lines(id),
  amount_kop              bigint NOT NULL CHECK (amount_kop > 0),
  detected_at             timestamptz NOT NULL DEFAULT now(),
  resolution              text NULL CHECK (resolution IN ('linked','refunded','kept_as_advance','moved_to_deposit','acknowledged','ignored','auto_closed')),
  resolution_note         text NULL,
  resolved_by             uuid NULL REFERENCES users(id),
  resolved_at             timestamptz NULL,
  CHECK (kind <> 'unidentified' OR bank_statement_line_id IS NOT NULL),
  CHECK (kind <> 'overpayment' OR payment_id IS NOT NULL),
  CHECK (kind <> 'underpayment' OR contract_id IS NOT NULL),
  CHECK (status <> 'resolved' OR resolution IS NOT NULL)
);
CREATE UNIQUE INDEX disc_under_uq ON discrepancies (contract_id) WHERE kind = 'underpayment' AND status = 'open';
CREATE UNIQUE INDEX disc_over_uq ON discrepancies (payment_id) WHERE kind = 'overpayment';
CREATE UNIQUE INDEX disc_unident_uq ON discrepancies (bank_statement_line_id) WHERE kind = 'unidentified';

------------------------------------------------------------------
-- Заявки и затраты
------------------------------------------------------------------
CREATE TABLE maintenance_requests (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  number        text NOT NULL UNIQUE,               -- «З-2026-014»
  property_id   uuid NOT NULL REFERENCES properties(id),
  contract_id   uuid NULL REFERENCES contracts(id),
  author_id     uuid NOT NULL REFERENCES users(id),
  author_role   text NOT NULL CHECK (author_role IN ('owner','tenant')),
  category      text NOT NULL CHECK (category IN ('plumbing','electrical','heating','appliance','furniture','doors_windows','other')),
  title         text NOT NULL CHECK (length(title) BETWEEN 3 AND 120),
  description   text NOT NULL CHECK (length(description) <= 4000),
  priority      text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high')),
  status        text NOT NULL DEFAULT 'new' CHECK (status IN ('new','in_progress','done','closed')),
  contractor    text NULL,
  planned_date  date NULL,
  result_note   text NULL,
  tenant_fault  boolean NOT NULL DEFAULT false,
  done_at       timestamptz NULL,
  closed_at     timestamptz NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE maintenance_request_photos (
  request_id  uuid NOT NULL REFERENCES maintenance_requests(id),
  file_id     uuid NOT NULL REFERENCES files(id),
  PRIMARY KEY (request_id, file_id)
);
CREATE TABLE maintenance_request_events (           -- лента: смены статуса и комментарии
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  request_id   uuid NOT NULL REFERENCES maintenance_requests(id),
  author_id    uuid NULL REFERENCES users(id),
  kind         text NOT NULL CHECK (kind IN ('status','comment')),
  from_status  text NULL,
  to_status    text NULL,
  text         text NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE expenses (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  property_id             uuid NOT NULL REFERENCES properties(id),
  category                text NOT NULL CHECK (category IN ('repair','maintenance','utilities_owner','tax','insurance','furniture_appliances','commission','other')),
  amount_kop              bigint NOT NULL CHECK (amount_kop > 0),
  paid_on                 date NOT NULL,
  period_month            date NOT NULL CHECK (extract(day FROM period_month) = 1),
  description             text NOT NULL,
  counterparty            text NULL,
  file_id                 uuid NULL REFERENCES files(id),     -- чек, акт подрядчика
  maintenance_request_id  uuid NULL REFERENCES maintenance_requests(id),
  recovery                text NOT NULL DEFAULT 'none' CHECK (recovery IN ('none','deposit','charge')),  -- за счёт нанимателя
  deposit_transaction_id  uuid NULL REFERENCES deposit_transactions(id),
  charge_id               uuid NULL REFERENCES charges(id),
  created_by              uuid NULL REFERENCES users(id),
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CHECK (recovery <> 'deposit' OR deposit_transaction_id IS NOT NULL),
  CHECK (recovery <> 'charge' OR charge_id IS NOT NULL)
);
CREATE INDEX expenses_property_idx ON expenses (property_id, paid_on);
ALTER TABLE deposit_transactions ADD CONSTRAINT dt_req_fk FOREIGN KEY (maintenance_request_id) REFERENCES maintenance_requests(id);
ALTER TABLE deposit_transactions ADD CONSTRAINT dt_exp_fk FOREIGN KEY (expense_id) REFERENCES expenses(id);

------------------------------------------------------------------
-- Календарь
------------------------------------------------------------------
CREATE TABLE calendar_events (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  kind         text NOT NULL CHECK (kind IN ('contract_end','contract_end_notice','registration','indexation',
                 'meter_verification','showing','repair','custom')),
  title        text NOT NULL,
  starts_on    date NOT NULL,
  start_time   time NULL,
  end_time     time NULL,
  property_id  uuid NULL REFERENCES properties(id),
  contract_id  uuid NULL REFERENCES contracts(id),
  source       text NOT NULL CHECK (source IN ('auto','manual')),
  auto_key     text NULL UNIQUE,                    -- 'contract_end:<id>', 'contract_end_notice:<id>:90' и т.п.
  done         boolean NOT NULL DEFAULT false,
  done_at      timestamptz NULL,
  notes        text NULL,
  created_by   uuid NULL REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CHECK ((source = 'auto') = (auto_key IS NOT NULL)),
  CHECK (end_time IS NULL OR start_time IS NOT NULL)
);
CREATE INDEX calendar_events_date_idx ON calendar_events (starts_on);

------------------------------------------------------------------
-- Письма
------------------------------------------------------------------
CREATE TABLE email_templates (
  code        text PRIMARY KEY,                     -- список кодов в «10 Уведомления»
  name        text NOT NULL,
  recipient   text NOT NULL CHECK (recipient IN ('tenant','owner','user')),
  subject     text NOT NULL,
  body_html   text NOT NULL,
  enabled     boolean NOT NULL DEFAULT true,
  updated_by  uuid NULL REFERENCES users(id),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE email_log (
  id                   uuid PRIMARY KEY DEFAULT uuidv7(),
  template_code        text NOT NULL REFERENCES email_templates(code),
  to_email             citext NOT NULL,
  subject              text NOT NULL,
  body_html            text NOT NULL,
  attachments          jsonb NOT NULL DEFAULT '[]',  -- [{"fileId":…, "name":"Квитанция КВ-2026-00048.pdf"}]
  status               text NOT NULL CHECK (status IN ('queued','sent','failed','skipped')),
  attempts             int NOT NULL DEFAULT 0,
  last_error           text NULL,
  dedupe_key           text NOT NULL UNIQUE,
  related_entity_type  text NULL,
  related_entity_id    uuid NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  sent_at              timestamptz NULL
);

------------------------------------------------------------------
-- Шаблоны документов и документы
------------------------------------------------------------------
CREATE TABLE document_templates (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  kind        text NOT NULL CHECK (kind IN ('contract_ch35','contract_ch34','act_handover','act_return','amendment','payment_receipt','reconciliation_act','pdn_consent')),
  name        text NOT NULL,
  body_html   text NOT NULL,
  version     int NOT NULL,
  is_active   boolean NOT NULL DEFAULT true,
  is_system   boolean NOT NULL DEFAULT false,       -- поставляется с системой (для «Сбросить к стандартному»)
  created_by  uuid NULL REFERENCES users(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, version)
);
CREATE UNIQUE INDEX doc_tpl_active_uq ON document_templates (kind) WHERE is_active;

CREATE TABLE documents (
  id                   uuid PRIMARY KEY DEFAULT uuidv7(),
  kind                 text NOT NULL CHECK (kind IN ('contract_ch35','contract_ch34','act_handover','act_return','amendment','payment_receipt','reconciliation_act','pdn_consent')),
  number               text NULL,
  title                text NOT NULL,               -- «Договор найма № 2026-007 от 05.03.2026»
  contract_id          uuid NULL REFERENCES contracts(id),
  tenant_id            uuid NULL REFERENCES tenants(id),   -- для pdn_consent (без договора)
  payment_id           uuid NULL REFERENCES payments(id),
  amendment_id         uuid NULL REFERENCES contract_amendments(id),
  act_id               uuid NULL REFERENCES contract_acts(id),
  template_id          uuid NOT NULL REFERENCES document_templates(id),
  file_id              uuid NOT NULL REFERENCES files(id),
  html_snapshot        text NOT NULL,
  data_snapshot        jsonb NOT NULL,
  period_from          date NULL,                   -- для акта сверки
  period_to            date NULL,
  visible_to_tenant    boolean NOT NULL DEFAULT true,
  signed_scan_file_id  uuid NULL REFERENCES files(id),
  superseded_by        uuid NULL REFERENCES documents(id),  -- при повторном формировании
  created_by           uuid NULL REFERENCES users(id),
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_contract_idx ON documents (contract_id);
CREATE INDEX documents_tenant_idx ON documents (tenant_id) WHERE tenant_id IS NOT NULL;
ALTER TABLE pdn_consents ADD CONSTRAINT consents_doc_fk FOREIGN KEY (document_id) REFERENCES documents(id);
ALTER TABLE contract_acts ADD CONSTRAINT acts_doc_fk FOREIGN KEY (document_id) REFERENCES documents(id);

------------------------------------------------------------------
-- Нумерация, служебное
------------------------------------------------------------------
CREATE TABLE number_sequences (
  name        text NOT NULL CHECK (name IN ('contract','receipt','request','reconciliation_act','pdn_consent')),
  year        int NOT NULL,
  last_value  int NOT NULL DEFAULT 0,
  PRIMARY KEY (name, year)
);

CREATE TABLE job_runs (                             -- история запусков плановых задач (экран «Служебное»)
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  job_name     text NOT NULL,
  business_date date NOT NULL,
  trigger      text NOT NULL CHECK (trigger IN ('schedule','manual')),
  started_at   timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz NULL,
  status       text NOT NULL CHECK (status IN ('running','succeeded','failed')),
  stats        jsonb NOT NULL DEFAULT '{}',          -- {"contracts":12,"created":8,"skipped":4}
  error_text   text NULL
);
CREATE INDEX job_runs_name_idx ON job_runs (job_name, started_at DESC);

CREATE TABLE demo_clock (
  id              smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  offset_seconds  bigint NOT NULL DEFAULT 0,
  updated_at      timestamptz NOT NULL DEFAULT now()
);

------------------------------------------------------------------
-- Журнал действий (FR-12, NFR-Sec-04)
------------------------------------------------------------------
CREATE TABLE audit_log (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  occurred_at    timestamptz NOT NULL DEFAULT now(),
  actor_user_id  uuid NULL,                         -- без FK: запись живёт дольше пользователя
  actor_role     text NOT NULL CHECK (actor_role IN ('owner','tenant','system')),
  action         text NOT NULL,                     -- «contract.activate» (полный список — 12 Безопасность и ПДн)
  entity_type    text NOT NULL,
  entity_id      text NOT NULL,
  summary        text NOT NULL,                     -- человекочитаемое описание на русском
  before         jsonb NULL,
  after          jsonb NULL,
  ip             inet NULL,
  user_agent     text NULL,
  request_id     text NULL,
  prev_hash      bytea NOT NULL,
  hash           bytea NOT NULL
);
CREATE INDEX audit_entity_idx ON audit_log (entity_type, entity_id);
CREATE INDEX audit_time_idx ON audit_log (occurred_at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_log_chain() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE last_hash bytea;
BEGIN
  PERFORM pg_advisory_xact_lock(7428001);
  SELECT hash INTO last_hash FROM audit_log ORDER BY id DESC LIMIT 1;
  NEW.prev_hash := COALESCE(last_hash, '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea);
  NEW.hash := digest(NEW.prev_hash || convert_to(concat_ws('|',
      NEW.id::text,
      to_char(NEW.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      COALESCE(NEW.actor_user_id::text, ''), NEW.actor_role, NEW.action, NEW.entity_type, NEW.entity_id,
      NEW.summary, COALESCE(NEW.before::text, ''), COALESCE(NEW.after::text, '')), 'UTF8'), 'sha256');
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER audit_log_chain_trg BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION audit_log_chain();

-- +goose StatementBegin
CREATE FUNCTION audit_log_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Журнал действий не может быть изменён или удалён'; END $$;
-- +goose StatementEnd
CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit_log FOR EACH STATEMENT EXECUTE FUNCTION audit_log_immutable();

------------------------------------------------------------------
-- Триггеры updated_at (для всех таблиц со столбцом updated_at)
------------------------------------------------------------------
CREATE TRIGGER tenants_updated BEFORE UPDATE ON tenants FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER users_updated BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER organization_settings_updated BEFORE UPDATE ON organization_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER properties_updated BEFORE UPDATE ON properties FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER meters_updated BEFORE UPDATE ON meters FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER contracts_updated BEFORE UPDATE ON contracts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER contract_amendments_updated BEFORE UPDATE ON contract_amendments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER contract_acts_updated BEFORE UPDATE ON contract_acts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER charges_updated BEFORE UPDATE ON charges FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER payments_updated BEFORE UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER fiscal_receipts_updated BEFORE UPDATE ON fiscal_receipts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER maintenance_requests_updated BEFORE UPDATE ON maintenance_requests FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER expenses_updated BEFORE UPDATE ON expenses FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER calendar_events_updated BEFORE UPDATE ON calendar_events FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER email_templates_updated BEFORE UPDATE ON email_templates FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER demo_clock_updated BEFORE UPDATE ON demo_clock FOR EACH ROW EXECUTE FUNCTION set_updated_at();

------------------------------------------------------------------
-- Описание таблиц и ключевых столбцов (используется в docs/database.md)
------------------------------------------------------------------
COMMENT ON TABLE files IS 'Загруженные и сформированные файлы (фото, сканы, PDF, выписки); содержимое — в каталоге FILES_DIR';
COMMENT ON COLUMN files.storage_key IS 'Путь файла относительно FILES_DIR: ГГГГ/ММ/<uuid>.<расширение>';
COMMENT ON COLUMN files.sha256 IS 'Контрольная сумма содержимого';
COMMENT ON COLUMN files.deleted_at IS 'Файл удалён с диска (очистка или обезличивание)';

COMMENT ON TABLE tenants IS 'Наниматели (арендаторы): физические и юридические лица (FR-02)';
COMMENT ON COLUMN tenants.kind IS 'individual — физлицо, legal — юрлицо';
COMMENT ON COLUMN tenants.doc_number_enc IS 'Серия и номер документа, AES-256-GCM (NFR-Sec-03)';
COMMENT ON COLUMN tenants.doc_issued_by_enc IS 'Кем выдан документ, AES-256-GCM';
COMMENT ON COLUMN tenants.doc_division_code_enc IS 'Код подразделения, AES-256-GCM';
COMMENT ON COLUMN tenants.registration_address_enc IS 'Адрес регистрации, AES-256-GCM';
COMMENT ON COLUMN tenants.anonymized_at IS 'Момент обезличивания (NFR-Sec-05)';

COMMENT ON TABLE users IS 'Учётные записи собственников и нанимателей';
COMMENT ON COLUMN users.password_hash IS 'Хеш argon2id в формате PHC; NULL — приглашение не принято';
COMMENT ON COLUMN users.tenant_id IS 'Наниматель, которому принадлежит учётная запись (только роль tenant)';
COMMENT ON COLUMN users.policy_accepted_version IS 'Версия политики обработки ПДн, с которой пользователь ознакомлен (не согласие)';

COMMENT ON TABLE sessions IS 'Сессии входа; значение cookie хранится как sha256';
COMMENT ON COLUMN sessions.expires_at IS 'Абсолютный срок сессии';
COMMENT ON TABLE auth_tokens IS 'Одноразовые токены приглашения и восстановления пароля (хранится sha256)';
COMMENT ON TABLE login_attempts IS 'Попытки входа для ограничения частоты (NFR-Sec-01)';
COMMENT ON TABLE pdn_consents IS 'Отдельные согласия на обработку ПДн для цели вне договора (ст. 9 152-ФЗ)';

COMMENT ON TABLE organization_settings IS 'Реквизиты и настройки организации (одна строка, FR-14.1)';
COMMENT ON COLUMN organization_settings.tax_regime IS 'Режим налогообложения: usn_income, usn_income_expense, npd, psn, osn';
COMMENT ON COLUMN organization_settings.pdn_retention_years IS 'Срок хранения ПДн после окончания последнего договора, лет';
COMMENT ON COLUMN organization_settings.contract_end_notice_days IS 'За сколько дней до окончания договора уведомлять нанимателя';

COMMENT ON TABLE properties IS 'Объекты недвижимости (FR-01)';
COMMENT ON COLUMN properties.code IS 'Короткое название объекта («Студия 1»)';
COMMENT ON COLUMN properties.expected_rent_kop IS 'Ожидаемая плата в месяц, коп. (для недополученного дохода)';
COMMENT ON COLUMN properties.fiscal_mode IS 'Режим чека: none, cloud_kkt, npd_manual';
COMMENT ON COLUMN properties.status IS 'Текущий статус: free, reserved, rented, repair';
COMMENT ON TABLE property_status_history IS 'История статусов объекта (FR-01.2); интервалы не пересекаются';
COMMENT ON COLUMN property_status_history.valid_to IS 'Последний день включительно; NULL — текущая запись';
COMMENT ON TABLE property_photos IS 'Фотографии объекта';
COMMENT ON TABLE property_inventory_items IS 'Опись имущества объекта (копируется в акт приёма-передачи)';
COMMENT ON TABLE meters IS 'Приборы учёта объекта';
COMMENT ON COLUMN meters.verification_due IS 'Дата следующей поверки';
COMMENT ON TABLE tariffs IS 'Тарифы на ресурсы с даты; property_id NULL — для всех объектов';

COMMENT ON TABLE contracts IS 'Договоры найма (гл. 35 ГК РФ) и аренды (гл. 34 ГК РФ) (FR-03)';
COMMENT ON COLUMN contracts.number IS 'Номер договора ГГГГ-NNN';
COMMENT ON COLUMN contracts.legal_basis IS 'ch35 — договор найма, ch34 — договор аренды';
COMMENT ON COLUMN contracts.end_date IS 'Последний день срока включительно';
COMMENT ON COLUMN contracts.termination_date IS 'Последний день пользования при расторжении';
COMMENT ON COLUMN contracts.actual_end_date IS 'Фактический конец: дата расторжения или окончания';
COMMENT ON COLUMN contracts.registration_required IS 'Срок от года — требуется государственная регистрация';
COMMENT ON COLUMN contracts.is_short_term IS 'Краткосрочный наём (ст. 683 ГК РФ)';
COMMENT ON COLUMN contracts.first_due_date IS 'Срок оплаты первого периода';
COMMENT ON COLUMN contracts.deposit_kop IS 'Обеспечительный платёж по договору, коп.';
COMMENT ON COLUMN contracts.penalty_rate_pct IS 'Ставка пеней, % в день';
COMMENT ON COLUMN contracts.next_indexation_on IS 'Дата следующей индексации платы';
COMMENT ON TABLE contract_amendments IS 'Дополнительные соглашения к договору';
COMMENT ON TABLE contract_terms IS 'Версии условий договора: плата, фиксированная часть ЖКУ, день оплаты';
COMMENT ON COLUMN contract_terms.effective_from IS 'Дата вступления версии в силу (для версий > 1 — первое число месяца)';
COMMENT ON TABLE contract_occupants IS 'Граждане, постоянно проживающие с нанимателем (ст. 677 ГК РФ)';
COMMENT ON TABLE contract_acts IS 'Акты приёма-передачи и возврата помещения (FR-03.4)';

COMMENT ON TABLE meter_readings IS 'Показания приборов учёта по месяцам (FR-05.5)';
COMMENT ON COLUMN meter_readings.period_month IS 'Месяц потребления (первое число)';

COMMENT ON TABLE charges IS 'Начисления: плата, ЖКУ, пени, возмещение ущерба, прочее (FR-05)';
COMMENT ON COLUMN charges.amount_kop IS 'Текущая сумма начисления, коп.';
COMMENT ON COLUMN charges.original_amount_kop IS 'Сумма при создании, коп.';
COMMENT ON COLUMN charges.paid_kop IS 'Сумма активных распределений, коп.';
COMMENT ON COLUMN charges.penalty_state IS 'Состояние пеней: accruing, final, manual';
COMMENT ON COLUMN charges.source_charge_id IS 'Начисление, по которому считаются пени';
COMMENT ON COLUMN charges.idempotency_key IS 'Ключ автоначисления (rent:<договор>:ГГГГ-ММ и т.п.)';
COMMENT ON TABLE charge_adjustments IS 'Корректировки сумм начислений';

COMMENT ON TABLE payments IS 'Платежи (поступления) по договорам (FR-06)';
COMMENT ON COLUMN payments.received_on IS 'Дата поступления (кассовый метод)';
COMMENT ON COLUMN payments.requested_charge_ids IS 'Начисления, выбранные плательщиком для погашения';
COMMENT ON COLUMN payments.provider_payment_id IS 'PaymentId платёжного сервиса';
COMMENT ON COLUMN payments.receipt_number IS 'Номер квитанции КВ-ГГГГ-NNNNN';
COMMENT ON TABLE payment_allocations IS 'Распределение платежей по начислениям (FR-07.1)';
COMMENT ON TABLE refunds IS 'Возвраты переплаты (аванса) по платежам';
COMMENT ON TABLE deposit_transactions IS 'Операции с обеспечительным платежом (FR-05.2)';
COMMENT ON COLUMN deposit_transactions.from_advance IS 'Внесение за счёт переплаты платежа payment_id';

COMMENT ON TABLE fiscal_receipts IS 'Кассовые чеки 54-ФЗ и чеки НПД (FR-06.2)';
COMMENT ON COLUMN fiscal_receipts.external_uuid IS 'Идентификатор документа в сервисе фискализации';
COMMENT ON TABLE webhook_events IS 'Входящие уведомления платёжного сервиса и кассы (идемпотентность, NFR-Rel-02)';

COMMENT ON TABLE bank_statements IS 'Загруженные выписки банка (FR-07.2)';
COMMENT ON TABLE bank_statement_lines IS 'Строки выписок банка';
COMMENT ON COLUMN bank_statement_lines.fingerprint IS 'Отпечаток строки для поиска дублей';
COMMENT ON TABLE discrepancies IS 'Расхождения: недоплата, переплата, неопознанный платёж (FR-07.3)';

COMMENT ON TABLE maintenance_requests IS 'Заявки на обслуживание (FR-08)';
COMMENT ON TABLE maintenance_request_photos IS 'Фотографии к заявкам';
COMMENT ON TABLE maintenance_request_events IS 'Лента заявки: смены статуса и комментарии';
COMMENT ON TABLE expenses IS 'Затраты собственника по объектам (FR-05.6)';
COMMENT ON COLUMN expenses.recovery IS 'Возмещение за счёт нанимателя: none, deposit, charge';

COMMENT ON TABLE calendar_events IS 'Хранимые события календаря: автоматические и ручные (FR-09)';
COMMENT ON COLUMN calendar_events.auto_key IS 'Ключ автособытия (contract_end:<id> и т.п.)';

COMMENT ON TABLE email_templates IS 'Шаблоны писем (FR-10.3)';
COMMENT ON TABLE email_log IS 'Журнал отправки писем (FR-10)';
COMMENT ON COLUMN email_log.dedupe_key IS 'Ключ, исключающий повторную отправку одного и того же письма';

COMMENT ON TABLE document_templates IS 'Шаблоны документов по видам и версиям (FR-04.1)';
COMMENT ON TABLE documents IS 'Сформированные документы (FR-04.2)';
COMMENT ON COLUMN documents.superseded_by IS 'Документ, сформированный взамен этого';

COMMENT ON TABLE number_sequences IS 'Счётчики номеров документов по годам';
COMMENT ON TABLE job_runs IS 'История запусков плановых задач';
COMMENT ON TABLE demo_clock IS 'Сдвиг системной даты в демонстрационном режиме (одна строка)';

COMMENT ON TABLE audit_log IS 'Неизменяемый журнал действий с цепочкой хешей SHA-256 (FR-12, NFR-Sec-04)';
COMMENT ON COLUMN audit_log.action IS 'Код действия (contract.activate и т.п.)';
COMMENT ON COLUMN audit_log.summary IS 'Описание действия на русском без персональных данных';
COMMENT ON COLUMN audit_log.prev_hash IS 'Хеш предыдущей записи';
COMMENT ON COLUMN audit_log.hash IS 'sha256(prev_hash || поля записи)';

------------------------------------------------------------------
-- Права роли приложения (выполняется под landlord_owner)
------------------------------------------------------------------
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO landlord_app;
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM landlord_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO landlord_app;

-- +goose Down
DROP TABLE IF EXISTS
  audit_log, demo_clock, job_runs, number_sequences, documents, document_templates, email_log, email_templates,
  calendar_events, expenses, maintenance_request_events, maintenance_request_photos, maintenance_requests,
  discrepancies, bank_statement_lines, bank_statements, webhook_events, fiscal_receipts, deposit_transactions,
  refunds, payment_allocations, payments, charge_adjustments, charges, meter_readings, contract_acts,
  contract_occupants, contract_terms, contract_amendments, contracts, tariffs, meters, property_inventory_items,
  property_photos, property_status_history, properties, organization_settings, pdn_consents, login_attempts,
  auth_tokens, sessions, users, tenants, files
  CASCADE;
DROP FUNCTION IF EXISTS audit_log_immutable();
DROP FUNCTION IF EXISTS audit_log_chain();
DROP FUNCTION IF EXISTS set_updated_at();
DROP EXTENSION IF EXISTS pgcrypto;
DROP EXTENSION IF EXISTS btree_gist;
DROP EXTENSION IF EXISTS citext;
