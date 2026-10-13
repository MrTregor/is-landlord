package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Config — настройки эмулятора (08 разд. 2.1).
type Config struct {
	PublicURL string
	Terminals map[string]string // ключ терминала → пароль
	Retry     []time.Duration   // расписание попыток доставки уведомления (от первой попытки)
	StateFile string
}

// Server — эмулятор Т-Банк эквайринга.
type Server struct {
	cfg    Config
	store  *Store
	client *http.Client
	now    func() time.Time
	log    *slog.Logger
	// wg — фоновые доставки уведомлений (ожидание в тестах).
	wg sync.WaitGroup
	// ctx отменяется при /admin/reset: доставки прекращаются.
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewServer создаёт эмулятор.
func NewServer(cfg Config, store *Store, log *slog.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, store: store, client: &http.Client{Timeout: 5 * time.Second}, now: time.Now, log: log,
		ctx: ctx, cancel: cancel}
}

// Handler — маршруты эмулятора (08 разд. 2.2).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /v2/Init", s.handleInit)
	mux.HandleFunc("POST /v2/GetState", s.handleGetState)
	mux.HandleFunc("GET /pay/{id}", s.handlePayPage)
	mux.HandleFunc("POST /pay/{id}/action", s.handlePayAction)
	mux.HandleFunc("GET /{$}", s.handleList)
	mux.HandleFunc("POST /admin/{id}/resend", s.handleResend)
	mux.HandleFunc("POST /admin/{id}/status", s.handleForceStatus)
	mux.HandleFunc("POST /admin/reset", s.handleReset)
	mux.HandleFunc("GET /api/payments", s.handleAPIPayments)
	return mux
}

// ---------------------------------------------------------------- API банка

// apiError — ответ Success: false (08 разд. 2.2).
func apiError(w http.ResponseWriter, code, message, details string) {
	writeJSON(w, map[string]any{"Success": false, "ErrorCode": code, "Message": message, "Details": details})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// readFields — корневые поля запроса с сохранением чисел (json.Number) для подписи.
func readFields(r *http.Request) (map[string]any, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

// auth — терминал и подпись запроса; при ошибке ответ уже записан.
func (s *Server) auth(w http.ResponseWriter, m map[string]any) (string, bool) {
	terminal := str(m, "TerminalKey")
	password, ok := s.cfg.Terminals[terminal]
	if !ok {
		apiError(w, "202", "Терминал не найден", "")
		return "", false
	}
	if token := str(m, "Token"); token == "" || token != sign(m, password) {
		apiError(w, "204", "Неверный токен", "")
		return "", false
	}
	return terminal, true
}

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	m, err := readFields(r)
	if err != nil {
		apiError(w, "100", "Неверные параметры запроса: тело", err.Error())
		return
	}
	terminal, ok := s.auth(w, m)
	if !ok {
		return
	}
	amountNum, isNum := m["Amount"].(json.Number)
	amount, err := strconv.ParseInt(string(amountNum), 10, 64)
	if !isNum || err != nil || amount < 100 {
		apiError(w, "100", "Неверные параметры запроса: Amount", "")
		return
	}
	order := str(m, "OrderId")
	if n := utf8.RuneCountInString(order); n < 1 || n > 36 {
		apiError(w, "100", "Неверные параметры запроса: OrderId", "")
		return
	}
	notifyURL, successURL := str(m, "NotificationURL"), str(m, "SuccessURL")
	if notifyURL == "" {
		apiError(w, "100", "Неверные параметры запроса: NotificationURL", "")
		return
	}
	if successURL == "" {
		apiError(w, "100", "Неверные параметры запроса: SuccessURL", "")
		return
	}
	failURL := str(m, "FailURL")
	if failURL == "" {
		failURL = successURL
	}
	due := s.now().Add(24 * time.Hour)
	if v := str(m, "RedirectDueDate"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apiError(w, "100", "Неверные параметры запроса: RedirectDueDate", "")
			return
		}
		due = t
	}
	data := map[string]string{}
	if d, ok := m["DATA"].(map[string]any); ok {
		for k := range d {
			data[k] = str(d, k)
		}
	}
	var p Payment
	var dup bool
	err = s.store.With(func(st *State) bool {
		if st.HasOrder(terminal, order) {
			dup = true
			return false
		}
		np := &Payment{
			PaymentID: st.NextID(), TerminalKey: terminal, OrderID: order, Description: str(m, "Description"), Amount: amount,
			Status: StatusNew, ErrorCode: "0", NotificationURL: notifyURL, SuccessURL: successURL, FailURL: failURL,
			RedirectDueDate: due, Data: data, CreatedAt: s.now(), Notices: []*Notice{},
		}
		st.Payments = append(st.Payments, np)
		p = copyPayment(np)
		return true
	})
	switch {
	case dup:
		apiError(w, "309", "Заказ с таким OrderId уже существует", "")
		return
	case err != nil:
		apiError(w, "9999", "Внутренняя ошибка", err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"Success": true, "ErrorCode": "0", "TerminalKey": terminal, "Status": p.Status, "PaymentId": p.PaymentID,
		"OrderId": p.OrderID, "Amount": p.Amount, "PaymentURL": strings.TrimRight(s.cfg.PublicURL, "/") + "/pay/" + p.PaymentID,
	})
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	m, err := readFields(r)
	if err != nil {
		apiError(w, "100", "Неверные параметры запроса: тело", err.Error())
		return
	}
	terminal, ok := s.auth(w, m)
	if !ok {
		return
	}
	id := str(m, "PaymentId")
	var p *Payment
	s.store.Read(func(st *State) {
		if f := st.Find(id); f != nil && f.TerminalKey == terminal {
			c := copyPayment(f)
			p = &c
		}
	})
	if p == nil {
		apiError(w, "100", "Неверные параметры запроса: PaymentId", "")
		return
	}
	writeJSON(w, map[string]any{
		"Success": true, "ErrorCode": "0", "TerminalKey": terminal, "Status": p.Status, "PaymentId": p.PaymentID,
		"OrderId": p.OrderID, "Amount": p.Amount,
	})
}

// ---------------------------------------------------------------- Уведомления

// notificationBody — тело уведомления (08 разд. 2.4) с подписью паролем терминала.
func (s *Server) notificationBody(p Payment, status string) map[string]any {
	success := status != StatusRejected && status != StatusDeadlineExpired
	errCode := "0"
	if !success {
		errCode = p.ErrorCode
		if errCode == "0" || errCode == "" {
			errCode = "1051"
		}
	}
	body := map[string]any{
		"TerminalKey": p.TerminalKey, "OrderId": p.OrderID, "Success": success, "Status": status, "PaymentId": p.PaymentID,
		"ErrorCode": errCode, "Amount": p.Amount,
	}
	// Для оплаты СБП полей карты нет (08 разд. 2.3).
	if p.Method == "card" {
		body["CardId"] = 101
		body["Pan"] = "220000******0004"
		body["ExpDate"] = "1230"
	}
	body["Token"] = sign(body, s.cfg.Terminals[p.TerminalKey])
	return body
}

// setStatus меняет статус и ставит уведомления: по одному на каждый статус из statuses (итоговый — последний),
// twice — каждое дважды; доставка последовательная, delay — задержка до первой попытки.
func (s *Server) setStatus(id string, statuses []string, errCode, message string, twice bool, delay time.Duration) error {
	var notices []*Notice
	var p Payment
	err := s.store.With(func(st *State) bool {
		f := st.Find(id)
		if f == nil {
			return false
		}
		f.Status = statuses[len(statuses)-1]
		if errCode != "" {
			f.ErrorCode, f.Message = errCode, message
		}
		for _, status := range statuses {
			n := 1
			if twice {
				n = 2
			}
			for range n {
				nt := &Notice{Status: status, CreatedAt: s.now(), Attempts: []Attempt{}}
				f.Notices = append(f.Notices, nt)
				notices = append(notices, nt)
			}
		}
		p = copyPayment(f)
		return true
	})
	if err != nil {
		return err
	}
	s.deliver(p, notices, delay)
	return nil
}

// deliver отправляет уведомления по очереди; каждое — по расписанию NOTIFY_RETRY (успех — HTTP 200 и тело ровно
// «OK», после последней неудачной попытки — «не доставлено»).
func (s *Server) deliver(p Payment, notices []*Notice, delay time.Duration) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for k, nt := range notices {
			body, _ := json.Marshal(s.notificationBody(p, nt.Status))
			var prev time.Duration
			for i, at := range s.cfg.Retry {
				wait := at - prev
				if i == 0 && k == 0 {
					wait = delay + at
				}
				prev = at
				if wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
				}
				if ctx.Err() != nil {
					return
				}
				result, ok := s.post(ctx, p.NotificationURL, body)
				last := i == len(s.cfg.Retry)-1
				_ = s.store.With(func(*State) bool {
					nt.Attempts = append(nt.Attempts, Attempt{At: s.now(), Result: result})
					nt.Delivered = ok
					nt.GaveUp = !ok && last
					return true
				})
				if ok {
					break
				}
			}
		}
	}()
}

func (s *Server) post(ctx context.Context, target string, body []byte) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err.Error(), false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "ошибка: " + err.Error(), false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusOK && string(b) == "OK" {
		return "200 OK", true
	}
	return fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(b))), false
}

// Wait — ожидание фоновых доставок (тесты).
func (s *Server) Wait() { s.wg.Wait() }

// ---------------------------------------------------------------- Страница оплаты и действия

// expireIfDue — истёкший срок ссылки: DEADLINE_EXPIRED и уведомление.
func (s *Server) expireIfDue(p Payment) (Payment, error) {
	if (p.Status == StatusNew || p.Status == StatusFormShowed) && s.now().After(p.RedirectDueDate) {
		if err := s.setStatus(p.PaymentID, []string{StatusDeadlineExpired}, "", "", false, 0); err != nil {
			return p, err
		}
		p.Status = StatusDeadlineExpired
	}
	return p, nil
}

func (s *Server) payment(id string) (Payment, bool) {
	var p Payment
	var ok bool
	s.store.Read(func(st *State) {
		if f := st.Find(id); f != nil {
			p, ok = copyPayment(f), true
		}
	})
	return p, ok
}

func (s *Server) handlePayPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.payment(r.PathValue("id"))
	if !ok {
		renderPage(w, http.StatusNotFound, "Платёж не найден", pageData{Message: "Платёж не найден."})
		return
	}
	p, err := s.expireIfDue(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.Status == StatusNew {
		_ = s.store.With(func(st *State) bool {
			if f := st.Find(p.PaymentID); f != nil && f.Status == StatusNew {
				f.Status = StatusFormShowed
				return true
			}
			return false
		})
		p.Status = StatusFormShowed
	}
	renderPayPage(w, p)
}

func (s *Server) handlePayAction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.payment(r.PathValue("id"))
	if !ok {
		renderPage(w, http.StatusNotFound, "Платёж не найден", pageData{Message: "Платёж не найден."})
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action := r.PostFormValue("action")
	if action == "back" {
		http.Redirect(w, r, withQuery(p.FailURL, false, p), http.StatusSeeOther)
		return
	}
	p, err := s.expireIfDue(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.Status != StatusNew && p.Status != StatusFormShowed {
		renderPayPage(w, p)
		return
	}
	twice := r.PostFormValue("notifyTwice") != ""
	delaySec, _ := strconv.Atoi(r.PostFormValue("notifyDelaySec"))
	delaySec = max(0, min(delaySec, 120))
	delay := time.Duration(delaySec) * time.Second
	switch action {
	case "card_success", "sbp_success":
		method := "card"
		if action == "sbp_success" {
			method = "sbp"
		}
		_ = s.store.With(func(st *State) bool {
			st.Find(p.PaymentID).Method = method
			return true
		})
		if err := s.setStatus(p.PaymentID, []string{StatusAuthorized, StatusConfirmed}, "", "", twice, delay); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, withQuery(p.SuccessURL, true, p), http.StatusSeeOther)
	case "reject":
		if err := s.setStatus(p.PaymentID, []string{StatusRejected}, "1051", "Недостаточно средств на карте", twice, delay); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, withQuery(p.FailURL, false, p), http.StatusSeeOther)
	default:
		http.Error(w, "неизвестное действие", http.StatusBadRequest)
	}
}

// withQuery — адрес возврата с параметрами Success, PaymentId, OrderId.
func withQuery(target string, success bool, p Payment) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := u.Query()
	q.Set("Success", strconv.FormatBool(success))
	q.Set("PaymentId", p.PaymentID)
	q.Set("OrderId", p.OrderID)
	u.RawQuery = q.Encode()
	return u.String()
}

// ---------------------------------------------------------------- Экран списка и служебные действия

func (s *Server) list() []Payment {
	var out []Payment
	s.store.Read(func(st *State) {
		for i := len(st.Payments) - 1; i >= 0; i-- {
			out = append(out, copyPayment(st.Payments[i]))
		}
	})
	return out
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) { renderList(w, s.list()) }

func (s *Server) handleAPIPayments(w http.ResponseWriter, _ *http.Request) {
	list := s.list()
	if list == nil {
		list = []Payment{}
	}
	writeJSON(w, list)
}

func (s *Server) handleResend(w http.ResponseWriter, r *http.Request) {
	p, ok := s.payment(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	status := p.Status
	if n := len(p.Notices); n > 0 {
		status = p.Notices[n-1].Status
	}
	if err := s.setStatus(p.PaymentID, []string{status}, "", "", false, 0); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleForceStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.payment(r.PathValue("id")); !ok {
		http.NotFound(w, r)
		return
	}
	status := r.FormValue("status")
	var err error
	switch status {
	case StatusConfirmed, StatusDeadlineExpired:
		err = s.setStatus(r.PathValue("id"), []string{status}, "", "", false, 0)
	case StatusRejected:
		err = s.setStatus(r.PathValue("id"), []string{status}, "1051", "Недостаточно средств на карте", false, 0)
	default:
		http.Error(w, "недопустимый статус", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.cancel()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Unlock()
	// Нумерация PaymentId не сбрасывается: в системе уже сохранены PaymentId прежних платежей.
	if err := s.store.With(func(st *State) bool {
		st.Payments = nil
		return true
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
