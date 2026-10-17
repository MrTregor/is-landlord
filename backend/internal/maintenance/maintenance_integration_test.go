//go:build integration

package maintenance_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-заявки-1"

type world struct {
	env      *testsupport.Env
	c        *testsupport.Client
	email    string
	sfx      string
	property string
	tenant   string
	contract string
}

// setup — собственник, объект, наниматель и действующий договор с залогом 30 000 ₽ (внесён).
func setup(t *testing.T, sfx string) world {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-15")
	email := "requests-" + sfx + "@landlord.local"
	c := env.ActiveOwner(t, email, password)
	w := world{env: env, c: c, email: email, sfx: sfx}
	r := c.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Студия " + sfx, "kind": "residential", "address": "г. Руза, ул. Федеративная, д. 14, кв. " + sfx, "areaSqm": "26.10",
		"floor": 3, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": true,
		"expectedRentKop": 2700000, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.property = r.ID(t)
	r = c.Do(http.MethodPost, "/tenants", map[string]any{
		"kind": "individual", "lastName": "Орлова", "firstName": "Мария", "middleName": "Дмитриевна", "docType": "passport_rf",
		"docNumber": "45 18 555666", "registrationAddress": "г. Москва, ул. Примерная, д. 4", "email": "orlova-" + sfx + "@landlord.local",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.tenant = r.ID(t)
	r = c.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": w.property, "tenantId": w.tenant, "startDate": "2026-09-01", "endDate": "2027-07-31", "rentKop": 2700000,
		"paymentDay": 1, "depositKop": 3000000, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false,
		"penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.contract = r.ID(t)
	r = c.Do(http.MethodPost, "/contracts/"+w.contract+"/activate", map[string]any{"concludedOn": "2026-09-01", "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = c.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": w.contract, "purpose": "deposit", "amountKop": 3000000, "receivedOn": "2026-09-01", "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	return w
}

type request struct {
	ID          string `json:"id"`
	Number      string `json:"number"`
	Status      string `json:"status"`
	AuthorRole  string `json:"authorRole"`
	TenantFault bool   `json:"tenantFault"`
	Contractor  *string
	ResultNote  *string `json:"resultNote"`
	Contract    *struct {
		ID string `json:"id"`
	} `json:"contract"`
	Photos []struct {
		ID string `json:"id"`
	} `json:"photos"`
	Events []struct {
		Kind     string  `json:"kind"`
		ToStatus *string `json:"toStatus"`
		Text     *string `json:"text"`
	} `json:"events"`
	Expenses []struct {
		ID       string `json:"id"`
		Recovery string `json:"recovery"`
	} `json:"expenses"`
}

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// ТК-24: заявка нанимателя с фото — статус «Новая», лента, письмо собственнику; чужой договор и чужое фото — отказ
// (FR-08.1, FR-08.2).
func TestTenantRequestWithPhoto_TK24_FR_08_1(t *testing.T) {
	w := setup(t, "t24")
	tenant := w.env.TenantUser(t, w.tenant, "orlova-cab-t24@landlord.local", password)
	r := tenant.Upload("request_photo", "кран.png", testsupport.PNG)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	photo := r.ID(t)
	ownerPhoto := w.c.Upload("request_photo", "чужое.png", testsupport.PNG).ID(t)

	body := map[string]any{"contractId": w.contract, "category": "plumbing", "title": "Течёт смеситель на кухне",
		"description": "Капает из-под рычага, лужа на столешнице", "photoFileIds": []string{ownerPhoto}}
	r = tenant.Do(http.MethodPost, "/cabinet/requests", body)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status, "чужое фото не прикладывается")
	body["photoFileIds"] = []string{photo}
	r = tenant.Do(http.MethodPost, "/cabinet/requests", body)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var created struct {
		ID     string `json:"id"`
		Number string `json:"number"`
		Status string `json:"status"`
		Photos []struct {
			ID string `json:"id"`
		} `json:"photos"`
	}
	r.JSON(t, &created)
	assert.Equal(t, "new", created.Status)
	assert.Regexp(t, `^З-2026-\d{3}$`, created.Number)
	require.Len(t, created.Photos, 1)
	assert.Equal(t, photo, created.Photos[0].ID)
	assert.NotContains(t, string(r.Body), "contractor", "без внутренних полей")

	owners := count(t, `SELECT count(*) FROM users WHERE role = 'owner' AND status = 'active'`)
	assert.Equal(t, owners, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'owner_request_created' AND related_entity_id = $1`,
		created.ID), "письмо каждому собственнику")
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'request.transition' AND entity_id = $1`, created.ID))

	var detail request
	w.c.Do(http.MethodGet, "/maintenance-requests/"+created.ID, nil).JSON(t, &detail)
	assert.Equal(t, "tenant", detail.AuthorRole)
	require.NotNil(t, detail.Contract)
	assert.Equal(t, w.contract, detail.Contract.ID)
	require.Len(t, detail.Events, 1)

	var list []struct {
		ID string `json:"id"`
	}
	tenant.Do(http.MethodGet, "/cabinet/requests?contractId="+w.contract, nil).JSON(t, &list)
	require.Len(t, list, 1)
	var openReq struct {
		Stats struct {
			OpenRequests int `json:"openRequests"`
		} `json:"stats"`
	}
	w.c.Do(http.MethodGet, "/properties/"+w.property, nil).JSON(t, &openReq)
	assert.Equal(t, 1, openReq.Stats.OpenRequests)

	// Чужой наниматель: заявки и договор другого — 404.
	other := setup(t, "t24b")
	stranger := other.env.TenantUser(t, other.tenant, "orlova-cab-t24b@landlord.local", password)
	assert.Equal(t, http.StatusNotFound, stranger.Do(http.MethodGet, "/cabinet/requests/"+created.ID, nil).Status)
	assert.Equal(t, http.StatusNotFound, stranger.Do(http.MethodGet, "/cabinet/requests?contractId="+w.contract, nil).Status)
	assert.Equal(t, http.StatusNotFound, stranger.Do(http.MethodPost, "/cabinet/requests", map[string]any{
		"contractId": w.contract, "category": "other", "title": "Чужая заявка", "description": "—", "photoFileIds": []string{},
	}).Status)
	assert.Equal(t, http.StatusNotFound, stranger.Do(http.MethodPost, "/cabinet/requests/"+created.ID+"/comments",
		map[string]any{"text": "Привет"}).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/maintenance-requests", nil).Status)
}

// Переходы статусов по таблице 05 разд. 14.1, лента, письма нанимателю (FR-08.2).
func TestRequestTransitions_FR_08_2(t *testing.T) {
	w := setup(t, "tr")
	tenant := w.env.TenantUser(t, w.tenant, "orlova-cab-tr@landlord.local", password)
	r := w.c.Do(http.MethodPost, "/maintenance-requests", map[string]any{
		"propertyId": w.property, "category": "appliance", "title": "Не работает духовка", "description": "Не греет",
		"priority": "high", "photoFileIds": []string{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var q request
	r.JSON(t, &q)
	require.NotNil(t, q.Contract, "подставлен действующий договор объекта")
	assert.Equal(t, "owner", q.AuthorRole)
	path := "/maintenance-requests/" + q.ID

	move := func(to string, extra map[string]any) int {
		body := map[string]any{"toStatus": to}
		for k, v := range extra {
			body[k] = v
		}
		return w.c.Do(http.MethodPost, path+"/transition", body).Status
	}
	assert.Equal(t, http.StatusConflict, move("done", nil), "из «Новая» сразу в «Выполнена» нельзя")
	assert.Equal(t, http.StatusOK, move("in_progress", nil))
	r = w.c.Do(http.MethodPatch, path, map[string]any{"contractor": "Сервис-Руза", "plannedDate": "2026-10-18", "tenantFault": true})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &q)
	assert.True(t, q.TenantFault)
	assert.Equal(t, http.StatusUnprocessableEntity, move("done", nil), "нужен результат работ")
	assert.Equal(t, http.StatusOK, move("done", map[string]any{"resultNote": "Заменён ТЭН"}))

	// Наниматель: «Не устранено» без комментария — ошибка, с комментарием — снова в работе; затем подтверждение.
	assert.Equal(t, http.StatusBadRequest, tenant.Do(http.MethodPost, "/cabinet/requests/"+q.ID+"/reopen", map[string]any{}).Status)
	r = tenant.Do(http.MethodPost, "/cabinet/requests/"+q.ID+"/reopen", map[string]any{"comment": "Опять не греет"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, http.StatusConflict, tenant.Do(http.MethodPost, "/cabinet/requests/"+q.ID+"/confirm", nil).Status,
		"подтвердить можно только выполненную")
	assert.Equal(t, http.StatusOK, move("done", map[string]any{"resultNote": "Заменён терморегулятор"}))
	r = tenant.Do(http.MethodPost, "/cabinet/requests/"+q.ID+"/comments", map[string]any{"text": "Спасибо, работает"})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = tenant.Do(http.MethodPost, "/cabinet/requests/"+q.ID+"/confirm", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))

	w.c.Do(http.MethodGet, path, nil).JSON(t, &q)
	assert.Equal(t, "closed", q.Status)
	require.NotNil(t, q.ResultNote)
	assert.Equal(t, "Заменён терморегулятор", *q.ResultNote)
	kinds := []string{}
	for _, e := range q.Events {
		kinds = append(kinds, e.Kind)
	}
	assert.Equal(t, []string{"status", "status", "status", "status", "status", "comment", "status"}, kinds)
	// Письма нанимателю — только о переходах собственника: в работе, выполнена (дважды).
	assert.Equal(t, 3, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'request_status_changed' AND related_entity_id = $1`, q.ID))
	assert.Equal(t, http.StatusConflict, w.c.Do(http.MethodPost, path+"/comments", map[string]any{"text": "Ещё"}).Status,
		"после закрытия комментарии не принимаются")

	// «Новая» → «Закрыта» — с причиной.
	r = w.c.Do(http.MethodPost, "/maintenance-requests", map[string]any{
		"propertyId": w.property, "category": "other", "title": "Дубль заявки", "description": "—", "priority": "low", "photoFileIds": []string{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	path = "/maintenance-requests/" + r.ID(t)
	assert.Equal(t, http.StatusUnprocessableEntity, move("closed", nil))
	assert.Equal(t, http.StatusOK, move("closed", map[string]any{"comment": "Дубль"}))

	var list struct {
		Total int `json:"total"`
	}
	w.c.Do(http.MethodGet, "/maintenance-requests?status=new&status=in_progress&status=done&propertyId="+w.property, nil).JSON(t, &list)
	assert.Equal(t, 0, list.Total, "все заявки объекта закрыты")
}

// ТК-16: затрата по заявке за счёт нанимателя — удержание из залога со связью с заявкой и затратой; затрата
// с начислением «возмещение ущерба»; удаление возмещённой затраты запрещено (FR-05.6).
func TestExpenseRecovery_TK16_FR_05_6(t *testing.T) {
	w := setup(t, "t16")
	r := w.c.Do(http.MethodPost, "/maintenance-requests", map[string]any{
		"propertyId": w.property, "category": "furniture", "title": "Сломана дверца шкафа", "description": "Оторвана петля",
		"priority": "normal", "photoFileIds": []string{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	reqID := r.ID(t)
	doc := w.c.Upload("expense_doc", "акт.pdf", []byte("%PDF-1.4\n%тест\n")).ID(t)

	exp := map[string]any{
		"propertyId": w.property, "category": "repair", "amountKop": 300000, "paidOn": "2026-10-14", "periodMonth": "2026-10-14",
		"description": "Ремонт дверцы шкафа", "counterparty": "Сервис-Руза", "fileId": doc, "maintenanceRequestId": reqID,
		"recovery": "deposit", "contractId": nil, "chargeDueDate": nil,
	}
	r = w.c.Do(http.MethodPost, "/expenses", exp)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var e struct {
		ID                   string  `json:"id"`
		Recovery             string  `json:"recovery"`
		PeriodMonth          string  `json:"periodMonth"`
		DepositTransactionID *string `json:"depositTransactionId"`
		File                 *struct {
			ID string `json:"id"`
		} `json:"file"`
		MaintenanceRequest *struct {
			Number string `json:"number"`
		} `json:"maintenanceRequest"`
	}
	r.JSON(t, &e)
	assert.Equal(t, "deposit", e.Recovery)
	assert.Equal(t, "2026-10-01", e.PeriodMonth, "месяц отнесения — первое число")
	require.NotNil(t, e.DepositTransactionID)
	require.NotNil(t, e.File)
	require.NotNil(t, e.MaintenanceRequest)

	var dep struct {
		BalanceKop int64 `json:"balanceKop"`
		Operations []struct {
			ID                   string  `json:"id"`
			Kind                 string  `json:"kind"`
			MaintenanceRequestID *string `json:"maintenanceRequestId"`
			ExpenseID            *string `json:"expenseId"`
		} `json:"operations"`
	}
	w.c.Do(http.MethodGet, "/contracts/"+w.contract+"/deposit", nil).JSON(t, &dep)
	assert.Equal(t, int64(2700000), dep.BalanceKop)
	last := dep.Operations[len(dep.Operations)-1]
	assert.Equal(t, "withholding", last.Kind)
	require.NotNil(t, last.MaintenanceRequestID)
	assert.Equal(t, reqID, *last.MaintenanceRequestID)
	require.NotNil(t, last.ExpenseID)
	assert.Equal(t, e.ID, *last.ExpenseID)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE template_code = 'deposit_withheld' AND dedupe_key = $1`,
		"deposit_withheld:"+last.ID))

	var detail request
	w.c.Do(http.MethodGet, "/maintenance-requests/"+reqID, nil).JSON(t, &detail)
	require.Len(t, detail.Expenses, 1)
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodDelete, "/expenses/"+e.ID, nil).Status)
	upd := map[string]any{"propertyId": w.property, "category": "repair", "amountKop": 350000, "paidOn": "2026-10-14",
		"periodMonth": "2026-10-01", "description": "Ремонт дверцы шкафа", "counterparty": nil, "fileId": doc, "maintenanceRequestId": reqID}
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodPut, "/expenses/"+e.ID, upd).Status, "сумму возмещённой не менять")
	upd["amountKop"] = 300000
	upd["description"] = "Ремонт дверцы шкафа (петля)"
	assert.Equal(t, http.StatusOK, w.c.Do(http.MethodPut, "/expenses/"+e.ID, upd).Status)

	// Возмещение начислением: срок по умолчанию — сегодня + 10 дней.
	exp["recovery"], exp["amountKop"], exp["description"], exp["fileId"] = "charge", 420000, "Замена смесителя", nil
	r = w.c.Do(http.MethodPost, "/expenses", exp)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	var e2 struct {
		ChargeID *string `json:"chargeId"`
	}
	r.JSON(t, &e2)
	require.NotNil(t, e2.ChargeID)
	var ch struct {
		Kind      string `json:"kind"`
		AmountKop int64  `json:"amountKop"`
		DueDate   string `json:"dueDate"`
	}
	w.c.Do(http.MethodGet, "/charges/"+*e2.ChargeID, nil).JSON(t, &ch)
	assert.Equal(t, "damage", ch.Kind)
	assert.Equal(t, int64(420000), ch.AmountKop)
	assert.Equal(t, "2026-10-25", ch.DueDate)

	// Без возмещения — удаляется; удержание больше остатка — ошибка, затрата не создаётся.
	exp["recovery"], exp["amountKop"], exp["maintenanceRequestId"] = "none", 280000, nil
	r = w.c.Do(http.MethodPost, "/expenses", exp)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	assert.Equal(t, http.StatusNoContent, w.c.Do(http.MethodDelete, "/expenses/"+r.ID(t), nil).Status)
	exp["recovery"], exp["amountKop"] = "deposit", 9900000
	assert.Equal(t, http.StatusUnprocessableEntity, w.c.Do(http.MethodPost, "/expenses", exp).Status)

	var list struct {
		Total   int `json:"total"`
		Summary struct {
			AmountKop int64 `json:"amountKop"`
		} `json:"summary"`
	}
	w.c.Do(http.MethodGet, "/expenses?propertyId="+w.property, nil).JSON(t, &list)
	assert.Equal(t, 2, list.Total)
	assert.Equal(t, int64(720000), list.Summary.AmountKop)
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM audit_log WHERE action = 'expense.delete' AND summary LIKE 'Удалена затрата%Замена смесителя'`))
}
