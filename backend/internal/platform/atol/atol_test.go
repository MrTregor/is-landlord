package atol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"landlord/backend/internal/platform/clock"
)

// fake — АТОЛ с одним действующим токеном; expire — следующий запрос с токеном получит код 11.
type fake struct {
	mu     sync.Mutex
	tokens int
	valid  string
	expire bool
	bodies []string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/possystem/v5/getToken":
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["login"] != "demo" || req["pass"] != "demo" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":12,"text":"Неверный логин или пароль","type":"system"},"token":null}`))
			return
		}
		f.tokens++
		f.valid = "t" + string(rune('0'+f.tokens))
		_, _ = w.Write([]byte(`{"error":null,"token":"` + f.valid + `","timestamp":"16.10.2026 19:30:00"}`))
	case r.Header.Get("Token") != f.valid || f.expire:
		f.expire = false
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"uuid":null,"status":"fail","error":{"code":11,"text":"Срок действия токена истёк","type":"system"}}`))
	case r.Method == http.MethodPost && r.URL.Path == "/possystem/v5/landlord-demo/sell":
		b, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, string(b))
		_, _ = w.Write([]byte(`{"uuid":"u-1","status":"wait","error":null,"timestamp":"16.10.2026 19:30:00"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/possystem/v5/landlord-demo/sell_refund":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"uuid":null,"status":"fail","error":{"code":32,"text":"Ошибка проверки сумм","type":"agent"}}`))
	case r.URL.Path == "/possystem/v5/landlord-demo/report/u-1":
		_, _ = w.Write([]byte(`{"uuid":"u-1","status":"done","error":null,"external_id":"e-1","payload":{"total":45000.00,
			"fn_number":"9999078900012345","shift_number":23,"receipt_datetime":"16.10.2026 19:30:05","fiscal_receipt_number":7,
			"fiscal_document_number":1042,"fiscal_document_attribute":3326595612,"ofd_receipt_url":"http://localhost:8082/receipt/u-1"}}`))
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func newClient(t *testing.T) (*Client, *fake, *clock.Fixed) {
	t.Helper()
	f := &fake{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	clk := clock.NewFixed(time.Date(2026, 10, 16, 16, 30, 0, 0, time.UTC), time.UTC)
	return NewClient(srv.URL+"/possystem/v5", "demo", "demo", "landlord-demo", clk), f, clk
}

// Токен кэшируется на 23 часа; код 11 → новый токен и один повтор (FR-06.2).
func TestTokenCacheAndRefresh_FR_06_2(t *testing.T) {
	c, f, clk := newClient(t)
	ctx := context.Background()
	body := []byte(`{"external_id":"e-1","receipt":{"total":45000.00}}`)
	r, err := c.Send(ctx, OperationSell, body)
	if err != nil || r.UUID == nil || *r.UUID != "u-1" || r.Status != StatusWait || r.Error != nil {
		t.Fatalf("Send: %+v %v", r, err)
	}
	if string(r.Raw) == "" || f.bodies[0] != string(body) {
		t.Fatalf("тело запроса или ответа: %q %q", f.bodies, r.Raw)
	}
	if _, err := c.Send(ctx, OperationSell, body); err != nil || f.tokens != 1 {
		t.Fatalf("токен не взят из кэша: %d %v", f.tokens, err)
	}
	clk.Advance(23*time.Hour + time.Second)
	if _, err := c.Send(ctx, OperationSell, body); err != nil || f.tokens != 2 {
		t.Fatalf("токен старше 23 ч не обновлён: %d %v", f.tokens, err)
	}
	f.expire = true
	if r, err := c.Send(ctx, OperationSell, body); err != nil || r.Status != StatusWait || f.tokens != 3 {
		t.Fatalf("код 11 → новый токен и повтор: %+v %d %v", r, f.tokens, err)
	}
}

func TestSendErrorAndReport(t *testing.T) {
	c, _, _ := newClient(t)
	ctx := context.Background()
	r, err := c.Send(ctx, OperationSellRefund, []byte(`{}`))
	if err != nil || r.Status != StatusFail || r.Error == nil || r.Error.Code != 32 || r.UUID != nil {
		t.Fatalf("ошибка проверки данных — ответ, не сбой: %+v %v", r, err)
	}
	rep, err := c.Report(ctx, "u-1")
	if err != nil || rep.Status != StatusDone || rep.Payload == nil {
		t.Fatalf("Report: %+v %v", rep, err)
	}
	p := rep.Payload
	if p.Total.String() != "45000.00" || p.FiscalDocumentNumber != 1042 || p.FiscalDocumentAttribute != 3326595612 ||
		p.FiscalReceiptNumber != 7 || p.ShiftNumber != 23 || p.OfdReceiptURL != "http://localhost:8082/receipt/u-1" {
		t.Fatalf("реквизиты: %+v", p)
	}
	at, err := ParseTimestamp(p.ReceiptDatetime, time.FixedZone("MSK", 3*3600))
	if err != nil || at.UTC() != time.Date(2026, 10, 16, 16, 30, 5, 0, time.UTC) {
		t.Fatalf("время чека: %v %v", at, err)
	}
	if _, err := c.Report(ctx, "нет"); err == nil {
		t.Fatal("HTTP 500 — сбой, задача повторится")
	}
}

func TestBadLogin(t *testing.T) {
	c, _, _ := newClient(t)
	c.password = "нет"
	if _, err := c.Send(context.Background(), OperationSell, []byte(`{}`)); err == nil {
		t.Fatal("ожидалась ошибка получения токена")
	}
}
