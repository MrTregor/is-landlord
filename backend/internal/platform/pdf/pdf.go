// Package pdf — клиент Gotenberg (HTML → PDF, docs/spec/08 Интеграции и эмуляторы.md, раздел 6).
package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Converter превращает HTML-документ в PDF. Реализации: Client (Gotenberg) и подделка в тестах.
type Converter interface {
	Convert(ctx context.Context, html string) ([]byte, error)
}

// ErrUnavailable — Gotenberg не ответил или ответил ошибкой.
var ErrUnavailable = errors.New("сервис формирования PDF недоступен")

// Client — клиент Gotenberg.
type Client struct {
	baseURL string
	http    *http.Client
	retries int
	pause   time.Duration
}

// New создаёт клиент для адреса GOTENBERG_URL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
		retries: 2,
		pause:   500 * time.Millisecond,
	}
}

// Health проверяет, что Gotenberg отвечает (GET /health).
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gotenberg недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gotenberg: статус %d", resp.StatusCode)
	}
	return nil
}

// Footer — нижний колонтитул: «стр. N из M», 9 pt, по центру.
const Footer = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
html, body { margin: 0; padding: 0; width: 100%; }
.footer { width: 100%; text-align: center; font-family: "Liberation Serif", "Times New Roman", serif; font-size: 9pt; }
</style></head><body><div class="footer">стр.&nbsp;<span class="pageNumber"></span>&nbsp;из&nbsp;<span class="totalPages"></span></div></body></html>`

// Поля страницы A4: 20/20/30/15 мм (дюймы).
var fields = [][2]string{
	{"paperWidth", "8.27"}, {"paperHeight", "11.7"},
	{"marginTop", "0.79"}, {"marginBottom", "0.79"}, {"marginLeft", "1.18"}, {"marginRight", "0.59"},
	{"printBackground", "true"},
}

func form(html string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, f := range []struct{ name, content string }{{"index.html", html}, {"footer.html", Footer}} {
		fw, err := w.CreateFormFile("files", f.name)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.WriteString(fw, f.content); err != nil {
			return nil, "", err
		}
	}
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &body, w.FormDataContentType(), nil
}

// Convert формирует PDF (POST /forms/chromium/convert/html). Таймаут 30 с, 2 повтора при 5xx и сетевых ошибках.
func (c *Client) Convert(ctx context.Context, html string) ([]byte, error) {
	var last error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.pause):
			}
		}
		out, retry, err := c.convertOnce(ctx, html)
		if err == nil {
			return out, nil
		}
		last = err
		if !retry {
			break
		}
	}
	return nil, fmt.Errorf("%w: %w", ErrUnavailable, last)
}

func (c *Client) convertOnce(ctx context.Context, html string) (out []byte, retry bool, err error) {
	body, ct, err := form(html)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/forms/chromium/convert/html", body)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", ct)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("gotenberg: статус %d: %.200s", resp.StatusCode, data)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return nil, false, errors.New("gotenberg: ответ не является PDF")
	}
	return data, false, nil
}
