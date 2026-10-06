package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
)

type fakeCheck struct{ err error }

func (f fakeCheck) Ping(context.Context) error   { return f.err }
func (f fakeCheck) Health(context.Context) error { return f.err }

func newTestServer(dbErr, gotenbergErr error) *Server {
	loc, _ := time.LoadLocation("Europe/Moscow")
	s := NewServer(Deps{
		Config:    &config.Config{AppEnv: config.EnvDev, AppName: "Наймодатель"},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:     clock.NewFixed(time.Date(2026, 10, 16, 16, 29, 0, 0, time.UTC), loc),
		Gotenberg: fakeCheck{err: gotenbergErr},
	})
	s.dbPinger = fakeCheck{err: dbErr}
	return s
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
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
	for _, path := range []string{"/api/v1/properties", "/api/v1/dashboard", "/api/v1/cabinet/summary"} {
		rec := do(t, h, http.MethodGet, path)
		assert.Equal(t, http.StatusNotImplemented, rec.Code, path)
		assert.Equal(t, "application/problem+json; charset=utf-8", rec.Header().Get("Content-Type"))
		var p apperr.Problem
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
		assert.Equal(t, apperr.CodeNotImplemented, p.Code)
		assert.Equal(t, rec.Header().Get("X-Request-Id"), p.RequestID)
	}
}

func TestUnknownPathIsProblem404(t *testing.T) {
	rec := do(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/no-such-path")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	var p apperr.Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, apperr.CodeNotFound, p.Code)
}

func TestInvalidPathParamIsBadRequest(t *testing.T) {
	rec := do(t, newTestServer(nil, nil).Handler(), http.MethodGet, "/api/v1/properties/not-a-uuid")
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
