//go:build integration

package tenant_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

const anonPass = "Пароль-обезличивания-1"

type anonWorld struct {
	env      *testsupport.Env
	owner    *testsupport.Client
	tenant   string
	contract string
	doc      string
	email    string
}

// anonSetup — наниматель с кабинетом, действующий договор с 01.09.2026, платёж и сформированный договор (PDF).
func anonSetup(t *testing.T, sfx string) anonWorld {
	t.Helper()
	env := testsupport.NewEnv(t, "2026-10-15")
	owner := env.ActiveOwner(t, "anon-"+sfx+"@landlord.local", anonPass)
	w := anonWorld{env: env, owner: owner, email: "orlova-" + sfx + "@landlord.local"}
	r := owner.Do(http.MethodPost, "/properties", map[string]any{
		"code": "Обезл " + sfx, "kind": "residential", "address": "г. Руза, ул. Солнечная, д. 7, кв. " + sfx, "areaSqm": "30.00",
		"floor": 1, "rooms": 1, "cadastralNumber": nil, "ownershipBasis": nil, "description": nil, "furnished": false,
		"expectedRentKop": 2500000, "fiscalMode": "none", "initialStatus": "free", "statusSince": "2025-01-01",
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	property := r.ID(t)
	p := person()
	p["email"], p["lastName"] = w.email, "Орлова"
	r = owner.Do(http.MethodPost, "/tenants", p)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.tenant = r.ID(t)
	r = owner.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": property, "tenantId": w.tenant, "startDate": "2026-09-01", "endDate": "2027-07-31", "rentKop": 2500000,
		"paymentDay": 5, "depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false,
		"penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12,
		"occupants": []any{map[string]any{"fullName": "Орлов Иван Петрович", "birthDate": "2012-01-02", "relation": "сын"}},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.contract = r.ID(t)
	r = owner.Do(http.MethodPost, "/contracts/"+w.contract+"/activate", map[string]any{"concludedOn": "2026-09-01", "inviteTenant": false})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r = owner.Do(http.MethodPost, "/payments", map[string]any{
		"contractId": w.contract, "purpose": "charges", "amountKop": 2500000, "receivedOn": "2026-10-05", "method": "cash",
		"chargeIds": []string{}, "comment": nil,
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	r = owner.Do(http.MethodPost, "/contracts/"+w.contract+"/documents/contract", nil)
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	w.doc = r.ID(t)
	env.TenantUser(t, w.tenant, "cab-"+w.email, anonPass)
	return w
}

func (w anonWorld) terminate(t *testing.T, on string) {
	t.Helper()
	r := w.owner.Do(http.MethodPost, "/contracts/"+w.contract+"/terminate", map[string]any{
		"terminationDate": on, "initiator": "tenant", "reason": "Переезд"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
}

func scalar[T any](t *testing.T, sql string, args ...any) T {
	t.Helper()
	var v T
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&v))
	return v
}

// checkAnonymized — ПДн очищены, файлы документов удалены, деньги сохранены, вход невозможен (NFR-Sec-05).
func (w anonWorld) checkAnonymized(t *testing.T) {
	t.Helper()
	assert.Equal(t, "Обезличен", scalar[string](t, `SELECT last_name FROM tenants WHERE id = $1`, w.tenant))
	assert.Equal(t, 0, scalar[int](t, `SELECT count(*) FROM tenants WHERE id = $1 AND (phone IS NOT NULL OR email IS NOT NULL
		OR birth_date IS NOT NULL OR doc_number_enc IS NOT NULL OR registration_address_enc IS NOT NULL OR anonymized_at IS NULL)`, w.tenant))
	assert.Equal(t, "Обезличено", scalar[string](t, `SELECT full_name FROM contract_occupants WHERE contract_id = $1`, w.contract))
	assert.Equal(t, "blocked", scalar[string](t, `SELECT status FROM users WHERE tenant_id = $1`, w.tenant))
	assert.Equal(t, 0, scalar[int](t, `SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE u.tenant_id = $1 AND s.revoked_at IS NULL`, w.tenant))
	key := scalar[string](t, `SELECT f.storage_key FROM documents d JOIN files f ON f.id = d.file_id WHERE d.id = $1 AND f.deleted_at IS NOT NULL`, w.doc)
	_, err := os.Stat(filepath.Join(w.env.App.Config.FilesDir, key))
	assert.True(t, os.IsNotExist(err), "PDF договора удалён с диска")
	assert.Equal(t, "", scalar[string](t, `SELECT html_snapshot FROM documents WHERE id = $1`, w.doc), "строка документа сохранена без содержимого")
	assert.Equal(t, 0, scalar[int](t, `SELECT count(*) FROM email_log WHERE to_email IN ($1, $2)`, w.email, "cab-"+w.email))
	assert.Equal(t, int64(2500000), scalar[int64](t, `SELECT sum(amount_kop) FROM payments WHERE contract_id = $1 AND status = 'succeeded'`, w.contract),
		"денежные данные сохранены")
	assert.Positive(t, scalar[int](t, `SELECT count(*) FROM charges WHERE contract_id = $1`, w.contract))
	c := w.env.NewClient(t)
	r := c.Do(http.MethodPost, "/auth/login", map[string]string{"email": "cab-" + w.email, "password": anonPass})
	assert.Equal(t, http.StatusUnauthorized, r.Status, "вход нанимателя невозможен")
	assert.Equal(t, 1, scalar[int](t, `SELECT count(*) FROM audit_log WHERE action = 'tenant.anonymize' AND entity_id = $1`, w.tenant))
}

// ТК-31: ручное обезличивание — при действующем договоре отказ, после расторжения ПДн удалены, финансы сохранены
// (NFR-Sec-05).
func TestAnonymizeManual_TK31_NFR_Sec_05(t *testing.T) {
	w := anonSetup(t, "m31")
	r := w.owner.Do(http.MethodPost, "/tenants/"+w.tenant+"/anonymize", map[string]any{"confirm": true})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, string(r.Body))
	assert.Contains(t, string(r.Body), "есть договоры в статусах")

	w.terminate(t, "2026-10-15")
	r = w.owner.Do(http.MethodPost, "/tenants/"+w.tenant+"/anonymize", map[string]any{"confirm": true})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var d tenantDetail
	r.JSON(t, &d)
	assert.Nil(t, d.Phone)
	assert.Nil(t, d.Email)
	w.checkAnonymized(t)
	assert.Equal(t, http.StatusConflict, w.owner.Do(http.MethodPost, "/tenants/"+w.tenant+"/anonymize", map[string]any{"confirm": true}).Status)
}

// IT-13: обезличивание по сроку хранения — через 3 года и 1 день после окончания договора (NFR-Sec-05).
func TestRetentionDaily_IT13_NFR_Sec_05(t *testing.T) {
	w := anonSetup(t, "r13")
	w.terminate(t, "2026-10-31")
	svc := w.env.App.Tenants
	at := func(day string) {
		d, _ := time.ParseInLocation("2006-01-02", day, testsupport.Moscow())
		w.env.Clock.Set(d.Add(12 * time.Hour))
	}
	at("2029-10-31")
	st, err := svc.RunRetentionDaily(context.Background(), jobs.DailyArgs{})
	require.NoError(t, err)
	assert.Empty(t, st.Errors)
	assert.Equal(t, 0, scalar[int](t, `SELECT count(*) FROM tenants WHERE id = $1 AND anonymized_at IS NOT NULL`, w.tenant),
		"ровно 3 года — ещё хранится")

	at("2029-11-01")
	st, err = svc.RunRetentionDaily(context.Background(), jobs.DailyArgs{})
	require.NoError(t, err)
	assert.Empty(t, st.Errors)
	w.checkAnonymized(t)
	assert.Equal(t, "succeeded", scalar[string](t, `SELECT status FROM job_runs WHERE job_name = 'pdn.retention_daily' ORDER BY started_at DESC LIMIT 1`))
}

// № 105, № 108: наниматель без договоров (кроме черновиков) обезличивается через 6 месяцев после последнего изменения
// карточки или черновика — цель «заключение договора» отпала (ч. 4 ст. 21 152-ФЗ) (NFR-Sec-05).
func TestRetentionWithoutContracts_NFR_Sec_05(t *testing.T) {
	w := anonSetup(t, "n05")
	ctx := context.Background()
	newTenant := func(email string) string {
		p := person()
		p["email"], p["lastName"], p["phone"] = email, "Седова", nil // не мешать поиску «кузнец» и «916-123» в TK-04
		r := w.owner.Do(http.MethodPost, "/tenants", p)
		require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
		return r.ID(t)
	}
	// Наниматель anonSetup здесь не обезличивается — его телефон не должен мешать поиску «916-123» в TK-04.
	_, err := testsupport.Owner(t).Exec(ctx, `UPDATE tenants SET phone = NULL WHERE id = $1`, w.tenant)
	require.NoError(t, err)
	lonely := newTenant("lonely-n05@landlord.local")
	drafty := newTenant("drafty-n05@landlord.local")
	var property string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx, `SELECT property_id FROM contracts WHERE id = $1`, w.contract).Scan(&property))
	r := w.owner.Do(http.MethodPost, "/contracts", map[string]any{
		"propertyId": property, "tenantId": drafty, "startDate": "2027-08-01", "endDate": "2028-06-30", "rentKop": 2500000,
		"paymentDay": 5, "depositKop": 0, "utilitiesMode": "included", "utilitiesFixedKop": 0, "penaltyEnabled": false,
		"penaltyGraceDays": 0, "penaltyOnUtilities": false, "indexationEnabled": false, "indexationPeriodMonths": 12, "occupants": []any{},
	})
	require.Equal(t, http.StatusCreated, r.Status, string(r.Body))
	draft := r.ID(t)
	// Даты создания и изменения — мимо триггера updated_at.
	conn := testsupport.Owner(t)
	_, err = conn.Exec(ctx, `SET session_replication_role = replica`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE tenants SET updated_at = '2026-10-15 12:00+03' WHERE id IN ($1, $2)`, lonely, drafty)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `UPDATE contracts SET updated_at = '2027-01-10 12:00+03' WHERE id = $1`, draft)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `SET session_replication_role = DEFAULT`)
	require.NoError(t, err)

	anonymized := func(id string) bool {
		return scalar[int](t, `SELECT count(*) FROM tenants WHERE id = $1 AND anonymized_at IS NOT NULL`, id) == 1
	}
	run := func(day string) {
		t.Helper()
		d, _ := time.ParseInLocation("2006-01-02", day, testsupport.Moscow())
		w.env.Clock.Set(d.Add(12 * time.Hour))
		st, err := w.env.App.Tenants.RunRetentionDaily(ctx, jobs.DailyArgs{})
		require.NoError(t, err)
		assert.Empty(t, st.Errors)
	}
	run("2027-04-15")
	assert.False(t, anonymized(lonely), "ровно 6 месяцев после изменения карточки — ещё хранится")
	run("2027-04-16")
	assert.True(t, anonymized(lonely), "без договоров — через 6 месяцев после изменения карточки")
	assert.False(t, anonymized(drafty), "черновик изменён 10.01.2027 — срок от него")
	assert.False(t, anonymized(w.tenant), "действующий договор")
	run("2027-07-11")
	assert.True(t, anonymized(drafty), "только черновики — через 6 месяцев после последнего изменения черновика")
	assert.False(t, anonymized(w.tenant), "договор действует, срок хранения — в годах после окончания")
}
