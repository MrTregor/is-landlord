//go:build integration

package tenant_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

func person() map[string]any {
	return map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич",
		"birthDate": "1995-04-12", "docType": "passport_rf", "docNumber": "45 18 908172",
		"docIssuedBy": "ГУ МВД России по г. Москве", "docIssuedOn": "2018-05-20", "docDivisionCode": "770-001",
		"registrationAddress": "г. Москва, ул. Примерная, д. 1, кв. 5", "phone": "8 (916) 123-45-67",
		"email": "Kuznetsov@Landlord.local",
	}
}

type tenantDetail struct {
	ID                        string  `json:"id"`
	DisplayName               string  `json:"displayName"`
	Phone                     *string `json:"phone"`
	Email                     *string `json:"email"`
	DocNumberMasked           *string `json:"docNumberMasked"`
	RegistrationAddressMasked *string `json:"registrationAddressMasked"`
	HasDocument               bool    `json:"hasDocument"`
	Consents                  []struct {
		ID        string  `json:"id"`
		Method    string  `json:"method"`
		RevokedOn *string `json:"revokedOn"`
	} `json:"consents"`
	User *struct{ Status string } `json:"user"`
}

// ТК-04: паспорт хранится зашифрованным, в карточке — маскированно, «Показать» раскрывает и пишет в журнал
// (FR-02.1, NFR-Sec-03).
func TestTenantPdnEncryptedMaskedRevealed_TK04_NFR_Sec_03(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "tenants@landlord.local", "Пароль-наним-11")

	r := c.Do(http.MethodPost, "/tenants", person())
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d tenantDetail
	r.JSON(t, &d)
	assert.Equal(t, "Кузнецов П. И.", d.DisplayName)
	assert.Equal(t, "+79161234567", *d.Phone, "телефон нормализуется")
	assert.Equal(t, "kuznetsov@landlord.local", *d.Email)
	assert.Equal(t, "45 ** ****72", *d.DocNumberMasked)
	assert.Equal(t, "г. Москва, …", *d.RegistrationAddressMasked)
	assert.True(t, d.HasDocument)
	assert.NotContains(t, string(r.Body), "908172")

	// В БД — шифротекст.
	conn := testsupport.Owner(t)
	var enc []byte
	require.NoError(t, conn.QueryRow(ctx, `SELECT doc_number_enc FROM tenants WHERE id = $1`, d.ID).Scan(&enc))
	assert.Len(t, enc, 12+len("45 18 908172")+16)
	assert.NotContains(t, string(enc), "908172")

	r = c.Do(http.MethodPost, "/tenants/"+d.ID+"/reveal-pdn", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.JSONEq(t, `{"docNumber":"45 18 908172","docIssuedBy":"ГУ МВД России по г. Москве","docDivisionCode":"770-001",
		"registrationAddress":"г. Москва, ул. Примерная, д. 1, кв. 5"}`, string(r.Body))

	var views, leaks int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'tenant.pdn_view' AND entity_id = $1`, d.ID).Scan(&views))
	assert.Equal(t, 1, views)
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE concat(summary, before::text, after::text)
		~ '(908172|Кузнецов|Павел|Примерная|916|kuznetsov)'`).Scan(&leaks))
	assert.Equal(t, 0, leaks, "ПДн не попадают в журнал")

	// Поля документа — только запись: отсутствует → не меняется, "" → очистить.
	upd := person()
	delete(upd, "docNumber")
	delete(upd, "docIssuedBy")
	delete(upd, "docDivisionCode")
	delete(upd, "registrationAddress")
	upd["note"] = "Работает в Москве"
	r = c.Do(http.MethodPut, "/tenants/"+d.ID, upd)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &d)
	assert.True(t, d.HasDocument)

	upd["registrationAddress"] = ""
	c.Do(http.MethodPut, "/tenants/"+d.ID, upd).JSON(t, &d)
	assert.False(t, d.HasDocument)
	assert.Nil(t, d.RegistrationAddressMasked)

	upd["kind"] = "legal"
	r = c.Do(http.MethodPut, "/tenants/"+d.ID, upd)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "тип не меняется")

	// Поиск по ФИО и телефону.
	var list struct {
		Total int
		Items []struct{ DisplayName, CabinetStatus string }
	}
	c.Do(http.MethodGet, "/tenants?q=кузнец", nil).JSON(t, &list)
	assert.Equal(t, 1, list.Total)
	c.Do(http.MethodGet, "/tenants?q=916-123", nil).JSON(t, &list)
	assert.Equal(t, 1, list.Total)
	assert.Equal(t, "none", list.Items[0].CabinetStatus)
}

// ТК-05: юрлицо с неверным ИНН — «Неверный ИНН»; физлицу нужен телефон или e-mail (FR-02.3).
func TestTenantValidation_TK05_FR02_3(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "valid@landlord.local", "Пароль-провер-1")

	legal := map[string]any{
		"kind": "legal", "orgName": "ООО «Ромашка»", "inn": "5075123458", "kpp": "507501001", "ogrn": "1235000012345",
		"legalAddress": "Московская область, г. Руза, ул. Социалистическая, д. 3", "signatoryName": "Иванов Иван Иванович",
		"signatoryPosition": "генеральный директор", "signatoryBasis": "Устава", "email": "romashka@landlord.local",
	}
	r := c.Do(http.MethodPost, "/tenants", legal)
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), `"field":"inn","message":"Неверный ИНН"`)

	legal["inn"] = "5075123457"
	r = c.Do(http.MethodPost, "/tenants", legal)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d tenantDetail
	r.JSON(t, &d)
	assert.Equal(t, "ООО «Ромашка»", d.DisplayName)
	assert.False(t, d.HasDocument)

	p := person()
	delete(p, "phone")
	delete(p, "email")
	r = c.Do(http.MethodPost, "/tenants", p)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Укажите телефон или e-mail")
}

// Отдельное согласие на ПДн: отметка при создании, отзыв (FR-02.1, 12 разд. 5); приглашение в кабинет (12 разд. 4).
func TestTenantConsentAndInvite_FR02_1_FR13_2(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "consent@landlord.local", "Пароль-соглас-1")

	scan := c.Upload("consent_scan", "согласие.pdf", []byte("%PDF-1.4 скан"))
	require.Equal(t, http.StatusCreated, scan.Status, string(scan.Body))
	p := person()
	p["consent"] = map[string]any{"givenOn": "2026-10-10", "fileId": scan.ID(t)}
	r := c.Do(http.MethodPost, "/tenants", p)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d tenantDetail
	r.JSON(t, &d)
	require.Len(t, d.Consents, 1)
	assert.Equal(t, "paper", d.Consents[0].Method)

	consent := d.Consents[0].ID
	r = c.Do(http.MethodPost, "/tenants/"+d.ID+"/consents/"+consent+"/revoke", map[string]any{"revokedOn": "2026-10-16", "note": "Заявление"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = c.Do(http.MethodPost, "/tenants/"+d.ID+"/consents/"+consent+"/revoke", map[string]any{"revokedOn": "2026-10-16"})
	assert.Equal(t, http.StatusConflict, r.Status)

	// Приглашение: учётная запись invited, письмо invite, повтор — 409.
	r = c.Do(http.MethodPost, "/tenants/"+d.ID+"/invite", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var u struct{ Role, Status, DisplayName, Email string }
	r.JSON(t, &u)
	assert.Equal(t, "tenant", u.Role)
	assert.Equal(t, "invited", u.Status)
	assert.Equal(t, "Павел Игоревич", u.DisplayName)
	assert.Equal(t, http.StatusConflict, c.Do(http.MethodPost, "/tenants/"+d.ID+"/invite", nil).Status)

	var mails int
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT count(*) FROM email_log WHERE template_code = 'invite' AND to_email = 'kuznetsov@landlord.local'`).Scan(&mails))
	assert.Equal(t, 1, mails)

	c.Do(http.MethodGet, "/tenants/"+d.ID, nil).JSON(t, &d)
	require.NotNil(t, d.User)
	assert.Equal(t, "invited", d.User.Status)
	require.NotNil(t, d.Consents[0].RevokedOn)

	// Без e-mail пригласить нельзя.
	noMail := person()
	delete(noMail, "email")
	id := c.Do(http.MethodPost, "/tenants", noMail).ID(t)
	r = c.Do(http.MethodPost, "/tenants/"+id+"/invite", nil)
	assert.Equal(t, http.StatusConflict, r.Status)
}
