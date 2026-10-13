//go:build integration

package testsupport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"

	"landlord/backend/internal/platform/tbank"
)

// Терминал фейкового банка в тестах.
const (
	BankTerminal = "LandlordDemo"
	BankPassword = "demo-terminal-password"
)

// bankSeq — PaymentId, сквозной для всех окружений пакета (база данных общая).
var bankSeq atomic.Int64

func init() { bankSeq.Store(7000000000) }

// FakeBank — подделка API Т-Банка (Init, GetState) с проверкой подписи; статус платежа задаётся тестом.
type FakeBank struct {
	mu       sync.Mutex
	last     int64
	orders   map[string]string // PaymentId → OrderId
	amounts  map[string]int64
	statuses map[string]string
	// InitError — код ошибки ответа Init (пусто — успех).
	InitError string
	Server    *httptest.Server
}

func newFakeBank() *FakeBank {
	b := &FakeBank{orders: map[string]string{}, amounts: map[string]int64{}, statuses: map[string]string{}}
	b.Server = httptest.NewServer(http.HandlerFunc(b.serve))
	return b
}

func (b *FakeBank) serve(w http.ResponseWriter, r *http.Request) {
	var m map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	_ = dec.Decode(&m)
	w.Header().Set("Content-Type", "application/json")
	if m["Token"] != tbank.Sign(m, BankPassword) {
		_ = json.NewEncoder(w).Encode(map[string]any{"Success": false, "ErrorCode": "204", "Message": "Неверный токен"})
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch r.URL.Path {
	case "/v2/Init":
		if b.InitError != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"Success": false, "ErrorCode": b.InitError, "Message": "Ошибка банка"})
			return
		}
		b.last = bankSeq.Add(1)
		id := strconv.FormatInt(b.last, 10)
		amount, _ := strconv.ParseInt(string(m["Amount"].(json.Number)), 10, 64)
		b.orders[id], b.amounts[id], b.statuses[id] = m["OrderId"].(string), amount, tbank.StatusNew
		_ = json.NewEncoder(w).Encode(map[string]any{"Success": true, "ErrorCode": "0", "Status": "NEW", "PaymentId": id,
			"OrderId": m["OrderId"], "Amount": amount, "PaymentURL": b.Server.URL + "/pay/" + id})
	case "/v2/GetState":
		id, _ := m["PaymentId"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"Success": true, "ErrorCode": "0", "Status": b.statuses[id], "PaymentId": id,
			"OrderId": b.orders[id], "Amount": b.amounts[id]})
	default:
		http.NotFound(w, r)
	}
}

// Last — PaymentId последнего платежа.
func (b *FakeBank) Last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strconv.FormatInt(b.last, 10)
}

// SetStatus — статус платежа для GetState.
func (b *FakeBank) SetStatus(id, status string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.statuses[id] = status
}

// Notification — подписанное уведомление банка о платеже.
func (b *FakeBank) Notification(paymentID, status string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := map[string]any{
		"TerminalKey": BankTerminal, "OrderId": b.orders[paymentID], "Success": status != tbank.StatusRejected, "Status": status,
		"PaymentId": paymentID, "ErrorCode": "0", "Amount": b.amounts[paymentID], "CardId": 101, "Pan": "220000******0004",
		"ExpDate": "1230",
	}
	n["Token"] = tbank.Sign(n, BankPassword)
	return n
}
