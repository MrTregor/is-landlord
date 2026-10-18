//go:build integration

package payment_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/mailer"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-платежи-1"

// captureSender запоминает вложения отправленных писем.
type captureSender struct {
	names, data []string
}

func (c *captureSender) Send(_ context.Context, m mailer.Message) error {
	for _, a := range m.Attachments {
		b, err := io.ReadAll(a.Data)
		if err != nil {
			return err
		}
		c.names = append(c.names, a.Name)
		c.data = append(c.data, string(b))
	}
	return nil
}

type world struct {
	env      *testsupport.Env
	c        *testsupport.Client
	email    string
	property string
	tenant   string
}

func setup(t *testing.T, today, suffix string) world {
	t.Helper()
	env := testsupport.NewEnv(t, today)
	email := "payments-" + suffix + "@landlord.local"
	c := env.ActiveOwner(t, email, password)
	r := c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "П " + suffix, "kind": "residential", "address": "г. Руза, ул. Садовая, д. 2, кв. " + suffix, "areaSqm": "40.00",
		"floor": 2, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": false,
		"expectedRentKop": 2800000, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич", "docType": "passport_rf",
		"docNumber": "45 18 333444", "registrationAddress": "г. Москва, ул. Тихая, д. 3", "email": "kuznetsov-" + suffix + "@landlord.local",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return world{env: env, c: c, email: email, property: prop, tenant: r.ID(t)}
}

func (w world) at(t *testing.T, day string) {
	t.Helper()
	w.env.Clock.Set(date.MustParse(day).Time().Add(12 * time.Hour))
	w.c.Login(w.email, password)
}

func (w world) contract(t *testing.T, start, end string, rent int64, mod func(map[string]any)) string {
	t.Helper()
	body := map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": start, "endDate": end, "rentKop": rent, "paymentDay": 10,
		"depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false, "penaltyGraceDays": 0,
		"penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	}
	if mod != nil {
		mod(body)
	}
	r := w.c.Do(http.MethodPost, "/contracts", body)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := r.ID(t)
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": start, "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return id
}

type charge struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	PeriodMonth *string `json:"periodMonth"`
	AmountKop   int64   `json:"amountKop"`
	PaidKop     int64   `json:"paidKop"`
	Status      string  `json:"status"`
}

func (w world) charges(t *testing.T, contractID string) []charge {
	t.Helper()
	var list struct {
		Items []charge `json:"items"`
	}
	r := w.c.Do(http.MethodGet, "/charges?pageSize=100&sort=dueDate&contractId="+contractID, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	return list.Items
}

func rentOf(list []charge, month string) charge {
	for _, c := range list {
		if c.Kind == "rent" && c.PeriodMonth != nil && *c.PeriodMonth == month && c.Status != "cancelled" {
			return c
		}
	}
	return charge{}
}

type payment struct {
	ID                string  `json:"id"`
	AmountKop         int64   `json:"amountKop"`
	AllocatedKop      int64   `json:"allocatedKop"`
	AdvanceKop        int64   `json:"advanceKop"`
	Status            string  `json:"status"`
	Source            string  `json:"source"`
	Method            string  `json:"method"`
	ReceiptNumber     *string `json:"receiptNumber"`
	ReceiptDocumentID *string `json:"receiptDocumentId"`
	Allocations       []struct {
		ChargeID   string  `json:"chargeId"`
		AmountKop  int64   `json:"amountKop"`
		ReversedAt *string `json:"reversedAt"`
	} `json:"allocations"`
	Refunds []struct {
		AmountKop int64 `json:"amountKop"`
	} `json:"refunds"`
}

func (w world) pay(t *testing.T, contractID string, amount int64, on string, chargeIDs ...string) payment {
	t.Helper()
	ids := []string{}
	ids = append(ids, chargeIDs...)
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contractID, "purpose": "charges", "amountKop": amount, "receivedOn": on, "method": "bank_transfer",
		"chargeIds": ids, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var p payment
	r.JSON(t, &p)
	return p
}

func (w world) payment(t *testing.T, id string) payment {
	t.Helper()
	var p payment
	r := w.c.Do(http.MethodGet, "/payments/"+id, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	return p
}

type discrepancy struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	AmountKop  int64   `json:"amountKop"`
	PaymentID  *string `json:"paymentId"`
	Resolution *string `json:"resolution"`
}

func (w world) discrepancies(t *testing.T, contractID, status string) []discrepancy {
	t.Helper()
	var list struct {
		Items []discrepancy `json:"items"`
	}
	r := w.c.Do(http.MethodGet, "/discrepancies?status="+status+"&contractId="+contractID, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	return list.Items
}

func byKind(list []discrepancy, kind string) *discrepancy {
	for i := range list {
		if list[i].Kind == kind {
			return &list[i]
		}
	}
	return nil
}

// IT-04, ТК-21: ручной платёж 40 000 (пример A1) — сентябрь 28 000, октябрь 12 000; номер квитанции КВ-2026-NNNNN,
// задача квитанции; квитанция PDF создана, письмо payment_receipt с вложением в email_log; недоплата 16 000
// (FR-06.1, FR-06.3, FR-07.1).
func TestManualPaymentAllocation_IT04_TK21_FR06_1_FR07_1(t *testing.T) {
	w := setup(t, "2026-10-12", "a1")
	id := w.contract(t, "2026-09-01", "2027-08-31", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-09-10" })
	list := w.charges(t, id)
	sep, oct := rentOf(list, "2026-09-01"), rentOf(list, "2026-10-01")
	require.NotEmpty(t, sep.ID)
	require.NotEmpty(t, oct.ID)

	p := w.pay(t, id, 4000000, "2026-10-12")
	require.NotNil(t, p.ReceiptNumber)
	assert.Regexp(t, `^КВ-2026-\d{5}$`, *p.ReceiptNumber)
	assert.Equal(t, "succeeded", p.Status)
	assert.Equal(t, int64(4000000), p.AllocatedKop)
	assert.Equal(t, int64(0), p.AdvanceKop)
	got := map[string]int64{}
	for _, a := range p.Allocations {
		got[a.ChargeID] += a.AmountKop
	}
	assert.Equal(t, int64(2800000), got[sep.ID], "сентябрь")
	assert.Equal(t, int64(1200000), got[oct.ID], "октябрь")

	under := byKind(w.discrepancies(t, id, "open"), "underpayment")
	require.NotNil(t, under, "недоплата по сроку 10.10")
	assert.Equal(t, int64(1600000), under.AmountKop)

	ctx := context.Background()
	conn := testsupport.Owner(t)
	var jobs int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'document.payment_receipt' AND args->>'paymentId' = $1`,
		p.ID).Scan(&jobs))
	assert.Equal(t, 1, jobs, "задача квитанции поставлена в транзакции платежа")

	// Выполнение задачи document.payment_receipt (07): квитанция и письмо; повтор ничего не создаёт.
	_, err := w.env.App.Payments.Receipt(ctx, uuid.MustParse(p.ID))
	require.NoError(t, err)
	_, err = w.env.App.Payments.Receipt(ctx, uuid.MustParse(p.ID))
	require.NoError(t, err)
	assert.Contains(t, w.env.PDF.Last(), *p.ReceiptNumber)
	assert.Contains(t, w.env.PDF.Last(), "Плата за наём")
	assert.Contains(t, w.env.PDF.Last(), "сорок тысяч рублей 00 копеек")
	p = w.payment(t, p.ID)
	require.NotNil(t, p.ReceiptDocumentID, "квитанция — документ")
	var mails int
	var attachments string
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*), max(attachments::text) FROM email_log
		WHERE template_code = 'payment_receipt' AND dedupe_key = 'payment_receipt:' || $1`, p.ID).Scan(&mails, &attachments))
	assert.Equal(t, 1, mails)
	assert.Contains(t, attachments, "Квитанция "+*p.ReceiptNumber+".pdf")
	var docs int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM documents WHERE payment_id = $1 AND kind = 'payment_receipt'`, p.ID).Scan(&docs))
	assert.Equal(t, 1, docs)

	// Отправка письма воркером email.send: квитанция PDF во вложении.
	var logID string
	require.NoError(t, conn.QueryRow(ctx, `SELECT id FROM email_log WHERE dedupe_key = 'payment_receipt:' || $1`, p.ID).Scan(&logID))
	sender := &captureSender{}
	ew := &notify.EmailSendWorker{DB: w.env.App.DB, Clock: w.env.Clock, Sender: sender, Files: w.env.App.Files.Store()}
	require.NoError(t, ew.Work(ctx, &river.Job[notify.EmailSendArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 10},
		Args: notify.EmailSendArgs{EmailLogID: uuid.MustParse(logID)}}))
	require.Len(t, sender.names, 1)
	assert.Equal(t, "Квитанция "+*p.ReceiptNumber+".pdf", sender.names[0])
	assert.Contains(t, sender.data[0], "%PDF")

	r := w.c.Do(http.MethodGet, "/payments/"+p.ID+"/receipt.pdf", nil)
	require.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, string(r.Body), "%PDF")

	// Список: поиск по номеру квитанции.
	var page struct {
		Items []payment `json:"items"`
		Total int       `json:"total"`
	}
	w.c.Do(http.MethodGet, "/payments?q="+*p.ReceiptNumber, nil).JSON(t, &page)
	require.Equal(t, 1, page.Total)
	assert.Equal(t, p.ID, page.Items[0].ID)
}

// Выбранные начисления погашаются первыми; переплата → расхождение «переплата», недоплата закрывается; возврат
// и перенос в залог уменьшают аванс; новое начисление погашается авансом (FR-07.1, FR-07.3).
func TestOverpaymentLifecycle_FR07_3(t *testing.T) {
	w := setup(t, "2026-10-12", "b2")
	id := w.contract(t, "2026-09-01", "2027-08-31", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-09-10" })
	list := w.charges(t, id)
	sep, oct := rentOf(list, "2026-09-01"), rentOf(list, "2026-10-01")

	p1 := w.pay(t, id, 1000000, "2026-10-12", oct.ID)
	require.Len(t, p1.Allocations, 1)
	assert.Equal(t, oct.ID, p1.Allocations[0].ChargeID, "выбранное начисление — первым")
	assert.Equal(t, int64(1000000), p1.Allocations[0].AmountKop)

	p2 := w.pay(t, id, 6000000, "2026-10-12")
	assert.Equal(t, int64(4600000), p2.AllocatedKop)
	assert.Equal(t, int64(1400000), p2.AdvanceKop)
	open := w.discrepancies(t, id, "open")
	over := byKind(open, "overpayment")
	require.NotNil(t, over)
	assert.Equal(t, int64(1400000), over.AmountKop)
	assert.Nil(t, byKind(open, "underpayment"), "долг погашен — недоплаты нет")
	_ = sep

	// Возврат части переплаты через разбор расхождения: расхождение остаётся открытым, сумма уменьшается.
	r := w.c.Do(http.MethodPost, "/discrepancies/"+over.ID+"/resolve", map[string]any{
		"action": "refund", "amountKop": 400000, "refundedOn": "2026-10-12", "method": "cash", "note": "Вернули наличными",
	})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var res discrepancy
	r.JSON(t, &res)
	assert.Equal(t, "open", res.Status)
	assert.Equal(t, int64(1000000), res.AmountKop)
	p := w.payment(t, p2.ID)
	assert.Equal(t, int64(1000000), p.AdvanceKop)
	require.Len(t, p.Refunds, 1)

	// Возврат больше аванса — ошибка.
	r = w.c.Do(http.MethodPost, "/payments/"+p2.ID+"/refunds", map[string]any{
		"amountKop": 2000000, "refundedOn": "2026-10-12", "method": "cash", "reason": "Ошибка",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Сумма больше аванса")

	// Перенос в залог из карточки платежа.
	r = w.c.Do(http.MethodPost, "/payments/"+p2.ID+"/move-to-deposit", map[string]any{"amountKop": 500000, "opDate": "2026-10-12"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var dep struct {
		BalanceKop int64 `json:"balanceKop"`
		Operations []struct {
			Kind        string `json:"kind"`
			FromAdvance bool   `json:"fromAdvance"`
		} `json:"operations"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/deposit", nil).JSON(t, &dep)
	assert.Equal(t, int64(500000), dep.BalanceKop)
	require.Len(t, dep.Operations, 1)
	assert.True(t, dep.Operations[0].FromAdvance)
	over = byKind(w.discrepancies(t, id, "open"), "overpayment")
	require.NotNil(t, over, "частичный перенос — расхождение открыто")
	assert.Equal(t, int64(500000), over.AmountKop)

	// Новое начисление погашается авансом (05 разд. 10.4).
	r = w.c.Do(http.MethodPost, "/charges", map[string]any{
		"contractId": id, "kind": "damage", "title": "Замена замка", "amountKop": 300000, "dueDate": "2026-10-20", "reason": "Сломан замок",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var dmg charge
	r.JSON(t, &dmg)
	assert.Equal(t, int64(300000), dmg.PaidKop)
	assert.Equal(t, int64(200000), w.payment(t, p2.ID).AdvanceKop)

	var settle struct {
		DebtKop             int64 `json:"debtKop"`
		AdvanceKop          int64 `json:"advanceKop"`
		DepositBalanceKop   int64 `json:"depositBalanceKop"`
		PaymentsWithAdvance []struct {
			PaymentID string `json:"paymentId"`
		} `json:"paymentsWithAdvance"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/settlement", nil).JSON(t, &settle)
	assert.Equal(t, int64(0), settle.DebtKop)
	assert.Equal(t, int64(200000), settle.AdvanceKop)
	assert.Equal(t, int64(500000), settle.DepositBalanceKop)
	require.Len(t, settle.PaymentsWithAdvance, 1)
	assert.Equal(t, p2.ID, settle.PaymentsWithAdvance[0].PaymentID)
	over = byKind(w.discrepancies(t, id, "open"), "overpayment")
	require.NotNil(t, over)
	assert.Equal(t, int64(200000), over.AmountKop, "сумма расхождения следует за авансом")

	// Возврат всего аванса из карточки платежа закрывает расхождение решением «возвращено».
	r = w.c.Do(http.MethodPost, "/payments/"+p2.ID+"/refunds", map[string]any{
		"amountKop": 200000, "refundedOn": "2026-10-12", "method": "bank_transfer", "reason": "Возврат остатка",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	assert.Nil(t, byKind(w.discrepancies(t, id, "open"), "overpayment"))
	closed := byKind(w.discrepancies(t, id, "resolved"), "overpayment")
	require.NotNil(t, closed)
	assert.Equal(t, "refunded", *closed.Resolution)
}

// № 46 (open-questions): платёж с действующим переносом переплаты в залог не отменяется, пока перенос не снят
// отменой последней операции по залогу; перенос из переплаты возвращает сумму в аванс и платёж не отменяет (№ 47).
func TestCancelAfterAdvanceToDeposit_FR06_1(t *testing.T) {
	w := setup(t, "2026-10-12", "k9")
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-10-10" })
	p := w.pay(t, id, 3000000, "2026-10-12")
	r := w.c.Do(http.MethodPost, "/payments/"+p.ID+"/move-to-deposit", map[string]any{"amountKop": 200000, "opDate": "2026-10-12"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var op struct {
		ID string `json:"id"`
	}
	r.JSON(t, &op)
	over := byKind(w.discrepancies(t, id, "resolved"), "overpayment")
	require.NotNil(t, over, "перенесён весь аванс — расхождение закрыто")
	assert.Equal(t, "moved_to_deposit", *over.Resolution)

	r = w.c.Do(http.MethodPost, "/payments/"+p.ID+"/cancel", map[string]any{"reason": "Ошибка"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Сначала отмените его в разделе «Залог»")

	r = w.c.Do(http.MethodPost, "/deposit-operations/"+op.ID+"/cancel", map[string]any{"reason": "Перенос ошибочный"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	pp := w.payment(t, p.ID)
	assert.Equal(t, "succeeded", pp.Status, "отмена переноса не отменяет платёж")
	assert.Equal(t, int64(200000), pp.AdvanceKop, "сумма вернулась в аванс")

	r = w.c.Do(http.MethodPost, "/payments/"+p.ID+"/cancel", map[string]any{"reason": "Ошибка"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
}

// № 47 (open-questions): внесение залога онлайн-платежом не отменяется (05 разд. 10.5 — только manual,
// bank_import, deposit_offset), ошибка та же, что при отмене платежа.
func TestCancelOnlineDepositReceipt_FR05_2(t *testing.T) {
	// Залог вносится онлайн-оплатой из кабинета через банк (M7, № 26), а не записью в БД.
	o := newOnline(t, "m0", func(m map[string]any) { m["depositKop"] = 2800000 })
	w := o.world
	pid := o.start(t, "deposit").PaymentID
	require.Equal(t, http.StatusOK, o.notify(t, o.env.Bank.Notification(o.env.Bank.Last(), "CONFIRMED")).Status)
	o.process(t)
	require.Equal(t, "succeeded", w.payment(t, pid).Status)
	var opID string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT id FROM deposit_transactions WHERE payment_id = $1 AND kind = 'receipt'`, pid).Scan(&opID))

	r := w.c.Do(http.MethodPost, "/payments/"+pid+"/cancel", map[string]any{"reason": "Ошибка"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	want := r.Code()
	body := string(r.Body)
	r = w.c.Do(http.MethodPost, "/deposit-operations/"+opID+"/cancel", map[string]any{"reason": "Ошибка"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Equal(t, want, r.Code())
	assert.Contains(t, string(r.Body), "Онлайн-платёж не отменяется")
	assert.Contains(t, body, "Онлайн-платёж не отменяется")
	assert.Equal(t, "succeeded", w.payment(t, pid).Status)
}

// Отмена платежа: распределения отменяются, начисления снова открыты, переплата закрывается автоматически;
// повторная отмена — 409; платёж с возвратом не отменяется (FR-06.1, 05 разд. 10.5).
func TestCancelPayment_FR06_1(t *testing.T) {
	w := setup(t, "2026-10-12", "c3")
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-10-10" })
	oct := rentOf(w.charges(t, id), "2026-10-01")

	p := w.pay(t, id, 3000000, "2026-10-12")
	assert.Equal(t, int64(200000), p.AdvanceKop)
	r := w.c.Do(http.MethodPost, "/payments/"+p.ID+"/cancel", map[string]any{"reason": "Ошибочная регистрация"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var c payment
	r.JSON(t, &c)
	assert.Equal(t, "cancelled", c.Status)
	for _, a := range c.Allocations {
		assert.NotNil(t, a.ReversedAt)
	}
	for _, ch := range w.charges(t, id) {
		if ch.ID == oct.ID {
			assert.Equal(t, "open", ch.Status)
			assert.Equal(t, int64(0), ch.PaidKop)
		}
	}
	assert.Nil(t, byKind(w.discrepancies(t, id, "open"), "overpayment"))
	closed := byKind(w.discrepancies(t, id, "resolved"), "overpayment")
	require.NotNil(t, closed)
	assert.Equal(t, "auto_closed", *closed.Resolution)

	r = w.c.Do(http.MethodPost, "/payments/"+p.ID+"/cancel", map[string]any{"reason": "Ещё раз"})
	assert.Equal(t, http.StatusConflict, r.Status)

	p2 := w.pay(t, id, 3000000, "2026-10-12")
	r = w.c.Do(http.MethodPost, "/payments/"+p2.ID+"/refunds", map[string]any{
		"amountKop": 100000, "refundedOn": "2026-10-12", "method": "bank_transfer", "reason": "Возврат переплаты",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/payments/"+p2.ID+"/cancel", map[string]any{"reason": "Ошибка"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "По платежу оформлен возврат переплаты — отмена невозможна")

	// Чек по платежу в статусе done запрещает отмену (409 fiscal_receipt_exists).
	p3 := w.pay(t, id, 100000, "2026-10-12")
	_, err := testsupport.Owner(t).Exec(context.Background(), `INSERT INTO fiscal_receipts (payment_id, operation, provider, status, total_kop)
		VALUES ($1, 'sell', 'atol', 'done', 100000)`, p3.ID)
	require.NoError(t, err)
	r = w.c.Do(http.MethodPost, "/payments/"+p3.ID+"/cancel", map[string]any{"reason": "Ошибка"})
	assert.Equal(t, http.StatusConflict, r.Status)
	assert.Equal(t, "fiscal_receipt_exists", r.Code())
}

// IT-16, ТК-15: залог — внесение, удержание больше остатка → 422 «Сумма больше остатка залога», удержание, зачёт
// в счёт долга → платёж deposit_offset, возврат остатка → остаток 0; отмена только последней операции (FR-05.2).
func TestDeposit_IT16_TK15_FR05_2(t *testing.T) {
	w := setup(t, "2026-10-12", "d4")
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) {
		m["firstDueDate"] = "2026-10-10"
		m["depositKop"] = 2800000
	})
	oct := rentOf(w.charges(t, id), "2026-10-01")

	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": id, "purpose": "deposit", "amountKop": 2800000, "receivedOn": "2026-10-01", "method": "cash", "chargeIds": []string{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var depPay payment
	r.JSON(t, &depPay)
	assert.Equal(t, int64(0), depPay.AllocatedKop, "залог не распределяется на начисления")
	assert.Equal(t, int64(0), depPay.AdvanceKop)

	type depositDTO struct {
		ExpectedKop int64 `json:"expectedKop"`
		ReceivedKop int64 `json:"receivedKop"`
		BalanceKop  int64 `json:"balanceKop"`
		Operations  []struct {
			ID        string  `json:"id"`
			Kind      string  `json:"kind"`
			AmountKop int64   `json:"amountKop"`
			PaymentID *string `json:"paymentId"`
			CanCancel bool    `json:"canCancel"`
		} `json:"operations"`
	}
	deposit := func() depositDTO {
		var d depositDTO
		r := w.c.Do(http.MethodGet, "/contracts/"+id+"/deposit", nil)
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
		r.JSON(t, &d)
		return d
	}
	d := deposit()
	assert.Equal(t, int64(2800000), d.ExpectedKop)
	assert.Equal(t, int64(2800000), d.ReceivedKop)
	assert.Equal(t, int64(2800000), d.BalanceKop)

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/deposit/withholdings", map[string]any{
		"amountKop": 3000000, "opDate": "2026-10-12", "reason": "Ремонт двери",
	})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Equal(t, "business_rule", r.Code())
	assert.Contains(t, string(r.Body), "Сумма больше остатка залога")

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/deposit/withholdings", map[string]any{
		"amountKop": 300000, "opDate": "2026-10-12", "reason": "Ремонт двери",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/deposit/offsets", map[string]any{
		"amountKop": 1000000, "opDate": "2026-10-12", "chargeIds": []string{}, "reason": "Зачёт в счёт платы за октябрь",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var off struct {
		ID        string  `json:"id"`
		Kind      string  `json:"kind"`
		PaymentID *string `json:"paymentId"`
	}
	r.JSON(t, &off)
	assert.Equal(t, "offset", off.Kind)
	require.NotNil(t, off.PaymentID)
	op := w.payment(t, *off.PaymentID)
	assert.Equal(t, "deposit_offset", op.Source)
	assert.Equal(t, "deposit_offset", op.Method)
	require.Len(t, op.Allocations, 1)
	assert.Equal(t, oct.ID, op.Allocations[0].ChargeID)

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/deposit/refunds", map[string]any{
		"amountKop": 1500000, "opDate": "2026-10-12", "method": "bank_transfer", "reason": "Возврат остатка",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	d = deposit()
	assert.Equal(t, int64(0), d.BalanceKop, "остаток 0")
	require.Len(t, d.Operations, 4)
	assert.True(t, d.Operations[3].CanCancel)
	assert.False(t, d.Operations[1].CanCancel)

	// Отменить можно только последнюю операцию.
	r = w.c.Do(http.MethodPost, "/deposit-operations/"+d.Operations[1].ID+"/cancel", map[string]any{"reason": "Ошибка"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = w.c.Do(http.MethodPost, "/deposit-operations/"+d.Operations[3].ID+"/cancel", map[string]any{"reason": "Не вернули"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	// Отмена зачёта отменяет связанный платёж.
	r = w.c.Do(http.MethodPost, "/deposit-operations/"+d.Operations[2].ID+"/cancel", map[string]any{"reason": "Зачёт ошибочный"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, "cancelled", w.payment(t, *off.PaymentID).Status)
	d = deposit()
	assert.Equal(t, int64(2500000), d.BalanceKop)

	var mails int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM email_log WHERE template_code IN ('deposit_withheld', 'deposit_refunded')`).Scan(&mails))
	assert.Equal(t, 2, mails, "письма об удержании и возврате")

	var detail struct {
		Balances struct {
			DepositReceivedKop int64 `json:"depositReceivedKop"`
			DepositBalanceKop  int64 `json:"depositBalanceKop"`
		} `json:"balances"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id, nil).JSON(t, &detail)
	assert.Equal(t, int64(2800000), detail.Balances.DepositReceivedKop)
	assert.Equal(t, int64(2500000), detail.Balances.DepositBalanceKop)
}

// IT-12 полностью, R4: расторжение с оплаченным ноябрём → корректировка, отмена декабря, аванс 18 666,67 и
// расхождение «переплата» на эту сумму (FR-03.2).
func TestTerminationOverpayment_IT12_R4_FR03_2(t *testing.T) {
	w := setup(t, "2026-11-28", "e5")
	id := w.contract(t, "2026-11-01", "2027-10-31", 2800000, func(m map[string]any) { m["paymentDay"] = 1 })
	nov := rentOf(w.charges(t, id), "2026-11-01")
	require.NotEmpty(t, nov.ID)
	w.at(t, "2026-11-01")
	p := w.pay(t, id, 2800000, "2026-11-01", nov.ID)
	w.at(t, "2026-11-28")

	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/terminate", map[string]any{"terminationDate": "2026-11-10", "initiator": "tenant", "reason": "Переезд"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	over := byKind(w.discrepancies(t, id, "open"), "overpayment")
	require.NotNil(t, over, "расхождение «переплата»")
	assert.Equal(t, int64(1866667), over.AmountKop)
	require.NotNil(t, over.PaymentID)
	assert.Equal(t, p.ID, *over.PaymentID)
	assert.Equal(t, int64(1866667), w.payment(t, p.ID).AdvanceKop)

	// «Оставить авансом».
	r = w.c.Do(http.MethodPost, "/discrepancies/"+over.ID+"/resolve", map[string]any{"action": "keep_advance", "note": nil})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	// Неподходящее действие для вида — 422.
	r = w.c.Do(http.MethodPost, "/discrepancies/"+over.ID+"/resolve", map[string]any{"action": "acknowledge", "note": "x"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

// Недоплата: «Принять к сведению» требует комментарий; задача пеней закрывает погашенную недоплату (05 разд. 10.7).
func TestUnderpayment_FR07_3(t *testing.T) {
	w := setup(t, "2026-10-12", "f6")
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-10-10" })
	w.pay(t, id, 1000000, "2026-10-12")
	under := byKind(w.discrepancies(t, id, "open"), "underpayment")
	require.NotNil(t, under)
	assert.Equal(t, int64(1800000), under.AmountKop)

	r := w.c.Do(http.MethodPost, "/discrepancies/"+under.ID+"/resolve", map[string]any{"action": "acknowledge"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	// Доплата позже без задачи: новая недоплата не создаётся, открытая закрывается при погашении.
	w.pay(t, id, 1800000, "2026-10-12")
	assert.Nil(t, byKind(w.discrepancies(t, id, "open"), "underpayment"))
}

// Недоплата закрывается задачей billing.penalties_daily, если долг погашен другим способом (05 разд. 10.7).
func TestUnderpaymentClosedByPenaltiesJob_FR07_3(t *testing.T) {
	w := setup(t, "2026-10-12", "g7")
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, func(m map[string]any) { m["firstDueDate"] = "2026-10-10" })
	w.pay(t, id, 1000000, "2026-10-12")
	require.NotNil(t, byKind(w.discrepancies(t, id, "open"), "underpayment"))
	oct := rentOf(w.charges(t, id), "2026-10-01")
	r := w.c.Do(http.MethodPost, "/charges/"+oct.ID+"/adjust", map[string]any{"deltaKop": -1800000, "reason": "Скидка"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	_, err := w.env.App.Billing.RunPenaltiesDaily(context.Background(), jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-13")})
	require.NoError(t, err)
	assert.Nil(t, byKind(w.discrepancies(t, id, "open"), "underpayment"))
}

// Методы платежей, залога и расхождений — только собственнику (NFR-Sec-02).
func TestPaymentsOwnerOnly_NFR_Sec_02(t *testing.T) {
	w := setup(t, "2026-10-12", "h8")
	tc := w.env.ActiveTenant(t, "tenant-h8@landlord.local", password)
	id := w.contract(t, "2026-10-01", "2027-09-30", 2800000, nil)
	p := w.pay(t, id, 100000, "2026-10-12")
	for _, path := range []string{"/payments", "/payments/" + p.ID, "/discrepancies", "/contracts/" + id + "/deposit",
		"/contracts/" + id + "/settlement"} {
		r := tc.Do(http.MethodGet, path, nil)
		assert.Equal(t, http.StatusForbidden, r.Status, path)
	}
	r := tc.Do(http.MethodPost, "/payments/"+p.ID+"/cancel", map[string]any{"reason": "x"})
	assert.Equal(t, http.StatusForbidden, r.Status)
}
