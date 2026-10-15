package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config — настройки эмулятора (08 разд. 4).
type Config struct {
	PublicURL        string
	Login, Password  string
	GroupCode        string
	ProcessDelay     time.Duration // PROCESS_DELAY_MS
	StateFile        string
	CallbackAttempts int           // до 3 попыток
	CallbackInterval time.Duration // с интервалом 10 с
}

// tokenTTL — срок действия токена (08 разд. 4).
const tokenTTL = 24 * time.Hour

// Постоянные реквизиты кассы эмулятора (пример отчёта, 08 разд. 3.3).
const (
	fnNumber   = "9999078900012345"
	ecrNumber  = "0000000001012345"
	ofdINN     = "7704211201"
	fnsSite    = "www.nalog.gov.ru"
	daemonCode = "mock"
	deviceCode = "MOCK-01"
)

// Server — эмулятор АТОЛ Онлайн.
type Server struct {
	cfg    Config
	store  *Store
	client *http.Client
	now    func() time.Time
	log    *slog.Logger
	wg     sync.WaitGroup // фоновые обработки и колбэки (ожидание в тестах)
	stop   chan struct{}  // закрывается Close: ожидающие таймеры прекращаются
	once   sync.Once

	mu     sync.Mutex
	tokens map[string]time.Time // токен → срок действия (в памяти: после перезапуска токен получают заново)
}

// NewServer создаёт эмулятор.
func NewServer(cfg Config, store *Store, log *slog.Logger) *Server {
	if cfg.CallbackAttempts <= 0 {
		cfg.CallbackAttempts = 3
	}
	return &Server{cfg: cfg, store: store, client: &http.Client{Timeout: 5 * time.Second}, now: time.Now, log: log,
		tokens: map[string]time.Time{}, stop: make(chan struct{})}
}

// Close останавливает ожидающие обработки и повторы колбэков (тесты).
func (s *Server) Close() { s.once.Do(func() { close(s.stop) }) }

// sleep — пауза, прерываемая Close; false — эмулятор остановлен.
func (s *Server) sleep(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-s.stop:
		return false
	case <-time.After(d):
		return true
	}
}

// Handler — маршруты эмулятора (08 разд. 4).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /possystem/v5/getToken", s.handleToken)
	mux.HandleFunc("POST /possystem/v5/{group}/sell", s.handleReceipt("sell"))
	mux.HandleFunc("POST /possystem/v5/{group}/sell_refund", s.handleReceipt("sell_refund"))
	mux.HandleFunc("GET /possystem/v5/{group}/report/{uuid}", s.handleReport)
	mux.HandleFunc("GET /receipt/{uuid}", s.handleReceiptPage)
	mux.HandleFunc("GET /{$}", s.handleList)
	mux.HandleFunc("POST /admin/settings", s.handleSettings)
	mux.HandleFunc("GET /api/receipts", s.handleAPIReceipts)
	return mux
}

// ---------------------------------------------------------------- Ответы

func (s *Server) timestamp() string { return s.now().In(moscow).Format(timestampLayout) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail — ответ с ошибкой на запрос документа: { uuid: null, status: "fail", error, timestamp }.
func (s *Server) fail(w http.ResponseWriter, status int, e *APIError) {
	writeJSON(w, status, map[string]any{"uuid": nil, "status": StatusFail, "error": e, "timestamp": s.timestamp()})
}

// ---------------------------------------------------------------- Токен

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login string `json:"login"`
		Pass  string `json:"pass"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": formatError("тело"), "token": nil, "timestamp": s.timestamp()})
		return
	}
	if req.Login != s.cfg.Login || req.Pass != s.cfg.Password {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": &APIError{Code: codeBadLogin, Text: "Неверный логин или пароль", Type: "system"}, "token": nil, "timestamp": s.timestamp(),
		})
		return
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.tokens[token] = s.now().Add(tokenTTL)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"error": nil, "token": token, "timestamp": s.timestamp()})
}

// authorized — токен из заголовка Token (или параметра token) действует.
func (s *Server) authorized(r *http.Request) bool {
	token := r.Header.Get("Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[token]
	return ok && s.now().Before(exp)
}

// expireTokens — все токены считаются истёкшими (тесты повторного получения токена).
func (s *Server) expireTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = map[string]time.Time{}
}

// guard — токен и группа касс; при ошибке ответ уже записан.
func (s *Server) guard(w http.ResponseWriter, r *http.Request) bool {
	if !s.authorized(r) {
		s.fail(w, http.StatusUnauthorized, &APIError{Code: codeTokenExpired, Text: "Срок действия токена истёк", Type: "system"})
		return false
	}
	if r.PathValue("group") != s.cfg.GroupCode {
		s.fail(w, http.StatusBadRequest, &APIError{Code: codeNoGroup, Text: "Группа ККТ не найдена", Type: "system"})
		return false
	}
	return true
}

// ---------------------------------------------------------------- Чек прихода и возврата прихода

func (s *Server) delay(st *State) time.Duration {
	if st.DelayMs != nil {
		return time.Duration(*st.DelayMs) * time.Millisecond
	}
	return s.cfg.ProcessDelay
}

func (s *Server) handleReceipt(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.guard(w, r) {
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			s.fail(w, http.StatusBadRequest, formatError("тело"))
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			s.fail(w, http.StatusBadRequest, formatError("тело"))
			return
		}
		info, apiErr := validateRequest(m)
		if apiErr != nil {
			s.fail(w, http.StatusBadRequest, apiErr)
			return
		}
		canon := canonical(m)
		group := r.PathValue("group")
		var doc Doc
		var conflict, created bool
		err = s.store.With(func(st *State) bool {
			// Повтор того же external_id: то же тело — тот же документ; другое тело — ошибка 33.
			// Документ с ошибкой можно отправить заново (docs/open-questions.md, M8).
			if prev := st.LastByExternal(group, info.ExternalID); prev != nil && prev.Status != StatusFail {
				if prev.Canonical != canon || prev.Operation != operation {
					conflict = true
					return false
				}
				doc = copyDoc(prev)
				return false
			}
			now := s.now()
			d := &Doc{
				UUID: st.NextUUID(), ExternalID: info.ExternalID, Group: group, Operation: operation, Request: raw,
				Canonical: canon, TotalKop: info.TotalKop, CallbackURL: info.CallbackURL, Status: StatusWait,
				CreatedAt: now, DueAt: now.Add(s.delay(st)), WillFail: st.FailNext, Callbacks: []Attempt{},
			}
			st.FailNext = false
			st.Docs = append(st.Docs, d)
			doc, created = copyDoc(d), true
			return true
		})
		switch {
		case conflict:
			s.fail(w, http.StatusConflict, &APIError{Code: codeDuplicate,
				Text: "Документ с таким external_id уже зарегистрирован с другими данными", Type: "system"})
			return
		case err != nil:
			s.fail(w, http.StatusInternalServerError, &APIError{Code: 1, Text: "Внутренняя ошибка: " + err.Error(), Type: "system"})
			return
		}
		if created {
			s.schedule(doc.UUID, doc.DueAt)
		}
		writeJSON(w, http.StatusOK, map[string]any{"uuid": doc.UUID, "status": StatusWait, "error": nil, "timestamp": s.timestamp()})
	}
}

// schedule — документ переходит в итоговый статус в момент due (и при первом обращении после него).
func (s *Server) schedule(uuid string, due time.Time) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if s.sleep(due.Sub(s.now())) {
			s.complete(uuid)
		}
	}()
}

// Resume — после перезапуска: документы, ещё ожидающие обработки, ставятся в расписание.
func (s *Server) Resume() {
	var waiting []Doc
	s.store.Read(func(st *State) {
		for _, d := range st.Docs {
			if d.Status == StatusWait {
				waiting = append(waiting, copyDoc(d))
			}
		}
	})
	for _, d := range waiting {
		s.schedule(d.UUID, d.DueAt)
	}
}

// randomAttribute — фискальный признак документа: 10 случайных цифр (первая не ноль).
func randomAttribute() int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(9_000_000_000))
	if err != nil {
		return 1_000_000_000
	}
	return n.Int64() + 1_000_000_000
}

// complete — итоговый статус документа (done с реквизитами или fail при включённом переключателе ошибки) и колбэк.
// Повторный вызов ничего не делает.
func (s *Server) complete(uuid string) {
	var doc Doc
	var changed, sendCallback bool
	err := s.store.With(func(st *State) bool {
		d := st.Find(uuid)
		if d == nil || d.Status != StatusWait || s.now().Before(d.DueAt) {
			return false
		}
		if d.WillFail {
			d.Status = StatusFail
			d.Error = &APIError{Code: codeDriver, Text: "Эмулятор: ошибка фискального накопителя", Type: "driver"}
		} else {
			now := s.now()
			shift := st.shiftNumber(now)
			key := strconv.Itoa(shift)
			st.Shifts[key]++
			st.DocSeq++
			d.Status = StatusDone
			d.Requisites = &Requisites{
				FnNumber: fnNumber, ShiftNumber: shift, FiscalReceiptNumber: st.Shifts[key], FiscalDocumentNumber: st.DocSeq,
				FiscalDocumentAttribute: randomAttribute(), EcrRegistrationNumber: ecrNumber, ReceiptDatetime: now,
			}
		}
		d.CallbackOff = st.NoCallbacks || d.CallbackURL == ""
		sendCallback = !d.CallbackOff
		doc, changed = copyDoc(d), true
		return true
	})
	if err != nil {
		s.log.Error("сохранение состояния", slog.String("error", err.Error()))
	}
	if changed && sendCallback {
		s.callback(doc)
	}
}

// completeDue — документы с наступившим сроком (обращение раньше, чем сработал таймер).
func (s *Server) completeDue() {
	var due []string
	s.store.Read(func(st *State) {
		for _, d := range st.Docs {
			if d.Status == StatusWait && !s.now().Before(d.DueAt) {
				due = append(due, d.UUID)
			}
		}
	})
	for _, id := range due {
		s.complete(id)
	}
}

// callback — POST callback_url с телом отчёта, до CallbackAttempts попыток с интервалом CallbackInterval;
// успех — любой ответ 2xx.
func (s *Server) callback(d Doc) {
	body, _ := json.Marshal(s.report(d))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for i := range s.cfg.CallbackAttempts {
			if i > 0 && !s.sleep(s.cfg.CallbackInterval) {
				return
			}
			result, ok := s.post(d.CallbackURL, body)
			_ = s.store.With(func(st *State) bool {
				if f := st.Find(d.UUID); f != nil {
					f.Callbacks = append(f.Callbacks, Attempt{At: s.now(), Result: result})
					return true
				}
				return false
			})
			if ok {
				return
			}
		}
	}()
}

func (s *Server) post(target string, body []byte) (string, bool) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err.Error(), false
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := s.client.Do(req)
	if err != nil {
		return "ошибка: " + err.Error(), false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.Status, resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Wait — ожидание фоновых обработок и колбэков (тесты).
func (s *Server) Wait() { s.wg.Wait() }

// ---------------------------------------------------------------- Отчёт

// kopNumber — «45000.00» из копеек.
func kopNumber(kop int64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", kop/100, kop%100))
}

// report — тело отчёта о документе (08 разд. 3.3).
func (s *Server) report(d Doc) map[string]any {
	out := map[string]any{
		"uuid": d.UUID, "status": d.Status, "error": d.Error, "timestamp": s.timestamp(), "external_id": d.ExternalID,
		"group_code": d.Group, "daemon_code": daemonCode, "device_code": deviceCode, "callback_url": d.CallbackURL, "payload": nil,
	}
	if d.Status == StatusDone && d.Requisites != nil {
		r := d.Requisites
		out["payload"] = map[string]any{
			"total": kopNumber(d.TotalKop), "fns_site": fnsSite, "fn_number": r.FnNumber, "shift_number": r.ShiftNumber,
			"receipt_datetime": r.ReceiptDatetime.In(moscow).Format(timestampLayout), "fiscal_receipt_number": r.FiscalReceiptNumber,
			"fiscal_document_number": r.FiscalDocumentNumber, "ecr_registration_number": r.EcrRegistrationNumber,
			"fiscal_document_attribute": r.FiscalDocumentAttribute, "ofd_inn": ofdINN,
			"ofd_receipt_url": strings.TrimRight(s.cfg.PublicURL, "/") + "/receipt/" + d.UUID,
		}
	}
	return out
}

func (s *Server) doc(uuid string) (Doc, bool) {
	var d Doc
	var ok bool
	s.store.Read(func(st *State) {
		if f := st.Find(uuid); f != nil {
			d, ok = copyDoc(f), true
		}
	})
	return d, ok
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	s.complete(r.PathValue("uuid"))
	d, ok := s.doc(r.PathValue("uuid"))
	if !ok || d.Group != r.PathValue("group") {
		s.fail(w, http.StatusNotFound, formatError("uuid"))
		return
	}
	writeJSON(w, http.StatusOK, s.report(d))
}

// ---------------------------------------------------------------- Экран, чек, служебные действия

func (s *Server) list() []Doc {
	s.completeDue()
	var out []Doc
	s.store.Read(func(st *State) {
		for i := len(st.Docs) - 1; i >= 0; i-- {
			out = append(out, copyDoc(st.Docs[i]))
		}
	})
	return out
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	var settings listSettings
	s.store.Read(func(st *State) {
		settings = listSettings{FailNext: st.FailNext, DelayMs: int(s.delay(st) / time.Millisecond), NoCallbacks: st.NoCallbacks}
	})
	renderList(w, s.list(), settings)
}

func (s *Server) handleAPIReceipts(w http.ResponseWriter, _ *http.Request) {
	list := s.list()
	if list == nil {
		list = []Doc{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleReceiptPage(w http.ResponseWriter, r *http.Request) {
	s.complete(r.PathValue("uuid"))
	d, ok := s.doc(r.PathValue("uuid"))
	if !ok || d.Status != StatusDone {
		renderMessage(w, http.StatusNotFound, "Чек не найден или ещё не сформирован.")
		return
	}
	renderReceipt(w, d)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var delay *int
	if v := strings.TrimSpace(r.PostFormValue("delayMs")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 600_000 {
			http.Error(w, "задержка — целое число от 0 до 600000 мс", http.StatusBadRequest)
			return
		}
		delay = &n
	}
	if err := s.store.With(func(st *State) bool {
		st.FailNext = r.PostFormValue("failNext") != ""
		st.NoCallbacks = r.PostFormValue("noCallbacks") != ""
		st.DelayMs = delay
		return true
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
