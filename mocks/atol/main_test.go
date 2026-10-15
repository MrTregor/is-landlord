package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// shop — система учёта: принимает колбэки (первые fail запросов — ошибкой).
type shop struct {
	mu   sync.Mutex
	got  []map[string]any
	fail int
}

func (s *shop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var m map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	_ = dec.Decode(&m)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	if s.fail > 0 {
		s.fail--
		http.Error(w, "занято", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *shop) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

type env struct {
	srv   *Server
	h     http.Handler
	shop  *shop
	hook  *httptest.Server
	token string
	file  string
}

func newEnv(t *testing.T, delay time.Duration) *env {
	t.Helper()
	file := filepath.Join(t.TempDir(), "atol-state.json")
	return newEnvFile(t, delay, file)
}

func newEnvFile(t *testing.T, delay time.Duration, file string) *env {
	t.Helper()
	st, err := NewStore(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Init(time.Now()); err != nil {
		t.Fatal(err)
	}
	sh := &shop{}
	hook := httptest.NewServer(sh)
	t.Cleanup(hook.Close)
	cfg := Config{PublicURL: "http://atol.test", Login: "demo", Password: "demo", GroupCode: "landlord-demo",
		ProcessDelay: delay, CallbackAttempts: 3, CallbackInterval: 10 * time.Millisecond}
	srv := NewServer(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e := &env{srv: srv, h: srv.Handler(), shop: sh, hook: hook, file: file}
	t.Cleanup(func() {
		srv.Close()
		srv.Wait()
	})
	e.token = e.getToken(t, "demo", "demo")
	return e
}

func (e *env) do(t *testing.T, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Token", token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var m map[string]any
	dec := json.NewDecoder(rec.Body)
	dec.UseNumber()
	_ = dec.Decode(&m)
	return rec.Code, m
}

func (e *env) getToken(t *testing.T, login, pass string) string {
	t.Helper()
	_, m := e.do(t, http.MethodPost, "/possystem/v5/getToken", map[string]string{"login": login, "pass": pass}, "")
	tok, _ := m["token"].(string)
	return tok
}

// receipt — чек прихода из примера 08 разд. 3.2.
func (e *env) receipt(externalID string) map[string]any {
	return map[string]any{
		"external_id": externalID,
		"timestamp":   "16.10.2026 19:30:00",
		"service":     map[string]any{"callback_url": e.hook.URL + "/api/v1/integrations/atol/callback"},
		"receipt": map[string]any{
			"client":  map[string]any{"email": "tenant@example.ru", "name": "ООО «Ромашка»", "inn": "5075000000"},
			"company": map[string]any{"email": "owner@example.ru", "sno": "usn_income", "inn": "507500363778", "payment_address": "https://landlord.example"},
			"items": []any{map[string]any{
				"name": "Арендная плата за октябрь 2026, договор № 2026-011", "price": json.Number("45000.00"),
				"quantity": json.Number("1.000"), "sum": json.Number("45000.00"), "measure": 0, "payment_method": "full_payment",
				"payment_object": 4, "vat": map[string]any{"type": "none"},
			}},
			"payments": []any{map[string]any{"type": 1, "sum": json.Number("45000.00")}},
			"total":    json.Number("45000.00"),
		},
	}
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(json.Number)
	return string(c)
}

func errText(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["text"].(string)
	return s
}

const sellPath = "/possystem/v5/landlord-demo/sell"

func TestToken(t *testing.T) {
	e := newEnv(t, 0)
	if e.token == "" {
		t.Fatal("токен не выдан")
	}
	code, m := e.do(t, http.MethodPost, "/possystem/v5/getToken", map[string]string{"login": "demo", "pass": "нет"}, "")
	if code != http.StatusUnauthorized || errCode(m) != "12" || errText(m) != "Неверный логин или пароль" || m["token"] != nil {
		t.Fatalf("неверный пароль: %d %v", code, m)
	}
}

func TestSellChecks(t *testing.T) {
	e := newEnv(t, time.Hour)
	if _, m := e.do(t, http.MethodPost, sellPath, e.receipt("a"), ""); errCode(m) != "11" || errText(m) != "Срок действия токена истёк" {
		t.Fatalf("без токена: %v", m)
	}
	if _, m := e.do(t, http.MethodPost, sellPath, e.receipt("a"), "чужой"); errCode(m) != "11" {
		t.Fatalf("чужой токен: %v", m)
	}
	if _, m := e.do(t, http.MethodPost, "/possystem/v5/other/sell", e.receipt("a"), e.token); errCode(m) != "34" ||
		errText(m) != "Группа ККТ не найдена" {
		t.Fatalf("группа: %v", m)
	}
	cases := []struct {
		name, code, text string
		edit             func(r map[string]any)
	}{
		{"нет e-mail и телефона", "30", "Неверный формат запроса: client.email", func(r map[string]any) {
			r["receipt"].(map[string]any)["client"] = map[string]any{"name": "ООО «Ромашка»"}
		}},
		{"СНО вне списка", "30", "Неверный формат запроса: company.sno", func(r map[string]any) {
			r["receipt"].(map[string]any)["company"].(map[string]any)["sno"] = "npd"
		}},
		{"нет external_id", "30", "Неверный формат запроса: external_id", func(r map[string]any) { delete(r, "external_id") }},
		{"формат времени", "30", "Неверный формат запроса: timestamp", func(r map[string]any) { r["timestamp"] = "2026-10-16" }},
		{"три знака в цене", "30", "Неверный формат запроса: items[0].price", func(r map[string]any) {
			r["receipt"].(map[string]any)["items"].([]any)[0].(map[string]any)["price"] = json.Number("45000.001")
		}},
		{"sum ≠ price × quantity", "32", "Ошибка проверки сумм", func(r map[string]any) {
			r["receipt"].(map[string]any)["items"].([]any)[0].(map[string]any)["sum"] = json.Number("44000.00")
		}},
		{"total ≠ Σ payments", "32", "Ошибка проверки сумм", func(r map[string]any) {
			r["receipt"].(map[string]any)["payments"] = []any{map[string]any{"type": 1, "sum": json.Number("40000.00")}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := e.receipt("x-" + c.name)
			c.edit(r)
			code, m := e.do(t, http.MethodPost, sellPath, r, e.token)
			if code != http.StatusBadRequest || m["status"] != "fail" || m["uuid"] != nil || errCode(m) != c.code || errText(m) != c.text {
				t.Fatalf("%d %v", code, m)
			}
		})
	}
	var docs []*Doc
	e.srv.store.Read(func(st *State) { docs = st.Docs })
	if len(docs) != 0 {
		t.Fatalf("документы с ошибками сохранены: %d", len(docs))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("условие не выполнилось за 5 с")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Поток чека: wait → done через PROCESS_DELAY_MS, реквизиты в отчёте, колбэк с телом отчёта, HTML чека с QR-кодом.
func TestSellDoneReportCallbackAndReceipt(t *testing.T) {
	e := newEnv(t, 30*time.Millisecond)
	code, m := e.do(t, http.MethodPost, sellPath, e.receipt("r-1"), e.token)
	uuid, _ := m["uuid"].(string)
	if code != http.StatusOK || m["status"] != "wait" || uuid == "" || m["error"] != nil {
		t.Fatalf("ответ sell: %d %v", code, m)
	}
	_, rep := e.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+uuid, nil, e.token)
	if rep["status"] != "wait" || rep["payload"] != nil {
		t.Fatalf("до задержки: %v", rep)
	}
	waitFor(t, func() bool { return e.shop.count() == 1 })
	_, rep = e.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+uuid, nil, e.token)
	p, _ := rep["payload"].(map[string]any)
	if rep["status"] != "done" || rep["external_id"] != "r-1" || rep["group_code"] != "landlord-demo" || p == nil {
		t.Fatalf("отчёт: %v", rep)
	}
	if p["total"] != json.Number("45000.00") || p["fn_number"] != fnNumber || p["fiscal_document_number"] != json.Number("1000") ||
		p["fiscal_receipt_number"] != json.Number("1") || p["shift_number"] != json.Number("1") ||
		p["ofd_receipt_url"] != "http://atol.test/receipt/"+uuid {
		t.Fatalf("реквизиты: %v", p)
	}
	if fp, _ := p["fiscal_document_attribute"].(json.Number); len(fp) != 10 {
		t.Fatalf("ФП не из 10 цифр: %v", fp)
	}
	e.shop.mu.Lock()
	cb := e.shop.got[0]
	e.shop.mu.Unlock()
	if cb["uuid"] != uuid || cb["status"] != "done" {
		t.Fatalf("колбэк: %v", cb)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/receipt/"+uuid, nil))
	body := rec.Body.String()
	for _, want := range []string{"КАССОВЫЙ ЧЕК", "ПРИХОД", "ИНН 507500363778", "Арендная плата за октябрь 2026, договор № 2026-011",
		"45 000,00", "УСН доход", "Безналичными", "data:image/png;base64,", "&amp;s=45000.00&amp;fn=9999078900012345&amp;i=1000&amp;fp="} {
		if !strings.Contains(body, want) {
			t.Fatalf("на странице чека нет %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "&amp;n=1") {
		t.Fatal("признак расчёта в QR-коде")
	}
}

// Повтор external_id: то же тело — тот же uuid, другое — код 33.
func TestSellIdempotent(t *testing.T) {
	e := newEnv(t, time.Hour)
	_, m1 := e.do(t, http.MethodPost, sellPath, e.receipt("dup"), e.token)
	_, m2 := e.do(t, http.MethodPost, sellPath, e.receipt("dup"), e.token)
	if m1["uuid"] == nil || m1["uuid"] != m2["uuid"] {
		t.Fatalf("повтор с тем же телом: %v %v", m1, m2)
	}
	other := e.receipt("dup")
	other["timestamp"] = "16.10.2026 19:31:00"
	if code, m := e.do(t, http.MethodPost, sellPath, other, e.token); code != http.StatusConflict || errCode(m) != "33" {
		t.Fatalf("повтор с другим телом: %d %v", code, m)
	}
}

// Переключатель ошибки: документ fail с кодом 50; повтор external_id после ошибки — новый документ.
func TestFailToggleAndResendAfterFail(t *testing.T) {
	e := newEnv(t, 0)
	form := url.Values{"failNext": {"1"}, "delayMs": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(httptest.NewRecorder(), req)
	_, m := e.do(t, http.MethodPost, sellPath, e.receipt("f-1"), e.token)
	uuid := m["uuid"].(string)
	waitFor(t, func() bool { return e.shop.count() == 1 })
	_, rep := e.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+uuid, nil, e.token)
	if rep["status"] != "fail" || errCode(rep) != "50" || errText(rep) != "Эмулятор: ошибка фискального накопителя" || rep["payload"] != nil {
		t.Fatalf("отчёт об ошибке: %v", rep)
	}
	_, m2 := e.do(t, http.MethodPost, sellPath, e.receipt("f-1"), e.token)
	if m2["uuid"] == uuid || m2["status"] != "wait" {
		t.Fatalf("повтор после ошибки: %v", m2)
	}
	waitFor(t, func() bool { return e.shop.count() == 2 })
	_, rep = e.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+m2["uuid"].(string), nil, e.token)
	if rep["status"] != "done" {
		t.Fatalf("переключатель ошибки действует только на один документ: %v", rep)
	}
}

// Колбэк: до 3 попыток; «Не отправлять колбэки» — без колбэка.
func TestCallbackRetriesAndToggle(t *testing.T) {
	e := newEnv(t, 0)
	e.shop.fail = 5
	_, m := e.do(t, http.MethodPost, sellPath, e.receipt("c-1"), e.token)
	e.srv.Wait()
	if e.shop.count() != 3 {
		t.Fatalf("попыток колбэка %d, ожидалось 3", e.shop.count())
	}
	d, _ := e.srv.doc(m["uuid"].(string))
	if len(d.Callbacks) != 3 {
		t.Fatalf("журнал попыток: %v", d.Callbacks)
	}
	e.shop.fail = 0
	form := url.Values{"noCallbacks": {"1"}, "delayMs": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(httptest.NewRecorder(), req)
	_, m = e.do(t, http.MethodPost, sellPath, e.receipt("c-2"), e.token)
	e.srv.Wait()
	_, rep := e.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+m["uuid"].(string), nil, e.token)
	if e.shop.count() != 3 || rep["status"] != "done" {
		t.Fatalf("колбэк отправлен при выключенных колбэках: %d %v", e.shop.count(), rep)
	}
}

// Возврат прихода: страница «ВОЗВРАТ ПРИХОДА», n=2 в QR-коде; повтор того же external_id как sell — код 33.
func TestSellRefund(t *testing.T) {
	e := newEnv(t, 0)
	r := e.receipt("rf-1")
	items := r["receipt"].(map[string]any)["items"].([]any)
	items[0].(map[string]any)["name"] = "Возврат переплаты по договору № 2026-011"
	items[0].(map[string]any)["payment_method"] = "advance"
	_, m := e.do(t, http.MethodPost, "/possystem/v5/landlord-demo/sell_refund", r, e.token)
	e.srv.Wait()
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/receipt/"+m["uuid"].(string), nil))
	if body := rec.Body.String(); !strings.Contains(body, "ВОЗВРАТ ПРИХОДА") || !strings.Contains(body, "&amp;n=2") ||
		!strings.Contains(body, "Аванс") {
		t.Fatalf("чек возврата:\n%s", body)
	}
	if code, m := e.do(t, http.MethodPost, sellPath, r, e.token); code != http.StatusConflict || errCode(m) != "33" {
		t.Fatalf("тот же external_id другой операцией: %v", m)
	}
}

// Нумерация документов и смены продолжается после перезапуска; токены — нет.
func TestStatePersists(t *testing.T) {
	e := newEnv(t, 0)
	e.do(t, http.MethodPost, sellPath, e.receipt("p-1"), e.token)
	e.srv.Wait()
	e2 := newEnvFile(t, 0, e.file)
	if _, m := e2.do(t, http.MethodPost, sellPath, e2.receipt("p-2"), e.token); errCode(m) != "11" {
		t.Fatalf("старый токен после перезапуска: %v", m)
	}
	_, m := e2.do(t, http.MethodPost, sellPath, e2.receipt("p-2"), e2.token)
	e2.srv.Wait()
	_, rep := e2.do(t, http.MethodGet, "/possystem/v5/landlord-demo/report/"+m["uuid"].(string), nil, e2.token)
	p := rep["payload"].(map[string]any)
	if p["fiscal_document_number"] != json.Number("1001") || p["fiscal_receipt_number"] != json.Number("2") {
		t.Fatalf("нумерация после перезапуска: %v", p)
	}
	rec := httptest.NewRecorder()
	e2.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/receipts", nil))
	var docs []Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &docs); err != nil || len(docs) != 2 {
		t.Fatalf("/api/receipts: %v %s", err, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	e2.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "p-2") || !strings.Contains(rec.Body.String(), banner) {
		t.Fatal("экран документов")
	}
}

func TestShiftNumber(t *testing.T) {
	st := State{FirstDay: "2026-10-16"}
	at := time.Date(2026, 10, 18, 0, 30, 0, 0, moscow)
	if got := st.shiftNumber(at); got != 3 {
		t.Fatalf("смена %d", got)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t, 0)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("неожиданный ответ: %d %s", rec.Code, rec.Body.String())
	}
}
