// Package tenant — наниматели (физлица и юрлица), шифрование и маскирование ПДн, отдельные согласия,
// приглашение в личный кабинет (FR-02, NFR-Sec-03).
package tenant

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/settings"
)

// Виды нанимателя.
const (
	KindIndividual = "individual"
	KindLegal      = "legal"
)

// Зашифрованные поля (AAD = tenant:<id>:<поле>).
const (
	fieldDocNumber    = "doc_number"
	fieldDocIssuedBy  = "doc_issued_by"
	fieldDivisionCode = "doc_division_code"
	fieldRegAddress   = "registration_address"
)

// Service — наниматели.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	cipher   *crypto.Cipher
	auth     *auth.Service
	settings *settings.Service
	files    *file.Service
}

// NewService создаёт сервис нанимателей.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, c *crypto.Cipher, as *auth.Service,
	st *settings.Service, fs *file.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, cipher: c, auth: as, settings: st, files: fs}
}

// Input — поля нанимателя. Поля документа и адреса регистрации — только запись:
// nil — не менять, "" — очистить.
type Input struct {
	Kind                                                string
	LastName, FirstName, MiddleName                     *string
	BirthDate                                           *date.Date
	DocType                                             *string
	DocNumber, DocIssuedBy, DocDivisionCode, RegAddress *string
	DocIssuedOn                                         *date.Date
	OrgName, Inn, Kpp, Ogrn, LegalAddress               *string
	SignatoryName, SignatoryPosition, SignatoryBasis    *string
	Phone, Email, Note                                  *string
}

func trim(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func empty(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }

// normalize проверяет поля по виду лица (05 разд. 3, 14 разд. 3.2).
func (in *Input) normalize(today date.Date) error {
	for _, p := range []**string{&in.LastName, &in.FirstName, &in.MiddleName, &in.DocType, &in.OrgName, &in.Inn, &in.Kpp,
		&in.Ogrn, &in.LegalAddress, &in.SignatoryName, &in.SignatoryPosition, &in.SignatoryBasis, &in.Phone, &in.Email, &in.Note} {
		*p = trim(*p)
	}
	var errs []apperr.FieldError
	add := func(field, msg string) { errs = append(errs, apperr.FieldError{Field: field, Message: msg}) }
	switch in.Kind {
	case KindIndividual:
		if in.LastName == nil {
			add("lastName", "Укажите фамилию")
		}
		if in.FirstName == nil {
			add("firstName", "Укажите имя")
		}
		if in.BirthDate != nil && in.BirthDate.After(today) {
			add("birthDate", "Дата рождения не может быть в будущем")
		}
		if in.Inn != nil && (len(*in.Inn) != 12 || !ValidINN(*in.Inn)) {
			add("inn", "Неверный ИНН")
		}
		if in.Phone == nil && in.Email == nil {
			add("phone", "Укажите телефон или e-mail")
		}
		// Реквизиты юрлица у физлица не хранятся.
		in.OrgName, in.Kpp, in.Ogrn, in.LegalAddress = nil, nil, nil, nil
		in.SignatoryName, in.SignatoryPosition, in.SignatoryBasis = nil, nil, nil
	case KindLegal:
		if in.OrgName == nil {
			add("orgName", "Укажите наименование")
		}
		if in.Inn == nil {
			add("inn", "Укажите ИНН")
		} else if len(*in.Inn) != 10 || !ValidINN(*in.Inn) {
			add("inn", "Неверный ИНН")
		}
		if in.LegalAddress == nil {
			add("legalAddress", "Укажите юридический адрес")
		}
		if in.SignatoryName == nil {
			add("signatoryName", "Укажите подписанта")
		}
		if in.SignatoryPosition == nil {
			add("signatoryPosition", "Укажите должность подписанта")
		}
		if in.SignatoryBasis == nil {
			add("signatoryBasis", "Укажите основание полномочий")
		}
		// Документ и адрес регистрации — только у физлица.
		in.LastName, in.FirstName, in.MiddleName, in.BirthDate, in.DocType, in.DocIssuedOn = nil, nil, nil, nil, nil, nil
		blank := ""
		in.DocNumber, in.DocIssuedBy, in.DocDivisionCode, in.RegAddress = &blank, &blank, &blank, &blank
	default:
		add("kind", "Укажите тип нанимателя")
	}
	if in.Phone != nil {
		p, ok := NormalizePhone(*in.Phone)
		if !ok {
			add("phone", "Телефон — 10 цифр после +7")
		} else {
			in.Phone = &p
		}
	}
	if in.Email != nil {
		e := strings.ToLower(*in.Email)
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			add("email", "Неверный формат e-mail")
		}
		in.Email = &e
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// encrypt — значение поля для БД: nil (не менять) → прежнее значение, "" → NULL, иначе шифротекст.
func (s *Service) encrypt(id uuid.UUID, field string, v *string, prev []byte) ([]byte, error) {
	if v == nil {
		return prev, nil
	}
	return s.cipher.Encrypt(strings.TrimSpace(*v), crypto.TenantAAD(id.String(), field))
}

func (s *Service) decrypt(t sqlc.Tenant, field string, v []byte) (string, error) {
	return s.cipher.Decrypt(v, crypto.TenantAAD(t.ID.String(), field))
}

// snapshot — наниматель для журнала: ПДн маскируются журналом, документ — признаком «задан».
func snapshot(t sqlc.Tenant) map[string]any {
	set := func(b []byte) any {
		if len(b) == 0 {
			return nil
		}
		return "задан"
	}
	return map[string]any{
		"kind": t.Kind, "lastName": t.LastName, "firstName": t.FirstName, "middleName": t.MiddleName,
		"birthDate": t.BirthDate, "docType": t.DocType, "docNumber": set(t.DocNumberEnc), "docIssuedBy": set(t.DocIssuedByEnc),
		"docIssuedOn": t.DocIssuedOn, "docDivisionCode": set(t.DocDivisionCodeEnc),
		"registrationAddress": set(t.RegistrationAddressEnc), "orgName": t.OrgName, "inn": t.Inn, "kpp": t.Kpp, "ogrn": t.Ogrn,
		"legalAddress": t.LegalAddress, "signatoryName": t.SignatoryName, "signatoryPosition": t.SignatoryPosition,
		"signatoryBasis": t.SignatoryBasis, "phone": t.Phone, "email": t.Email, "note": t.Note,
	}
}

// changedFields — имена изменённых полей (в журнале значения ПДн скрыты, но видно, что именно менялось).
func changedFields(before, after map[string]any) []string {
	var out []string
	for k, a := range after {
		if fmt.Sprint(deref(a)) != fmt.Sprint(deref(before[k])) {
			out = append(out, k)
		}
	}
	return out
}

func deref(v any) any {
	switch x := v.(type) {
	case *string:
		if x == nil {
			return nil
		}
		return *x
	case *date.Date:
		if x == nil {
			return nil
		}
		return x.String()
	}
	return v
}

// summaryName — наниматель в описании журнала: без имени (12 разд. 5).
func summaryName(t sqlc.Tenant) string {
	if t.Kind == KindLegal && t.OrgName != nil {
		return "арендатор-юрлицо"
	}
	return "наниматель"
}

// ConsentOnCreate — отметка о получении отдельного бумажного согласия при создании.
type ConsentOnCreate struct {
	GivenOn date.Date
	FileID  *uuid.UUID
}

// Create — новый наниматель; документ и адрес регистрации шифруются.
func (s *Service) Create(ctx context.Context, in Input, consent *ConsentOnCreate) (sqlc.Tenant, error) {
	if err := in.normalize(s.clock.Today()); err != nil {
		return sqlc.Tenant{}, err
	}
	id := uuid.Must(uuid.NewV7())
	p := sqlc.CreateTenantParams{
		ID: id, Kind: in.Kind, LastName: in.LastName, FirstName: in.FirstName, MiddleName: in.MiddleName,
		BirthDate: in.BirthDate, DocType: in.DocType, DocIssuedOn: in.DocIssuedOn, OrgName: in.OrgName, Inn: in.Inn,
		Kpp: in.Kpp, Ogrn: in.Ogrn, LegalAddress: in.LegalAddress, SignatoryName: in.SignatoryName,
		SignatoryPosition: in.SignatoryPosition, SignatoryBasis: in.SignatoryBasis, Phone: in.Phone, Email: in.Email,
		Note: in.Note, Now: s.clock.Now(),
	}
	var err error
	if p.DocNumberEnc, err = s.encrypt(id, fieldDocNumber, in.DocNumber, nil); err != nil {
		return sqlc.Tenant{}, err
	}
	if p.DocIssuedByEnc, err = s.encrypt(id, fieldDocIssuedBy, in.DocIssuedBy, nil); err != nil {
		return sqlc.Tenant{}, err
	}
	if p.DocDivisionCodeEnc, err = s.encrypt(id, fieldDivisionCode, in.DocDivisionCode, nil); err != nil {
		return sqlc.Tenant{}, err
	}
	if p.RegistrationAddressEnc, err = s.encrypt(id, fieldRegAddress, in.RegAddress, nil); err != nil {
		return sqlc.Tenant{}, err
	}
	var t sqlc.Tenant
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		if t, err = tx.Q.CreateTenant(ctx, p); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tenant.create", EntityType: "tenant", EntityID: id.String(),
			Summary: "Добавлен " + summaryName(t), After: snapshot(t),
		}); err != nil {
			return err
		}
		if consent != nil {
			if t.Kind != KindIndividual {
				return apperr.Validation(apperr.FieldError{Field: "consent", Message: "Согласие на обработку ПДн оформляется только для физлица"})
			}
			_, err := s.recordConsent(ctx, tx, t, consent.GivenOn, consent.FileID, nil, "paper")
			return err
		}
		return nil
	})
	return t, err
}

// Update — изменить нанимателя; тип лица не меняется, обезличенного изменить нельзя.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (sqlc.Tenant, error) {
	var t sqlc.Tenant
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.AnonymizedAt != nil {
			return apperr.BusinessRule("Наниматель обезличен, изменить данные нельзя")
		}
		if in.Kind != before.Kind {
			return apperr.Validation(apperr.FieldError{Field: "kind", Message: "Тип нанимателя не меняется"})
		}
		if err := in.normalize(s.clock.Today()); err != nil {
			return err
		}
		p := sqlc.UpdateTenantParams{
			ID: id, LastName: in.LastName, FirstName: in.FirstName, MiddleName: in.MiddleName, BirthDate: in.BirthDate,
			DocType: in.DocType, DocIssuedOn: in.DocIssuedOn, OrgName: in.OrgName, Inn: in.Inn, Kpp: in.Kpp, Ogrn: in.Ogrn,
			LegalAddress: in.LegalAddress, SignatoryName: in.SignatoryName, SignatoryPosition: in.SignatoryPosition,
			SignatoryBasis: in.SignatoryBasis, Phone: in.Phone, Email: in.Email, Note: in.Note,
		}
		if p.DocNumberEnc, err = s.encrypt(id, fieldDocNumber, in.DocNumber, before.DocNumberEnc); err != nil {
			return err
		}
		if p.DocIssuedByEnc, err = s.encrypt(id, fieldDocIssuedBy, in.DocIssuedBy, before.DocIssuedByEnc); err != nil {
			return err
		}
		if p.DocDivisionCodeEnc, err = s.encrypt(id, fieldDivisionCode, in.DocDivisionCode, before.DocDivisionCodeEnc); err != nil {
			return err
		}
		if p.RegistrationAddressEnc, err = s.encrypt(id, fieldRegAddress, in.RegAddress, before.RegistrationAddressEnc); err != nil {
			return err
		}
		if t, err = tx.Q.UpdateTenant(ctx, p); err != nil {
			return err
		}
		b, a := snapshot(before), snapshot(t)
		// Перешифрованный документ без изменения значения не считается изменением.
		for _, f := range []struct {
			key  string
			v    *string
			name string
			prev []byte
		}{{"docNumber", in.DocNumber, fieldDocNumber, before.DocNumberEnc}, {"docIssuedBy", in.DocIssuedBy, fieldDocIssuedBy, before.DocIssuedByEnc},
			{"docDivisionCode", in.DocDivisionCode, fieldDivisionCode, before.DocDivisionCodeEnc},
			{"registrationAddress", in.RegAddress, fieldRegAddress, before.RegistrationAddressEnc}} {
			if f.v != nil {
				old, _ := s.decrypt(before, f.name, f.prev)
				if old != strings.TrimSpace(*f.v) {
					a[f.key] = "изменён"
				}
			}
		}
		a["changedFields"] = changedFields(b, a)
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tenant.update", EntityType: "tenant", EntityID: id.String(),
			Summary: "Изменены данные: " + summaryName(t), Before: b, After: a,
		})
	})
	return t, err
}

func (s *Service) lock(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Tenant, error) {
	t, err := tx.Q.GetTenantForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return t, apperr.NotFound()
	}
	return t, err
}

// Get — наниматель.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (sqlc.Tenant, error) {
	t, err := s.db.Queries().GetTenant(ctx, id)
	if db.IsNoRows(err) {
		return t, apperr.NotFound()
	}
	return t, err
}

// Masked — маскированные документ и адрес регистрации для карточки.
type Masked struct {
	DocNumber, RegAddress *string
	HasDocument           bool
}

// Mask расшифровывает и маскирует документ и адрес регистрации.
func (s *Service) Mask(t sqlc.Tenant) (Masked, error) {
	var m Masked
	doc, err := s.decrypt(t, fieldDocNumber, t.DocNumberEnc)
	if err != nil {
		return m, err
	}
	addr, err := s.decrypt(t, fieldRegAddress, t.RegistrationAddressEnc)
	if err != nil {
		return m, err
	}
	if doc != "" {
		v := MaskDocNumber(doc)
		m.DocNumber = &v
	}
	if addr != "" {
		v := MaskAddress(addr)
		m.RegAddress = &v
	}
	m.HasDocument = doc != "" && addr != ""
	return m, nil
}

// PDN — полные значения документа и адреса регистрации.
type PDN struct {
	DocNumber, DocIssuedBy, DivisionCode, RegAddress *string
}

// Decrypt — полные значения документа и адреса регистрации без записи в журнал:
// для формирования документов (запись document.generate делает модуль документов).
func (s *Service) Decrypt(t sqlc.Tenant) (PDN, error) {
	var out PDN
	vals := []struct {
		dst   **string
		field string
		v     []byte
	}{{&out.DocNumber, fieldDocNumber, t.DocNumberEnc}, {&out.DocIssuedBy, fieldDocIssuedBy, t.DocIssuedByEnc},
		{&out.DivisionCode, fieldDivisionCode, t.DocDivisionCodeEnc}, {&out.RegAddress, fieldRegAddress, t.RegistrationAddressEnc}}
	for _, v := range vals {
		plain, err := s.decrypt(t, v.field, v.v)
		if err != nil {
			return out, err
		}
		if plain != "" {
			p := plain
			*v.dst = &p
		}
	}
	return out, nil
}

// Reveal — полные значения по кнопке «Показать» с записью tenant.pdn_view в журнал.
func (s *Service) Reveal(ctx context.Context, id uuid.UUID) (PDN, error) {
	var out PDN
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if out, err = s.Decrypt(t); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tenant.pdn_view", EntityType: "tenant", EntityID: id.String(),
			Summary: "Просмотр паспортных данных и адреса регистрации: " + summaryName(t),
		})
	})
	return out, err
}

func (s *Service) recordConsent(ctx context.Context, tx *db.Tx, t sqlc.Tenant, givenOn date.Date, fileID, documentID *uuid.UUID,
	method string) (sqlc.PdnConsent, error) {
	if givenOn.After(s.clock.Today()) {
		return sqlc.PdnConsent{}, apperr.Validation(apperr.FieldError{Field: "givenOn", Message: "Дата не может быть позже сегодняшней"})
	}
	if fileID != nil {
		f, err := s.files.Get(ctx, tx.Q, *fileID)
		if err != nil {
			return sqlc.PdnConsent{}, err
		}
		if f.Category != file.CategoryConsentScan {
			return sqlc.PdnConsent{}, apperr.Validation(apperr.FieldError{Field: "fileId", Message: "Файл загружен не как скан согласия"})
		}
	}
	org, err := s.settings.Get(ctx, tx.Q)
	if err != nil {
		return sqlc.PdnConsent{}, err
	}
	c, err := tx.Q.InsertConsent(ctx, sqlc.InsertConsentParams{
		TenantID: t.ID, Purpose: org.PdnConsentPurpose, DocumentID: documentID, GivenOn: givenOn, Method: method,
		FileID: fileID, RecordedBy: actorID(ctx), Ip: reqctx.MetaFrom(ctx).IP, Now: s.clock.Now(),
	})
	if code, _, ok := db.ConstraintError(err); ok && code == db.CodeForeignKey {
		return c, apperr.Validation(apperr.FieldError{Field: "documentId", Message: "Документ не найден"})
	}
	if err != nil {
		return c, err
	}
	return c, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "tenant.consent", EntityType: "tenant", EntityID: t.ID.String(),
		Summary: fmt.Sprintf("Отмечено отдельное согласие на обработку ПДн от %s (%s)", givenOn.Format(), consentMethod(method)),
	})
}

func consentMethod(m string) string {
	if m == "electronic" {
		return "в личном кабинете"
	}
	return "бумажное"
}

// RecordConsent — отметка о получении подписанного бланка согласия (только физлицо).
func (s *Service) RecordConsent(ctx context.Context, id uuid.UUID, givenOn date.Date, fileID, documentID *uuid.UUID) (sqlc.PdnConsent, error) {
	var c sqlc.PdnConsent
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if t.Kind != KindIndividual {
			return apperr.BusinessRule("Согласие на обработку ПДн оформляется только для физлица")
		}
		if t.AnonymizedAt != nil {
			return apperr.BusinessRule("Наниматель обезличен")
		}
		c, err = s.recordConsent(ctx, tx, t, givenOn, fileID, documentID, "paper")
		return err
	})
	return c, err
}

// RevokeConsent — отзыв согласия: после отзыва данные для этой цели не используются.
func (s *Service) RevokeConsent(ctx context.Context, id, consentID uuid.UUID, revokedOn date.Date, note *string) (sqlc.PdnConsent, error) {
	var c sqlc.PdnConsent
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		cur, err := tx.Q.GetConsent(ctx, sqlc.GetConsentParams{ID: consentID, TenantID: id})
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if cur.RevokedOn != nil {
			return apperr.Conflict(apperr.CodeConflictState, "Согласие уже отозвано")
		}
		if revokedOn.Before(cur.GivenOn) || revokedOn.After(s.clock.Today()) {
			return apperr.Validation(apperr.FieldError{Field: "revokedOn", Message: "Дата отзыва — не раньше даты согласия и не позже сегодняшней"})
		}
		if c, err = tx.Q.RevokeConsent(ctx, sqlc.RevokeConsentParams{ID: consentID, TenantID: id, RevokedOn: &revokedOn, RevokeNote: trim(note)}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tenant.consent_revoke", EntityType: "tenant", EntityID: id.String(),
			Summary: fmt.Sprintf("Отзыв согласия на обработку ПДн с %s: %s", revokedOn.Format(), summaryName(t)),
		})
	})
	return c, err
}

// Invite — учётная запись нанимателя (invited) и письмо invite (12 разд. 4).
func (s *Service) Invite(ctx context.Context, id uuid.UUID) (sqlc.User, error) {
	var u sqlc.User
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		u, err = s.InviteTx(ctx, tx, t)
		return err
	})
	return u, err
}

// InviteTx — приглашение в транзакции вызывающего (активация договора с отметкой «Пригласить»).
func (s *Service) InviteTx(ctx context.Context, tx *db.Tx, t sqlc.Tenant) (sqlc.User, error) {
	if t.AnonymizedAt != nil {
		return sqlc.User{}, apperr.BusinessRule("Наниматель обезличен")
	}
	if empty(t.Email) {
		return sqlc.User{}, apperr.Conflict(apperr.CodeConflictState, "Укажите e-mail нанимателя, чтобы пригласить его в личный кабинет")
	}
	if _, err := tx.Q.GetUserByTenantID(ctx, &t.ID); err == nil {
		return sqlc.User{}, apperr.Conflict(apperr.CodeConflictExists, "У нанимателя уже есть учётная запись")
	} else if !db.IsNoRows(err) {
		return sqlc.User{}, err
	}
	display := names.Greeting(t.FirstName, t.MiddleName)
	if t.Kind == KindLegal && t.OrgName != nil {
		display = *t.OrgName
	}
	u, err := tx.Q.CreateUser(ctx, sqlc.CreateUserParams{
		Email: strings.ToLower(*t.Email), Role: reqctx.RoleTenant, TenantID: &t.ID, DisplayName: display, Now: s.clock.Now(),
	})
	if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
		return u, apperr.Conflict(apperr.CodeConflictExists, "Пользователь с таким e-mail уже есть")
	}
	if err != nil {
		return u, err
	}
	_, err = s.auth.Invite(ctx, tx, u)
	return u, err
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}
