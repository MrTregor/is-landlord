//go:build integration

package payment_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

const tenantPassword = "Пароль-кабинет-1"

// online — договор с начислением за октябрь и наниматель с учётной записью.
type online struct {
	world
	id     string
	tenant *testsupport.Client
}

func newOnline(t *testing.T, suffix string, mod func(map[string]any)) online {
	t.Helper()
	w := setup(t, "2026-10-12", suffix)
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) {
		m["firstDueDate"] = "2026-10-10"
		if mod != nil {
			mod(m)
		}
	})
	tc := w.env.TenantUser(t, w.tenant, "kuznetsov-"+suffix+"@landlord.local", tenantPassword)
	return online{world: w, id: id, tenant: tc}
}

type started struct {
	PaymentID  string `json:"paymentId"`
	PaymentURL string `json:"paymentUrl"`
}

func (o online) start(t *testing.T, purpose string, chargeIDs ...string) started {
	t.Helper()
	ids := append([]string{}, chargeIDs...)
	r := o.tenant.Do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": o.id, "purpose": purpose, "chargeIds": ids})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var s started
	r.JSON(t, &s)
	return s
}

// notify — уведомление банка от имени эмулятора (без сессии и CSRF).
func (o online) notify(t *testing.T, body map[string]any) testsupport.Response {
	t.Helper()
	return o.env.NewClient(t).Do(http.MethodPost, "/integrations/tbank/notify", body)
}

// process — выполнение задач tbank.process_notification по необработанным событиям.
func (o online) process(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	rows, err := testsupport.Owner(t).Query(ctx, `SELECT id FROM webhook_events WHERE processed_at IS NULL ORDER BY received_at`)
	require.NoError(t, err)
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		require.NoError(t, o.env.App.Payments.ProcessNotification(ctx, id))
	}
	return len(ids)
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// IT-05: онлайн-оплата — Init в фейковый Т-Банк → уведомление CONFIRMED → платёж succeeded; то же уведомление ещё
// два раза → один платёж, одна квитанция, три ответа «OK» (NFR-Rel-02, FR-06.1).
func TestOnlinePayment_IT05_NFR_Rel_02(t *testing.T) {
	o := newOnline(t, "o1", nil)
	s := o.start(t, "charges")
	assert.True(t, strings.HasPrefix(s.PaymentURL, o.env.Bank.Server.URL+"/pay/"), s.PaymentURL)
	again := o.start(t, "charges")
	assert.Equal(t, s.PaymentID, again.PaymentID, "начатая оплата с тем же составом возвращается повторно")

	var sum struct {
		DueTotalKop    int64 `json:"dueTotalKop"`
		PendingPayment *struct {
			ID        string `json:"id"`
			AmountKop int64  `json:"amountKop"`
		} `json:"pendingPayment"`
	}
	o.tenant.Do(http.MethodGet, "/cabinet/summary", nil).JSON(t, &sum)
	assert.Equal(t, int64(2800000), sum.DueTotalKop)
	require.NotNil(t, sum.PendingPayment)
	assert.Equal(t, s.PaymentID, sum.PendingPayment.ID)

	bankID := o.env.Bank.Last()
	n := o.env.Bank.Notification(bankID, "CONFIRMED")
	for range 3 {
		r := o.notify(t, n)
		require.Equal(t, http.StatusOK, r.Status)
		assert.Equal(t, "OK", string(r.Body))
	}
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM webhook_events WHERE payload->>'OrderId' = $1`, s.PaymentID))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM river_job j JOIN webhook_events e ON e.id::text = j.args->>'webhookEventId'
		WHERE j.kind = 'tbank.process_notification' AND e.payload->>'OrderId' = $1`, s.PaymentID))
	assert.Equal(t, 1, o.process(t))

	p := o.payment(t, s.PaymentID)
	assert.Equal(t, "succeeded", p.Status)
	assert.Equal(t, "online", p.Method)
	require.NotNil(t, p.ReceiptNumber)
	assert.Equal(t, int64(2800000), p.AllocatedKop)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM payments WHERE contract_id = $1`, o.id))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM river_job WHERE kind = 'document.payment_receipt' AND args->>'paymentId' = $1`, s.PaymentID))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'owner_payment_received' AND related_entity_id = $1`,
		s.PaymentID))
	var received string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT received_on::text FROM payments WHERE id = $1`, s.PaymentID).Scan(&received))
	assert.Equal(t, "2026-10-12", received)

	// Уведомление AUTHORIZED после CONFIRMED не меняет проведённый платёж.
	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(bankID, "AUTHORIZED")).Status)
	o.process(t)
	assert.Equal(t, "succeeded", o.payment(t, s.PaymentID).Status)

	_, err := o.env.App.Payments.Receipt(context.Background(), uuid.MustParse(s.PaymentID))
	require.NoError(t, err)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM documents WHERE payment_id = $1`, s.PaymentID))

	var st struct {
		Status        string  `json:"status"`
		AmountKop     int64   `json:"amountKop"`
		ReceiptNumber *string `json:"receiptNumber"`
	}
	o.tenant.Do(http.MethodGet, "/cabinet/payments/"+s.PaymentID, nil).JSON(t, &st)
	assert.Equal(t, "succeeded", st.Status)
	assert.Equal(t, p.ReceiptNumber, st.ReceiptNumber)
	var pays []struct {
		ID         string  `json:"id"`
		ReceiptURL *string `json:"receiptUrl"`
	}
	o.tenant.Do(http.MethodGet, "/cabinet/payments?contractId="+o.id, nil).JSON(t, &pays)
	require.Len(t, pays, 1)
	require.NotNil(t, pays[0].ReceiptURL)
	r := o.tenant.Do(http.MethodGet, strings.TrimPrefix(*pays[0].ReceiptURL, "/api/v1"), nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Contains(t, string(r.Body), "%PDF")

	o.tenant.Do(http.MethodGet, "/cabinet/summary", nil).JSON(t, &sum)
	assert.Equal(t, int64(0), sum.DueTotalKop)
	assert.Nil(t, sum.PendingPayment)
}

// IT-06: уведомление с неверным Token → 403 без тела, платёж не меняется, событие не сохраняется (NFR-Sec-01).
func TestNotificationBadToken_IT06_NFR_Sec_01(t *testing.T) {
	o := newOnline(t, "o2", nil)
	s := o.start(t, "charges")
	n := o.env.Bank.Notification(o.env.Bank.Last(), "CONFIRMED")
	n["Token"] = strings.Repeat("0", 64)
	r := o.notify(t, n)
	assert.Equal(t, http.StatusForbidden, r.Status)
	assert.Empty(t, string(r.Body))
	n = o.env.Bank.Notification(o.env.Bank.Last(), "CONFIRMED")
	n["Amount"] = 1
	assert.Equal(t, http.StatusForbidden, o.notify(t, n).Status, "изменённое после подписи уведомление")
	assert.Equal(t, 0, count(t, `SELECT count(*) FROM webhook_events WHERE payload->>'OrderId' = $1`, s.PaymentID))
	assert.Equal(t, "pending", o.payment(t, s.PaymentID).Status)
}

// Отказ банка: REJECTED → failed; позже CONFIRMED по тому же платежу → проведён (08 разд. 1.5).
func TestRejectedThenConfirmed_FR06_1(t *testing.T) {
	o := newOnline(t, "o3", nil)
	s := o.start(t, "charges")
	bankID := o.env.Bank.Last()
	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(bankID, "REJECTED")).Status)
	o.process(t)
	assert.Equal(t, "failed", o.payment(t, s.PaymentID).Status)
	assert.NotEqual(t, s.PaymentID, o.start(t, "charges").PaymentID, "неуспешная оплата не переиспользуется")

	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(bankID, "CONFIRMED")).Status)
	o.process(t)
	assert.Equal(t, "succeeded", o.payment(t, s.PaymentID).Status)
}

// Ошибка Init: платёж failed с кодом банка, клиенту 502 «Платёжный сервис недоступен» (08 разд. 1.3).
func TestInitError_FR06_1(t *testing.T) {
	o := newOnline(t, "o4", nil)
	o.env.Bank.InitError = "9999"
	r := o.tenant.Do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": o.id, "purpose": "charges", "chargeIds": []string{}})
	assert.Equal(t, http.StatusBadGateway, r.Status)
	assert.Contains(t, string(r.Body), "Платёжный сервис недоступен")
	var status, provider string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT status, provider_status FROM payments WHERE contract_id = $1`, o.id).Scan(&status, &provider))
	assert.Equal(t, "failed", status)
	assert.Equal(t, "9999", provider)
}

// Залог онлайн: сумма — «осталось внести»; после оплаты остаток залога растёт (FR-05.2).
func TestOnlineDeposit_FR05_2(t *testing.T) {
	o := newOnline(t, "o5", func(m map[string]any) { m["depositKop"] = 2800000 })
	s := o.start(t, "deposit")
	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(o.env.Bank.Last(), "CONFIRMED")).Status)
	o.process(t)
	p := o.payment(t, s.PaymentID)
	assert.Equal(t, "succeeded", p.Status)
	assert.Equal(t, int64(2800000), p.AmountKop)
	var sum struct {
		DepositBalanceKop int64 `json:"depositBalanceKop"`
		DepositToPayKop   int64 `json:"depositToPayKop"`
	}
	o.tenant.Do(http.MethodGet, "/cabinet/summary?contractId="+o.id, nil).JSON(t, &sum)
	assert.Equal(t, int64(2800000), sum.DepositBalanceKop)
	assert.Equal(t, int64(0), sum.DepositToPayKop)
	r := o.tenant.Do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": o.id, "purpose": "deposit", "chargeIds": []string{}})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "залог внесён полностью")
}

// «Проверить статус» (08 разд. 1.6) и задача payments.expire_pending (07): оплаченный по GetState — проводится,
// неоплаченный с истёкшей ссылкой — failed DEADLINE_EXPIRED.
func TestCheckStatusAndExpire_FR06_1(t *testing.T) {
	o := newOnline(t, "o6", nil)
	oct := rentOf(o.charges(t, o.id), "2026-10-01")
	s := o.start(t, "charges", oct.ID)
	o.env.Bank.SetStatus(o.env.Bank.Last(), "CONFIRMED")
	r := o.c.Do(http.MethodPost, "/payments/"+s.PaymentID+"/check-status", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, "succeeded", o.payment(t, s.PaymentID).Status)
	r = o.c.Do(http.MethodPost, "/payments/"+s.PaymentID+"/check-status", nil)
	assert.Equal(t, http.StatusConflict, r.Status, "проведённый платёж не проверяется")

	// Новое начисление и неоплаченная ссылка, срок которой истёк.
	r = o.c.Do(http.MethodPost, "/charges", map[string]any{
		"contractId": o.id, "kind": "other", "title": "Ключи", "amountKop": 50000, "dueDate": "2026-10-20", "reason": "Дубликат ключей",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	s2 := o.start(t, "charges")
	o.env.Clock.Advance(25 * time.Hour)
	_, err := o.env.App.Payments.RunExpirePending(context.Background(), jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-13")})
	require.NoError(t, err)
	var status, provider string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT status, provider_status FROM payments WHERE id = $1`, s2.PaymentID).Scan(&status, &provider))
	assert.Equal(t, "failed", status)
	assert.Equal(t, "DEADLINE_EXPIRED", provider)
}

// Кабинет: наниматель B не видит договор, платежи и квитанции нанимателя A (404); методы кабинета — только
// нанимателю (собственнику 403) (NFR-Sec-02, IT-08 в части платежей).
func TestCabinetAccess_NFR_Sec_02(t *testing.T) {
	o := newOnline(t, "o7", nil)
	s := o.start(t, "charges")
	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(o.env.Bank.Last(), "CONFIRMED")).Status)
	o.process(t)
	other := o.env.ActiveTenant(t, "other-o7@landlord.local", tenantPassword)
	for _, path := range []string{"/cabinet/summary?contractId=" + o.id, "/cabinet/charges?contractId=" + o.id,
		"/cabinet/payments?contractId=" + o.id, "/cabinet/payments/" + s.PaymentID, "/cabinet/payments/" + s.PaymentID + "/receipt.pdf"} {
		assert.Equal(t, http.StatusNotFound, other.Do(http.MethodGet, path, nil).Status, path)
	}
	r := other.Do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": o.id, "purpose": "charges", "chargeIds": []string{}})
	assert.Equal(t, http.StatusNotFound, r.Status)
	var sum struct {
		Contracts  []any   `json:"contracts"`
		ContractID *string `json:"contractId"`
	}
	other.Do(http.MethodGet, "/cabinet/summary", nil).JSON(t, &sum)
	assert.Empty(t, sum.Contracts)
	assert.Nil(t, sum.ContractID)
	assert.Equal(t, http.StatusForbidden, o.c.Do(http.MethodGet, "/cabinet/summary", nil).Status)

	var charges []struct {
		Kind      string `json:"kind"`
		AmountKop int64  `json:"amountKop"`
	}
	r = o.tenant.Do(http.MethodGet, "/cabinet/charges?contractId="+o.id, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &charges)
	assert.NotEmpty(t, charges)
}
