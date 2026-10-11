//go:build integration

package document_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/document"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

// last — HTML последнего документа; неразрывные пробелы в суммах («28 000,00») заменены обычными.
func last(env *testsupport.Env) string { return strings.ReplaceAll(env.PDF.Last(), "\u00a0", " ") }

type docDTO struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	Number          *string `json:"number"`
	Title           string  `json:"title"`
	FileURL         string  `json:"fileUrl"`
	VisibleToTenant bool    `json:"visibleToTenant"`
	SignedScanURL   *string `json:"signedScanUrl"`
	SupersededBy    *string `json:"supersededBy"`
}

type world struct {
	env      *testsupport.Env
	c        *testsupport.Client
	property string
	tenant   string
}

// setup — собственник с реквизитами, объект и наниматель-физлицо с паспортом.
func setup(t *testing.T, suffix string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "docs-"+suffix+"@landlord.local", "Пароль-документы-1")
	var org map[string]any
	c.Do(http.MethodGet, "/settings/organization", nil).JSON(t, &org)
	for k, v := range map[string]any{
		"fullName": "Индивидуальный предприниматель Гордейчик Александр Васильевич", "shortName": "ИП Гордейчик А.В.",
		"inn": "507500363778", "ogrnip": "317502400029263", "address": "Московская область, г. Руза",
		"email": "owner@landlord.local", "bankName": "АО «Демо-Банк»", "bankBik": "044525999",
		"bankAccount": "40802810900000012345", "bankCorrAccount": "30101810400000000999", "signatoryName": "А.В. Гордейчик",
		"pdnConsentPurpose": "направление сообщений о свободных объектах",
	} {
		org[k] = v
	}
	delete(org, "updatedAt")
	delete(org, "updatedBy")
	r := c.Do(http.MethodPut, "/settings/organization", org)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Студия " + suffix, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 3, кв. " + suffix,
		"areaSqm": "24.50", "floor": 3, "rooms": 0, "cadastralNumber": nil, "ownershipBasis": "выписка из ЕГРН от 12.03.2019",
		"description": nil, "furnished": true, "expectedRentKop": 2800000, "fiscalMode": "none",
		"initialStatus": "free", "statusSince": "2026-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич", "birthDate": "1995-04-12",
		"docType": "passport_rf", "docNumber": "45 18 123456", "docIssuedBy": "ГУ МВД России по г. Москве",
		"docIssuedOn": "2018-05-20", "docDivisionCode": "770-001", "phone": "+7 916 123-45-67",
		"registrationAddress": "г. Москва, ул. Лесная, д. 5, кв. 12", "email": "kuznetsov-" + suffix + "@landlord.local",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return world{env: env, c: c, property: prop, tenant: r.ID(t)}
}

func (w world) contract(t *testing.T, start, end string) string {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": start, "endDate": end, "rentKop": 2800000,
		"paymentDay": 15, "depositKop": 2800000, "utilitiesMode": "meters", "utilitiesFixedKop": 45000,
		"penaltyEnabled": true, "penaltyRatePct": "0.1", "penaltyGraceDays": 3, "penaltyCapPct": "10",
		"penaltyOnUtilities": false, "indexationEnabled": true, "indexationPct": "5", "indexationPeriodMonths": 12,
		"occupants": []map[string]any{{"fullName": "Кузнецова Мария Андреевна", "birthDate": "1997-08-03", "relation": "супруга"}},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return r.ID(t)
}

func (w world) meter(t *testing.T) string {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": "123456", "initialValue": "10.000", "initialDate": "2026-01-01",
		"verificationDue": nil, "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return r.ID(t)
}

func (w world) activate(t *testing.T, id string) {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": "2026-03-05", "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
}

func (w world) docs(t *testing.T, contractID string) []docDTO {
	t.Helper()
	var list []docDTO
	r := w.c.Do(http.MethodGet, "/contracts/"+contractID+"/documents", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	return list
}

// Договор найма по системному шаблону: условия, ПДн в документе (не в журнале), PDF, замена при повторном
// формировании, автоматическое формирование при активации (FR-04.1, 05 разд. 4.4 п. 7).
func TestContractDocument_FR04_1(t *testing.T) {
	w := setup(t, "a1")
	w.meter(t)
	id := w.contract(t, "2026-03-15", "2027-03-14")

	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var first docDTO
	r.JSON(t, &first)
	assert.Equal(t, document.KindContractCh35, first.Kind)
	assert.Contains(t, first.Title, "(проект)")
	html := last(w.env)
	for _, want := range []string{
		"Договор найма жилого помещения № " + *first.Number,
		"гражданин Российской Федерации Кузнецов Павел Игоревич, 12.04.1995 года рождения, паспорт 45 18 123456, " +
			"выдан ГУ МВД России по г. Москве 20.05.2018, код подразделения 770-001, зарегистрированный по адресу: " +
			"г. Москва, ул. Лесная, д. 5, кв. 12",
		"квартира-студия общей площадью 24,5 кв. м",
		"28 000,00 руб. (двадцать восемь тысяч рублей 00 копеек) в месяц",
		"обеспечительный платёж в размере 28 000,00 руб. (двадцать восемь тысяч рублей 00 копеек) не позднее 15.03.2026",
		"а также фиксированную часть в размере 450,00 руб. в месяц",
		"пени в размере 0,1 % от просроченной суммы за каждый день просрочки начиная с 4-го дня после установленного срока, " +
			"но не более 10 % от просроченной суммы",
		"не чаще одного раза в 12 месяцев, но не более чем на 5 % в год",
		"подлежит государственной регистрации (п. 2 ст. 674 ГК РФ)",
		"(ст. 684 ГК РФ)",
		"с 15.03.2026 по 14.03.2027 включительно",
		"<td>Кузнецова Мария Андреевна</td><td>03.08.1997</td><td>супруга</td>",
		"р/с 40802810900000012345 в АО «Демо-Банк», БИК 044525999",
		"кадастровый номер ________________",
		"Liberation Serif",
	} {
		assert.Contains(t, html, want)
	}
	assert.NotContains(t, html, "{{")

	pdf := w.c.Do(http.MethodGet, "/documents/"+first.ID+"/pdf", nil)
	require.Equal(t, http.StatusOK, pdf.Status)
	assert.Equal(t, "application/pdf", pdf.Header.Get("Content-Type"))
	assert.True(t, strings.HasPrefix(string(pdf.Body), "%PDF"))

	// Журнал: документ сформирован, ФИО и паспорта в журнале нет.
	var summary string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT summary || coalesce(after::text, '') FROM audit_log WHERE action = 'document.generate' AND entity_id = $1`,
		first.ID).Scan(&summary))
	assert.NotContains(t, summary, "Кузнецов")
	assert.NotContains(t, summary, "123456")

	// Документ сформирован после последнего изменения черновика — при активации новый не создаётся.
	w.activate(t, id)
	list := w.docs(t, id)
	require.Len(t, list, 1)

	// Повторное формирование: прежний документ заменён.
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var second docDTO
	r.JSON(t, &second)
	assert.Equal(t, "Договор найма № "+*first.Number+" от 05.03.2026", second.Title)
	assert.Contains(t, w.env.PDF.Last(), "«05» марта 2026 г.")
	list = w.docs(t, id)
	require.Len(t, list, 2)
	assert.Equal(t, second.ID, list[0].ID)
	require.NotNil(t, list[1].SupersededBy)
	assert.Equal(t, second.ID, *list[1].SupersededBy)

	// Видимость нанимателю.
	r = w.c.Do(http.MethodPatch, "/documents/"+second.ID, map[string]any{"visibleToTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var upd docDTO
	r.JSON(t, &upd)
	assert.False(t, upd.VisibleToTenant)

	// Скан подписанного экземпляра — только файл категории «signed_scan».
	other := w.c.Upload("other", "скан.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, other.Status, string(other.Body))
	assert.Equal(t, http.StatusUnprocessableEntity,
		w.c.Do(http.MethodPost, "/documents/"+second.ID+"/signed-scan", map[string]any{"fileId": other.ID(t)}).Status)
	scan := w.c.Upload("signed_scan", "скан.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, scan.Status, string(scan.Body))
	r = w.c.Do(http.MethodPost, "/documents/"+second.ID+"/signed-scan", map[string]any{"fileId": scan.ID(t)})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &upd)
	require.NotNil(t, upd.SignedScanURL)
}

// Активация без сформированного вручную документа создаёт документ договора (05 разд. 4.4 п. 7);
// подписание акта формирует PDF акта (05 разд. 4.10); допсоглашение — PDF по кнопке (FR-04.1).
func TestDocumentsOnActivationActsAmendments_FR04_1(t *testing.T) {
	w := setup(t, "b2")
	meter := w.meter(t)
	id := w.contract(t, "2026-03-15", "2027-03-14")
	w.activate(t, id)
	list := w.docs(t, id)
	require.Len(t, list, 1)
	assert.Equal(t, document.KindContractCh35, list[0].Kind)

	var acts []struct {
		ID string `json:"id"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/acts", nil).JSON(t, &acts)
	require.Len(t, acts, 1)
	r := w.c.Do(http.MethodPut, "/acts/"+acts[0].ID, map[string]any{
		"actDate": "2026-03-15", "keysCount": 2, "conditionNotes": "Хорошее, после ремонта",
		"inventory": []map[string]any{{"name": "Холодильник Atlant", "quantity": 1, "condition": "исправно"}},
		"readings":  []map[string]any{{"meterId": meter, "value": "15.5"}},
	})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/acts/"+acts[0].ID+"/sign", map[string]any{"signedOn": "2026-03-15"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var act struct {
		DocumentID *string `json:"documentId"`
	}
	r.JSON(t, &act)
	require.NotNil(t, act.DocumentID, "PDF акта сформирован при подписании")
	html := last(w.env)
	assert.Contains(t, html, "Акт приёма-передачи помещения")
	assert.Contains(t, html, "к договору найма жилого помещения №")
	assert.Contains(t, html, "<td>Холодильник Atlant</td>")
	assert.Contains(t, html, "<td>Холодная вода</td><td>123456</td><td class=\"num\">15,5</td>")
	assert.Contains(t, html, "Передано ключей: 2 шт.")

	// Кнопка «PDF» у акта — новый документ взамен прежнего.
	r = w.c.Do(http.MethodPost, "/acts/"+acts[0].ID+"/document", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var actDoc docDTO
	r.JSON(t, &actDoc)
	assert.Equal(t, document.KindActHandover, actDoc.Kind)
	assert.True(t, strings.HasSuffix(*actDoc.Number, "-АП"))

	// Допсоглашение о снижении платы.
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/amendments", map[string]any{
		"kind": "rent_change", "effectiveFrom": "2026-11-01", "newRentKop": 2600000, "bodyText": "Иные условия не меняются."})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	am := r.ID(t)
	r = w.c.Do(http.MethodPost, "/amendments/"+am+"/document", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var amDoc docDTO
	r.JSON(t, &amDoc)
	assert.True(t, strings.HasSuffix(*amDoc.Number, "/ДС-1"))
	html = last(w.env)
	assert.Contains(t, html, "1. С 01.11.2026 размер платы за наём составляет 26 000,00 руб. "+
		"(двадцать шесть тысяч рублей 00 копеек) в месяц.<br>2. Иные условия не меняются.")
	var ams []struct {
		DocumentID *string `json:"documentId"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/amendments", nil).JSON(t, &ams)
	require.Len(t, ams, 1)
	require.NotNil(t, ams[0].DocumentID)
	assert.Equal(t, amDoc.ID, *ams[0].DocumentID)
}

// Акт сверки: начисления, оплаты, сальдо на начало и конец, номер АС-ГГГГ-NNN (09 разд. 4.6, FR-04.1).
func TestReconciliationAct_FR04_1(t *testing.T) {
	w := setup(t, "c3")
	w.meter(t)
	id := w.contract(t, "2026-09-01", "2027-08-31")
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/reconciliation-act",
		map[string]any{"from": "2026-09-01", "to": "2026-10-31"}).Status, "черновик")
	w.activate(t, id)
	ctx := context.Background()
	_, err := testsupport.Owner(t).Exec(ctx, `INSERT INTO payments (contract_id, purpose, amount_kop, received_on, method, source,
		status, receipt_number) VALUES ($1, 'charges', 3000000, '2026-09-01', 'cash', 'manual', 'succeeded', 'КВ-2026-00001'),
		($1, 'charges', 1000000, '2026-10-10', 'cash', 'manual', 'succeeded', 'КВ-2026-00002')`, id)
	require.NoError(t, err)

	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/reconciliation-act", map[string]any{"from": "2026-10-01", "to": "2026-10-31"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d docDTO
	r.JSON(t, &d)
	assert.Equal(t, "АС-2026-001", *d.Number)
	html := last(w.env)
	// Сентябрь: начислено 28 000, оплачено 30 000 → переплата 2 000 на 01.10.
	assert.Contains(t, html, "Сальдо на 01.10.2026: переплата Нанимателя 2 000,00 руб.")
	assert.Contains(t, html, "Плата за наём, октябрь 2026")
	assert.Contains(t, html, "Оплата, квитанция КВ-2026-00002")
	// Октябрь: +28 000 − 10 000 → задолженность 16 000.
	assert.Contains(t, html, "Сальдо на 31.10.2026: задолженность Нанимателя 16 000,00 руб.")
	assert.NotContains(t, html, "КВ-2026-00001", "платёж до периода — только в сальдо")

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/reconciliation-act", map[string]any{"from": "2026-10-31", "to": "2026-10-01"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

// Бланк согласия на обработку ПДн — только физлицо, документ полностью (09 разд. 4.7, NFR-Sec-04).
func TestConsentForm_FR04_1(t *testing.T) {
	w := setup(t, "d4")
	r := w.c.Do(http.MethodPost, "/tenants/"+w.tenant+"/consent-form", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d docDTO
	r.JSON(t, &d)
	assert.Equal(t, "СГ-2026-001", *d.Number)
	html := last(w.env)
	assert.Contains(t, html, "паспорт гражданина Российской Федерации 45 18 № 123456, выдан ГУ МВД России по г. Москве, "+
		"дата выдачи 20.05.2018, код подразделения 770-001")
	assert.Contains(t, html, "направление сообщений о свободных объектах")
	assert.Contains(t, html, "не более 3 лет после окончания последнего договора")

	r = w.c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "legal", "orgName": "ООО «Ромашка»", "inn": "5075123457", "kpp": "507501001", "ogrn": "1235000012345",
		"legalAddress": "Московская область, г. Руза", "signatoryName": "Иванов Иван Иванович",
		"signatoryPosition": "Директор", "signatoryBasis": "Устава",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodPost, "/tenants/"+r.ID(t)+"/consent-form", nil).Status)
}

type tplDetail struct {
	Active struct {
		ID       string `json:"id"`
		Version  int    `json:"version"`
		BodyHTML string `json:"bodyHtml"`
		IsSystem bool   `json:"isSystem"`
		Name     string `json:"name"`
	} `json:"active"`
	Versions []struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	} `json:"versions"`
}

// ТК-10: изменить шаблон договора, сформировать договор — в PDF новый текст, версия шаблона +1; проверка ключей,
// очистка HTML, сброс к стандартному, предпросмотр всех видов на демо-данных (FR-04.1, NFR-Sec-06).
func TestTemplateVersions_TK10_FR04_1(t *testing.T) {
	w := setup(t, "e5")
	var list []struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
	}
	w.c.Do(http.MethodGet, "/settings/document-templates", nil).JSON(t, &list)
	require.Len(t, list, 8)

	var tpl tplDetail
	r := w.c.Do(http.MethodGet, "/settings/document-templates/contract_ch35", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &tpl)
	require.True(t, tpl.Active.IsSystem)
	start := tpl.Active.Version
	system := tpl.Active.BodyHTML

	r = w.c.Do(http.MethodPut, "/settings/document-templates/contract_ch35", map[string]any{
		"name": tpl.Active.Name, "bodyHtml": system + "<p>{{foo.bar}} {{block:inventory}}</p>"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Неизвестные поля: block:inventory, foo.bar")

	body := system + `<p onclick="alert(1)">10.1. Новый пункт договора: {{contract.number}}.</p><script>alert(1)</script>`
	r = w.c.Do(http.MethodPut, "/settings/document-templates/contract_ch35", map[string]any{"name": tpl.Active.Name, "bodyHtml": body})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &tpl)
	assert.Equal(t, start+1, tpl.Active.Version)
	assert.Len(t, tpl.Versions, 2)
	assert.NotContains(t, tpl.Active.BodyHTML, "<script")
	assert.NotContains(t, tpl.Active.BodyHTML, "onclick")
	assert.Contains(t, tpl.Active.BodyHTML, `<div class="place-date">`)

	id := w.contract(t, "2026-03-15", "2027-03-14")
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d docDTO
	r.JSON(t, &d)
	assert.Contains(t, w.env.PDF.Last(), "10.1. Новый пункт договора: "+*d.Number+".")
	var tplVersion int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT t.version FROM documents d JOIN document_templates t ON t.id = d.template_id WHERE d.id = $1`, d.ID).Scan(&tplVersion))
	assert.Equal(t, start+1, tplVersion)

	// Старая версия — только чтение.
	r = w.c.Do(http.MethodGet, "/settings/document-templates/contract_ch35/versions/"+tpl.Versions[1].ID, nil)
	require.Equal(t, http.StatusOK, r.Status)

	r = w.c.Do(http.MethodPost, "/settings/document-templates/contract_ch35/reset", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &tpl)
	assert.Equal(t, start+2, tpl.Active.Version)
	assert.Equal(t, system, tpl.Active.BodyHTML)
	assert.False(t, tpl.Active.IsSystem)

	// Предпросмотр всех 8 видов на демо-данных: без незаменённых полей.
	for _, k := range document.Kinds {
		var cur tplDetail
		w.c.Do(http.MethodGet, "/settings/document-templates/"+k, nil).JSON(t, &cur)
		r = w.c.Do(http.MethodPost, "/settings/document-templates/"+k+"/preview", map[string]any{"bodyHtml": cur.Active.BodyHTML})
		require.Equal(t, http.StatusOK, r.Status, k+": "+string(r.Body))
		assert.Equal(t, "application/pdf", r.Header.Get("Content-Type"))
		html := last(w.env)
		assert.NotContains(t, html, "{{", k)
		assert.NotContains(t, html, "________________", k+": у всех полей есть пример")
	}

	// Gotenberg недоступен — 502 с понятным текстом.
	w.env.PDF.Error = errors.New("connection refused")
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil)
	assert.Equal(t, http.StatusBadGateway, r.Status)
	assert.Contains(t, string(r.Body), "Сервис формирования PDF недоступен")
	w.env.PDF.Error = nil

	var vars struct {
		Documents map[string][]struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		} `json:"documents"`
	}
	w.c.Do(http.MethodGet, "/settings/template-variables", nil).JSON(t, &vars)
	assert.Len(t, vars.Documents, 8)
	assert.NotEmpty(t, vars.Documents["act_return"])
}

// Документы и шаблоны — только собственнику (NFR-Sec-02).
func TestDocumentsOwnerOnly_NFR_Sec_02(t *testing.T) {
	w := setup(t, "f6")
	id := w.contract(t, "2026-03-15", "2027-03-14")
	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status)
	doc := r.ID(t)
	tc := w.env.ActiveTenant(t, "tenant-f6@landlord.local", "Пароль-наним-66")
	for _, path := range []string{"/documents/" + doc, "/documents/" + doc + "/pdf", "/contracts/" + id + "/documents",
		"/settings/document-templates", "/settings/template-variables"} {
		assert.Equal(t, http.StatusForbidden, tc.Do(http.MethodGet, path, nil).Status, path)
	}
	assert.Equal(t, http.StatusForbidden, tc.Do(http.MethodPost, "/contracts/"+id+"/documents/contract", nil).Status)
}
