//go:build integration

package cabinet_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-кабинета-1"

type world struct {
	env      *testsupport.Env
	owner    *testsupport.Client
	property string
	tenant   string
	contract string
}

// setup — собственник, объект со счётчиком холодной воды, наниматель-физлицо и действующий договор с ЖКУ по счётчикам
// с 01.09.2026; сегодня — 21.10.2026 (окно приёма показаний за октябрь открыто).
func setup(t *testing.T, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-21")
	owner := env.ActiveOwner(t, "cabinet-"+sfx+"@landlord.local", password)
	w := world{env: env, owner: owner}
	w.add(t, sfx)
	return w
}

// add — ещё один объект, наниматель и договор того же собственника.
func (w *world) add(t *testing.T, sfx string) {
	t.Helper()
	c := w.owner
	r := c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Кв " + sfx, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 5, кв. " + sfx, "areaSqm": "34.00",
		"floor": 2, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": true,
		"expectedRentKop": 2800000, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.property = r.ID(t)
	r = c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": "ХВ-" + sfx, "initialValue": "100.000", "initialDate": "2026-09-01",
		"verificationDue": nil, "note": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич", "docType": "passport_rf",
		"docNumber": "46 10 123456", "registrationAddress": "г. Москва, ул. Примерная, д. 1", "email": "kuznetsov-" + sfx + "@landlord.local",
		"phone": "+79001234567",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.tenant = r.ID(t)
	r = c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": "2026-09-01", "endDate": "2027-07-31", "rentKop": 2800000,
		"paymentDay": 10, "depositKop": 0, "utilitiesMode": "meters", "utilitiesFixedKop": 0, "penaltyEnabled": true,
		"penaltyRatePct": "0.1", "penaltyGraceDays": 3, "penaltyOnUtilities": false, "indexationEnabled": false,
		"indexationPeriodMonths": 12, "notes": "Заметка собственника",
		"occupants": []any{map[string]any{"fullName": "Кузнецова Анна Павловна", "birthDate": "2015-04-02", "relation": "дочь"}},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.contract = r.ID(t)
	r = c.Do(http.MethodPost, "/contracts/"+w.contract+"/activate", map[string]any{"concludedOn": "2026-09-01", "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

type meters struct {
	Meters []struct {
		MeterID   string  `json:"meterId"`
		LastValue *string `json:"lastValue"`
		LastDate  *string `json:"lastDate"`
		Current   *struct {
			Value        string  `json:"value"`
			Status       string  `json:"status"`
			RejectReason *string `json:"rejectReason"`
		} `json:"current"`
		History []struct {
			Status string `json:"status"`
		} `json:"history"`
	} `json:"meters"`
	Window struct {
		PeriodMonth string `json:"periodMonth"`
		Open        bool   `json:"open"`
		From        string `json:"from"`
		To          string `json:"to"`
	} `json:"window"`
}

// IT-08, ТК-29: наниматель A не видит договор, начисления, платёж, документ, файл и счётчики нанимателя B — 404;
// методы собственника — 403 (NFR-Sec-02, FR-13.1).
func TestForeignResources_IT08_TK29_NFR_Sec_02(t *testing.T) {
	w := setup(t, "a08")
	a := w.env.TenantUser(t, w.tenant, "tenant-a08@landlord.local", password)
	contractA := w.contract
	w.add(t, "b08")
	b := w.env.TenantUser(t, w.tenant, "tenant-b08@landlord.local", password)
	contractB := w.contract

	r := w.owner.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contractB, "purpose": "charges", "amountKop": 100000, "receivedOn": "2026-10-20", "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	paymentB := r.ID(t)
	r = w.owner.Do(http.MethodPost, "/contracts/"+contractB+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	docB := r.ID(t)
	r = b.Upload("request_photo", "кран.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	fileB := r.ID(t)

	// Свои ресурсы B доступны.
	for _, p := range []string{"/cabinet/contracts/" + contractB, "/cabinet/documents/" + docB + "/pdf", "/files/" + fileB,
		"/cabinet/payments/" + paymentB} {
		assert.Equal(t, http.StatusOK, b.Do(http.MethodGet, p, nil).Status, p)
	}
	// Чужие для A — «Запись не найдена».
	for _, p := range []string{
		"/cabinet/contracts/" + contractB, "/cabinet/charges?contractId=" + contractB, "/cabinet/payments?contractId=" + contractB,
		"/cabinet/payments/" + paymentB, "/cabinet/payments/" + paymentB + "/receipt.pdf", "/cabinet/documents/" + docB + "/pdf",
		"/files/" + fileB, "/cabinet/meters?contractId=" + contractB, "/cabinet/requests?contractId=" + contractB,
		"/cabinet/summary?contractId=" + contractB,
	} {
		r := a.Do(http.MethodGet, p, nil)
		assert.Equal(t, http.StatusNotFound, r.Status, p)
		assert.Equal(t, "not_found", r.Code(), p)
	}
	r = a.Do(http.MethodPost, "/cabinet/meter-readings", map[string]any{"contractId": contractB, "periodMonth": "2026-10-01",
		"readings": []any{}})
	assert.Equal(t, http.StatusNotFound, r.Status, "показания по чужому договору")
	r = a.Do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": contractB, "purpose": "charges", "chargeIds": []string{}})
	assert.Equal(t, http.StatusNotFound, r.Status, "оплата чужого договора")
	assert.Equal(t, http.StatusOK, a.Do(http.MethodGet, "/cabinet/contracts/"+contractA, nil).Status)

	// Методы собственника нанимателю запрещены.
	for _, p := range []string{"/properties", "/contracts/" + contractA, "/tenants", "/payments", "/documents/" + docB} {
		assert.Equal(t, http.StatusForbidden, a.Do(http.MethodGet, p, nil).Status, p)
	}

	// Скрытый от нанимателя документ — 404 и для своего договора.
	r = w.owner.Do(http.MethodPatch, "/documents/"+docB, map[string]any{"visibleToTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, http.StatusNotFound, b.Do(http.MethodGet, "/cabinet/documents/"+docB+"/pdf", nil).Status)
}

// Договор и документы в кабинете (15 разд. 7): условия без заметок, проживающие, документы, история платы (FR-13.1).
func TestCabinetContract_FR_13_1(t *testing.T) {
	w := setup(t, "c13")
	tenant := w.env.TenantUser(t, w.tenant, "tenant-c13@landlord.local", password)
	r := w.owner.Do(http.MethodPost, "/contracts/"+w.contract+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	doc := r.ID(t)

	r = tenant.Do(http.MethodGet, "/cabinet/contracts/"+w.contract, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var c map[string]any
	r.JSON(t, &c)
	assert.NotContains(t, c, "notes", "заметки собственника нанимателю не показываются")
	assert.Equal(t, "meters", c["utilitiesMode"])
	assert.EqualValues(t, 2800000, c["currentTerms"].(map[string]any)["rentKop"])
	assert.Len(t, c["termsHistory"], 1)
	assert.Len(t, c["occupants"], 1)
	docs := c["documents"].([]any)
	require.Len(t, docs, 1)
	assert.Equal(t, "/api/v1/cabinet/documents/"+doc+"/pdf", docs[0].(map[string]any)["pdfUrl"])
	r = tenant.Do(http.MethodGet, "/cabinet/documents/"+doc+"/pdf", nil)
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Equal(t, "application/pdf", r.Header.Get("Content-Type"))
}

// Показания нанимателя (FR-05.5, 05 разд. 6.2, 15 разд. 5): окно приёма, проверка «меньше предыдущего», статус
// «На проверке», письмо собственнику, повтор после отклонения, вне окна — 422.
func TestTenantReadings_FR_05_5(t *testing.T) {
	w := setup(t, "r55")
	tenant := w.env.TenantUser(t, w.tenant, "tenant-r55@landlord.local", password)
	var m meters
	r := tenant.Do(http.MethodGet, "/cabinet/meters?contractId="+w.contract, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &m)
	require.Len(t, m.Meters, 1)
	assert.Equal(t, "2026-10-01", m.Window.PeriodMonth)
	assert.True(t, m.Window.Open)
	assert.Equal(t, "2026-10-20", m.Window.From)
	assert.Equal(t, "2026-11-05", m.Window.To)
	assert.Equal(t, "100", *m.Meters[0].LastValue)
	assert.Nil(t, m.Meters[0].Current)
	meterID := m.Meters[0].MeterID

	submit := func(month, value string) testsupport.Response {
		return tenant.Do(http.MethodPost, "/cabinet/meter-readings", map[string]any{"contractId": w.contract, "periodMonth": month,
			"readings": []any{map[string]any{"meterId": meterID, "value": value}}})
	}
	r = submit("2026-10-01", "99.5")
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
	assert.Contains(t, string(r.Body), "Показание меньше предыдущего (100 от 01.09.2026)")
	assert.Contains(t, string(r.Body), "readings[0].value")
	r = tenant.Do(http.MethodPost, "/cabinet/meter-readings", map[string]any{"contractId": w.contract, "periodMonth": "2026-10-01",
		"readings": []any{}})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "все счётчики объекта")
	r = submit("2026-09-01", "103.5")
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Показания за сентябрь 2026 принимаются с 20.09.2026 по 05.10.2026")

	r = submit("2026-10-01", "103.5")
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r.JSON(t, &m)
	require.NotNil(t, m.Meters[0].Current)
	assert.Equal(t, "submitted", m.Meters[0].Current.Status)
	assert.Equal(t, "103.5", m.Meters[0].Current.Value)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'owner_readings_submitted' AND related_entity_id = $1 AND to_email = $2`,
		w.contract, "cabinet-r55@landlord.local"))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'reading.submit' AND summary LIKE '%' || $1 || '%'`,
		"103,5"))
	r = submit("2026-10-01", "104")
	assert.Equal(t, http.StatusConflict, r.Status, "повторно — только после отклонения")

	var readingID string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT id FROM meter_readings WHERE contract_id = $1 AND source = 'tenant'`, w.contract).Scan(&readingID))
	r = w.owner.Do(http.MethodPost, "/meter-readings/"+readingID+"/reject", map[string]any{"reason": "Неразборчиво на фото"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = tenant.Do(http.MethodGet, "/cabinet/meters?contractId="+w.contract, nil)
	r.JSON(t, &m)
	assert.Equal(t, "rejected", m.Meters[0].Current.Status)
	assert.Equal(t, "Неразборчиво на фото", *m.Meters[0].Current.RejectReason)
	r = submit("2026-10-01", "103.6")
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r.JSON(t, &m)
	assert.Equal(t, "submitted", m.Meters[0].Current.Status)
	assert.Len(t, m.Meters[0].History, 2)

	// Окно закрыто с 06.11 до 19.11 — за ноябрь.
	w.env.Clock.Set(time.Date(2026, 11, 10, 12, 0, 0, 0, testsupport.Moscow()))
	tenant.Login("tenant-r55@landlord.local", password)
	r = tenant.Do(http.MethodGet, "/cabinet/meters?contractId="+w.contract, nil)
	r.JSON(t, &m)
	assert.Equal(t, "2026-11-01", m.Window.PeriodMonth)
	assert.False(t, m.Window.Open)
	r = submit("2026-11-01", "110")
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Показания за ноябрь 2026 принимаются с 20.11.2026 по 05.12.2026")
}

type profile struct {
	DisplayName      string  `json:"displayName"`
	Email            string  `json:"email"`
	Phone            *string `json:"phone"`
	ConsentPurpose   string  `json:"consentPurpose"`
	ConsentAvailable bool    `json:"consentAvailable"`
	Consent          *struct {
		GivenOn string `json:"givenOn"`
		Method  string `json:"method"`
	} `json:"consent"`
	Organization struct {
		ShortName string `json:"shortName"`
	} `json:"organization"`
}

// Профиль (15 разд. 8): телефон, отдельное электронное согласие и его отзыв; ПДн не попадают в журнал
// (FR-13.1, 12 разд. 2).
func TestCabinetProfileConsent_FR_13_1(t *testing.T) {
	w := setup(t, "p13")
	tenant := w.env.TenantUser(t, w.tenant, "tenant-p13@landlord.local", password)
	var p profile
	r := tenant.Do(http.MethodGet, "/cabinet/profile", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	assert.Equal(t, "tenant-p13@landlord.local", p.Email)
	assert.Equal(t, "+79001234567", *p.Phone)
	assert.True(t, p.ConsentAvailable)
	assert.Nil(t, p.Consent)

	r = tenant.Do(http.MethodPatch, "/cabinet/profile", map[string]any{"phone": "8 (916) 765-43-21"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	assert.Equal(t, "+79167654321", *p.Phone)
	r = tenant.Do(http.MethodPatch, "/cabinet/profile", map[string]any{"phone": "123"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Equal(t, 0, count(t, `SELECT count(*) FROM audit_log WHERE entity_id = $1 AND (summary LIKE '%7654321%' OR after::text LIKE '%7654321%')`,
		w.tenant), "телефон в журнале маскируется")

	r = tenant.Do(http.MethodPost, "/cabinet/consent", map[string]any{"agree": true})
	require.Equal(t, http.StatusNoContent, r.Status, string(r.Body))
	r = tenant.Do(http.MethodGet, "/cabinet/profile", nil)
	r.JSON(t, &p)
	require.NotNil(t, p.Consent)
	assert.Equal(t, "2026-10-21", p.Consent.GivenOn)
	assert.Equal(t, "electronic", p.Consent.Method)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM pdn_consents WHERE tenant_id = $1 AND method = 'electronic' AND ip IS NOT NULL`, w.tenant))
	r = tenant.Do(http.MethodPost, "/cabinet/consent", map[string]any{"agree": true})
	assert.Equal(t, http.StatusNoContent, r.Status, "повтор не создаёт второе согласие")
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM pdn_consents WHERE tenant_id = $1`, w.tenant))

	r = tenant.Do(http.MethodPost, "/cabinet/consent/revoke", nil)
	require.Equal(t, http.StatusNoContent, r.Status, string(r.Body))
	r = tenant.Do(http.MethodGet, "/cabinet/profile", nil)
	r.JSON(t, &p)
	assert.Nil(t, p.Consent)
	assert.Equal(t, http.StatusConflict, tenant.Do(http.MethodPost, "/cabinet/consent/revoke", nil).Status)

	// Смена пароля из профиля.
	r = tenant.Do(http.MethodPost, "/auth/password/change", map[string]any{"currentPassword": password, "newPassword": "Новый-пароль-2026"})
	assert.Equal(t, http.StatusNoContent, r.Status, string(r.Body))
}
