package seed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"

	"github.com/google/uuid"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/files"
	"landlord/backend/internal/platform/tbank"
)

// Терминал поддельного банка загрузки: онлайн-оплаты истории проводятся внутри процесса, эмулятор не нужен.
const (
	bankTerminal = "LandlordSeed"
	bankPassword = "seed-terminal-password"
)

type fakeBank struct {
	mu      sync.Mutex
	seq     int64
	orders  map[string]string
	amounts map[string]int64
	srv     *httptest.Server
}

func newFakeBank() *fakeBank {
	b := &fakeBank{seq: 9000000000, orders: map[string]string{}, amounts: map[string]int64{}}
	b.srv = httptest.NewServer(http.HandlerFunc(b.serve))
	return b
}

func (b *fakeBank) serve(w http.ResponseWriter, r *http.Request) {
	var m map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	_ = dec.Decode(&m)
	w.Header().Set("Content-Type", "application/json")
	b.mu.Lock()
	defer b.mu.Unlock()
	switch r.URL.Path {
	case "/v2/Init":
		b.seq++
		id := strconv.FormatInt(b.seq, 10)
		amount, _ := strconv.ParseInt(fmt.Sprint(m["Amount"]), 10, 64)
		b.orders[id], b.amounts[id] = fmt.Sprint(m["OrderId"]), amount
		_ = json.NewEncoder(w).Encode(map[string]any{"Success": true, "ErrorCode": "0", "Status": tbank.StatusNew, "PaymentId": id,
			"OrderId": m["OrderId"], "Amount": amount, "PaymentURL": b.srv.URL + "/pay/" + id})
	case "/v2/GetState":
		id := fmt.Sprint(m["PaymentId"])
		_ = json.NewEncoder(w).Encode(map[string]any{"Success": true, "ErrorCode": "0", "Status": tbank.StatusConfirmed,
			"PaymentId": id, "OrderId": b.orders[id], "Amount": b.amounts[id]})
	default:
		http.NotFound(w, r)
	}
}

// confirmed — подписанное уведомление CONFIRMED о последнем платеже.
func (b *fakeBank) confirmed() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := strconv.FormatInt(b.seq, 10)
	n := map[string]any{
		"TerminalKey": bankTerminal, "OrderId": b.orders[id], "Success": true, "Status": tbank.StatusConfirmed,
		"PaymentId": id, "ErrorCode": "0", "Amount": b.amounts[id], "CardId": 101, "Pan": "220000******0004", "ExpDate": "1230",
	}
	n["Token"] = tbank.Sign(n, bankPassword)
	return n
}

// payOnline — онлайн-оплата из кабинета: старт, уведомление банка и его обработка (как задача tbank.process_notification).
func (w *world) payOnline(tenant *client, contractID, purpose string) error {
	if err := tenant.do(http.MethodPost, "/cabinet/payments", map[string]any{"contractId": contractID, "purpose": purpose,
		"chargeIds": []string{}}, nil); err != nil {
		return err
	}
	if err := w.a.Payments.AcceptNotification(w.ctx, w.bank.confirmed()); err != nil {
		return err
	}
	rows, err := w.a.DB.Pool.Query(w.ctx, `SELECT id FROM webhook_events WHERE processed_at IS NULL ORDER BY received_at`)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := w.a.Payments.ProcessNotification(w.ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func fileStore(cfg *config.Config) *files.Store { return files.NewStore(cfg.FilesDir) }
