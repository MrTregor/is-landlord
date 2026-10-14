package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const pass = "demo-terminal-password"

// Контрольный пример 1 (08 разд. 1.1): запрос Init.
func TestSignInit_Example1(t *testing.T) {
	got := sign(map[string]any{
		"Amount": json.Number("1503250"), "Description": "Оплата по договору № 2026-007",
		"OrderId": "0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90", "TerminalKey": "LandlordDemo",
		"DATA": map[string]any{"Email": "x@y.ru"}, "Token": "игнорируется",
	}, pass)
	if got != "5a78953bd4c2172bb91cec61bb8018abaffe72add6fba5df12964ca9dc92f709" {
		t.Fatalf("Token %s", got)
	}
}

// Контрольный пример 2 (08 разд. 1.1): уведомление.
func TestSignNotification_Example2(t *testing.T) {
	got := sign(map[string]any{
		"Amount": 1503250, "CardId": 101, "ErrorCode": "0", "ExpDate": "1230", "OrderId": "0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90",
		"Pan": "220000******0004", "PaymentId": "7000000001", "Status": "CONFIRMED", "Success": true, "TerminalKey": "LandlordDemo",
	}, pass)
	if got != "85a5909a3ef2de6ae0c3f79dfb7d247af3979f5508f9379b860de0ae11221f78" {
		t.Fatalf("Token %s", got)
	}
}

// shop — магазин: принимает уведомления, отвечает OK (или ошибкой для первых fail запросов).
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
	_, _ = w.Write([]byte("OK"))
}

func (s *shop) statuses() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.got {
		out = append(out, m["Status"].(string))
	}
	return out
}

type env struct {
	srv  *Server
	h    http.Handler
	shop *shop
	url  string
}

func newEnv(t *testing.T, file string) *env {
	t.Helper()
	sh := &shop{}
	ts := httptest.NewServer(sh)
	t.Cleanup(ts.Close)
	store, err := NewStore(file)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{PublicURL: "http://mock", Terminals: map[string]string{"LandlordDemo": pass},
		Retry: []time.Duration{0, 10 * time.Millisecond, 20 * time.Millisecond}, StateFile: file},
		store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &env{srv: srv, h: srv.Handler(), shop: sh, url: ts.URL}
}

func (e *env) call(t *testing.T, path string, body map[string]any) map[string]any {
	t.Helper()
	body["Token"] = sign(body, pass)
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %s", path, rec.Body.String())
	}
	return out
}

func (e *env) init(t *testing.T, order string, amount int64) map[string]any {
	t.Helper()
	return e.call(t, "/v2/Init", map[string]any{
		"TerminalKey": "LandlordDemo", "Amount": amount, "OrderId": order, "Description": "Оплата по договору № 2026-007",
		"NotificationURL": e.url, "SuccessURL": "http://app/cabinet/payments/1/result", "FailURL": "http://app/fail",
		"DATA": map[string]any{"Email": "tenant@example.ru"},
	})
}

func (e *env) action(t *testing.T, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pay/"+id+"/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := newEnv(t, "").get("/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("неожиданный ответ: %d %s", rec.Code, rec.Body.String())
	}
}

// Init → страница оплаты → карта: AUTHORIZED и CONFIRMED по очереди, подписанные; переход на SuccessURL.
func TestCardPaymentFlow(t *testing.T) {
	e := newEnv(t, "")
	r := e.init(t, "order-1", 1503250)
	if r["Success"] != true || r["PaymentId"] != "7000000001" || r["Status"] != "NEW" || r["PaymentURL"] != "http://mock/pay/7000000001" {
		t.Fatalf("Init: %v", r)
	}
	if rec := e.get("/pay/7000000001"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "15 032,50 ₽") ||
		!strings.Contains(rec.Body.String(), banner) {
		t.Fatalf("страница оплаты: %d", rec.Code)
	}
	st := e.call(t, "/v2/GetState", map[string]any{"TerminalKey": "LandlordDemo", "PaymentId": "7000000001"})
	if st["Status"] != StatusFormShowed {
		t.Fatalf("GetState: %v", st)
	}
	rec := e.action(t, "7000000001", url.Values{"action": {"card_success"}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "http://app/cabinet/payments/1/result?") ||
		!strings.Contains(rec.Header().Get("Location"), "Success=true") {
		t.Fatalf("переход: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	e.srv.Wait()
	if got := e.shop.statuses(); len(got) != 2 || got[0] != StatusAuthorized || got[1] != StatusConfirmed {
		t.Fatalf("уведомления %v", got)
	}
	n := e.shop.got[1]
	if n["Pan"] != "220000******0004" || n["Success"] != true || n["Token"] != sign(n, pass) {
		t.Fatalf("уведомление %v", n)
	}
}

// СБП: в уведомлении нет полей карты; «дважды» — каждое уведомление два раза.
func TestSBPTwice(t *testing.T) {
	e := newEnv(t, "")
	r := e.init(t, "order-2", 10000)
	e.action(t, r["PaymentId"].(string), url.Values{"action": {"sbp_success"}, "notifyTwice": {"1"}})
	e.srv.Wait()
	got := e.shop.statuses()
	if strings.Join(got, ",") != "AUTHORIZED,AUTHORIZED,CONFIRMED,CONFIRMED" {
		t.Fatalf("уведомления %v", got)
	}
	if _, ok := e.shop.got[3]["Pan"]; ok {
		t.Fatal("для СБП поля карты не передаются")
	}
}

// Отказ: REJECTED, ErrorCode 1051, Success false, переход на FailURL; повторные попытки доставки по расписанию.
func TestRejectAndRetry(t *testing.T) {
	e := newEnv(t, "")
	e.shop.fail = 2
	r := e.init(t, "order-3", 10000)
	rec := e.action(t, r["PaymentId"].(string), url.Values{"action": {"reject"}})
	if !strings.HasPrefix(rec.Header().Get("Location"), "http://app/fail?") {
		t.Fatalf("переход %s", rec.Header().Get("Location"))
	}
	e.srv.Wait()
	if len(e.shop.got) != 3 {
		t.Fatalf("попыток %d, ожидалось 3", len(e.shop.got))
	}
	n := e.shop.got[2]
	if n["Status"] != StatusRejected || n["ErrorCode"] != "1051" || n["Success"] != false {
		t.Fatalf("уведомление %v", n)
	}
	var list []Payment
	_ = json.Unmarshal(e.get("/api/payments").Body.Bytes(), &list)
	if len(list) != 1 || len(list[0].Notices) != 1 || !list[0].Notices[0].Delivered || len(list[0].Notices[0].Attempts) != 3 {
		t.Fatalf("журнал %+v", list)
	}
}

// Ошибки API (08 разд. 2.2).
func TestInitErrors(t *testing.T) {
	e := newEnv(t, "")
	e.init(t, "dup", 10000)
	cases := []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"TerminalKey": "Нет", "Amount": 10000, "OrderId": "x"}, "202"},
		{map[string]any{"TerminalKey": "LandlordDemo", "Amount": 50, "OrderId": "x", "NotificationURL": "u", "SuccessURL": "u"}, "100"},
		{map[string]any{"TerminalKey": "LandlordDemo", "Amount": 10000, "OrderId": "x", "SuccessURL": "u"}, "100"},
		{map[string]any{"TerminalKey": "LandlordDemo", "Amount": 10000, "OrderId": "dup", "NotificationURL": "u", "SuccessURL": "u"}, "309"},
	}
	for _, c := range cases {
		if r := e.call(t, "/v2/Init", c.body); r["Success"] != false || r["ErrorCode"] != c.code {
			t.Errorf("%v → %v", c.body, r)
		}
	}
	b, _ := json.Marshal(map[string]any{"TerminalKey": "LandlordDemo", "Amount": 10000, "OrderId": "y", "Token": "плохой"})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v2/Init", bytes.NewReader(b)))
	if !strings.Contains(rec.Body.String(), `"ErrorCode":"204"`) {
		t.Fatalf("неверный токен: %s", rec.Body.String())
	}
}

// Истёкший срок ссылки: страница «Срок оплаты истёк», статус DEADLINE_EXPIRED и уведомление.
func TestExpired(t *testing.T) {
	e := newEnv(t, "")
	r := e.init(t, "order-4", 10000)
	e.srv.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	rec := e.get("/pay/" + r["PaymentId"].(string))
	if !strings.Contains(rec.Body.String(), "Срок оплаты истёк") {
		t.Fatal("страница истёкшего срока")
	}
	e.srv.Wait()
	if got := e.shop.statuses(); len(got) != 1 || got[0] != StatusDeadlineExpired {
		t.Fatalf("уведомления %v", got)
	}
}

// Состояние сохраняется в файл и читается при старте; «Очистить всё» удаляет платежи, нумерация продолжается.
func TestStatePersistedAndReset(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state", "tbank.json")
	e := newEnv(t, file)
	e.init(t, "order-5", 10000)
	e.srv.Wait()
	e2 := newEnv(t, file) // перезапуск контейнера: счётчик читается из файла на томе
	if r := e2.init(t, "order-6", 10000); r["PaymentId"] != "7000000002" {
		t.Fatalf("после перезапуска %v", r)
	}
	rec := httptest.NewRecorder()
	e2.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/reset", nil))
	if r := e2.init(t, "order-5", 10000); r["PaymentId"] != "7000000003" {
		t.Fatalf("после сброса %v", r)
	}
	if !strings.Contains(e2.get("/").Body.String(), "order-5") {
		t.Fatal("экран списка")
	}
}

// Служебные действия: принудительный статус и повтор последнего уведомления.
func TestAdminActions(t *testing.T) {
	e := newEnv(t, "")
	r := e.init(t, "order-7", 10000)
	id := r["PaymentId"].(string)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/"+id+"/status", strings.NewReader("status=CONFIRMED"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(rec, req)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/"+id+"/resend", nil))
	e.srv.Wait()
	if got := e.shop.statuses(); strings.Join(got, ",") != "CONFIRMED,CONFIRMED" {
		t.Fatalf("уведомления %v", got)
	}
}

// Файл состояния недоступен для записи — Save возвращает ошибку (эмулятор не запускается, № 55).
func TestSaveFailsOnUnwritableState(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "файл")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(blocker, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if store.Save() == nil {
		t.Fatal("ожидалась ошибка записи")
	}
}
