//go:build integration

package reconciliation_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const (
	password   = "Пароль-сверка-1"
	ownAccount = "40802810900000012345"
)

type world struct {
	env *testsupport.Env
	c   *testsupport.Client
	sfx string
}

// setup — собственник и реквизиты организации с расчётным счётом из образцов выписок.
func setup(t *testing.T, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-15")
	c := env.ActiveOwner(t, "recon-"+sfx+"@landlord.local", password)
	var s map[string]any
	c.Do(http.MethodGet, "/settings/organization", nil).JSON(t, &s)
	s["fullName"], s["shortName"], s["inn"], s["address"] = "Индивидуальный предприниматель Гордейчик Александр Васильевич",
		"ИП Гордейчик А.В.", "507500363778", "Московская область, г. Руза"
	s["email"], s["signatoryName"], s["bankAccount"] = "owner@landlord.local", "А.В. Гордейчик", ownAccount
	delete(s, "updatedAt")
	delete(s, "updatedBy")
	r := c.Do(http.MethodPut, "/settings/organization", s)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return world{env: env, c: c, sfx: sfx}
}

// lease — объект, наниматель и действующий договор с платой rent с даты start.
func (w world) lease(t *testing.T, code, start string, rent int64, tenant map[string]any) string {
	t.Helper()
	kind, rooms := "residential", any(1)
	if tenant["kind"] == "legal" {
		kind, rooms = "non_residential", nil
	}
	r := w.c.Do(http.MethodPost, "/properties", map[string]any{
		"code": code + " " + w.sfx, "kind": kind, "address": "г. Руза, ул. Солнечная, д. 5, кв. " + code + w.sfx,
		"areaSqm": "24.50", "floor": 2, "rooms": rooms, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil,
		"furnished": false, "expectedRentKop": rent, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	prop := r.ID(t)
	r = w.c.Do(http.MethodPost, "/tenants", tenant)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = w.c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": prop, "tenantId": r.ID(t), "startDate": start, "endDate": "2027-02-28", "rentKop": rent,
		"paymentDay": 10, "depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false,
		"penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	id := r.ID(t)
	r = w.c.Do(http.MethodPost, "/contracts/"+id+"/activate", map[string]any{"concludedOn": start, "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	return id
}

func person(last, first, middle, email string) map[string]any {
	return map[string]any{
		"kind": "individual", "lastName": last, "firstName": first, "middleName": middle, "docType": "passport_rf",
		"docNumber": "45 18 333444", "registrationAddress": "г. Москва, ул. Примерная, д. 1", "email": email,
	}
}

// setNumber — номер договора, как в образце выписки (номера в общей базе пакета выдаются по порядку).
func setNumber(t *testing.T, contractID, number string) {
	t.Helper()
	conn := testsupport.Owner(t)
	ctx := context.Background()
	_, err := conn.Exec(ctx, `UPDATE contracts SET number = 'T-' || id::text WHERE number = $1 AND id <> $2`, number, contractID)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE contracts SET number = $1 WHERE id = $2`, number, contractID)
	require.NoError(t, err)
}

type statement struct {
	ID             string `json:"id"`
	Format         string `json:"format"`
	AccountNumber  string `json:"accountNumber"`
	DateFrom       string `json:"dateFrom"`
	DateTo         string `json:"dateTo"`
	LinesTotal     int    `json:"linesTotal"`
	LinesIncoming  int    `json:"linesIncoming"`
	LinesMatched   int    `json:"linesMatched"`
	LinesUnmatched int    `json:"linesUnmatched"`
	LinesDuplicate int    `json:"linesDuplicate"`
	Lines          []bankLine
}

type bankLine struct {
	ID          string  `json:"id"`
	LineNo      int     `json:"lineNo"`
	AmountKop   int64   `json:"amountKop"`
	PayerName   *string `json:"payerName"`
	Status      string  `json:"status"`
	MatchMethod *string `json:"matchMethod"`
	Contract    *struct {
		ID     string `json:"id"`
		Number string `json:"number"`
	} `json:"contract"`
	PaymentID         *string `json:"paymentId"`
	CandidatePayments []struct {
		ID string `json:"id"`
	} `json:"candidatePayments"`
}

type discrepancy struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	AmountKop  int64     `json:"amountKop"`
	Resolution *string   `json:"resolution"`
	PaymentID  *string   `json:"paymentId"`
	BankLine   *bankLine `json:"bankLine"`
	Contract   *struct {
		ID string `json:"id"`
	} `json:"contract"`
}

type payment struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Source      string    `json:"source"`
	Method      string    `json:"method"`
	AmountKop   int64     `json:"amountKop"`
	ReceivedOn  string    `json:"receivedOn"`
	PayerName   *string   `json:"payerName"`
	PayerInn    *string   `json:"payerInn"`
	BankPurpose *string   `json:"bankPurpose"`
	BankLine    *bankLine `json:"bankLine"`
}

func (w world) upload(t *testing.T, name string, data []byte) statement {
	t.Helper()
	r := w.c.Multipart("/bank-statements", nil, name, data)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var st statement
	r.JSON(t, &st)
	return st
}

func (w world) statement(t *testing.T, id string) statement {
	t.Helper()
	var st statement
	r := w.c.Do(http.MethodGet, "/bank-statements/"+id, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &st)
	return st
}

func (w world) payment(t *testing.T, id string) payment {
	t.Helper()
	var p payment
	r := w.c.Do(http.MethodGet, "/payments/"+id, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &p)
	return p
}

func (w world) unidentified(t *testing.T, lineID, status string) discrepancy {
	t.Helper()
	var list struct {
		Items []discrepancy `json:"items"`
	}
	r := w.c.Do(http.MethodGet, "/discrepancies?kind=unidentified&pageSize=100&status="+status, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	for _, d := range list.Items {
		if d.BankLine != nil && d.BankLine.ID == lineID {
			return d
		}
	}
	t.Fatalf("нет расхождения «неопознанный» (%s) по строке %s", status, lineID)
	return discrepancy{}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	require.NoError(t, err)
	return b
}

// IT-11: выписка 1С из testdata → 3 сопоставлено, 1 неопознанный (расхождение), 1 исходящий пропущен; повторная
// загрузка → 4 дубля. ТК-23: неопознанный платёж привязан к договору, расхождение решено.
func TestStatement1C_IT11_TK22_TK23_FR_07_2_FR_07_3(t *testing.T) {
	w := setup(t, "it11")
	smirnova := w.lease(t, "Студия 1", "2026-03-10", 2800000, person("Смирнова", "Анна", "Сергеевна", "smirnova-it11@landlord.local"))
	setNumber(t, smirnova, "2026-001")
	kuznetsov := w.lease(t, "Студия 2", "2026-03-05", 2650000, person("Кузнецов", "Павел", "Игоревич", "kuznetsov-it11@landlord.local"))
	romashka := w.lease(t, "Помещение 1", "2026-05-01", 4500000, map[string]any{
		"kind": "legal", "orgName": "ООО «Ромашка»", "inn": "5075123457", "kpp": "507501001", "ogrn": "1235000012345",
		"legalAddress": "Московская область, г. Руза", "signatoryName": "Иванов Иван Иванович", "signatoryPosition": "Директор",
		"signatoryBasis": "Устава", "email": "romashka-it11@landlord.local",
	})

	st := w.upload(t, "statement_1c_win1251.txt", readTestdata(t, "statement_1c_win1251.txt"))
	assert.Equal(t, "1c", st.Format)
	assert.Equal(t, ownAccount, st.AccountNumber)
	assert.Equal(t, "2026-10-01", st.DateFrom)
	assert.Equal(t, "2026-10-14", st.DateTo)
	assert.Equal(t, 5, st.LinesTotal)
	assert.Equal(t, 4, st.LinesIncoming, "исходящий документ пропущен")
	assert.Equal(t, 3, st.LinesMatched)
	assert.Equal(t, 1, st.LinesUnmatched)
	assert.Equal(t, 0, st.LinesDuplicate)

	st = w.statement(t, st.ID)
	require.Len(t, st.Lines, 4)
	want := []struct{ status, method, contract string }{
		{"matched", "contract_number", smirnova}, {"matched", "payer_inn", romashka}, {"matched", "payer_name", kuznetsov},
		{"unmatched", "", ""},
	}
	for i, l := range st.Lines {
		assert.Equal(t, want[i].status, l.Status, "строка %d", l.LineNo)
		if want[i].method == "" {
			assert.Nil(t, l.MatchMethod)
			assert.Nil(t, l.PaymentID)
			continue
		}
		require.NotNil(t, l.MatchMethod)
		assert.Equal(t, want[i].method, *l.MatchMethod)
		require.NotNil(t, l.Contract)
		assert.Equal(t, want[i].contract, l.Contract.ID)
		require.NotNil(t, l.PaymentID)
		p := w.payment(t, *l.PaymentID)
		assert.Equal(t, "succeeded", p.Status)
		assert.Equal(t, "bank_import", p.Source)
		assert.Equal(t, "bank_transfer", p.Method)
		assert.Equal(t, l.AmountKop, p.AmountKop)
		require.NotNil(t, p.BankLine, "карточка платежа показывает строку выписки")
		assert.Equal(t, l.ID, p.BankLine.ID)
		require.NotNil(t, p.PayerName)
		assert.Equal(t, *l.PayerName, *p.PayerName)
	}
	romashkaPay := w.payment(t, *st.Lines[1].PaymentID)
	assert.Equal(t, "2026-10-02", romashkaPay.ReceivedOn, "дата поступления — ДатаПоступило")
	require.NotNil(t, romashkaPay.PayerInn)
	assert.Equal(t, "5075123457", *romashkaPay.PayerInn)

	// Письмо собственнику о платеже из выписки.
	var mails int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM email_log WHERE template_code = 'owner_payment_received' AND related_entity_id = $1`,
		romashkaPay.ID).Scan(&mails))
	assert.Equal(t, 1, mails)

	// Неопознанный — расхождение с данными строки.
	unmatched := st.Lines[3]
	d := w.unidentified(t, unmatched.ID, "open")
	assert.Equal(t, int64(1200000), d.AmountKop)
	require.NotNil(t, d.BankLine.PayerName)
	assert.Equal(t, "Иванова Т. В.", *d.BankLine.PayerName)

	// Повторная загрузка того же файла и CSV с теми же строками — только дубли.
	again := w.upload(t, "statement_1c_win1251.txt", readTestdata(t, "statement_1c_win1251.txt"))
	assert.Equal(t, 4, again.LinesDuplicate)
	assert.Equal(t, 0, again.LinesMatched)
	assert.Equal(t, 0, again.LinesUnmatched)
	csv := w.upload(t, "statement.csv", readTestdata(t, "statement.csv"))
	assert.Equal(t, "csv", csv.Format)
	assert.Equal(t, 4, csv.LinesDuplicate)
	for _, l := range w.statement(t, csv.ID).Lines {
		assert.Equal(t, "duplicate", l.Status)
	}

	// ТК-23: привязать к договору.
	r := w.c.Do(http.MethodPost, "/discrepancies/"+d.ID+"/resolve", map[string]any{"action": "link", "contractId": nil, "note": nil})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = w.c.Do(http.MethodPost, "/discrepancies/"+d.ID+"/resolve", map[string]any{
		"action": "link", "contractId": kuznetsov, "note": "Иванова — мать нанимателя",
	})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var res discrepancy
	r.JSON(t, &res)
	assert.Equal(t, "resolved", res.Status)
	require.NotNil(t, res.Resolution)
	assert.Equal(t, "linked", *res.Resolution)
	require.NotNil(t, res.Contract)
	assert.Equal(t, kuznetsov, res.Contract.ID)
	require.NotNil(t, res.PaymentID)
	require.NotNil(t, res.BankLine)
	assert.Equal(t, "linked_manual", res.BankLine.Status)
	assert.Equal(t, "manual", *res.BankLine.MatchMethod)
	linked := w.payment(t, *res.PaymentID)
	assert.Equal(t, "bank_import", linked.Source)
	assert.Equal(t, "2026-10-12", linked.ReceivedOn)
	r = w.c.Do(http.MethodPost, "/discrepancies/"+d.ID+"/resolve", map[string]any{"action": "ignore", "note": nil})
	assert.Equal(t, http.StatusConflict, r.Status, "уже разобрано")

	// Отмена платежа из выписки — строка снова не опознана, расхождение открыто (05 разд. 10.5).
	r = w.c.Do(http.MethodPost, "/payments/"+linked.ID+"/cancel", map[string]any{"reason": "Привязан ошибочно"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	d = w.unidentified(t, unmatched.ID, "open")
	assert.Equal(t, "unmatched", d.BankLine.Status)
	assert.Nil(t, d.PaymentID)

	// «Не относится к аренде».
	r = w.c.Do(http.MethodPost, "/discrepancies/"+d.ID+"/resolve", map[string]any{"action": "ignore", "note": "Возврат долга"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &res)
	assert.Equal(t, "ignored", *res.Resolution)
	assert.Equal(t, "ignored", res.BankLine.Status)

	// Список загрузок и образец CSV.
	var list struct {
		Items []statement `json:"items"`
		Total int         `json:"total"`
	}
	w.c.Do(http.MethodGet, "/bank-statements", nil).JSON(t, &list)
	assert.GreaterOrEqual(t, list.Total, 3)
	assert.Equal(t, csv.ID, list.Items[0].ID, "последняя загрузка — первой")
	r = w.c.Do(http.MethodGet, "/bank-statements/csv-template", nil)
	require.Equal(t, http.StatusOK, r.Status)
	assert.True(t, strings.HasPrefix(string(r.Body), "\xEF\xBB\xBF"+"Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа\r\n"))
	assert.Contains(t, r.Header.Get("Content-Disposition"), "attachment")

	// Журнал: загрузка выписки и разбор.
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = 'statement.upload' AND entity_id = $1`, st.ID).Scan(&n))
	assert.Equal(t, 1, n)
}

// Возможный дубль ±3 дня: «Это тот же платёж» и «Это другой платёж» (05 разд. 15.1, шаг 5).
func TestPossibleDuplicate_FR_07_2(t *testing.T) {
	w := setup(t, "dup")
	contract := w.lease(t, "Студия 7", "2026-09-01", 3000000, person("Белова", "Ольга", "Витальевна", "belova-dup@landlord.local"))
	var c struct {
		Number string `json:"number"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+contract, nil).JSON(t, &c)
	r := w.c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": contract, "purpose": "charges", "amountKop": 3000000, "receivedOn": "2026-10-08", "method": "bank_transfer",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	manual := r.ID(t)

	body := "Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа\n" +
		"11.10.2026;30 000,00;Белова Ольга Витальевна;;40817810100000000777;Оплата по договору " + c.Number + ";901\n" +
		"10.10.2026;30 000,00;Белова Ольга Витальевна;;40817810100000000777;Оплата по договору " + c.Number + ";902\n" +
		"12.10.2026;29 000,00;Белова Ольга Витальевна;;40817810100000000777;Оплата по договору " + c.Number + ";903\n"
	st := w.upload(t, "dup.csv", []byte(body))
	assert.Equal(t, 1, st.LinesMatched, "другая сумма — обычный платёж")
	assert.Equal(t, 2, st.LinesDuplicate, "возможные дубли входят в счётчик дублей")
	lines := w.statement(t, st.ID).Lines
	require.Len(t, lines, 3)
	assert.Equal(t, "possible_duplicate", lines[0].Status)
	assert.Equal(t, "possible_duplicate", lines[1].Status)
	assert.Nil(t, lines[0].PaymentID, "платёж не создаётся")
	require.Len(t, lines[0].CandidatePayments, 1)
	assert.Equal(t, manual, lines[0].CandidatePayments[0].ID)
	assert.Equal(t, "matched", lines[2].Status)

	// Платёж не из найденных — ошибка; «тот же платёж» — строка связана с ручным платежом.
	r = w.c.Do(http.MethodPost, "/bank-statement-lines/"+lines[0].ID+"/same-payment", map[string]any{"paymentId": *lines[2].PaymentID})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = w.c.Do(http.MethodPost, "/bank-statement-lines/"+lines[0].ID+"/same-payment", map[string]any{"paymentId": manual})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var l bankLine
	r.JSON(t, &l)
	assert.Equal(t, "matched", l.Status)
	require.NotNil(t, l.PaymentID)
	assert.Equal(t, manual, *l.PaymentID)
	p := w.payment(t, manual)
	assert.Equal(t, "manual", p.Source)
	require.NotNil(t, p.PayerName)
	assert.Equal(t, "Белова Ольга Витальевна", *p.PayerName)
	require.NotNil(t, p.BankPurpose)
	require.NotNil(t, p.BankLine)

	// Вторая строка: ручной платёж уже связан — кандидатов нет; «другой платёж» создаёт платёж.
	lines = w.statement(t, st.ID).Lines
	assert.Empty(t, lines[1].CandidatePayments)
	r = w.c.Do(http.MethodPost, "/bank-statement-lines/"+lines[1].ID+"/different-payment", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &l)
	assert.Equal(t, "matched", l.Status)
	require.NotNil(t, l.PaymentID)
	assert.Equal(t, "bank_import", w.payment(t, *l.PaymentID).Source)
	r = w.c.Do(http.MethodPost, "/bank-statement-lines/"+lines[1].ID+"/different-payment", nil)
	assert.Equal(t, http.StatusConflict, r.Status, "строка уже разобрана")

	// Отмена ручного платежа, связанного со строкой, — строка снова не опознана (docs/open-questions.md, № 72).
	r = w.c.Do(http.MethodPost, "/payments/"+manual+"/cancel", map[string]any{"reason": "Дубль"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	d := w.unidentified(t, lines[0].ID, "open")
	assert.Equal(t, int64(3000000), d.AmountKop)
}

// Ошибки формата: 422 со списком «Строка N: …», выписка не сохраняется.
func TestStatementErrors_FR_07_2(t *testing.T) {
	w := setup(t, "err")
	var before struct {
		Total int `json:"total"`
	}
	w.c.Do(http.MethodGet, "/bank-statements", nil).JSON(t, &before)
	body := "Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа\n" +
		"10.10.2026;100;Иванов;;;Оплата;1\n31.02.2026;100;Иванов;;;Оплата;2\n"
	r := w.c.Multipart("/bank-statements", nil, "bad.csv", []byte(body))
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
	assert.Contains(t, string(r.Body), "Строка 3: неверная дата «31.02.2026»")
	var after struct {
		Total int `json:"total"`
	}
	w.c.Do(http.MethodGet, "/bank-statements", nil).JSON(t, &after)
	assert.Equal(t, before.Total, after.Total)

	r = w.c.Multipart("/bank-statements", nil, "photo.png", testsupport.PNG)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	// Наниматель не имеет доступа к сверке.
	tenant := w.env.ActiveTenant(t, "recon-tenant-err@landlord.local", password)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/bank-statements", nil).Status)
}
