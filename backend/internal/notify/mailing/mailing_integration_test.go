//go:build integration

package mailing_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-письма-1"

type world struct {
	env   *testsupport.Env
	c     *testsupport.Client
	email string
	sfx   string
}

func setup(t *testing.T, today, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, today)
	email := "mailing-" + sfx + "@landlord.local"
	return world{env: env, c: env.ActiveOwner(t, email, password), email: email, sfx: sfx}
}

func (w world) at(t *testing.T, day string) {
	t.Helper()
	w.env.Clock.Set(date.MustParse(day).Time().Add(12 * time.Hour))
	w.c.Login(w.email, password)
}

// lease — объект, наниматель с e-mail и действующий договор.
func (w world) lease(t *testing.T, code, start, end string, rent int64, payDay int, mod func(map[string]any)) (string, string) {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/properties", map[string]any{
		"code": code + " " + w.sfx, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 5, кв. " + code + w.sfx,
		"areaSqm": "24.50", "floor": 2, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil,
		"furnished": false, "expectedRentKop": rent, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	tenantEmail := "tenant-" + strings.ReplaceAll(code, " ", "") + "-" + w.sfx + "@landlord.local"
	r = w.c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич", "docType": "passport_rf",
		"docNumber": "45 18 333444", "registrationAddress": "г. Москва, ул. Тихая, д. 3", "email": tenantEmail,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	body := map[string]any{
		"propertyId": prop, "tenantId": r.ID(t), "startDate": start, "endDate": end, "rentKop": rent, "paymentDay": payDay,
		"depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false, "penaltyGraceDays": 0,
		"penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	}
	if mod != nil {
		mod(body)
	}
	if body["utilitiesMode"] == "meters" {
		r = w.c.Do(http.MethodPost, "/properties/"+prop+"/meters", map[string]any{
			"type": "cold_water", "serialNumber": "ХВ-" + w.sfx, "initialValue": "100.000", "initialDate": start,
			"verificationDue": nil, "note": nil,
		})
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}
	r = w.c.Do(http.MethodPost, "/contracts", body)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := r.ID(t)
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": start, "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return id, tenantEmail
}

type mail struct {
	ID, To, Subject, Body, Status, Key string
}

// mails — письма вида code по сущности (договор и т.п.).
func mails(t *testing.T, code, relatedID string) []mail {
	t.Helper()
	rows, err := testsupport.Owner(t).Query(context.Background(),
		`SELECT id::text, to_email::text, subject, body_html, status, dedupe_key FROM email_log
		 WHERE template_code = $1 AND related_entity_id::text = $2 ORDER BY created_at, id`, code, relatedID)
	require.NoError(t, err)
	defer rows.Close()
	var out []mail
	for rows.Next() {
		var m mail
		require.NoError(t, rows.Scan(&m.ID, &m.To, &m.Subject, &m.Body, &m.Status, &m.Key))
		out = append(out, m)
	}
	return out
}

func firstDue(d string) func(map[string]any) { return func(b map[string]any) { b["firstDueDate"] = d } }

func day(s string) jobs.DailyArgs { return jobs.DailyArgs{BusinessDate: date.MustParse(s)} }

// ТК-26: за 3 дня до срока — напоминание об оплате с таблицей начислений; повторный запуск не дублирует (FR-10.1).
func TestPaymentReminder_TK26_FR_10_1(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "2026-10-15", "rem")
	contract, tenantEmail := w.lease(t, "Студия 1", "2026-10-01", "2027-08-31", 2800000, 18, firstDue("2026-10-18"))

	st, err := w.env.App.Mailing.RunPaymentReminders(ctx, day("2026-10-15"))
	require.NoError(t, err)
	assert.Empty(t, st.Errors)
	list := mails(t, "payment_reminder", contract)
	require.Len(t, list, 1)
	m := list[0]
	assert.Equal(t, strings.ToLower(tenantEmail), m.To)
	assert.Equal(t, "queued", m.Status)
	assert.Equal(t, "Напоминание об оплате до 18.10.2026", m.Subject)
	assert.Contains(t, m.Body, "Павел Игоревич, здравствуйте!")
	assert.Contains(t, m.Body, "Плата за наём, октябрь 2026")
	assert.Contains(t, m.Body, "28 000,00 ₽")
	assert.Equal(t, "payment_reminder:"+contract+":2026-10-18", m.Key)
	var jobsN int
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT count(*) FROM river_job WHERE kind = 'email.send' AND args->>'emailLogId' = $1`, m.ID).Scan(&jobsN))
	assert.Equal(t, 1, jobsN, "задача email.send в той же транзакции")

	_, err = w.env.App.Mailing.RunPaymentReminders(ctx, day("2026-10-15"))
	require.NoError(t, err)
	assert.Len(t, mails(t, "payment_reminder", contract), 1, "повторный запуск не дублирует письмо")

	// Оплачено — напоминания нет (долга по сроку нет).
	other, _ := w.lease(t, "Студия 2", "2026-10-01", "2027-08-31", 2650000, 18, firstDue("2026-10-18"))
	w.at(t, "2026-10-15")
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": other, "purpose": "charges", "amountKop": 2650000, "receivedOn": "2026-10-15", "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	_, err = w.env.App.Mailing.RunPaymentReminders(ctx, day("2026-10-15"))
	require.NoError(t, err)
	assert.Empty(t, mails(t, "payment_reminder", other))
}

// Просрочка: письмо в первый день просрочки (срок + льгота + 1) и через overdue_repeat_days (FR-10.1).
func TestPaymentOverdue_FR_10_1(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "2026-10-15", "over")
	contract, _ := w.lease(t, "Студия 3", "2026-10-01", "2027-08-31", 2700000, 10, func(b map[string]any) {
		b["penaltyEnabled"], b["penaltyRatePct"], b["penaltyGraceDays"] = true, "0.1000", 2
		b["firstDueDate"] = "2026-10-10"
	})
	for _, d := range []string{"2026-10-11", "2026-10-12"} {
		_, err := w.env.App.Mailing.RunPaymentReminders(ctx, day(d))
		require.NoError(t, err)
	}
	assert.Empty(t, mails(t, "payment_overdue", contract), "в льготный период письма нет")
	for _, d := range []string{"2026-10-13", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-20"} {
		_, err := w.env.App.Mailing.RunPaymentReminders(ctx, day(d))
		require.NoError(t, err)
	}
	list := mails(t, "payment_overdue", contract)
	require.Len(t, list, 2, "13.10 и через 7 дней — 20.10")
	assert.Equal(t, "payment_overdue:"+contract+":2026-10-13", list[0].Key)
	assert.Equal(t, "payment_overdue:"+contract+":2026-10-20", list[1].Key)
	assert.Contains(t, list[0].Body, "27 000,00 ₽")
	assert.Contains(t, list[0].Body, "Начисленные на сегодня пени")
}

// Окончание договора: письмо за 30 дней; повторный запуск и следующий день — без новых писем (FR-10.1).
func TestContractEnding_FR_10_1(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "2026-10-15", "end")
	contract, _ := w.lease(t, "Студия 4", "2026-06-01", "2026-11-14", 3000000, 5, nil)
	for _, d := range []string{"2026-10-15", "2026-10-15", "2026-10-16"} {
		st, err := w.env.App.Mailing.RunContractEnd(ctx, day(d))
		require.NoError(t, err)
		assert.Empty(t, st.Errors)
	}
	list := mails(t, "contract_ending", contract)
	require.Len(t, list, 1)
	assert.Equal(t, "contract_ending:"+contract+":30", list[0].Key)
	assert.Contains(t, list[0].Subject, "заканчивается 14.11.2026")
	assert.Contains(t, list[0].Body, "через 30 дн.")
}

// Показания: с readings_reminder_day — одно письмо на договор и месяц, окно — по 5-е число (FR-05.5).
func TestReadingsReminder_FR_05_5(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "2026-10-15", "read")
	contract, _ := w.lease(t, "Студия 5", "2026-09-01", "2027-07-31", 2800000, 10, func(b map[string]any) {
		b["utilitiesMode"] = "meters"
	})
	_, err := w.env.App.Mailing.RunReadingsReminder(ctx, day("2026-10-19"))
	require.NoError(t, err)
	assert.Empty(t, mails(t, "readings_reminder", contract), "до 20-го письма нет")
	for _, d := range []string{"2026-10-20", "2026-10-21"} {
		_, err = w.env.App.Mailing.RunReadingsReminder(ctx, day(d))
		require.NoError(t, err)
	}
	list := mails(t, "readings_reminder", contract)
	require.Len(t, list, 1)
	assert.Equal(t, "Передайте показания счётчиков за октябрь 2026", list[0].Subject)
	assert.Contains(t, list[0].Body, "до 05.11.2026")
}

// Утренняя сводка: просрочки и расхождения; письмо каждому собственнику, повтор — без дубля (FR-10.2).
// Переплата — письмо owner_discrepancy (10 Уведомления, раздел 3).
func TestOwnerDigestAndDiscrepancy_FR_10_2(t *testing.T) {
	ctx := context.Background()
	w := setup(t, "2026-10-15", "dig")
	contract, _ := w.lease(t, "Студия 6", "2026-09-01", "2027-07-31", 2000000, 5, nil)
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contract, "purpose": "charges", "amountKop": 6100000, "receivedOn": "2026-10-15", "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var disc struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	w.c.Do(http.MethodGet, "/discrepancies?kind=overpayment&contractId="+contract, nil).JSON(t, &disc)
	require.Len(t, disc.Items, 1)
	letters := mails(t, "owner_discrepancy", disc.Items[0].ID)
	require.NotEmpty(t, letters)
	assert.Equal(t, "Расхождение при сверке: Переплата", letters[0].Subject)
	assert.Contains(t, letters[0].Body, "Переплата на сумму")

	st, err := w.env.App.Mailing.RunOwnerDigest(ctx, day("2026-10-16"))
	require.NoError(t, err)
	assert.Empty(t, st.Errors)
	var n int
	var body string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT count(*), max(body_html) FROM email_log WHERE template_code = 'owner_daily_digest' AND dedupe_key LIKE 'owner_digest:2026-10-16:%'
		   AND to_email = $1`, w.email).Scan(&n, &body))
	assert.Equal(t, 1, n)
	assert.Contains(t, body, "Расхождения")
	assert.Contains(t, body, "Переплата: ")
	_, err = w.env.App.Mailing.RunOwnerDigest(ctx, day("2026-10-16"))
	require.NoError(t, err)
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT count(*) FROM email_log WHERE dedupe_key LIKE 'owner_digest:2026-10-16:%' AND to_email = $1`, w.email).Scan(&n))
	assert.Equal(t, 1, n, "повтор не дублирует сводку")
}

// Шаблоны писем: проверка переменных, предпросмотр, сброс к стандартному; журнал и повторная отправка (FR-10.3).
func TestTemplatesAndLog_FR_10_3(t *testing.T) {
	w := setup(t, "2026-10-15", "tpl")
	var list []struct {
		Code      string `json:"code"`
		Recipient string `json:"recipient"`
		Enabled   bool   `json:"enabled"`
	}
	r := w.c.Do(http.MethodGet, "/settings/email-templates", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	assert.Len(t, list, 21)

	put := func(subject, body string, enabled bool) testsupport.Response {
		return w.c.Do(http.MethodPut, "/settings/email-templates/contract_ending", map[string]any{
			"subject": subject, "bodyHtml": body, "enabled": enabled,
		})
	}
	r = put("Договор {{contract.number}}", "<p>Осталось {{days_left}} дн. {{tenant.passport}}</p>", true)
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "Неизвестные поля: tenant.passport")
	r = put("Договор {{contract.number}} заканчивается", `<p>Осталось {{days_left}} дн.</p><p><a href="{{cabinet_url}}">Кабинет</a></p><script>alert(1)</script>`, false)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var tpl struct {
		Subject  string `json:"subject"`
		BodyHtml string `json:"bodyHtml"`
		Enabled  bool   `json:"enabled"`
	}
	r.JSON(t, &tpl)
	assert.False(t, tpl.Enabled)
	assert.NotContains(t, tpl.BodyHtml, "<script>")
	assert.Contains(t, tpl.BodyHtml, `href="{{cabinet_url}}"`, "переменные в ссылках сохраняются")

	var pv struct {
		Subject string `json:"subject"`
		HTML    string `json:"html"`
	}
	r = w.c.Do(http.MethodPost, "/settings/email-templates/contract_ending/preview", map[string]any{
		"subject": tpl.Subject, "bodyHtml": tpl.BodyHtml,
	})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &pv)
	assert.Equal(t, "Договор 2026-002 заканчивается", pv.Subject)
	assert.Contains(t, pv.HTML, "Осталось 30 дн.")
	assert.Contains(t, pv.HTML, "Это автоматическое письмо системы")

	// Выключенный шаблон — письмо skipped.
	contract, _ := w.lease(t, "Студия 8", "2026-06-01", "2026-11-14", 3000000, 5, nil)
	_, err := w.env.App.Mailing.RunContractEnd(context.Background(), day("2026-10-15"))
	require.NoError(t, err)
	got := mails(t, "contract_ending", contract)
	require.Len(t, got, 1)
	assert.Equal(t, "skipped", got[0].Status)

	r = w.c.Do(http.MethodPost, "/settings/email-templates/contract_ending/reset", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &tpl)
	assert.Equal(t, "Договор № {{contract.number}} заканчивается {{end_date}}", tpl.Subject)
	assert.False(t, tpl.Enabled, "сброс не включает шаблон")
	r = put(tpl.Subject, tpl.BodyHtml, true)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))

	// Журнал отправки и повторная отправка.
	reminderContract, _ := w.lease(t, "Студия 9", "2026-10-01", "2027-08-31", 2800000, 18, firstDue("2026-10-18"))
	_, err = w.env.App.Mailing.RunPaymentReminders(context.Background(), day("2026-10-15"))
	require.NoError(t, err)
	src := mails(t, "payment_reminder", reminderContract)
	require.Len(t, src, 1)
	var log struct {
		Items []struct {
			ID           string `json:"id"`
			TemplateCode string `json:"templateCode"`
		} `json:"items"`
		Total int `json:"total"`
	}
	r = w.c.Do(http.MethodGet, "/email-log?templateCode=payment_reminder&q="+src[0].To, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &log)
	require.Equal(t, 1, log.Total)
	assert.Equal(t, src[0].ID, log.Items[0].ID)
	for i := 1; i <= 2; i++ {
		r = w.c.Do(http.MethodPost, "/email-log/"+log.Items[0].ID+"/resend", nil)
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	}
	again := mails(t, "payment_reminder", reminderContract)
	require.Len(t, again, 3)
	assert.Equal(t, src[0].Key+":resend:2", again[2].Key)
	assert.Equal(t, "queued", again[2].Status)
	r = w.c.Do(http.MethodPost, "/email-log/"+got[0].ID+"/resend", nil)
	assert.Equal(t, http.StatusCreated, r.Status, "пропущенное из-за выключенного шаблона письмо с адресом отправляется")

	// Переменные писем для «Вставить поле».
	var vars struct {
		Emails map[string][]struct {
			Key string `json:"key"`
		} `json:"emails"`
	}
	w.c.Do(http.MethodGet, "/settings/template-variables", nil).JSON(t, &vars)
	assert.NotEmpty(t, vars.Emails["payment_reminder"])

	tenant := w.env.ActiveTenant(t, "mailing-tenant-tpl@landlord.local", password)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/email-log", nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/settings/email-templates", nil).Status)
}
