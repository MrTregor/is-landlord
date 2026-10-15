// Package atol — клиент АТОЛ Онлайн v5 (docs/spec/08 Интеграции и эмуляторы.md, раздел 3): токен (кэш в памяти
// процесса на 23 часа, при ответе «токен истёк» — новый токен и один повтор), чек прихода и возврата прихода,
// отчёт о документе. Деньги в запросах — строки json.Number, без float.
package atol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"landlord/backend/internal/platform/clock"
)

// Операции и статусы документа.
const (
	OperationSell       = "sell"
	OperationSellRefund = "sell_refund"

	StatusWait = "wait"
	StatusDone = "done"
	StatusFail = "fail"
)

// CodeTokenExpired — ошибка «Срок действия токена истёк».
const CodeTokenExpired = 11

// tokenTTL — срок кэширования токена (08 разд. 3.1).
const tokenTTL = 23 * time.Hour

// TimestampLayout — формат отметок времени АТОЛ: dd.MM.yyyy HH:mm:ss (по Москве).
const TimestampLayout = "02.01.2006 15:04:05"

// Error — ошибка в ответе АТОЛ.
type Error struct {
	Code int    `json:"code"`
	Text string `json:"text"`
	Type string `json:"type"`
}

// Response — ответ на чек прихода или возврата прихода.
type Response struct {
	UUID      *string `json:"uuid"`
	Status    string  `json:"status"`
	Error     *Error  `json:"error"`
	Timestamp string  `json:"timestamp"`
	// Raw — тело ответа как есть (fiscal_receipts.last_response).
	Raw []byte `json:"-"`
}

// Payload — реквизиты сформированного чека (поле payload отчёта).
type Payload struct {
	Total                   json.Number `json:"total"`
	FnsSite                 string      `json:"fns_site"`
	FnNumber                string      `json:"fn_number"`
	ShiftNumber             int32       `json:"shift_number"`
	ReceiptDatetime         string      `json:"receipt_datetime"`
	FiscalReceiptNumber     int32       `json:"fiscal_receipt_number"`
	FiscalDocumentNumber    int64       `json:"fiscal_document_number"`
	EcrRegistrationNumber   string      `json:"ecr_registration_number"`
	FiscalDocumentAttribute int64       `json:"fiscal_document_attribute"`
	OfdInn                  string      `json:"ofd_inn"`
	OfdReceiptURL           string      `json:"ofd_receipt_url"`
}

// Report — отчёт о документе (08 разд. 3.3).
type Report struct {
	UUID       string   `json:"uuid"`
	Status     string   `json:"status"`
	Error      *Error   `json:"error"`
	Timestamp  string   `json:"timestamp"`
	ExternalID string   `json:"external_id"`
	Payload    *Payload `json:"payload"`
	Raw        []byte   `json:"-"`
}

// Client — клиент API АТОЛ Онлайн.
type Client struct {
	baseURL, login, password, group string
	http                            *http.Client
	clock                           clock.Clock

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

// NewClient создаёт клиента: baseURL — ATOL_API_URL (…/possystem/v5), group — ATOL_GROUP_CODE.
func NewClient(baseURL, login, password, group string, clk clock.Clock) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), login: login, password: password, group: group,
		http: &http.Client{Timeout: 15 * time.Second}, clock: clk}
}

// tokenFor — кэшированный токен (fresh — получить заново).
func (c *Client) tokenFor(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !fresh && c.token != "" && c.clock.Now().Sub(c.tokenAt) < tokenTTL {
		return c.token, nil
	}
	body, err := json.Marshal(map[string]string{"login": c.login, "pass": c.password})
	if err != nil {
		return "", err
	}
	var resp struct {
		Error *Error  `json:"error"`
		Token *string `json:"token"`
	}
	if _, err := c.call(ctx, http.MethodPost, c.baseURL+"/getToken", "", body, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil || resp.Token == nil || *resp.Token == "" {
		if resp.Error != nil {
			return "", fmt.Errorf("АТОЛ getToken: %d %s", resp.Error.Code, resp.Error.Text)
		}
		return "", errors.New("АТОЛ getToken: нет токена в ответе")
	}
	c.token, c.tokenAt = *resp.Token, c.clock.Now()
	return c.token, nil
}

// call — HTTP-запрос и разбор JSON-ответа (тело разбирается и при 4xx — в нём ошибка АТОЛ). Ошибка — сеть,
// 5xx или ответ не JSON (задача повторится).
func (c *Client) call(ctx context.Context, method, url, token string, body []byte, out any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		req.Header.Set("Token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return raw, fmt.Errorf("АТОЛ: HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return raw, fmt.Errorf("АТОЛ: HTTP %d, ответ не JSON: %w", resp.StatusCode, err)
	}
	return raw, nil
}

// withToken — запрос с токеном; ответ «токен истёк» (код 11) → новый токен и один повтор (08 разд. 3.1).
func (c *Client) withToken(ctx context.Context, method, url string, body []byte, decode func([]byte) error) error {
	for fresh := false; ; fresh = true {
		token, err := c.tokenFor(ctx, fresh)
		if err != nil {
			return err
		}
		var probe struct {
			Error *Error `json:"error"`
		}
		raw, err := c.call(ctx, method, url, token, body, &probe)
		if err != nil {
			return err
		}
		if probe.Error != nil && probe.Error.Code == CodeTokenExpired && !fresh {
			continue
		}
		return decode(raw)
	}
}

func decodeInto(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// Send — чек прихода (sell) или возврата прихода (sell_refund): тело — готовый запрос (fiscal_receipts.request_payload).
func (c *Client) Send(ctx context.Context, operation string, body []byte) (Response, error) {
	var out Response
	err := c.withToken(ctx, http.MethodPost, c.baseURL+"/"+c.group+"/"+operation, body, func(raw []byte) error {
		out.Raw = raw
		return decodeInto(raw, &out)
	})
	return out, err
}

// Report — состояние документа по uuid.
func (c *Client) Report(ctx context.Context, uuid string) (Report, error) {
	var out Report
	err := c.withToken(ctx, http.MethodGet, c.baseURL+"/"+c.group+"/report/"+uuid, nil, func(raw []byte) error {
		out.Raw = raw
		return decodeInto(raw, &out)
	})
	return out, err
}

// ParseTimestamp — «16.10.2026 19:30:05» по Москве.
func ParseTimestamp(s string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation(TimestampLayout, s, loc)
}
