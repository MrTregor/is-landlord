//go:build integration

package billing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-начисл-1"

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
	email := "billing-" + suffix + "@landlord.local"
	c := env.ActiveOwner(t, email, password)
	r := c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Б " + suffix, "kind": "residential", "address": "г. Руза, ул. Лесная, д. 1, кв. " + suffix, "areaSqm": "30.00",
		"floor": 1, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": false,
		"expectedRentKop": 3000000, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Смирнов", "firstName": "Олег", "docType": "passport_rf", "docNumber": "45 18 111222",
		"registrationAddress": "г. Москва, ул. Тихая, д. 2", "email": "smirnov-" + suffix + "@landlord.local",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return world{env: env, c: c, email: email, property: prop, tenant: r.ID(t)}
}

func (w world) at(t *testing.T, day string) date.Date {
	t.Helper()
	d := date.MustParse(day)
	w.env.Clock.Set(d.Time().Add(12 * time.Hour))
	w.c.Login(w.email, password)
	return d
}

func (w world) contract(t *testing.T, start, end string, rent int64, mod func(map[string]any)) string {
	t.Helper()
	body := map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": start, "endDate": end, "rentKop": rent, "paymentDay": 15,
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
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	Title          string         `json:"title"`
	PeriodMonth    *string        `json:"periodMonth"`
	DueDate        *string        `json:"dueDate"`
	AmountKop      int64          `json:"amountKop"`
	PaidKop        int64          `json:"paidKop"`
	OutstandingKop int64          `json:"outstandingKop"`
	Status         string         `json:"status"`
	PenaltyState   *string        `json:"penaltyState"`
	IsOverdue      bool           `json:"isOverdue"`
	CalcDetails    map[string]any `json:"calcDetails"`
	Adjustments    []struct {
		Kind     string `json:"kind"`
		DeltaKop int64  `json:"deltaKop"`
	} `json:"adjustments"`
}

func (w world) charges(t *testing.T, contractID string) []charge {
	t.Helper()
	var list struct {
		Items []charge `json:"items"`
		Total int      `json:"total"`
	}
	r := w.c.Do(http.MethodGet, "/charges?pageSize=100&sort=dueDate&contractId="+contractID, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	return list.Items
}

func byMonth(list []charge, kind, month string) *charge {
	for i := range list {
		if list[i].Kind == kind && list[i].PeriodMonth != nil && *list[i].PeriodMonth == month && list[i].Status != "cancelled" {
			return &list[i]
		}
	}
	return nil
}

// pay вносит проведённый платёж и распределение напрямую в БД (минуя сервис платежей: проверяются только начисления).
func pay(t *testing.T, contractID, chargeID string, amount int64, on string) {
	t.Helper()
	ctx := context.Background()
	conn := testsupport.Owner(t)
	var pid string
	require.NoError(t, conn.QueryRow(ctx, `INSERT INTO payments (contract_id, purpose, amount_kop, received_on, method, source, status, receipt_number)
		VALUES ($1, 'charges', $2, $3, 'bank_transfer', 'manual', 'succeeded', 'КВ-ТЕСТ-' || gen_random_uuid()) RETURNING id`,
		contractID, amount, on).Scan(&pid))
	_, err := conn.Exec(ctx, `INSERT INTO payment_allocations (payment_id, charge_id, amount_kop) VALUES ($1, $2, $3)`, pid, chargeID, amount)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE charges SET paid_kop = paid_kop + $2,
		status = CASE WHEN paid_kop + $2 >= amount_kop THEN 'paid' ELSE 'partially_paid' END WHERE id = $1`, chargeID, amount)
	require.NoError(t, err)
}

// IT-01, ТК-11: договор с датой начала в прошлом — начисления за все месяцы; март — 16 451,61 ₽ (R1, FR-05.1).
func TestActivationCreatesCharges_IT01_TK11_FR05_1(t *testing.T) {
	w := setup(t, "2026-10-16", "a1")
	id := w.contract(t, "2026-03-15", "2027-03-14", 3000000, nil)
	list := w.charges(t, id)
	require.Len(t, list, 8, "март–октябрь; ноябрь появится 29.10")
	mar := byMonth(list, "rent", "2026-03-01")
	require.NotNil(t, mar)
	assert.Equal(t, int64(1645161), mar.AmountKop)
	assert.Equal(t, "Плата за наём, 15–31 марта 2026", mar.Title)
	assert.Equal(t, "2026-03-15", *mar.DueDate)
	assert.True(t, mar.IsOverdue)
	apr := byMonth(list, "rent", "2026-04-01")
	assert.Equal(t, int64(3000000), apr.AmountKop)
	assert.Equal(t, "2026-04-15", *apr.DueDate)

	// Карточка договора: долг и просрочка.
	var d struct {
		Balances struct {
			DebtKop    int64 `json:"debtKop"`
			OverdueKop int64 `json:"overdueKop"`
		} `json:"balances"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id, nil).JSON(t, &d)
	assert.Equal(t, int64(1645161+7*3000000), d.Balances.DebtKop)
	assert.Equal(t, int64(1645161+7*3000000), d.Balances.OverdueKop)

	// IT-03: повторный запуск — начислений не больше; 29.10 — появляется ноябрь (R2).
	ctx := context.Background()
	_, err := w.env.App.Billing.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-16")})
	require.NoError(t, err)
	_, err = w.env.App.Billing.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-16")})
	require.NoError(t, err)
	assert.Len(t, w.charges(t, id), 8, "IT-03: идемпотентность")
	w.at(t, "2026-10-28")
	_, err = w.env.App.Billing.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-28")})
	require.NoError(t, err)
	assert.Len(t, w.charges(t, id), 8)
	w.at(t, "2026-10-29")
	_, err = w.env.App.Billing.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-29")})
	require.NoError(t, err)
	list = w.charges(t, id)
	require.Len(t, list, 9)
	assert.NotNil(t, byMonth(list, "rent", "2026-11-01"))
}

// ТК-12, P1: просрочка с частичной оплатой → пени 92,00 «Начисляются»; полная оплата → final (FR-05.3).
func TestPenalties_TK12_FR05_3(t *testing.T) {
	w := setup(t, "2026-10-01", "b2")
	id := w.contract(t, "2026-10-01", "2027-08-31", 2650000, func(m map[string]any) {
		m["paymentDay"] = 5
		m["firstDueDate"] = "2026-10-05"
		m["penaltyEnabled"] = true
		m["penaltyRatePct"] = "0.1"
		m["penaltyGraceDays"] = 3
	})
	oct := byMonth(w.charges(t, id), "rent", "2026-10-01")
	require.NotNil(t, oct)
	require.Equal(t, int64(2650000), oct.AmountKop)
	pay(t, id, oct.ID, 1500000, "2026-10-07")

	ctx := context.Background()
	w.at(t, "2026-10-17")
	_, err := w.env.App.Billing.RunPenaltiesDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-17")})
	require.NoError(t, err)
	list := w.charges(t, id)
	var pen *charge
	for i := range list {
		if list[i].Kind == "penalty" {
			pen = &list[i]
		}
	}
	require.NotNil(t, pen, "пени созданы")
	assert.Equal(t, int64(9200), pen.AmountKop)
	assert.Equal(t, "accruing", *pen.PenaltyState)
	assert.Nil(t, pen.DueDate)
	assert.Equal(t, "Пени по начислению «Плата за наём, октябрь 2026»", pen.Title)

	// Повторный запуск в тот же день не меняет сумму; через день — 9 200 + 1 150.
	_, err = w.env.App.Billing.RunPenaltiesDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-17")})
	require.NoError(t, err)
	w.at(t, "2026-10-18")
	_, err = w.env.App.Billing.RunPenaltiesDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-18")})
	require.NoError(t, err)
	var p charge
	w.c.Do(http.MethodGet, "/charges/"+pen.ID, nil).JSON(t, &p)
	assert.Equal(t, int64(10350), p.AmountKop)

	// Полная оплата 18.10 → final со сроком 18.10.
	pay(t, id, oct.ID, 1150000, "2026-10-18")
	w.at(t, "2026-10-20")
	_, err = w.env.App.Billing.RunPenaltiesDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-20")})
	require.NoError(t, err)
	w.c.Do(http.MethodGet, "/charges/"+pen.ID, nil).JSON(t, &p)
	assert.Equal(t, "final", *p.PenaltyState)
	assert.Equal(t, "2026-10-18", *p.DueDate)
	assert.Equal(t, int64(11500), p.AmountKop, "дни 09–18 по 11 500 (день оплаты входит в просрочку) = 10 × 11,50")

	// Ручное изменение и списание (manual) — задача больше не пересчитывает.
	r := w.c.Do(http.MethodPost, "/charges/"+pen.ID+"/penalty-set", map[string]any{"amountKop": 5000, "reason": "Договорились"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	assert.Equal(t, "manual", *p.PenaltyState)
	_, err = w.env.App.Billing.RunPenaltiesDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-20")})
	require.NoError(t, err)
	w.c.Do(http.MethodGet, "/charges/"+pen.ID, nil).JSON(t, &p)
	assert.Equal(t, int64(5000), p.AmountKop)
	r = w.c.Do(http.MethodPost, "/charges/"+pen.ID+"/penalty-write-off", map[string]any{"reason": "Списали"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	assert.Equal(t, int64(0), p.AmountKop)
	require.Len(t, p.Adjustments, 2)
	assert.Equal(t, "penalty_writeoff", p.Adjustments[1].Kind)
}

// IT-12 (расхождение «переплата» — в пакете payment), R4: расторжение с последним днём 10.11.2026 при оплаченном ноябре →
// корректировка −18 666,67, отмена декабря, излишек становится авансом (FR-03.2).
func TestTerminationRecalc_IT12_R4_FR03_2(t *testing.T) {
	w := setup(t, "2026-11-28", "c3")
	id := w.contract(t, "2026-11-01", "2027-10-31", 2800000, func(m map[string]any) { m["paymentDay"] = 1 })
	list := w.charges(t, id)
	nov := byMonth(list, "rent", "2026-11-01")
	dec := byMonth(list, "rent", "2026-12-01")
	require.NotNil(t, nov)
	require.NotNil(t, dec, "декабрь создан 28.11 (за 3 дня до срока)")
	pay(t, id, nov.ID, 2800000, "2026-11-01")

	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/terminate", map[string]any{"terminationDate": "2026-11-10", "initiator": "tenant", "reason": "Переезд в другой город"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))

	var n charge
	w.c.Do(http.MethodGet, "/charges/"+nov.ID, nil).JSON(t, &n)
	assert.Equal(t, int64(933333), n.AmountKop)
	assert.Equal(t, int64(933333), n.PaidKop, "излишек снят с распределения")
	assert.Equal(t, "paid", n.Status)
	require.Len(t, n.Adjustments, 1)
	assert.Equal(t, "termination", n.Adjustments[0].Kind)
	assert.Equal(t, int64(-1866667), n.Adjustments[0].DeltaKop)
	var dc charge
	w.c.Do(http.MethodGet, "/charges/"+dec.ID, nil).JSON(t, &dc)
	assert.Equal(t, "cancelled", dc.Status)

	var d struct {
		Balances struct {
			AdvanceKop int64 `json:"advanceKop"`
		} `json:"balances"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id, nil).JSON(t, &d)
	assert.Equal(t, int64(1866667), d.Balances.AdvanceKop, "18 666,67 ₽ — аванс")
}

// IT-14, ТК-13: индексация — за 30 дней черновик с 29 400; подписание → новая версия условий, ноябрь по новой плате (FR-05.4).
func TestIndexation_IT14_TK13_FR05_4(t *testing.T) {
	w := setup(t, "2026-10-01", "d4")
	id := w.contract(t, "2025-11-01", "2027-09-30", 2800000, func(m map[string]any) {
		m["paymentDay"] = 1
		m["indexationEnabled"] = true
		m["indexationPct"] = "5"
	})
	ctx := context.Background()
	_, err := w.env.App.Contract.RunIndexationDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-01")})
	require.NoError(t, err)
	var ams []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		NewRentKop  *int64 `json:"newRentKop"`
		AutoCreated bool   `json:"autoCreated"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/amendments", nil).JSON(t, &ams)
	require.Empty(t, ams, "за 31 день — рано")

	w.at(t, "2026-10-02")
	_, err = w.env.App.Contract.RunIndexationDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-02")})
	require.NoError(t, err)
	_, err = w.env.App.Contract.RunIndexationDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-02")})
	require.NoError(t, err)
	w.c.Do(http.MethodGet, "/contracts/"+id+"/amendments", nil).JSON(t, &ams)
	require.Len(t, ams, 1)
	assert.Equal(t, "indexation", ams[0].Kind)
	assert.True(t, ams[0].AutoCreated)
	assert.Equal(t, int64(2940000), *ams[0].NewRentKop)
	var mails int
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT count(*) FROM email_log WHERE template_code = 'owner_indexation_due' AND dedupe_key LIKE $1`,
		"owner_indexation:"+ams[0].ID+":%").Scan(&mails))
	assert.GreaterOrEqual(t, mails, 1, "по письму каждому активному собственнику")

	r := w.c.Do(http.MethodPost, "/amendments/"+ams[0].ID+"/sign", map[string]any{"signedOn": "2026-10-02"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	w.at(t, "2026-10-29")
	_, err = w.env.App.Billing.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-29")})
	require.NoError(t, err)
	nov := byMonth(w.charges(t, id), "rent", "2026-11-01")
	require.NotNil(t, nov)
	assert.Equal(t, int64(2940000), nov.AmountKop)
	var d struct {
		NextIndexationOn string `json:"nextIndexationOn"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id, nil).JSON(t, &d)
	assert.Equal(t, "2027-11-01", d.NextIndexationOn)
}

// Допсоглашение об изменении платы пересчитывает уже созданное оплаченное начисление: +1 400 (16 разд. 2, FR-05.4).
func TestTermsChangeRecalc_FR05_4(t *testing.T) {
	w := setup(t, "2027-03-29", "e5")
	id := w.contract(t, "2026-03-01", "2028-02-28", 2800000, func(m map[string]any) { m["paymentDay"] = 1 })
	apr := byMonth(w.charges(t, id), "rent", "2027-04-01")
	require.NotNil(t, apr)
	pay(t, id, apr.ID, 2800000, "2027-03-29")
	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/amendments", map[string]any{"kind": "rent_change", "effectiveFrom": "2027-04-01", "newRentKop": 2940000})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/amendments/"+r.ID(t)+"/sign", map[string]any{"signedOn": "2027-03-29"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var a charge
	w.c.Do(http.MethodGet, "/charges/"+apr.ID, nil).JSON(t, &a)
	assert.Equal(t, int64(2940000), a.AmountKop)
	assert.Equal(t, int64(140000), a.OutstandingKop, "долг 1 400")
	require.Len(t, a.Adjustments, 1)
	assert.Equal(t, int64(140000), a.Adjustments[0].DeltaKop)
}

// IT-15, ТК-14: показания нанимателя по всем счётчикам → принятие → начисление ЖКУ U1 1 739,83 ₽ (FR-05.5).
func TestUtilitiesFromReadings_IT15_TK14_FR05_5(t *testing.T) {
	w := setup(t, "2026-10-02", "f6")
	ctx := context.Background()
	meters := map[string]string{}
	for typ, init := range map[string]string{"cold_water": "112.400", "electricity": "8841"} {
		r := w.c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
			"type": typ, "serialNumber": nil, "initialValue": init, "initialDate": "2026-08-31", "verificationDue": nil, "note": nil})
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
		meters[typ] = r.ID(t)
	}
	for typ, price := range map[string]string{"cold_water": "54.37", "electricity": "6.43"} {
		r := w.c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": typ, "propertyId": w.property, "price": price, "effectiveFrom": "2026-01-01", "note": nil})
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}
	id := w.contract(t, "2026-09-01", "2027-07-31", 3000000, func(m map[string]any) {
		m["utilitiesMode"] = "meters"
		m["utilitiesFixedKop"] = 45000
		m["paymentDay"] = 10
	})
	conn := testsupport.Owner(t)
	var ids []string
	for typ, v := range map[string]string{"cold_water": "115.900", "electricity": "9012"} {
		var rid string
		require.NoError(t, conn.QueryRow(ctx, `INSERT INTO meter_readings (meter_id, contract_id, period_month, reading_date, value, source, status)
			VALUES ($1, $2, '2026-09-01', '2026-09-30', $3, 'tenant', 'submitted') RETURNING id`, meters[typ], id, v).Scan(&rid))
		ids = append(ids, rid)
	}
	var list struct {
		Items []struct {
			ID          string  `json:"id"`
			Consumption *string `json:"consumption"`
			Status      string  `json:"status"`
		} `json:"items"`
	}
	w.c.Do(http.MethodGet, "/meter-readings?status=submitted&propertyId="+w.property, nil).JSON(t, &list)
	require.Len(t, list.Items, 2)

	type decision struct {
		UtilitiesCharge *charge `json:"utilitiesCharge"`
	}
	var dec decision
	r := w.c.Do(http.MethodPost, "/meter-readings/"+ids[0]+"/accept", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &dec)
	assert.Nil(t, dec.UtilitiesCharge, "приняты показания не всех счётчиков")
	r = w.c.Do(http.MethodPost, "/meter-readings/"+ids[1]+"/accept", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &dec)
	require.NotNil(t, dec.UtilitiesCharge)
	assert.Equal(t, int64(173983), dec.UtilitiesCharge.AmountKop)
	assert.Equal(t, "Компенсация ЖКУ, сентябрь 2026", dec.UtilitiesCharge.Title)
	assert.Equal(t, "2026-10-10", *dec.UtilitiesCharge.DueDate)
	raw, _ := json.Marshal(dec.UtilitiesCharge.CalcDetails)
	assert.Contains(t, string(raw), `"tariff":"54.3700"`)

	// Повтор кнопкой — конфликт; показание меньше предыдущего — ошибка.
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/utilities-charge", map[string]any{"periodMonth": "2026-09-01"})
	assert.Equal(t, http.StatusConflict, r.Status)
	r = w.c.Do(http.MethodPost, "/meter-readings", map[string]any{"meterId": meters["cold_water"], "periodMonth": "2026-10-01", "readingDate": "2026-10-02", "value": "100"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Показание меньше предыдущего (115,9 от 30.09.2026)")

	// Кнопкой за октябрь без показаний электричества — расход 0 и предупреждение.
	r = w.c.Do(http.MethodPost, "/meter-readings", map[string]any{"meterId": meters["cold_water"], "periodMonth": "2026-10-01", "readingDate": "2026-10-02", "value": "116.900"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r.JSON(t, &dec)
	assert.Nil(t, dec.UtilitiesCharge)
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/utilities-charge", map[string]any{"periodMonth": "2026-10-01"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var oct charge
	r.JSON(t, &oct)
	assert.Equal(t, int64(5437+45000), oct.AmountKop)
	assert.NotEmpty(t, oct.CalcDetails["warnings"])

	// Отклонение показания нанимателя — письмо.
	var rid string
	require.NoError(t, conn.QueryRow(ctx, `INSERT INTO meter_readings (meter_id, contract_id, period_month, reading_date, value, source, status)
		VALUES ($1, $2, '2026-11-01', '2026-11-30', '9100', 'tenant', 'submitted') RETURNING id`, meters["electricity"], id).Scan(&rid))
	r = w.c.Do(http.MethodPost, "/meter-readings/"+rid+"/reject", map[string]any{"reason": "Нет фото счётчика"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var mails int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM email_log WHERE dedupe_key = $1`, "readings_rejected:"+rid).Scan(&mails))
	assert.Equal(t, 1, mails)
}

// Ручное начисление, корректировка и отмена (14 разд. 5, FR-05).
func TestManualChargeAdjustCancel_FR05(t *testing.T) {
	w := setup(t, "2026-10-16", "g7")
	id := w.contract(t, "2026-10-01", "2027-08-31", 3000000, nil)
	r := w.c.Do(http.MethodPost, "/charges", map[string]any{"contractId": id, "kind": "damage", "title": "Разбитое стекло", "amountKop": 500000, "dueDate": "2026-10-30", "reason": "Акт осмотра"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	cid := r.ID(t)
	r = w.c.Do(http.MethodPost, "/charges/"+cid+"/adjust", map[string]any{"deltaKop": -100000, "reason": "Скидка"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var c charge
	r.JSON(t, &c)
	assert.Equal(t, int64(400000), c.AmountKop)
	r = w.c.Do(http.MethodPost, "/charges/"+cid+"/adjust", map[string]any{"deltaKop": -500000, "reason": "Ошибка"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = w.c.Do(http.MethodPost, "/charges/"+cid+"/cancel", map[string]any{"reason": "Возместил сам"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &c)
	assert.Equal(t, "cancelled", c.Status)
	var sum struct {
		Summary struct {
			AmountKop int64 `json:"amountKop"`
		} `json:"summary"`
	}
	w.c.Do(http.MethodGet, "/charges?contractId="+id, nil).JSON(t, &sum)
	assert.Equal(t, int64(3000000), sum.Summary.AmountKop, "отменённые не входят в итоги")
}

// «Настройки → Служебное»: список задач, ручной запуск, нереализованная задача — отказ (06 разд. 4).
func TestJobsScreen_FR14_1(t *testing.T) {
	w := setup(t, "2026-10-16", "h8")
	var jl []struct {
		Name    string `json:"name"`
		LastRun *struct {
			Status string `json:"status"`
		} `json:"lastRun"`
	}
	r := w.c.Do(http.MethodGet, "/settings/jobs", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &jl)
	assert.Len(t, jl, 12)
	r = w.c.Do(http.MethodPost, "/settings/jobs/billing.rent_daily/run", nil)
	require.Equal(t, http.StatusAccepted, r.Status, string(r.Body))
	var acc struct {
		JobRunID string `json:"jobRunId"`
	}
	r.JSON(t, &acc)
	assert.NotEmpty(t, acc.JobRunID)
	r = w.c.Do(http.MethodPost, "/settings/jobs/pdn.retention_daily/run", nil)
	assert.Equal(t, http.StatusAccepted, r.Status, "все ежедневные задачи реализованы (M14)")
	var runs struct {
		Total int `json:"total"`
	}
	w.c.Do(http.MethodGet, "/settings/jobs/runs?name=billing.rent_daily", nil).JSON(t, &runs)
	assert.GreaterOrEqual(t, runs.Total, 1)
}

// Начисления и показания — только для собственника (NFR-Sec-02).
func TestChargesOwnerOnly_NFR_Sec_02(t *testing.T) {
	w := setup(t, "2026-10-16", "i9")
	tc := w.env.ActiveTenant(t, "tenant-b-i9@landlord.local", "Пароль-наним-99")
	for _, p := range []string{"/charges", "/meter-readings", "/settings/jobs"} {
		r := tc.Do(http.MethodGet, p, nil)
		assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.Status, p)
	}
}
