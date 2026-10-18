//go:build integration

package fiscal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-чеки-1"

type world struct {
	env   *testsupport.Env
	c     *testsupport.Client
	email string
	sfx   string
}

func setup(t *testing.T, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-12")
	email := "fiscal-" + sfx + "@landlord.local"
	c := env.ActiveOwner(t, email, password)
	var s map[string]any
	c.Do(http.MethodGet, "/settings/organization", nil).JSON(t, &s)
	s["fullName"], s["shortName"], s["inn"], s["address"] = "Индивидуальный предприниматель Гордейчик Александр Васильевич",
		"ИП Гордейчик А.В.", "507500363778", "Московская область, г. Руза"
	s["email"], s["signatoryName"] = "owner@landlord.local", "А.В. Гордейчик"
	delete(s, "updatedAt")
	delete(s, "updatedBy")
	r := c.Do(http.MethodPut, "/settings/organization", s)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return world{env: env, c: c, email: email, sfx: sfx}
}

// lease — объект с режимом чека mode, наниматель (legal — ООО «Ромашка» с e-mail в нежилом помещении;
// individual — физлицо только с телефоном), договор с 01.10.2026 на 45 000 ₽ в месяц.
func (w world) lease(t *testing.T, code, mode string, legal bool) string {
	t.Helper()
	kind, rooms := "residential", any(2)
	if legal {
		kind, rooms = "non_residential", nil
	}
	r := w.c.Do(http.MethodPost, "/properties", map[string]any{
		"code": code + " " + w.sfx, "kind": kind, "address": "г. Руза, ул. Солнечная, д. 7, пом. " + code + w.sfx, "areaSqm": "50.00",
		"floor": 1, "rooms": rooms, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": false,
		"expectedRentKop": 4500000, "fiscalMode": mode, "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	tenant := map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич", "docType": "passport_rf",
		"docNumber": "45 18 333444", "registrationAddress": "г. Москва, ул. Тихая, д. 3", "phone": "8 (900) 123-45-67",
	}
	if legal {
		tenant = map[string]any{
			"kind": "legal", "orgName": "ООО «Ромашка»", "inn": "5075123457", "kpp": "507501001", "ogrn": "1235000012345",
			"legalAddress": "Московская область, г. Руза", "signatoryName": "Иванов Иван Иванович", "signatoryPosition": "Директор",
			"signatoryBasis": "Устава", "email": "romashka-" + code + w.sfx + "@landlord.local",
		}
	}
	r = w.c.Do(http.MethodPost, "/tenants", tenant)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": prop, "tenantId": r.ID(t), "startDate": "2026-10-01", "endDate": "2027-08-31", "rentKop": 4500000,
		"paymentDay": 10, "depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false,
		"penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := r.ID(t)
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": "2026-10-01", "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return id
}

func (w world) pay(t *testing.T, contractID string, amount int64, method string) string {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contractID, "purpose": "charges", "amountKop": amount, "receivedOn": "2026-10-12", "method": method,
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return r.ID(t)
}

type receipt struct {
	ID            string  `json:"id"`
	PaymentID     *string `json:"paymentId"`
	RefundID      *string `json:"refundId"`
	ReceiptNumber *string `json:"receiptNumber"`
	Contract      *struct {
		Number string `json:"number"`
	} `json:"contract"`
	Operation               string  `json:"operation"`
	Provider                string  `json:"provider"`
	Status                  string  `json:"status"`
	TotalKop                int64   `json:"totalKop"`
	FiscalReceiptNumber     *int    `json:"fiscalReceiptNumber"`
	ShiftNumber             *int    `json:"shiftNumber"`
	FnNumber                *string `json:"fnNumber"`
	FiscalDocumentNumber    *int64  `json:"fiscalDocumentNumber"`
	FiscalDocumentAttribute *int64  `json:"fiscalDocumentAttribute"`
	ReceiptDatetime         *string `json:"receiptDatetime"`
	OfdReceiptURL           *string `json:"ofdReceiptUrl"`
	NpdReceiptURL           *string `json:"npdReceiptUrl"`
	ErrorCode               *string `json:"errorCode"`
	ErrorText               *string `json:"errorText"`
	Attempts                int     `json:"attempts"`
}

// paymentReceipt — чек из карточки платежа.
func (w world) paymentReceipt(t *testing.T, paymentID string) *receipt {
	t.Helper()
	var d struct {
		FiscalStatus  *string  `json:"fiscalStatus"`
		FiscalReceipt *receipt `json:"fiscalReceipt"`
	}
	r := w.c.Do(http.MethodGet, "/payments/"+paymentID, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &d)
	if d.FiscalReceipt != nil {
		require.NotNil(t, d.FiscalStatus)
		assert.Equal(t, d.FiscalReceipt.Status, *d.FiscalStatus)
	}
	return d.FiscalReceipt
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func jobCount(t *testing.T, kind, receiptID string) int {
	t.Helper()
	return count(t, `SELECT count(*) FROM river_job WHERE kind = $1 AND args->>'fiscalReceiptId' = $2`, kind, receiptID)
}

// IT-07: платёж по нежилому объекту (облачная касса) → чек queued → wait → касса done → реквизиты сохранены,
// письмо нанимателю; колбэк сохраняется один раз; отмена платежа с чеком запрещена; возврат переплаты — чек
// возврата прихода (FR-06.2, FR-06.4, NFR-Rel-02).
func TestReceiptFlow_IT07_FR_06_2_FR_06_4(t *testing.T) {
	w := setup(t, "it07")
	ctx := context.Background()
	contract := w.lease(t, "Офис", "cloud_kkt", true)
	pid := w.pay(t, contract, 4600000, "bank_transfer")

	f := w.paymentReceipt(t, pid)
	require.NotNil(t, f)
	assert.Equal(t, "queued", f.Status)
	assert.Equal(t, "sell", f.Operation)
	assert.Equal(t, "atol", f.Provider)
	assert.Equal(t, int64(4600000), f.TotalKop)
	assert.Equal(t, 1, jobCount(t, "fiscal.send", f.ID), "задача fiscal.send в транзакции проведения")
	id := uuid.MustParse(f.ID)

	require.NoError(t, w.env.App.Fiscal.Send(ctx, id))
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "wait", f.Status)
	assert.Equal(t, 1, f.Attempts)
	assert.Equal(t, 1, jobCount(t, "fiscal.poll", f.ID))
	doc := w.env.Atol.Last()

	// Запрос чека (08 разд. 3.2): юрлицо — e-mail, наименование, ИНН; позиции — распределение и аванс.
	body := w.env.Atol.LastBody()
	assert.Equal(t, f.ID, body["external_id"])
	rc := body["receipt"].(map[string]any)
	client := rc["client"].(map[string]any)
	assert.Equal(t, "romashka-офисit07@landlord.local", client["email"])
	assert.Equal(t, "ООО «Ромашка»", client["name"])
	assert.Equal(t, "5075123457", client["inn"])
	assert.NotContains(t, client, "phone")
	company := rc["company"].(map[string]any)
	assert.Equal(t, "usn_income", company["sno"])
	assert.Equal(t, "507500363778", company["inn"])
	items := rc["items"].([]any)
	require.Len(t, items, 2)
	var number string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT number FROM contracts WHERE id = $1`, contract).Scan(&number))
	first, adv := items[0].(map[string]any), items[1].(map[string]any)
	assert.Equal(t, "Арендная плата за октябрь 2026, договор № "+number, first["name"])
	assert.Equal(t, json.Number("45000.00"), first["sum"])
	assert.Equal(t, "full_payment", first["payment_method"])
	assert.Equal(t, "Аванс по договору № "+number, adv["name"])
	assert.Equal(t, json.Number("1000.00"), adv["sum"])
	assert.Equal(t, "advance", adv["payment_method"])
	assert.Equal(t, json.Number("46000.00"), rc["total"])
	assert.Equal(t, json.Number("1"), rc["payments"].([]any)[0].(map[string]any)["type"])

	// Касса ещё обрабатывает документ — опрос через 5 с.
	next, err := w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, next)

	// Колбэк: событие сохраняется один раз, ответ 200 (повтор — тоже 200, без дубля).
	w.env.Atol.Done(doc)
	cb := w.env.NewClient(t)
	for range 2 {
		r := cb.Do(http.MethodPost, "/integrations/atol/callback", w.env.Atol.Report(doc))
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	}
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM webhook_events WHERE provider = 'atol' AND event_key = $1`, doc+":done"))

	next, err = w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Zero(t, next)
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "done", f.Status)
	require.NotNil(t, f.FiscalDocumentNumber)
	assert.Equal(t, int64(1042), *f.FiscalDocumentNumber)
	assert.Equal(t, int64(3326595612), *f.FiscalDocumentAttribute)
	assert.Equal(t, 7, *f.FiscalReceiptNumber)
	assert.Equal(t, 23, *f.ShiftNumber)
	assert.Equal(t, "9999078900012345", *f.FnNumber)
	assert.Equal(t, "http://localhost:8082/receipt/"+doc, *f.OfdReceiptURL)
	at, err := time.Parse(time.RFC3339, *f.ReceiptDatetime)
	require.NoError(t, err)
	assert.True(t, at.Equal(time.Date(2026, 10, 16, 16, 30, 5, 0, time.UTC)), "время чека по Москве: %s", *f.ReceiptDatetime)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE dedupe_key = 'fiscal_receipt:' || $1`, f.ID))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'fiscal.done' AND entity_id = $1`, f.ID))
	next, err = w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Zero(t, next, "повторный опрос сформированного чека ничего не делает")
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE dedupe_key = 'fiscal_receipt:' || $1`, f.ID))

	// Кабинет нанимателя: «Чек придёт на e-mail» и ссылка на чек в списке платежей (15 разд. 3, 4).
	var tenantID string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT tenant_id FROM contracts WHERE id = $1`, contract).Scan(&tenantID))
	tc := w.env.TenantUser(t, tenantID, "romashka-cabinet-it07@landlord.local", password)
	var st struct {
		FiscalReceiptExpected bool `json:"fiscalReceiptExpected"`
	}
	r := tc.Do(http.MethodGet, "/cabinet/payments/"+pid, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &st)
	assert.True(t, st.FiscalReceiptExpected)
	var cp []struct {
		ID            string  `json:"id"`
		OfdReceiptURL *string `json:"ofdReceiptUrl"`
	}
	tc.Do(http.MethodGet, "/cabinet/payments?contractId="+contract, nil).JSON(t, &cp)
	require.Len(t, cp, 1)
	require.NotNil(t, cp[0].OfdReceiptURL)
	assert.Equal(t, "http://localhost:8082/receipt/"+doc, *cp[0].OfdReceiptURL)

	// Экран «Чеки».
	var list struct {
		Items []receipt `json:"items"`
		Total int       `json:"total"`
	}
	w.c.Do(http.MethodGet, "/fiscal-receipts?status=done&from=2026-10-01&to=2026-10-31", nil).JSON(t, &list)
	require.NotEmpty(t, list.Items)
	found := false
	for _, it := range list.Items {
		if it.ID == f.ID {
			found = true
			require.NotNil(t, it.Contract)
			assert.Equal(t, number, it.Contract.Number)
			require.NotNil(t, it.ReceiptNumber)
		}
	}
	assert.True(t, found)

	// Отмена платежа с чеком запрещена.
	r = w.c.Do(http.MethodPost, "/payments/"+pid+"/cancel", map[string]any{"reason": "ошибка"})
	assert.Equal(t, http.StatusConflict, r.Status)
	assert.Equal(t, "fiscal_receipt_exists", r.Code())

	// Возврат переплаты → чек возврата прихода.
	r = w.c.Do(http.MethodPost, "/payments/"+pid+"/refunds", map[string]any{
		"amountKop": 100000, "refundedOn": "2026-10-12", "method": "bank_transfer", "reason": "Возврат переплаты",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var refundReceipt string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT f.id FROM fiscal_receipts f JOIN refunds r ON r.id = f.refund_id
		WHERE r.payment_id = $1 AND f.operation = 'sell_refund' AND f.status = 'queued' AND f.total_kop = 100000`, pid).Scan(&refundReceipt))
	require.NoError(t, w.env.App.Fiscal.Send(ctx, uuid.MustParse(refundReceipt)))
	assert.Equal(t, "sell_refund", w.env.Atol.Paths[len(w.env.Atol.Paths)-1])
	ri := w.env.Atol.LastBody()["receipt"].(map[string]any)["items"].([]any)
	require.Len(t, ri, 1)
	assert.Equal(t, "Возврат переплаты по договору № "+number, ri[0].(map[string]any)["name"])
	assert.Equal(t, "advance", ri[0].(map[string]any)["payment_method"])
	assert.Equal(t, json.Number("1000.00"), ri[0].(map[string]any)["sum"])
}

// IT-07: ошибка кассы → fail и письмо собственнику; «Повторить» → новая отправка; ошибка при обработке документа
// → fail и ещё одно письмо; повтор чека не в статусе «Ошибка» — 409 (FR-06.4).
func TestReceiptErrors_IT07_FR_06_4(t *testing.T) {
	w := setup(t, "err")
	ctx := context.Background()
	contract := w.lease(t, "Склад", "cloud_kkt", true)
	pid := w.pay(t, contract, 4500000, "cash")
	f := w.paymentReceipt(t, pid)
	id := uuid.MustParse(f.ID)

	w.env.Atol.SendError = &[2]string{"32", "Ошибка проверки сумм"}
	require.NoError(t, w.env.App.Fiscal.Send(ctx, id))
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "fail", f.Status)
	assert.Equal(t, "32", *f.ErrorCode)
	assert.Equal(t, "Ошибка проверки сумм", *f.ErrorText)
	assert.Equal(t, json.Number("0"), w.env.Atol.LastBody()["receipt"].(map[string]any)["payments"].([]any)[0].(map[string]any)["type"],
		"наличные — тип оплаты 0")
	owners := count(t, `SELECT count(*) FROM users WHERE role = 'owner' AND status = 'active'`)
	assert.Equal(t, owners, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'owner_fiscal_error' AND related_entity_id = $1`, f.ID))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'fiscal.fail' AND entity_id = $1`, f.ID))

	// Отмена платежа с чеком в статусе «Ошибка» разрешена (05 разд. 10.5), но сначала — повтор.
	r := w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/retry", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var rr receipt
	r.JSON(t, &rr)
	assert.Equal(t, "queued", rr.Status)
	assert.Nil(t, rr.ErrorText)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'fiscal.retry' AND entity_id = $1`, f.ID))
	r = w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/retry", nil)
	assert.Equal(t, http.StatusConflict, r.Status)

	require.NoError(t, w.env.App.Fiscal.Send(ctx, id))
	doc := w.env.Atol.Last()
	w.env.Atol.Fail(doc, "50", "Ошибка фискального накопителя")
	next, err := w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Zero(t, next)
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "fail", f.Status)
	assert.Equal(t, "50", *f.ErrorCode)
	assert.Equal(t, 2, f.Attempts)
	assert.Equal(t, 2*owners, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'owner_fiscal_error' AND related_entity_id = $1`, f.ID))

	// Платёж с чеком «Ошибка» отменяется; повторить чек отменённого платежа нельзя.
	r = w.c.Do(http.MethodPost, "/payments/"+pid+"/cancel", map[string]any{"reason": "ошибочный платёж"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/retry", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
}

// Сбой связи с кассой — ошибка задачи (River повторит), чек остаётся в очереди; запрос сформирован один раз
// и при повторе отправляется то же тело (касса вернёт тот же документ) (FR-06.4, NFR-Rel-02).
func TestReceiptNetworkRetry_FR_06_4(t *testing.T) {
	w := setup(t, "net")
	ctx := context.Background()
	contract := w.lease(t, "Сеть", "cloud_kkt", true)
	pid := w.pay(t, contract, 4500000, "bank_transfer")
	f := w.paymentReceipt(t, pid)
	id := uuid.MustParse(f.ID)

	w.env.Atol.Down = true
	require.Error(t, w.env.App.Fiscal.Send(ctx, id))
	assert.Equal(t, "queued", w.paymentReceipt(t, pid).Status)
	var first string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT request_payload::text FROM fiscal_receipts WHERE id = $1`, f.ID).Scan(&first))

	w.env.Atol.Down = false
	w.env.Clock.Advance(time.Hour)
	require.NoError(t, w.env.App.Fiscal.Send(ctx, id))
	assert.Equal(t, "wait", w.paymentReceipt(t, pid).Status)
	var again string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT request_payload::text FROM fiscal_receipts WHERE id = $1`, f.ID).Scan(&again))
	assert.Equal(t, first, again)
	assert.Equal(t, "12.10.2026 12:00:00", w.env.Atol.LastBody()["timestamp"], "отметка времени — из первого формирования запроса")

	// Сбой связи при опросе — повтор через 5 с (первая минута ожидания).
	w.env.Atol.Down = true
	next, err := w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, next)
	w.env.Atol.Down = false
}

// Физлицо: в чек — только телефон; без e-mail и телефона — чек «Ошибка» без обращения к кассе.
func TestReceiptClientIndividual_FR_06_4(t *testing.T) {
	w := setup(t, "nocontact")
	ctx := context.Background()
	contract := w.lease(t, "Кв", "cloud_kkt", false)

	// Физлицо только с телефоном: в чек — телефон +7…, без ФИО (08 разд. 3.2, 12 разд. 5).
	pid := w.pay(t, contract, 4500000, "bank_transfer")
	f := w.paymentReceipt(t, pid)
	require.NoError(t, w.env.App.Fiscal.Send(ctx, uuid.MustParse(f.ID)))
	client := w.env.Atol.LastBody()["receipt"].(map[string]any)["client"].(map[string]any)
	assert.Equal(t, map[string]any{"phone": "+79001234567"}, client)

	// Контакты удалены (например, наниматель обезличен) — чек «Ошибка» без обращения к кассе.
	_, err := testsupport.Owner(t).Exec(ctx, `UPDATE tenants SET email = NULL, phone = NULL
		WHERE id = (SELECT tenant_id FROM contracts WHERE id = $1)`, contract)
	require.NoError(t, err)
	pid = w.pay(t, contract, 100000, "bank_transfer")
	f = w.paymentReceipt(t, pid)
	sent := w.env.Atol.Sent()
	require.NoError(t, w.env.App.Fiscal.Send(ctx, uuid.MustParse(f.ID)))
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "fail", f.Status)
	assert.Equal(t, "У нанимателя нет e-mail или телефона для отправки чека", *f.ErrorText)
	assert.Nil(t, f.ErrorCode)
	assert.Equal(t, sent, w.env.Atol.Sent())
}

// Режимы объекта: «Чек не требуется» — чека нет; «Чек НПД (вручную)» — задача собственнику и ссылка «Мой налог»
// (FR-06.2).
func TestReceiptModes_FR_06_2(t *testing.T) {
	w := setup(t, "modes")
	none := w.lease(t, "Н", "none", false)
	pid := w.pay(t, none, 4500000, "cash")
	assert.Nil(t, w.paymentReceipt(t, pid))
	var tenantID string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), `SELECT tenant_id FROM contracts WHERE id = $1`, none).Scan(&tenantID))
	var st struct {
		FiscalReceiptExpected *bool `json:"fiscalReceiptExpected"`
	}
	w.env.TenantUser(t, tenantID, "modes-cabinet@landlord.local", password).Do(http.MethodGet, "/cabinet/payments/"+pid, nil).JSON(t, &st)
	require.NotNil(t, st.FiscalReceiptExpected)
	assert.False(t, *st.FiscalReceiptExpected)

	npd := w.lease(t, "Д", "npd_manual", false)
	pid = w.pay(t, npd, 4500000, "bank_transfer")
	f := w.paymentReceipt(t, pid)
	require.NotNil(t, f)
	assert.Equal(t, "manual_required", f.Status)
	assert.Equal(t, "npd_manual", f.Provider)
	assert.Zero(t, jobCount(t, "fiscal.send", f.ID))

	r := w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/manual", map[string]any{"npdReceiptUrl": "https://example.ru/чек"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
	link := "https://lknpd.nalog.ru/api/v1/receipt/507500363778/200abc/print"
	r = w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/manual", map[string]any{"npdReceiptUrl": link})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "done", f.Status)
	assert.Equal(t, link, *f.NpdReceiptURL)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'fiscal.manual' AND entity_id = $1`, f.ID))
	r = w.c.Do(http.MethodPost, "/fiscal-receipts/"+f.ID+"/manual", map[string]any{"npdReceiptUrl": link})
	assert.Equal(t, http.StatusConflict, r.Status)

	var list struct {
		Items []receipt `json:"items"`
	}
	w.c.Do(http.MethodGet, "/fiscal-receipts?status=manual_required", nil).JSON(t, &list)
	for _, it := range list.Items {
		assert.NotEqual(t, f.ID, it.ID)
	}
	assert.Equal(t, http.StatusNotFound, w.c.Do(http.MethodPost, "/fiscal-receipts/"+uuid.NewString()+"/retry", nil).Status)
}

// fiscal.reconcile: чек в wait дольше минуты — опрос; в queued дольше 5 минут — отправка (если задачи нет);
// через 24 часа без ответа — «Нет ответа от кассы» (07 Фоновые задачи, FR-06.4).
func TestReconcileAndNoAnswer_FR_06_4(t *testing.T) {
	w := setup(t, "reconcile")
	ctx := context.Background()
	contract := w.lease(t, "Р", "cloud_kkt", true)
	pid := w.pay(t, contract, 4500000, "bank_transfer")
	f := w.paymentReceipt(t, pid)
	id := uuid.MustParse(f.ID)
	conn := testsupport.Owner(t)
	age := func(interval string) {
		t.Helper()
		_, err := conn.Exec(ctx, `SET session_replication_role = replica`)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `UPDATE fiscal_receipts SET updated_at = now() - $2::interval WHERE id = $1`, f.ID, interval)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `SET session_replication_role = DEFAULT`)
		require.NoError(t, err)
	}
	clearJobs := func() {
		_, err := conn.Exec(ctx, `DELETE FROM river_job WHERE args->>'fiscalReceiptId' = $1`, f.ID)
		require.NoError(t, err)
	}

	// В очереди 6 минут без задачи → fiscal.send.
	clearJobs()
	age("6 minutes")
	stats, err := w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, stats.Counts["sends"], 1)
	assert.Equal(t, 1, jobCount(t, "fiscal.send", f.ID))
	_, err = w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.Equal(t, 1, jobCount(t, "fiscal.send", f.ID), "задача уже есть — новая не ставится")

	require.NoError(t, w.env.App.Fiscal.Send(ctx, id))
	clearJobs()
	age("2 minutes")
	_, err = w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.Equal(t, 1, jobCount(t, "fiscal.poll", f.ID))

	// После минуты ожидания — опрос раз в 5 минут; через 24 часа — ошибка.
	next, err := w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, next)
	age("25 hours")
	next, err = w.env.App.Fiscal.Poll(ctx, id)
	require.NoError(t, err)
	assert.Zero(t, next)
	f = w.paymentReceipt(t, pid)
	assert.Equal(t, "fail", f.Status)
	assert.Equal(t, "Нет ответа от кассы", *f.ErrorText)
}

// № 63: касса недоступна — чек в queued дольше часа попадает в утреннюю сводку («Ошибки чеков и задач»),
// дольше суток — письмо собственнику owner_fiscal_error (одно на время ожидания); чек остаётся в очереди (FR-06.4).
func TestQueuedTooLong_FR_06_4(t *testing.T) {
	w := setup(t, "stuck")
	ctx := context.Background()
	contract := w.lease(t, "З", "cloud_kkt", true)
	pid := w.pay(t, contract, 4500000, "bank_transfer")
	f := w.paymentReceipt(t, pid)
	conn := testsupport.Owner(t)
	age := func(interval string) {
		t.Helper()
		_, err := conn.Exec(ctx, `SET session_replication_role = replica`)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `UPDATE fiscal_receipts SET updated_at = now() - $2::interval WHERE id = $1`, f.ID, interval)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, `SET session_replication_role = DEFAULT`)
		require.NoError(t, err)
	}
	letters := func() int {
		t.Helper()
		var n int
		require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM email_log WHERE template_code = 'owner_fiscal_error'
			AND related_entity_id = $1 AND to_email = $2`, f.ID, w.email).Scan(&n))
		return n
	}

	age("2 hours")
	_, err := w.env.App.Mailing.RunOwnerDigest(ctx, reconcileArgs())
	require.NoError(t, err)
	var digest string
	require.NoError(t, conn.QueryRow(ctx, `SELECT body_html FROM email_log WHERE template_code = 'owner_daily_digest'
		AND dedupe_key LIKE 'owner_digest:2026-10-12:%' AND to_email = $1`, w.email).Scan(&digest))
	assert.Contains(t, digest, "Чеки в очереди больше часа (касса недоступна): ")
	_, err = w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.Zero(t, letters(), "меньше суток — письма нет")

	age("25 hours")
	_, err = w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.Equal(t, 1, letters())
	var body string
	require.NoError(t, conn.QueryRow(ctx, `SELECT body_html FROM email_log WHERE template_code = 'owner_fiscal_error'
		AND related_entity_id = $1 AND to_email = $2`, f.ID, w.email).Scan(&body))
	assert.Contains(t, body, "больше суток не удаётся отправить в кассу")
	_, err = w.env.App.Fiscal.RunReconcile(ctx, reconcileArgs())
	require.NoError(t, err)
	assert.Equal(t, 1, letters(), "повтор не дублирует письмо")
	assert.Equal(t, "queued", w.paymentReceipt(t, pid).Status)
}

// Чеки — только собственнику; колбэк по неизвестному uuid — 200 без действий.
func TestReceiptsAccess(t *testing.T) {
	w := setup(t, "access")
	tenant := w.env.ActiveTenant(t, "fiscal-tenant@landlord.local", password)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/fiscal-receipts", nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodPost, "/fiscal-receipts/"+uuid.NewString()+"/retry", nil).Status)
	r := w.env.NewClient(t).Do(http.MethodPost, "/integrations/atol/callback", map[string]any{"uuid": "нет-такого", "status": "done"})
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM webhook_events WHERE provider = 'atol' AND event_key = 'нет-такого:done' AND result = 'ignored'`))
}

func reconcileArgs() jobs.DailyArgs {
	return jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-12")}
}
