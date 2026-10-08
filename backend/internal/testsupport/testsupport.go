//go:build integration

// Package testsupport — окружение интеграционных тестов: PostgreSQL 18 в контейнере (testcontainers),
// миграции, приложение под ролью landlord_app с управляемыми часами и HTTP-клиент с cookie и CSRF.
package testsupport

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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"landlord/backend/internal/app"
	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
)

// URL подключения: владелец схемы (миграции, правка данных в тестах) и роль приложения.
var (
	OwnerURL string
	AppURL   string
)

// Main запускает контейнер на время тестов пакета: func TestMain(m *testing.M) { testsupport.Main(m) }.
func Main(m *testing.M) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("landlord"),
		tcpostgres.WithUsername("landlord_owner"),
		tcpostgres.WithPassword("owner"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(120*time.Second)),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "контейнер PostgreSQL:", err)
		os.Exit(1)
	}
	code := func() int {
		defer func() { _ = testcontainers.TerminateContainer(ctr) }()
		OwnerURL, err = ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := prepare(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "подготовка БД:", err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

func prepare(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, OwnerURL)
	if err != nil {
		return err
	}
	// Как deploy/db-init/01-roles.sh.
	_, err = conn.Exec(ctx, `CREATE ROLE landlord_app LOGIN PASSWORD 'app';
		GRANT USAGE ON SCHEMA public TO landlord_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO landlord_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO landlord_app;`)
	_ = conn.Close(ctx)
	if err != nil {
		return err
	}
	if err := db.NewMigrator(OwnerURL, io.Discard).Up(ctx); err != nil {
		return err
	}
	AppURL = strings.Replace(OwnerURL, "landlord_owner:owner@", "landlord_app:app@", 1)
	return nil
}

// FastArgon — быстрые параметры хеширования для тестов (формат PHC тот же).
var FastArgon = crypto.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

// Env — приложение для одного теста.
type Env struct {
	App    *app.App
	Clock  *clock.Fixed
	Server *httptest.Server
}

// Moscow — часовой пояс бизнес-дат.
func Moscow() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		panic(err)
	}
	return loc
}

// NewEnv собирает приложение с часами на дату today (полдень по Москве).
func NewEnv(t *testing.T, today string) *Env {
	t.Helper()
	ctx := context.Background()
	cfg := &config.Config{
		AppEnv: config.EnvDev, AppName: "Наймодатель", AppPublicURL: "http://localhost:5173",
		SessionIdleHours: 8, SessionAbsoluteDays: 30, CookieSecure: false, Location: Moscow(),
		FilesDir: t.TempDir(), MaxUploadMB: 10, PDNKey: bytes.Repeat([]byte{42}, 32),
	}
	database, err := db.Open(ctx, AppURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	clk := clock.NewFixedDate(date.MustParse(today), Moscow())
	log := slog.New(slog.NewTextHandler(tWriter{t}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := app.New(cfg, log, clk, database, app.Options{Argon: &FastArgon})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Server(okGotenberg{}).Handler())
	t.Cleanup(srv.Close)
	return &Env{App: a, Clock: clk, Server: srv}
}

type okGotenberg struct{}

func (okGotenberg) Health(context.Context) error { return nil }

// Owner — прямое подключение владельцем схемы (подготовка данных, проверки в БД).
func Owner(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), OwnerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// Client — HTTP-клиент браузера: cookie и заголовок X-CSRF-Token.
type Client struct {
	t    *testing.T
	base string
	http *http.Client
	CSRF string
}

// NewClient создаёт клиента к серверу окружения.
func (e *Env) NewClient(t *testing.T) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{t: t, base: e.Server.URL + "/api/v1", http: &http.Client{Jar: jar}}
}

// Response — ответ: код и тело.
type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

// JSON разбирает тело в v.
func (r Response) JSON(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("тело ответа %d: %v: %s", r.Status, err, r.Body)
	}
}

// Code — поле code из problem+json.
func (r Response) Code() string {
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(r.Body, &p)
	return p.Code
}

// Do выполняет запрос; body — значение для JSON или nil.
func (c *Client) Do(method, path string, body any) Response {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.CSRF != "" {
		req.Header.Set("X-CSRF-Token", c.CSRF)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := Response{Status: resp.StatusCode, Body: b, Header: resp.Header}
	// Запоминаем CSRF-токен из ответов входа.
	if resp.StatusCode == http.StatusOK && strings.HasPrefix(path, "/auth/") {
		var a struct {
			CsrfToken string `json:"csrfToken"`
		}
		if json.Unmarshal(b, &a) == nil && a.CsrfToken != "" {
			c.CSRF = a.CsrfToken
		}
	}
	return r
}

// Login входит и запоминает CSRF-токен; падает при ошибке.
func (c *Client) Login(email, password string) {
	c.t.Helper()
	r := c.Do(http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password})
	if r.Status != http.StatusOK {
		c.t.Fatalf("вход %s: %d %s", email, r.Status, r.Body)
	}
}

// TokenFromLink — токен из ссылки приглашения или восстановления (последний сегмент пути).
func TokenFromLink(link string) string {
	return link[strings.LastIndex(link, "/")+1:]
}

// ActiveOwner создаёт собственника через приглашение и принимает его; возвращает клиента с открытой сессией.
func (e *Env) ActiveOwner(t *testing.T, email, password string) *Client {
	t.Helper()
	_, link, err := e.App.Users.CreateOwner(context.Background(), email, "Собственник")
	if err != nil {
		t.Fatal(err)
	}
	c := e.NewClient(t)
	r := c.Do(http.MethodPost, "/auth/invite/accept", map[string]any{"token": TokenFromLink(link), "password": password})
	if r.Status != http.StatusOK {
		t.Fatalf("принятие приглашения: %d %s", r.Status, r.Body)
	}
	return c
}

// ActiveTenant создаёт нанимателя-физлицо и его активную учётную запись (ознакомлен с политикой версии 1.0).
func (e *Env) ActiveTenant(t *testing.T, email, password string) *Client {
	t.Helper()
	ctx := context.Background()
	conn := Owner(t)
	hash, err := crypto.HashPassword(password, FastArgon)
	if err != nil {
		t.Fatal(err)
	}
	var tenantID string
	if err := conn.QueryRow(ctx, `INSERT INTO tenants (kind, last_name, first_name, email) VALUES ('individual','Тестов','Тест',$1) RETURNING id`, email).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, password_hash, role, status, tenant_id, display_name, policy_accepted_version)
		VALUES ($1, $2, 'tenant', 'active', $3, 'Тест', '1.0')`, email, hash, tenantID); err != nil {
		t.Fatal(err)
	}
	c := e.NewClient(t)
	c.Login(email, password)
	return c
}

// tWriter — журнал приложения в выводе теста (видно при падении или -v).
type tWriter struct{ t *testing.T }

func (w tWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// Upload — загрузка файла POST /files (multipart: file, category).
func (c *Client) Upload(category, name string, data []byte) Response {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("category", category)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		c.t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = w.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base+"/files", &body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-CSRF-Token", c.CSRF)
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return Response{Status: resp.StatusCode, Body: b, Header: resp.Header}
}

// PNG — минимальное изображение PNG для тестов.
var PNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// ID — поле id из ответа.
func (r Response) ID(t *testing.T) string {
	t.Helper()
	var v struct {
		ID string `json:"id"`
	}
	r.JSON(t, &v)
	if v.ID == "" {
		t.Fatalf("в ответе %d нет id: %s", r.Status, r.Body)
	}
	return v.ID
}
