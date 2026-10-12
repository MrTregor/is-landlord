package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/reqctx"
)

type fakeCheck struct{ err error }

// fakeAuth: cookie «owner» — собственник, «tenant» — наниматель (CSRF «csrf»), иначе — нет сессии.
type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, token string) (*reqctx.Actor, error) {
	v := "1.0"
	switch token {
	case "owner":
		return &reqctx.Actor{UserID: uuid.New(), SessionID: uuid.New(), Role: reqctx.RoleOwner, CSRFToken: "csrf"}, nil
	case "tenant":
		return &reqctx.Actor{UserID: uuid.New(), SessionID: uuid.New(), Role: reqctx.RoleTenant, CSRFToken: "csrf", PolicyAcceptedVersion: &v}, nil
	}
	return nil, nil
}

type fakePolicy struct{}

func (fakePolicy) CurrentPolicyVersion(context.Context) (string, error) { return "1.0", nil }

func (f fakeCheck) Ping(context.Context) error   { return f.err }
func (f fakeCheck) Health(context.Context) error { return f.err }

func newTestServer(dbErr, gotenbergErr error) *Server {
	loc, _ := time.LoadLocation("Europe/Moscow")
	s := NewServer(Deps{
		Config:    &config.Config{AppEnv: config.EnvDev, AppName: "Наймодатель"},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock.NewFixed(time.Date(2026, 10, 16, 16, 29, 0, 0, time.UTC), loc),
		Gotenberg: fakeCheck{err: gotenbergErr},
		Auth:      fakeAuth{},
		Policy:    fakePolicy{},
	})
	s.dbPinger = fakeCheck{err: dbErr}
	return s
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doAs(t, h, method, path, "", "")
}

// doAs — запрос с cookie сессии и заголовком X-CSRF-Token.
func doAs(t *testing.T, h http.Handler, method, path, session, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "ll_session", Value: session})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := do(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/healthz")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get("X-Request-Id"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

func TestReadyz(t *testing.T) {
	rec := do(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/readyz")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok","database":"ok","gotenberg":"ok"}`, rec.Body.String())

	rec = do(t, newTestServer(nil, errors.New("нет связи")).Handler(), http.MethodGet, "/api/v1/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"unavailable","database":"ok","gotenberg":"error"}`, rec.Body.String())

	rec = do(t, newTestServer(errors.New("нет связи"), nil).Handler(), http.MethodGet, "/api/v1/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// Заглушки этапа M0: каждый описанный в API метод отвечает 501 в формате problem+json.
func TestStubsReturnNotImplemented(t *testing.T) {
	h := newTestServer(nil, nil).Handler()
	for _, path := range []string{"/api/v1/fiscal-receipts", "/api/v1/dashboard", "/api/v1/cabinet/summary"} {
		rec := doAs(t, h, http.MethodGet, path, sessionFor(path), "")
		assert.Equal(t, http.StatusNotImplemented, rec.Code, path)
		assert.Equal(t, "application/problem+json; charset=utf-8", rec.Header().Get("Content-Type"))
		var p apperr.Problem
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
		assert.Equal(t, apperr.CodeNotImplemented, p.Code)
		assert.Equal(t, rec.Header().Get("X-Request-Id"), p.RequestID)
	}
}

func TestUnknownPathIsProblem404(t *testing.T) {
	rec := doAs(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/no-such-path", "owner", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var p apperr.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, apperr.CodeNotFound, p.Code)
}

func TestInvalidPathParamIsBadRequest(t *testing.T) {
	rec := doAs(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/properties/not-a-uuid", "owner", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRecoverWritesProblem(t *testing.T) {
	h := Recover(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("сбой")
	}))
	rec := do(t, RequestID(h), http.MethodGet, "/")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Внутренняя ошибка сервера. Код запроса: ")
}

func sessionFor(path string) string {
	if len(path) > len("/api/v1/cabinet") && path[:len("/api/v1/cabinet")] == "/api/v1/cabinet" {
		return "tenant"
	}
	return "owner"
}

// Права по ролям проверяются промежуточным обработчиком (NFR-Sec-02, 12 Безопасность и ПДн, раздел 3).
func TestRoleAccess_NFR_Sec_02(t *testing.T) {
	h := newTestServer(nil, nil).Handler()
	cases := []struct {
		name, method, path, session, csrf string
		want                              int
	}{
		{"без входа — 401", http.MethodGet, "/api/v1/properties", "", "", http.StatusUnauthorized},
		{"без входа кабинет — 401", http.MethodGet, "/api/v1/cabinet/summary", "", "", http.StatusUnauthorized},
		{"наниматель на методе собственника — 403", http.MethodGet, "/api/v1/properties", "tenant", "", http.StatusForbidden},
		{"собственник в кабинете — 403", http.MethodGet, "/api/v1/cabinet/summary", "owner", "", http.StatusForbidden},
		{"наниматель — политика только своя роль", http.MethodPost, "/api/v1/auth/accept-policy", "owner", "csrf", http.StatusForbidden},
		{"публичный метод без входа", http.MethodGet, "/api/v1/healthz", "", "", http.StatusOK},
		{"неизвестная сессия — 401", http.MethodGet, "/api/v1/dashboard", "unknown-session", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doAs(t, h, c.method, c.path, c.session, c.csrf)
			assert.Equal(t, c.want, rec.Code, rec.Body.String())
		})
	}
}

// Изменяющие методы требуют заголовок X-CSRF-Token (12 Безопасность и ПДн, раздел 2).
func TestCSRF_NFR_Sec_01(t *testing.T) {
	h := newTestServer(nil, nil).Handler()
	rec := doAs(t, h, http.MethodPost, "/api/v1/properties", "owner", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	var p apperr.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, apperr.CodeCSRFFailed, p.Code)

	rec = doAs(t, h, http.MethodPost, "/api/v1/properties", "owner", "неверный")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// С верным токеном запрос доходит до обработчика (заглушка или проверка тела).
	rec = doAs(t, h, http.MethodPost, "/api/v1/properties", "owner", "csrf")
	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}

func TestClassify(t *testing.T) {
	assert.Equal(t, accessPublic, classify("/auth/login"))
	assert.Equal(t, accessPublic, classify("/auth/invite/abc"))
	assert.Equal(t, accessPublic, classify("/integrations/tbank/notify"))
	assert.Equal(t, accessAny, classify("/auth/me"))
	assert.Equal(t, accessAny, classify("/files/1"))
	assert.Equal(t, accessTenant, classify("/cabinet/summary"))
	assert.Equal(t, accessOwner, classify("/settings/organization"))
	assert.True(t, csrfExempt("/auth/password/reset"))
	assert.False(t, csrfExempt("/auth/logout"))
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:5555"
	assert.Equal(t, "10.0.0.5", clientIP(r).String())
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.7")
	assert.Equal(t, "203.0.113.7", clientIP(r).String())
}

// Тело и параметры запроса проверяются по OpenAPI: недопустимое значение — 400 bad_request с полем.
func TestOpenAPIValidation_NFR_Comp_02(t *testing.T) {
	h := newTestServer(nil, nil).Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/charges?status=неизвестный", nil)
	req.AddCookie(&http.Cookie{Name: "ll_session", Value: "owner"})
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var p apperr.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, apperr.CodeBadRequest, p.Code)
	assert.Contains(t, p.Detail, "status")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/properties/0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90/status",
		strings.NewReader(`{"status":"rented","since":"2026-10-16"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", "csrf")
	req.AddCookie(&http.Cookie{Name: "ll_session", Value: "owner"})
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, "Недопустимое значение поля «status»", p.Detail)
}
