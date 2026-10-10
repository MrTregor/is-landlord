//go:build integration

package contract_test

import (
	"context"
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

type contractDTO struct {
	ID                   string   `json:"id"`
	Number               string   `json:"number"`
	LegalBasis           string   `json:"legalBasis"`
	Status               string   `json:"status"`
	IsShortTerm          bool     `json:"isShortTerm"`
	RegistrationRequired bool     `json:"registrationRequired"`
	EndDate              string   `json:"endDate"`
	NextIndexationOn     *string  `json:"nextIndexationOn"`
	Warnings             []string `json:"warnings"`
	CanActivate          bool     `json:"canActivate"`
	ActivationBlockers   []string `json:"activationBlockers"`
	CurrentTerms         struct {
		Version int   `json:"version"`
		RentKop int64 `json:"rentKop"`
	} `json:"currentTerms"`
	TermsHistory []struct {
		Version       int    `json:"version"`
		EffectiveFrom string `json:"effectiveFrom"`
		RentKop       int64  `json:"rentKop"`
	} `json:"termsHistory"`
	Occupants []struct {
		FullName string `json:"fullName"`
	} `json:"occupants"`
	NextDue *struct {
		Date      string `json:"date"`
		AmountKop int64  `json:"amountKop"`
	} `json:"nextDue"`
}

type world struct {
	env      *testsupport.Env
	c        *testsupport.Client
	property string
	tenant   string
	email    string
}

// at — сдвиг даты окружения (полдень по Москве) и повторный вход: сессия истекает по простою.
func (w world) at(t *testing.T, day string) date.Date {
	t.Helper()
	d := date.MustParse(day)
	w.env.Clock.Set(d.Time().Add(12 * time.Hour))
	w.c.Login(w.email, "Пароль-договоры-1")
	return d
}

// setup — собственник, объект (свободен с 01.01.2026) и наниматель-физлицо с паспортом.
func setup(t *testing.T, today, suffix string) world {
	t.Helper()
	env := testsupport.NewEnv(t, today)
	c := env.ActiveOwner(t, "contracts-"+suffix+"@landlord.local", "Пароль-договоры-1")
	r := c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Кв " + suffix, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 5, кв. " + suffix,
		"areaSqm": "41.00", "floor": 3, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil,
		"furnished": true, "expectedRentKop": 3000000, "fiscalMode": "none",
		"initialStatus": "free", "statusSince": "2026-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Кузнецов", "firstName": "Павел", "middleName": "Игоревич",
		"docType": "passport_rf", "docNumber": "45 18 908172", "docIssuedBy": "ГУ МВД России по г. Москве",
		"docIssuedOn": "2018-05-20", "docDivisionCode": "770-001",
		"registrationAddress": "г. Москва, ул. Примерная, д. 1, кв. 5", "email": "kuznetsov-" + suffix + "@landlord.local",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return world{env: env, c: c, property: prop, tenant: r.ID(t), email: "contracts-" + suffix + "@landlord.local"}
}

func (w world) draft(start, end string, rent int64) map[string]any {
	return map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": start, "endDate": end, "rentKop": rent,
		"paymentDay": 15, "depositKop": 3000000, "utilitiesMode": "included", "utilitiesFixedKop": 0,
		"penaltyEnabled": true, "penaltyRatePct": "0.1", "penaltyGraceDays": 3, "penaltyOnUtilities": false,
		"indexationEnabled": true, "indexationPct": "5", "indexationPeriodMonths": 12,
		"occupants": []map[string]any{{"fullName": "Кузнецова Анна Сергеевна", "relation": "супруга"}},
	}
}

func (w world) create(t *testing.T, body map[string]any) contractDTO {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/contracts", body)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var d contractDTO
	r.JSON(t, &d)
	return d
}

func (w world) activate(t *testing.T, id, concluded string, invite bool) contractDTO {
	t.Helper()
	r := w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": concluded, "inviteTenant": invite})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var d contractDTO
	r.JSON(t, &d)
	return d
}

type historyRow struct {
	Status    string  `json:"status"`
	ValidFrom string  `json:"validFrom"`
	ValidTo   *string `json:"validTo"`
}

func (w world) history(t *testing.T) []historyRow {
	var h []historyRow
	w.c.Do(http.MethodGet, "/properties/"+w.property+"/status-history", nil).JSON(t, &h)
	return h
}

type event struct {
	Kind      string `json:"kind"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	AmountKop *int64 `json:"amountKop"`
	Forecast  bool   `json:"forecast"`
	Done      bool   `json:"done"`
	ID        *string
}

func (w world) events(t *testing.T, from, to string) []event {
	var ev []event
	r := w.c.Do(http.MethodGet, "/calendar/events?from="+from+"&to="+to+"&propertyId="+w.property, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &ev)
	return ev
}

func kinds(ev []event) map[string]int {
	m := map[string]int{}
	for _, e := range ev {
		m[e.Kind]++
	}
	return m
}

// ТК-06, ТК-07, IT-01 (без начислений — M4): черновик с номером ГГГГ-NNN, вид договора, предупреждение о регистрации,
// активация с датой начала в прошлом → объект «Сдан» с даты начала, события календаря, акт, приглашение
// (FR-03.1, FR-03.3, FR-09.1).
func TestContractDraftAndActivate_FR03_1_FR03_3_FR09_1(t *testing.T) {
	w := setup(t, "2026-10-16", "a1")
	d := w.create(t, w.draft("2026-09-15", "2027-09-14", 3000000))
	assert.Regexp(t, `^2026-\d{3}$`, d.Number)
	assert.Equal(t, "ch35", d.LegalBasis)
	assert.Equal(t, "draft", d.Status)
	assert.True(t, d.RegistrationRequired)
	assert.False(t, d.IsShortTerm)
	assert.True(t, d.CanActivate, d.ActivationBlockers)
	require.NotEmpty(t, d.Warnings)
	assert.Contains(t, d.Warnings[0], "подлежит государственной регистрации")
	require.NotNil(t, d.NextIndexationOn)
	assert.Equal(t, "2027-10-01", *d.NextIndexationOn, "15.09.2026 + 12 мес. → 01.10.2027")
	assert.Len(t, d.Occupants, 1)

	// Правка черновика перезаписывает версию условий 1.
	body := w.draft("2026-09-15", "2027-09-14", 3200000)
	r := w.c.Do(http.MethodPut, "/contracts/"+d.ID, body)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &d)
	assert.Equal(t, int64(3200000), d.CurrentTerms.RentKop)
	assert.Len(t, d.TermsHistory, 1)

	d = w.activate(t, d.ID, "2026-09-10", true)
	assert.Equal(t, "active", d.Status)
	assert.Contains(t, d.Warnings, "Акт приёма-передачи не подписан")
	require.NotNil(t, d.NextDue)
	assert.Equal(t, "2026-11-15", d.NextDue.Date)

	h := w.history(t)
	require.Len(t, h, 2)
	assert.Equal(t, "rented", h[0].Status)
	assert.Equal(t, "2026-09-15", h[0].ValidFrom)
	assert.Equal(t, "free", h[1].Status)
	require.NotNil(t, h[1].ValidTo)
	assert.Equal(t, "2026-09-14", *h[1].ValidTo)

	ev := w.events(t, "2026-09-01", "2026-11-30")
	k := kinds(ev)
	assert.Equal(t, 1, k["registration"], "регистрация: дата подписания + 7 дней")
	assert.Equal(t, 3, k["payment_due"], "сроки 15.09, 15.10 — по начислениям, 15.11 — прогноз")
	assert.Equal(t, 2, k["overdue"], "сентябрь и октябрь не оплачены")
	for _, e := range ev {
		if e.Kind == "payment_due" && e.Date == "2026-09-15" {
			require.NotNil(t, e.AmountKop)
			assert.Equal(t, int64(1706667), *e.AmountKop, "32 000 × 16 / 30")
			assert.False(t, e.Forecast)
		}
		if e.Kind == "payment_due" && e.Date == "2026-11-15" {
			assert.True(t, e.Forecast)
		}
		if e.Kind == "registration" {
			assert.Equal(t, "2026-09-17", e.Date)
		}
	}
	ev = w.events(t, "2027-06-01", "2027-08-31")
	assert.Equal(t, 2, kinds(ev)["contract_end_notice"], "за 90 и 30 дней до 14.09.2027: 16.06 и 15.08.2027")

	// Акт приёма-передачи создан черновиком.
	var acts []struct {
		Kind   string `json:"kind"`
		Signed bool   `json:"signed"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+d.ID+"/acts", nil).JSON(t, &acts)
	require.Len(t, acts, 1)
	assert.Equal(t, "handover", acts[0].Kind)

	// Приглашение нанимателю создано.
	var tenant struct {
		User *struct{ Status string } `json:"user"`
	}
	w.c.Do(http.MethodGet, "/tenants/"+w.tenant, nil).JSON(t, &tenant)
	require.NotNil(t, tenant.User)
	assert.Equal(t, "invited", tenant.User.Status)

	// Активный договор целиком не правится и не удаляется.
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodPut, "/contracts/"+d.ID, body).Status)
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodDelete, "/contracts/"+d.ID, nil).Status)

	// Журнал по договору.
	var tl struct {
		Total int `json:"total"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+d.ID+"/timeline", nil).JSON(t, &tl)
	assert.GreaterOrEqual(t, tl.Total, 3)
}

// IT-02, ТК-08: второй договор на пересекающийся срок → 409 conflict_overlap; прямая вставка — ошибка EXCLUDE (UC3-3а).
func TestContractOverlap_IT02_UC3_3a(t *testing.T) {
	w := setup(t, "2026-10-16", "b2")
	first := w.create(t, w.draft("2026-10-01", "2027-08-31", 3000000))
	first = w.activate(t, first.ID, "2026-10-01", false)

	second := w.create(t, w.draft("2027-08-01", "2028-06-30", 3000000))
	assert.False(t, second.CanActivate)
	require.NotEmpty(t, second.ActivationBlockers)
	assert.Contains(t, second.ActivationBlockers[0], "Сроки пересекаются с договором "+first.Number)

	r := w.c.Do(http.MethodPost, "/contracts/"+second.ID+"/activate", map[string]any{"concludedOn": "2026-10-16", "inviteTenant": false})
	assert.Equal(t, http.StatusConflict, r.Status)
	assert.Equal(t, "conflict_overlap", r.Code())
	assert.Contains(t, string(r.Body), "Сроки пересекаются с договором "+first.Number)

	// Ограничение EXCLUDE — последняя защита.
	_, err := testsupport.Owner(t).Exec(context.Background(),
		`UPDATE contracts SET status = 'active' WHERE id = $1`, second.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "23P01")

	// Удаление черновика — физическое.
	assert.Equal(t, http.StatusNoContent, w.c.Do(http.MethodDelete, "/contracts/"+second.ID, nil).Status)
	assert.Equal(t, http.StatusNotFound, w.c.Do(http.MethodGet, "/contracts/"+second.ID, nil).Status)
}

// Проверки черновика (05 разд. 4.3, FR-03.1) и краткосрочный наём (ТК-34).
func TestContractDraftValidation_FR03_1(t *testing.T) {
	w := setup(t, "2026-10-16", "c3")
	cases := []struct {
		field, text string
		mod         func(m map[string]any)
	}{
		{"endDate", "Дата окончания должна быть позже даты начала", func(m map[string]any) { m["endDate"] = "2026-10-01" }},
		{"endDate", "не более пяти лет", func(m map[string]any) { m["endDate"] = "2031-10-01" }},
		{"rentKop", "Укажите размер платы", func(m map[string]any) { m["rentKop"] = 0 }},
		{"firstDueDate", "не дальше 30 дней", func(m map[string]any) { m["firstDueDate"] = "2026-11-15" }},
		{"penaltyRatePct", "от 0,01 до 1 % в день", func(m map[string]any) { m["penaltyRatePct"] = "1.5" }},
		{"utilitiesFixedKop", "Укажите фиксированную сумму ЖКУ", func(m map[string]any) { m["utilitiesMode"] = "fixed" }},
	}
	for _, tc := range cases {
		body := w.draft("2026-10-01", "2027-08-31", 3000000)
		tc.mod(body)
		r := w.c.Do(http.MethodPost, "/contracts", body)
		assert.Equal(t, http.StatusUnprocessableEntity, r.Status, tc.field)
		assert.Contains(t, string(r.Body), tc.text, tc.field)
	}
	d := w.create(t, w.draft("2026-10-01", "2027-08-31", 3000000))
	assert.True(t, d.IsShortTerm, "11 месяцев — краткосрочный наём")
	assert.False(t, d.RegistrationRequired)
	for _, warn := range d.Warnings {
		assert.NotContains(t, warn, "регистрации")
	}
}

// Будущая дата начала: объект «Забронирован», contracts.daily в день начала → «Сдан»; расторжение → «Свободен»
// со следующего дня; события после расторжения удаляются (05 разд. 4.4, 4.7, 4.8; FR-03.2, FR-01.2).
func TestContractLifecycleDaily_FR03_2_FR01_2(t *testing.T) {
	w := setup(t, "2026-10-16", "d4")
	d := w.create(t, w.draft("2026-11-01", "2027-10-31", 3000000))
	d = w.activate(t, d.ID, "2026-10-16", false)
	h := w.history(t)
	assert.Equal(t, "reserved", h[0].Status)
	assert.Equal(t, "2026-10-16", h[0].ValidFrom)

	ctx := context.Background()
	svc := w.env.App.Contract
	w.at(t, "2026-11-01")
	st, err := svc.RunDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-11-01"), Trigger: jobs.TriggerManual})
	require.NoError(t, err)
	assert.Empty(t, st.Errors)
	h = w.history(t)
	assert.Equal(t, "rented", h[0].Status)
	assert.Equal(t, "2026-11-01", h[0].ValidFrom)

	// Повторный запуск ничего не меняет.
	_, err = svc.RunDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-11-01"), Trigger: jobs.TriggerManual})
	require.NoError(t, err)
	assert.Len(t, w.history(t), len(h))

	r := w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/terminate", map[string]any{
		"terminationDate": "2026-11-20", "initiator": "tenant", "reason": "Переезд"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &d)
	assert.Equal(t, "terminated", d.Status)
	assert.Equal(t, "rented", w.history(t)[0].Status, "последний день пользования ещё впереди")

	w.at(t, "2026-11-21")
	_, err = svc.RunDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-11-21"), Trigger: jobs.TriggerManual})
	require.NoError(t, err)
	h = w.history(t)
	assert.Equal(t, "free", h[0].Status)
	assert.Equal(t, "2026-11-21", h[0].ValidFrom)
	require.NotNil(t, h[1].ValidTo)
	assert.Equal(t, "2026-11-20", *h[1].ValidTo)

	ev := w.events(t, "2026-11-01", "2027-01-31")
	k := kinds(ev)
	assert.Equal(t, 1, k["contract_end"], "окончание перенесено на дату расторжения")
	for _, e := range ev {
		if e.Kind == "contract_end" {
			assert.Equal(t, "2026-11-20", e.Date)
			assert.True(t, e.Done)
		}
		if e.Kind == "payment_due" {
			assert.LessOrEqual(t, e.Date, "2026-11-20", "сроки только до конца пользования")
		}
	}
	var runs int
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT count(*) FROM job_runs WHERE job_name = 'contracts.daily' AND status = 'succeeded'`).Scan(&runs))
	assert.GreaterOrEqual(t, runs, 3)
}

// Истёкший договор завершается задачей, объект освобождается (05 разд. 4.8).
func TestContractFinish_FR03_2(t *testing.T) {
	w := setup(t, "2026-10-16", "e5")
	d := w.create(t, w.draft("2026-02-01", "2026-12-31", 3000000))
	d = w.activate(t, d.ID, "2026-02-01", false)
	w.at(t, "2027-01-01")
	_, err := w.env.App.Contract.RunDaily(context.Background(), jobs.DailyArgs{BusinessDate: date.MustParse("2027-01-01")})
	require.NoError(t, err)
	w.c.Do(http.MethodGet, "/contracts/"+d.ID, nil).JSON(t, &d)
	assert.Equal(t, "finished", d.Status)
	h := w.history(t)
	assert.Equal(t, "free", h[0].Status)
	assert.Equal(t, "2027-01-01", h[0].ValidFrom)
}

// Допсоглашения: повышение платы не чаще раза в 12 месяцев, новая версия условий, письмо нанимателю, продление
// (05 разд. 4.6, 8.3; FR-03.2, FR-05.4).
func TestAmendments_FR03_2_FR05_4(t *testing.T) {
	w := setup(t, "2026-10-16", "f6")
	d := w.create(t, w.draft("2025-11-01", "2026-12-31", 2800000))
	d = w.activate(t, d.ID, "2025-11-01", false)

	r := w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/amendments", map[string]any{
		"kind": "rent_change", "effectiveFrom": "2026-10-15", "newRentKop": 2940000})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "первого числа")

	r = w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/amendments", map[string]any{
		"kind": "rent_change", "effectiveFrom": "2026-10-01", "newRentKop": 2940000})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "не чаще одного раза в 12 месяцев")

	r = w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/amendments", map[string]any{
		"kind": "rent_change", "effectiveFrom": "2026-11-01", "newRentKop": 2940000})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	amID := r.ID(t)
	r = w.c.Do(http.MethodPost, "/amendments/"+amID+"/sign", map[string]any{"signedOn": "2026-10-16"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))

	w.c.Do(http.MethodGet, "/contracts/"+d.ID, nil).JSON(t, &d)
	require.Len(t, d.TermsHistory, 2)
	assert.Equal(t, "2026-11-01", d.TermsHistory[1].EffectiveFrom)
	assert.Equal(t, int64(2940000), d.TermsHistory[1].RentKop)
	assert.Equal(t, int64(2800000), d.CurrentTerms.RentKop, "до 01.11 действует прежняя плата")

	var mails int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM email_log WHERE dedupe_key = $1`, "rent_changed:"+amID).Scan(&mails))
	assert.Equal(t, 1, mails)

	// Повторное подписание — конфликт состояния.
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodPost, "/amendments/"+amID+"/sign", map[string]any{"signedOn": "2026-10-16"}).Status)

	// Продление.
	r = w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/amendments", map[string]any{
		"kind": "extension", "effectiveFrom": "2026-12-31", "newEndDate": "2027-11-30"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	ext := r.ID(t)
	require.Equal(t, http.StatusOK, w.c.Do(http.MethodPost, "/amendments/"+ext+"/sign", map[string]any{"signedOn": "2026-10-16"}).Status)
	w.c.Do(http.MethodGet, "/contracts/"+d.ID, nil).JSON(t, &d)
	assert.Equal(t, "extended", d.Status)
	assert.Equal(t, "2027-11-30", d.EndDate)

	// Отмена черновика — с причиной.
	r = w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/amendments", map[string]any{"kind": "other", "effectiveFrom": "2026-11-01", "bodyText": "Разрешено животное"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	other := r.ID(t)
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodPost, "/amendments/"+other+"/cancel", map[string]any{"reason": " "}).Status)
	assert.Equal(t, http.StatusOK, w.c.Do(http.MethodPost, "/amendments/"+other+"/cancel", map[string]any{"reason": "Передумали"}).Status)
}

// ТК-09 (без PDF): акт приёма-передачи с описью и показаниями; после подписания показания приняты (FR-03.4).
func TestHandoverAct_TK09_FR03_4(t *testing.T) {
	w := setup(t, "2026-10-16", "g7")
	r := w.c.Do(http.MethodPost, "/properties/"+w.property+"/meters", map[string]any{
		"type": "cold_water", "serialNumber": "ХВ-1", "initialValue": "100.000", "initialDate": "2026-01-01",
		"verificationDue": nil, "note": nil})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	meter := r.ID(t)
	require.Equal(t, http.StatusOK, w.c.Do(http.MethodPut, "/properties/"+w.property+"/inventory", map[string]any{
		"items": []map[string]any{{"name": "Холодильник", "quantity": 1, "condition": "исправен"}}}).Status)

	d := w.create(t, w.draft("2026-10-01", "2027-08-31", 3000000))
	d = w.activate(t, d.ID, "2026-10-01", false)
	var acts []struct {
		ID        string `json:"id"`
		Inventory []struct {
			Name string `json:"name"`
		} `json:"inventory"`
		Readings []struct {
			MeterID       string  `json:"meterId"`
			Value         *string `json:"value"`
			PreviousValue *string `json:"previousValue"`
		} `json:"readings"`
		Signed bool `json:"signed"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+d.ID+"/acts", nil).JSON(t, &acts)
	require.Len(t, acts, 1)
	act := acts[0]
	require.Len(t, act.Inventory, 1, "опись скопирована из объекта")
	require.Len(t, act.Readings, 1)
	assert.Equal(t, "100", *act.Readings[0].PreviousValue)

	// Подписать без показаний нельзя.
	r = w.c.Do(http.MethodPost, "/acts/"+act.ID+"/sign", map[string]any{"signedOn": "2026-10-01"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	fill := map[string]any{
		"actDate": "2026-10-01", "keysCount": 2, "conditionNotes": "Хорошее",
		"inventory": []map[string]any{{"name": "Холодильник", "quantity": 1, "condition": "исправен"}},
		"readings":  []map[string]any{{"meterId": meter, "value": "99.5"}},
	}
	r = w.c.Do(http.MethodPut, "/acts/"+act.ID, fill)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, string(r.Body), "меньше предыдущего")

	fill["readings"] = []map[string]any{{"meterId": meter, "value": "123.456"}}
	r = w.c.Do(http.MethodPut, "/acts/"+act.ID, fill)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/acts/"+act.ID+"/sign", map[string]any{"signedOn": "2026-10-01"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodPut, "/acts/"+act.ID, fill).Status, "подписанный акт не редактируется")

	var keys int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), `SELECT keys_count FROM contract_acts WHERE id = $1`, act.ID).Scan(&keys))
	assert.Equal(t, 2, keys)
	var status, source, value string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT status, source, value::text FROM meter_readings WHERE act_id = $1`, act.ID).Scan(&status, &source, &value))
	assert.Equal(t, "accepted", status)
	assert.Equal(t, "act_handover", source)
	assert.Equal(t, "123.456", value)

	// Акт возврата: опись из акта приёма-передачи; второй — конфликт.
	r = w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/acts", map[string]any{"kind": "return"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodPost, "/contracts/"+d.ID+"/acts", map[string]any{"kind": "return"}).Status)
}

// ТК-25: ручное событие календаря (показ); автоматическое удалить нельзя (FR-09.2).
func TestCalendarManualEvents_TK25_FR09_2(t *testing.T) {
	w := setup(t, "2026-10-16", "h8")
	r := w.c.Do(http.MethodPost, "/calendar/events", map[string]any{
		"kind": "showing", "title": "Показ квартиры", "startsOn": "2026-10-20", "startTime": "18:30", "endTime": "19:00",
		"propertyId": w.property})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := r.ID(t)
	ev := w.events(t, "2026-10-19", "2026-10-25")
	require.Len(t, ev, 1)
	assert.Equal(t, "manual", ev[0].Source)

	r = w.c.Do(http.MethodPost, "/calendar/events", map[string]any{
		"kind": "custom", "title": "Ошибка", "startsOn": "2026-10-20", "startTime": "19:00", "endTime": "18:00"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	r = w.c.Do(http.MethodPut, "/calendar/events/"+id, map[string]any{
		"kind": "showing", "title": "Показ перенесён", "startsOn": "2026-10-21", "propertyId": w.property})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/calendar/events/"+id+"/done", map[string]any{"done": true})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, http.StatusNoContent, w.c.Do(http.MethodDelete, "/calendar/events/"+id, nil).Status)

	r = w.c.Do(http.MethodGet, "/calendar/events?from=2026-01-01&to=2026-12-31", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "не более 92 дней")

	d := w.create(t, w.draft("2026-10-01", "2027-08-31", 3000000))
	w.activate(t, d.ID, "2026-10-01", false)
	for _, e := range w.events(t, "2027-08-01", "2027-08-31") {
		if e.Kind == "contract_end" {
			require.NotNil(t, e.ID)
			assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodDelete, "/calendar/events/"+*e.ID, nil).Status)
		}
	}
}

// Методы договоров и календаря — только для собственника: наниматель получает отказ (NFR-Sec-02).
func TestContractsOwnerOnly_NFR_Sec_02(t *testing.T) {
	w := setup(t, "2026-10-16", "i9")
	d := w.create(t, w.draft("2026-10-01", "2027-08-31", 3000000))
	tc := w.env.ActiveTenant(t, "tenant-i9@landlord.local", "Пароль-наним-99")
	for _, path := range []string{"/contracts", "/contracts/" + d.ID, "/contracts/" + d.ID + "/acts", "/calendar/events?from=2026-10-01&to=2026-10-31"} {
		r := tc.Do(http.MethodGet, path, nil)
		assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.Status, path)
	}
}
