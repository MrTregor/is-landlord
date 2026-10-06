// Package pdf — клиент Gotenberg (HTML → PDF, docs/spec/08 Интеграции и эмуляторы.md, раздел 6).
// На этапе M0 — только проверка доступности для /readyz; формирование PDF добавляется на этапе M5.
package pdf

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client — клиент Gotenberg.
type Client struct {
	baseURL string
	http    *http.Client
}

// New создаёт клиент для адреса GOTENBERG_URL.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
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
