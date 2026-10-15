//go:build integration

package testsupport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
)

// Учётные данные фейковой кассы в тестах.
const (
	AtolLogin = "demo"
	AtolPass  = "demo"
	AtolGroup = "landlord-demo"
)

// atolSeq — номера документов, сквозные для всех окружений пакета (external_uuid уникален в общей базе).
var atolSeq atomic.Int64

// FakeAtol — подделка API АТОЛ Онлайн v5 (getToken, sell, sell_refund, report). Документ остаётся в wait, пока
// тест не вызовет Done или Fail; SendError — ответ на следующую отправку с ошибкой (код и текст).
type FakeAtol struct {
	mu        sync.Mutex
	statuses  map[string]string // uuid → wait/done/fail
	external  map[string]string // uuid → external_id
	errors    map[string][2]string
	totals    map[string]string
	Bodies    []map[string]any // тела отправленных чеков
	Paths     []string
	SendError *[2]string // код и текст ошибки следующей отправки
	Down      bool       // сбой связи: HTTP 503
	Server    *httptest.Server
}

func newFakeAtol() *FakeAtol {
	f := &FakeAtol{statuses: map[string]string{}, external: map[string]string{}, errors: map[string][2]string{},
		totals: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *FakeAtol) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if f.Down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	base := "/possystem/v5/" + AtolGroup + "/"
	switch {
	case r.URL.Path == "/possystem/v5/getToken":
		_, _ = w.Write([]byte(`{"error":null,"token":"fake-token","timestamp":"16.10.2026 19:30:00"}`))
	case r.Header.Get("Token") != "fake-token":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"uuid":null,"status":"fail","error":{"code":11,"text":"Срок действия токена истёк","type":"system"}}`))
	case r.Method == http.MethodPost && (r.URL.Path == base+"sell" || r.URL.Path == base+"sell_refund"):
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		_ = dec.Decode(&m)
		f.Bodies = append(f.Bodies, m)
		f.Paths = append(f.Paths, strings.TrimPrefix(r.URL.Path, base))
		if f.SendError != nil {
			e := *f.SendError
			f.SendError = nil
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"uuid":null,"status":"fail","error":{"code":%s,"text":%q,"type":"agent"}}`, e[0], e[1])
			return
		}
		id := fmt.Sprintf("atol-%d", atolSeq.Add(1))
		f.statuses[id], f.external[id] = "wait", fmt.Sprint(m["external_id"])
		if rc, ok := m["receipt"].(map[string]any); ok {
			f.totals[id] = fmt.Sprint(rc["total"])
		}
		_, _ = fmt.Fprintf(w, `{"uuid":%q,"status":"wait","error":null,"timestamp":"16.10.2026 19:30:00"}`, id)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, base+"report/"):
		id := strings.TrimPrefix(r.URL.Path, base+"report/")
		st, ok := f.statuses[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"uuid":null,"status":"fail","error":{"code":30,"text":"Неверный формат запроса: uuid","type":"agent"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(f.report(id, st))
	default:
		http.NotFound(w, r)
	}
}

func (f *FakeAtol) report(id, st string) map[string]any {
	out := map[string]any{"uuid": id, "status": st, "error": nil, "external_id": f.external[id], "group_code": AtolGroup,
		"timestamp": "16.10.2026 19:30:05", "payload": nil}
	switch st {
	case "done":
		out["payload"] = map[string]any{
			"total": json.Number(f.totals[id]), "fns_site": "www.nalog.gov.ru", "fn_number": "9999078900012345", "shift_number": 23,
			"receipt_datetime": "16.10.2026 19:30:05", "fiscal_receipt_number": 7, "fiscal_document_number": 1042,
			"ecr_registration_number": "0000000001012345", "fiscal_document_attribute": 3326595612, "ofd_inn": "7704211201",
			"ofd_receipt_url": "http://localhost:8082/receipt/" + id,
		}
	case "fail":
		e := f.errors[id]
		out["error"] = map[string]any{"code": json.Number(e[0]), "text": e[1], "type": "driver"}
	}
	return out
}

// Last — uuid последнего документа.
func (f *FakeAtol) Last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf("atol-%d", atolSeq.Load())
}

// LastBody — тело последнего отправленного чека.
func (f *FakeAtol) LastBody() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Bodies) == 0 {
		return nil
	}
	return f.Bodies[len(f.Bodies)-1]
}

// Sent — число отправленных чеков.
func (f *FakeAtol) Sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Bodies)
}

// Done — документ сформирован.
func (f *FakeAtol) Done(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[id] = "done"
}

// Fail — документ завершился ошибкой.
func (f *FakeAtol) Fail(id, code, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[id] = "fail"
	f.errors[id] = [2]string{code, text}
}

// Report — тело отчёта (для колбэка).
func (f *FakeAtol) Report(id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.report(id, f.statuses[id])
}
