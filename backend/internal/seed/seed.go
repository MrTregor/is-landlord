// Package seed — демонстрационные данные `cli seed --demo` и `--load` (docs/spec/17 Демо-данные.md).
//
// Команда очищает бизнес-таблицы и загружает данные теми же сервисами, что и в работе: приложение собирается
// в процессе с остановленными часами, методы API вызываются через встроенный HTTP-сервер, «сегодня» сдвигается
// по дням от самого раннего договора до реальной даты, ежедневные задачи выполняются за каждый день.
// Внешние системы не вызываются: онлайн-оплата идёт через поддельный банк внутри процесса, чеки и письма истории
// помечаются выполненными (раздел 6 и 5 файла 17).
package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"landlord/backend/internal/app"
	"landlord/backend/internal/config"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/pdf"
)

// Options — режимы команды.
type Options struct {
	// Load — дополнительно 100 объектов и нанимателей для нагрузочного теста (17 разд. 7).
	Load bool
	// PDF — формирование PDF (тесты); по умолчанию — Gotenberg из конфигурации.
	PDF pdf.Converter
	// Today — «сегодня» загрузки (тесты); по умолчанию — реальная дата.
	Today *date.Date
}

// Учётные записи демо (17 разд. 1).
const (
	OwnerEmail     = "owner@landlord.local"
	OwnerPassword  = "Demo-2026-owner"
	TenantPassword = "Demo-2026-tenant"
)

// fastArgon — хеширование паролей при загрузке: при первом входе пароль перехешируется с рабочими параметрами.
var fastArgon = crypto.Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

type world struct {
	ctx      context.Context
	cfg      *config.Config
	a        *app.App
	clk      *clock.Fixed
	srv      *httptest.Server
	bank     *fakeBank
	owner    *client
	out      io.Writer
	loc      *time.Location
	today    date.Date // реальная дата загрузки
	m0       date.Date // первое число текущего месяца
	day      date.Date // текущий день сценария
	policy   string
	ids      map[string]string // объекты, наниматели, договоры, счётчики по ключам сценария
	statLine *pendingStatement
	loadMode bool
}

// Run выполняет `cli seed --demo` (и `--load`).
func Run(ctx context.Context, base *config.Config, log *slog.Logger, out io.Writer, opt Options) error {
	cfg := *base
	cfg.CookieSecure = false // встроенный сервер доступен только этому процессу по http
	bank := newFakeBank()
	defer bank.srv.Close()
	cfg.TbankAPIURL, cfg.TbankTerminalKey, cfg.TbankPassword = bank.srv.URL+"/v2", bankTerminal, bankPassword
	cfg.TbankNotificationURL = "http://localhost/api/v1/integrations/tbank/notify"

	if err := clear(ctx, &cfg); err != nil {
		return fmt.Errorf("очистка: %w", err)
	}
	fmt.Fprintln(out, "Бизнес-таблицы очищены.")

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	real := clock.NewReal(cfg.Location)
	today := real.Today()
	if opt.Today != nil {
		today = *opt.Today
	}
	clk := clock.NewFixedDate(today, cfg.Location)
	conv := opt.PDF
	if conv == nil {
		conv = pdf.New(cfg.GotenbergURL)
	}
	a, err := app.New(&cfg, log, clk, database, app.Options{Argon: &fastArgon, PDF: conv, NoRateLimit: true})
	if err != nil {
		return err
	}
	srv := httptest.NewServer(a.Server(okHealth{}).Handler())
	defer srv.Close()
	w := &world{ctx: ctx, cfg: &cfg, a: a, clk: clk, srv: srv, bank: bank, out: out, loc: cfg.Location, today: today,
		m0: today.FirstOfMonth(), ids: map[string]string{}}
	w.loadMode = opt.Load
	if err := w.demo(); err != nil {
		return fmt.Errorf("демо-данные (%s): %w", w.day.Format(), err)
	}
	if err := w.finish(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Демо-данные загружены на %s.\nСобственник: %s / %s\nНаниматели: kuznetsov@landlord.local, romashka@landlord.local, "+
		"smirnova@landlord.local / %s; lebedev@landlord.local — приглашён (письмо в Mailpit)\n",
		today.Format(), OwnerEmail, OwnerPassword, TenantPassword)
	return nil
}

type okHealth struct{}

func (okHealth) Health(context.Context) error { return nil }

// bizTables — бизнес-таблицы (04): очищаются полностью. Журнал действий неизменяем и не очищается (№ 103).
var bizTables = []string{
	"files", "tenants", "users", "sessions", "auth_tokens", "login_attempts", "pdn_consents", "organization_settings",
	"properties", "property_status_history", "property_photos", "property_inventory_items", "meters", "tariffs",
	"contracts", "contract_amendments", "contract_terms", "contract_occupants", "contract_acts", "meter_readings",
	"charges", "charge_adjustments", "payments", "payment_allocations", "refunds", "deposit_transactions",
	"fiscal_receipts", "webhook_events", "bank_statements", "bank_statement_lines", "discrepancies",
	"maintenance_requests", "maintenance_request_photos", "maintenance_request_events", "expenses", "calendar_events",
	"email_log", "documents", "number_sequences", "job_runs", "email_templates", "document_templates",
}

// clear — очистка под владельцем схемы (TRUNCATE): системные шаблоны документов сохраняются, тексты писем
// возвращаются к стандартным, файлы удаляются с диска, очередь задач и сдвиг даты сбрасываются.
func clear(ctx context.Context, cfg *config.Config) error {
	conn, err := pgx.Connect(ctx, cfg.MigrateURL())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	keys := []string{}
	rows, err := conn.Query(ctx, `SELECT storage_key FROM files WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	defaults, err := notify.Defaults()
	if err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stmts := []string{
		`CREATE TEMP TABLE seed_et ON COMMIT DROP AS SELECT * FROM email_templates`,
		`CREATE TEMP TABLE seed_dt ON COMMIT DROP AS SELECT * FROM document_templates WHERE is_system`,
		`TRUNCATE ` + join(bizTables) + ` CASCADE`,
		`INSERT INTO email_templates SELECT code, name, recipient, subject, body_html, true, NULL, now() FROM seed_et`,
		`INSERT INTO document_templates (id, kind, name, body_html, version, is_active, is_system, created_by, created_at)
		 SELECT id, kind, name, body_html, version, true, true, NULL, created_at FROM seed_dt`,
		`DELETE FROM river_job`,
		`INSERT INTO demo_clock (id, offset_seconds) VALUES (1, 0) ON CONFLICT (id) DO UPDATE SET offset_seconds = 0`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s[:min(len(s), 40)], err)
		}
	}
	for code, d := range defaults {
		if _, err := tx.Exec(ctx, `UPDATE email_templates SET subject = $2, body_html = $3 WHERE code = $1`, code, d.Subject, d.BodyHTML); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	store := fileStore(cfg)
	for _, k := range keys {
		_ = store.Remove(k)
	}
	return nil
}

func join(list []string) string {
	var b bytes.Buffer
	for i, s := range list {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s)
	}
	return b.String()
}

// at — перейти на день сценария (10:00 по Москве).
func (w *world) at(d date.Date) {
	w.day = d
	w.clk.Set(time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, w.loc))
}

// ---------------------------------------------------------------- клиент API внутри процесса

type client struct {
	w        *world
	http     *http.Client
	csrf     string
	email    string
	password string
	loggedOn date.Date
}

func (w *world) newClient(email, password string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{w: w, http: &http.Client{Jar: jar}, email: email, password: password}
}

type apiError struct {
	method, path string
	status       int
	body         string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.method, e.path, e.status, e.body)
}

func (c *client) raw(method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(c.w.ctx, method, c.w.srv.URL+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return &apiError{method, path, res.StatusCode, string(data)}
	}
	if out != nil && len(data) > 0 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		return dec.Decode(out)
	}
	return nil
}

// login — вход (сессия по часам сценария; на новый день — заново).
func (c *client) login() error {
	c.csrf = ""
	var res struct {
		CsrfToken string `json:"csrfToken"`
	}
	b, _ := json.Marshal(map[string]string{"email": c.email, "password": c.password})
	if err := c.raw(http.MethodPost, "/auth/login", bytes.NewReader(b), "application/json", &res); err != nil {
		return err
	}
	c.csrf, c.loggedOn = res.CsrfToken, c.w.day
	return nil
}

// do — запрос JSON от имени пользователя; out — разбор ответа (nil — не нужен).
func (c *client) do(method, path string, body, out any) error {
	if c.loggedOn != c.w.day {
		if err := c.login(); err != nil {
			return err
		}
	}
	var rd io.Reader
	ct := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, ct = bytes.NewReader(b), "application/json"
	}
	return c.raw(method, path, rd, ct, out)
}

// id — запрос, возвращающий объект с полем id.
func (c *client) id(method, path string, body any) (string, error) {
	var v struct {
		ID string `json:"id"`
	}
	err := c.do(method, path, body, &v)
	return v.ID, err
}

// upload — загрузка файла (multipart) в category или по адресу path.
func (c *client) upload(path string, fields map[string]string, name string, data []byte) (string, error) {
	if c.loggedOn != c.w.day {
		if err := c.login(); err != nil {
			return "", err
		}
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	var v struct {
		ID string `json:"id"`
	}
	err = c.raw(http.MethodPost, path, &buf, mw.FormDataContentType(), &v)
	return v.ID, err
}

var inviteLink = regexp.MustCompile(`/invite/([A-Za-z0-9_-]+)`)

// acceptInvite — принять последнее приглашение на e-mail с паролем (наниматель — с ознакомлением с политикой).
func (w *world) acceptInvite(email, password string, tenant bool) (*client, error) {
	var body string
	err := w.a.DB.Pool.QueryRow(w.ctx, `SELECT body_html FROM email_log WHERE template_code = 'invite' AND to_email = $1
		ORDER BY created_at DESC LIMIT 1`, email).Scan(&body)
	if err != nil {
		return nil, fmt.Errorf("приглашение %s: %w", email, err)
	}
	m := inviteLink.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("в приглашении %s нет ссылки", email)
	}
	c := w.newClient(email, password)
	in := map[string]any{"token": m[1], "password": password}
	if tenant {
		in["acceptPolicyVersion"] = w.policy
	}
	b, _ := json.Marshal(in)
	if err := c.raw(http.MethodPost, "/auth/invite/accept", bytes.NewReader(b), "application/json", nil); err != nil {
		return nil, err
	}
	return c, nil
}
