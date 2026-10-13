package tbank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const password = "demo-terminal-password"

// Контрольный пример 1 (08 разд. 1.1): запрос Init (NFR-Sec-01).
func TestSignInitExample1_NFR_Sec_01(t *testing.T) {
	got := Sign(map[string]any{
		"Amount": int64(1503250), "Description": "Оплата по договору № 2026-007",
		"OrderId": "0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90", "TerminalKey": "LandlordDemo",
		"DATA": map[string]any{"Email": "tenant@example.ru"},
	}, password)
	if got != "5a78953bd4c2172bb91cec61bb8018abaffe72add6fba5df12964ca9dc92f709" {
		t.Fatalf("Token %s", got)
	}
}

// Контрольный пример 2 (08 разд. 1.1): уведомление; проверка подписи принятого JSON (NFR-Sec-01).
func TestSignNotificationExample2_NFR_Sec_01(t *testing.T) {
	raw := []byte(`{"TerminalKey":"LandlordDemo","OrderId":"0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90","Success":true,
		"Status":"CONFIRMED","PaymentId":7000000001,"ErrorCode":"0","Amount":1503250,"CardId":101,"Pan":"220000******0004",
		"ExpDate":"1230","Token":"85a5909a3ef2de6ae0c3f79dfb7d247af3979f5508f9379b860de0ae11221f78"}`)
	n, err := ParseNotification(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient("http://bank", "LandlordDemo", password)
	if !c.Verify(n) {
		t.Fatal("подпись контрольного примера не принята")
	}
	if n.PaymentID != "7000000001" || n.Amount != 1503250 || n.Status != StatusConfirmed || !n.Success {
		t.Fatalf("%+v", n)
	}
	n.Fields["Amount"] = json.Number("1503251")
	if c.Verify(n) {
		t.Fatal("изменённое уведомление принято")
	}
	if NewClient("http://bank", "Другой", password).Verify(n) {
		t.Fatal("чужой терминал принят")
	}
	// То же уведомление после стандартного декодера JSON и повторной сериализации (путь strict-обработчика API).
	var plain map[string]any
	if err := json.Unmarshal(raw, &plain); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(plain)
	n2, err := ParseNotification(again)
	if err != nil || !c.Verify(n2) {
		t.Fatal("подпись после повторной сериализации не принята")
	}
}

func TestInitAndGetState(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		_ = dec.Decode(&m)
		if m["Token"] != Sign(m, password) {
			http.Error(w, "неверный токен", http.StatusForbidden)
			return
		}
		if m["PaymentId"] == nil {
			got = m
		}
		_, _ = w.Write([]byte(`{"Success":true,"ErrorCode":"0","Status":"NEW","PaymentId":"7000000005","OrderId":"o","Amount":10000,"PaymentURL":"http://bank/pay/7000000005"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/v2", "LandlordDemo", password)
	r, err := c.Init(context.Background(), InitRequest{Amount: 10000, OrderID: "o", Description: "Оплата", NotificationURL: "n",
		SuccessURL: "s", FailURL: "f", RedirectDueDate: time.Date(2026, 10, 17, 19, 29, 0, 0, time.FixedZone("MSK", 3*3600)),
		ContractNumber: "2026-007"})
	if err != nil || !r.Success || r.PaymentID != "7000000005" || r.PaymentURL != "http://bank/pay/7000000005" {
		t.Fatalf("%+v %v", r, err)
	}
	if got["RedirectDueDate"] != "2026-10-17T19:29:00+03:00" || got["PayType"] != "O" {
		t.Fatalf("запрос %v", got)
	}
	if _, err := c.GetState(context.Background(), "7000000005"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient("http://127.0.0.1:1/v2", "LandlordDemo", password).GetState(context.Background(), "1"); err == nil {
		t.Fatal("ожидалась ошибка сети")
	}
}
