// Package tbank — клиент протокола Т-Банк эквайринга (docs/spec/08 Интеграции и эмуляторы.md, раздел 1):
// подпись Token, запросы Init и GetState, проверка подписи уведомлений.
package tbank

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Статусы платежа Т-Банка, которые обрабатывает система (08 разд. 1.5).
const (
	StatusNew             = "NEW"
	StatusFormShowed      = "FORM_SHOWED"
	StatusAuthorized      = "AUTHORIZED"
	StatusConfirmed       = "CONFIRMED"
	StatusRejected        = "REJECTED"
	StatusCanceled        = "CANCELED"
	StatusDeadlineExpired = "DEADLINE_EXPIRED"
	StatusAuthFail        = "AUTH_FAIL"
)

// Sign — Token (08 разд. 1.1): поля корневого уровня, кроме Token и вложенных объектов и массивов, плюс Password;
// пары по имени ключа (побайтово по возрастанию); значения без разделителей (логические — true/false, числа — без
// дробной части); SHA-256 в нижнем регистре.
func Sign(fields map[string]any, password string) string {
	values := map[string]string{"Password": password}
	for k, v := range fields {
		if k == "Token" {
			continue
		}
		if s, ok := scalar(v); ok {
			values[k] = s
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(values[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}

// Client — запросы к API банка.
type Client struct {
	baseURL  string
	terminal string
	password string
	http     *http.Client
}

// NewClient создаёт клиента; таймаут запроса — 10 с (08 разд. 1.3).
func NewClient(baseURL, terminal, password string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), terminal: terminal, password: password,
		http: &http.Client{Timeout: 10 * time.Second}}
}

// TerminalKey — ключ терминала.
func (c *Client) TerminalKey() string { return c.terminal }

// InitRequest — запрос Init (08 разд. 1.3).
type InitRequest struct {
	Amount          int64
	OrderID         string
	Description     string
	NotificationURL string
	SuccessURL      string
	FailURL         string
	RedirectDueDate time.Time
	Email           string
	ContractNumber  string
}

// Response — ответ банка (Init, GetState).
type Response struct {
	Success    bool   `json:"Success"`
	ErrorCode  string `json:"ErrorCode"`
	Message    string `json:"Message"`
	Status     string `json:"Status"`
	PaymentID  string `json:"-"`
	OrderID    string `json:"OrderId"`
	Amount     int64  `json:"Amount"`
	PaymentURL string `json:"PaymentURL"`
}

// ErrUnavailable — сеть, таймаут или ответ не в формате API.
var ErrUnavailable = errors.New("платёжный сервис недоступен")

// Init — создание платежа.
func (c *Client) Init(ctx context.Context, r InitRequest) (Response, error) {
	data := map[string]any{"ContractNumber": r.ContractNumber}
	if r.Email != "" {
		data["Email"] = r.Email
	}
	body := map[string]any{
		"TerminalKey": c.terminal, "Amount": r.Amount, "OrderId": r.OrderID, "Description": r.Description, "PayType": "O",
		"Language": "ru", "NotificationURL": r.NotificationURL, "SuccessURL": r.SuccessURL, "FailURL": r.FailURL,
		"RedirectDueDate": r.RedirectDueDate.Format(time.RFC3339), "DATA": data,
	}
	return c.call(ctx, "/Init", body)
}

// GetState — статус платежа (08 разд. 1.6).
func (c *Client) GetState(ctx context.Context, paymentID string) (Response, error) {
	return c.call(ctx, "/GetState", map[string]any{"TerminalKey": c.terminal, "PaymentId": paymentID})
}

func (c *Client) call(ctx context.Context, path string, body map[string]any) (Response, error) {
	body["Token"] = Sign(body, c.password)
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var out Response
	if err := json.Unmarshal(b, &out); err != nil {
		return Response{}, fmt.Errorf("%w: ответ %d: %w", ErrUnavailable, resp.StatusCode, err)
	}
	var ids struct {
		PaymentID json.RawMessage `json:"PaymentId"`
	}
	_ = json.Unmarshal(b, &ids)
	out.PaymentID = rawID(ids.PaymentID)
	return out, nil
}

// rawID — PaymentId строкой (банк может прислать строку или число).
func rawID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// Notification — уведомление банка (08 разд. 1.4, 2.4).
type Notification struct {
	Fields    map[string]any
	Terminal  string
	OrderID   string
	PaymentID string
	Status    string
	ErrorCode string
	Amount    int64
	Success   bool
}

// ParseNotification читает уведомление с сохранением чисел (json.Number) для проверки подписи.
func ParseNotification(raw []byte) (Notification, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return Notification{}, err
	}
	return FromMap(m), nil
}

// FromMap — уведомление из разобранного с json.Number JSON.
func FromMap(m map[string]any) Notification {
	n := Notification{Fields: m}
	n.Terminal, _ = scalar(m["TerminalKey"])
	n.OrderID, _ = scalar(m["OrderId"])
	n.PaymentID, _ = scalar(m["PaymentId"])
	n.Status, _ = scalar(m["Status"])
	n.ErrorCode, _ = scalar(m["ErrorCode"])
	if a, ok := scalar(m["Amount"]); ok {
		n.Amount, _ = strconv.ParseInt(a, 10, 64)
	}
	n.Success, _ = m["Success"].(bool)
	return n
}

// Verify — терминал и подпись уведомления.
func (c *Client) Verify(n Notification) bool {
	token, _ := n.Fields["Token"].(string)
	if n.Terminal != c.terminal || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(Sign(n.Fields, c.password))) == 1
}
