//go:build integration

package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

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

// pay регистрирует ручной платёж через сервис платежей (M6, № 26) с распределением на начисление.
func (w world) pay(t *testing.T, contractID, chargeID string, amount int64, on string) {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contractID, "purpose": "charges", "amountKop": amount, "receivedOn": on, "method": "bank_transfer",
		"chargeIds": []string{chargeID}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
}

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
	w.at(t, "2026-10-07")
	w.pay(t, id, oct.ID, 1500000, "2026-10-07")

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
	w.pay(t, id, oct.ID, 1150000, "2026-10-18")
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
	w.pay(t, id, nov.ID, 2800000, "2026-11-01")

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
	w.pay(t, id, apr.ID, 2800000, "2027-03-29")
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
	// Показания за сентябрь передаёт наниматель из кабинета (M13, № 26): окно 20.09–05.10.
	tenant := w.env.TenantUser(t, w.tenant, "tenant-f6@landlord.local", password)
	submit := func(month string, values map[string]string) {
		t.Helper()
		var readings []any
		for typ, v := range values {
			readings = append(readings, map[string]any{"meterId": meters[typ], "value": v})
		}
		r := tenant.Do(http.MethodPost, "/cabinet/meter-readings", map[string]any{"contractId": id, "periodMonth": month, "readings": readings})
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}
	submit("2026-09-01", map[string]string{"cold_water": "115.900", "electricity": "9012"})
	var list struct {
		Items []struct {
			ID          string  `json:"id"`
			MeterID     string  `json:"meterId"`
			ReadingDate string  `json:"readingDate"`
			Consumption *string `json:"consumption"`
			Status      string  `json:"status"`
		} `json:"items"`
	}
	w.c.Do(http.MethodGet, "/meter-readings?status=submitted&propertyId="+w.property, nil).JSON(t, &list)
	require.Len(t, list.Items, 2)
	assert.Equal(t, "2026-09-30", list.Items[0].ReadingDate)
	ids := []string{list.Items[0].ID, list.Items[1].ID}

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

	// Отклонение показания нанимателя — письмо (показания за ноябрь — из кабинета, окно 20.11–05.12).
	w.at(t, "2026-12-01")
	tenant.Login("tenant-f6@landlord.local", password)
	submit("2026-11-01", map[string]string{"cold_water": "118.000", "electricity": "9100"})
	conn := testsupport.Owner(t)
	var rid string
	require.NoError(t, conn.QueryRow(ctx, `SELECT id FROM meter_readings WHERE meter_id = $1 AND period_month = '2026-11-01' AND source = 'tenant'`,
		meters["electricity"]).Scan(&rid))
	r = w.c.Do(http.MethodPost, "/meter-readings/"+rid+"/reject", map[string]any{"reason": "Нет фото счётчика"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var mails int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM email_log WHERE dedupe_key = $1`, "readings_rejected:"+rid).Scan(&mails))
	assert.Equal(t, 1, mails)
}

// № 31: в месяце начала договора фиксированная часть ЖКУ по показаниям — пропорционально дням пользования
// (16 из 30 дней сентября: 450 × 16 / 30 = 240), как в итоговом начислении по акту возврата (FR-05.5).
func TestUtilitiesFixedPartFirstMonth_FR05_5(t *testing.T) {
	w := setup(t, "2026-10-02", "z1")
	r := w.c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": nil, "initialValue": "112.400", "initialDate": "2026-09-14", "verificationDue": nil, "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	meter := r.ID(t)
	r = w.c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": "cold_water", "propertyId": w.property, "price": "54.37", "effectiveFrom": "2026-01-01", "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.contract(t, "2026-09-15", "2027-07-31", 3000000, func(m map[string]any) {
		m["utilitiesMode"] = "meters"
		m["utilitiesFixedKop"] = 45000
		m["paymentDay"] = 10
	})
	r = w.c.Do(http.MethodPost, "/meter-readings", map[string]any{"meterId": meter, "periodMonth": "2026-09-01", "readingDate": "2026-09-30", "value": "115.900"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var dec struct {
		UtilitiesCharge *charge `json:"utilitiesCharge"`
	}
	r.JSON(t, &dec)
	require.NotNil(t, dec.UtilitiesCharge)
	assert.Equal(t, int64(19030+24000), dec.UtilitiesCharge.AmountKop, "3,5 м³ × 54,37 = 190,30 и фиксированная часть 240,00")
}

// Ручное начисление, корректировка и отмена (14 разд. 5, FR-05); журнал корректировки (ТК-28).
func TestManualChargeAdjustCancel_TK28_FR05_FR12_1(t *testing.T) {
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

	// ТК-28: в журнале — запись корректировки со значениями «до» и «после», цепочка не нарушена (FR-12.1).
	var log struct {
		Items []struct {
			Action string         `json:"action"`
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		} `json:"items"`
	}
	w.c.Do(http.MethodGet, "/audit?entityType=charge&entityId="+cid+"&action=charge.adjust", nil).JSON(t, &log)
	require.Len(t, log.Items, 1)
	assert.EqualValues(t, 500000, log.Items[0].Before["amountKop"])
	assert.EqualValues(t, 400000, log.Items[0].After["amountKop"])
	var verify struct {
		Ok bool `json:"ok"`
	}
	w.c.Do(http.MethodGet, "/audit/verify", nil).JSON(t, &verify)
	assert.True(t, verify.Ok)

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

// Выгрузка начислений в Excel (FR-11.4): фильтры списка, статус «просрочено N дн.», ручные начисления,
// отменённые не входят в итог.
func TestExportCharges_FR_11_4(t *testing.T) {
	w := setup(t, "2026-10-16", "j1")
	id := w.contract(t, "2026-08-01", "2027-06-30", 3000000, nil)
	list := w.charges(t, id)
	aug := byMonth(list, "rent", "2026-08-01")
	require.NotNil(t, aug)
	w.pay(t, id, aug.ID, 3000000, "2026-08-14")
	r := w.c.Do(http.MethodPost, "/charges", map[string]any{"contractId": id, "kind": "damage", "title": "Разбитое стекло",
		"amountKop": 500000, "dueDate": "2026-10-30", "reason": "Акт осмотра"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/charges", map[string]any{"contractId": id, "kind": "other", "title": "Ошибочное",
		"amountKop": 100000, "dueDate": "2026-10-30", "reason": "Проверка"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	require.Equal(t, http.StatusOK, w.c.Do(http.MethodPost, "/charges/"+r.ID(t)+"/cancel", map[string]any{"reason": "Ошибка ввода"}).Status)

	rows := func(t *testing.T, query string) [][]string {
		t.Helper()
		r := w.c.Do(http.MethodGet, "/charges.xlsx?contractId="+id+query, nil)
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
		f, err := excelize.OpenReader(bytes.NewReader(r.Body))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		out, err := f.GetRows("Начисления", excelize.Options{RawCellValue: true})
		require.NoError(t, err)
		return out
	}
	find := func(rows [][]string, first string) []string {
		for _, row := range rows {
			if len(row) > 0 && row[0] == first {
				return row
			}
		}
		return nil
	}

	all := rows(t, "&sort=dueDate")
	joined := ""
	for _, row := range all {
		joined += strings.Join(row, "|") + "\n"
	}
	assert.Contains(t, joined, "Оплачено|авто", "август оплачен")
	assert.Contains(t, joined, "Ожидает оплаты, просрочено 31 дн.", "сентябрь: срок 15.09")
	assert.Contains(t, joined, "Разбитое стекло")
	assert.Contains(t, joined, "вручную")
	assert.Contains(t, joined, "Отменено")
	total := find(all, "Итого (без отменённых)")
	require.NotNil(t, total)
	var sum int64
	for _, c := range list {
		if c.Status != "cancelled" {
			sum += c.AmountKop
		}
	}
	assert.Equal(t, money(sum+500000), total[5], "итог по сумме — без отменённого «Ошибочное»")
	assert.Equal(t, money(3000000), total[6], "оплачено — август")

	// Фильтры вида и статуса — как у списка: только неоплаченная плата за наём.
	only := rows(t, "&kind=rent&status=open&overdue=true")
	text := ""
	for _, row := range only {
		text += strings.Join(row, "|") + "\n"
	}
	assert.NotContains(t, text, "Разбитое стекло")
	assert.NotContains(t, text, "Оплачено|авто")
	assert.Contains(t, text, "просрочено")
}

// money — сумма в рублях, как её хранит ячейка XLSX (11 разд. 1).
func money(kop int64) string {
	rub := strconv.FormatInt(kop/100, 10)
	if kop%100 == 0 {
		return rub
	}
	return rub + "." + strings.TrimSuffix(fmt.Sprintf("%02d", kop%100), "0")
}

// Итоговое начисление ЖКУ по акту возврата (05 разд. 6.3, FR-05.5): расход от последнего показания до показания
// акта и фиксированная часть последнего месяца; повторного начисления нет.
func TestFinalUtilitiesByReturnAct_FR05_5(t *testing.T) {
	w := setup(t, "2026-10-20", "k2")
	r := w.c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": nil, "initialValue": "100", "initialDate": "2026-09-30", "verificationDue": nil, "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	meter := r.ID(t)
	r = w.c.Do(http.MethodPost, "/settings/tariffs", map[string]any{"meterType": "cold_water", "propertyId": w.property, "price": "50.00", "effectiveFrom": "2026-01-01", "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := w.contract(t, "2026-10-01", "2027-08-31", 3000000, func(m map[string]any) {
		m["utilitiesMode"] = "meters"
		m["utilitiesFixedKop"] = 30000
		m["paymentDay"] = 10
	})
	var acts []struct {
		ID string `json:"id"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+id+"/acts", nil).JSON(t, &acts)
	require.Len(t, acts, 1)
	sign := func(act, day, value string) {
		t.Helper()
		r := w.c.Do(http.MethodPut, "/acts/"+act, map[string]any{
			"actDate": day, "keysCount": 2, "conditionNotes": "Хорошее", "inventory": []map[string]any{},
			"readings": []map[string]any{{"meterId": meter, "value": value}},
		})
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
		r = w.c.Do(http.MethodPost, "/acts/"+act+"/sign", map[string]any{"signedOn": day})
		require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	}
	sign(acts[0].ID, "2026-10-01", "100")

	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/acts", map[string]any{"kind": "return"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	sign(r.ID(t), "2026-10-20", "110")

	var final []charge
	for _, c := range w.charges(t, id) {
		if c.Kind == "utilities" && strings.HasPrefix(c.Title, "Компенсация ЖКУ по акту возврата") {
			final = append(final, c)
		}
	}
	require.Len(t, final, 1, "одно итоговое начисление ЖКУ")
	assert.Equal(t, int64(10*5000+30000), final[0].AmountKop, "10 м³ × 50,00 и фиксированная часть 300,00")
	assert.Equal(t, "Компенсация ЖКУ по акту возврата от 20.10.2026", final[0].Title)

	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'charge.create' AND entity_id = $1`, final[0].ID).Scan(&n))
	assert.Equal(t, 1, n, "начисление записано в журнал действий")
}

// Расхождение «переплата» (05 разд. 10.6): аванс уходит на новые начисления — сумма расхождения уменьшается,
// аванс израсходован — расхождение закрывается автоматически.
func TestOverpaymentSync_FR07_3(t *testing.T) {
	w := setup(t, "2026-10-16", "l3")
	id := w.contract(t, "2026-10-01", "2027-08-31", 3000000, nil)
	oct := byMonth(w.charges(t, id), "rent", "2026-10-01")
	require.NotNil(t, oct)
	w.pay(t, id, oct.ID, 4000000, "2026-10-14")

	db := testsupport.Owner(t)
	state := func() (string, int64) {
		t.Helper()
		var status string
		var amount int64
		require.NoError(t, db.QueryRow(context.Background(),
			`SELECT status || coalesce('/' || resolution, ''), amount_kop FROM discrepancies
			 WHERE contract_id = $1 AND kind = 'overpayment'`, id).Scan(&status, &amount))
		return status, amount
	}
	status, amount := state()
	assert.Equal(t, "open", status)
	assert.Equal(t, int64(1000000), amount, "переплата 10 000 ₽")

	manual := func(amount int64) {
		t.Helper()
		r := w.c.Do(http.MethodPost, "/charges", map[string]any{"contractId": id, "kind": "other", "title": "Ключи",
			"amountKop": amount, "dueDate": "2026-10-30", "reason": "Утеря ключей"})
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}
	manual(600000)
	status, amount = state()
	assert.Equal(t, "open", status)
	assert.Equal(t, int64(400000), amount, "аванс частично ушёл на новое начисление")

	manual(500000)
	status, amount = state()
	assert.Equal(t, "resolved/auto_closed", status, "аванс израсходован")
	assert.Equal(t, int64(400000), amount)
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
