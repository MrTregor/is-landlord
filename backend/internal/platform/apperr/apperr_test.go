package apperr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationProblem(t *testing.T) {
	e := Validation(FieldError{Field: "endDate", Message: "Дата окончания должна быть позже даты начала"})
	p := ToProblem(e, "req-1")
	assert.Equal(t, 422, p.Status)
	assert.Equal(t, "validation_failed", p.Code)
	assert.Equal(t, "https://landlord.local/errors/validation_failed", p.Type)
	assert.Equal(t, "Проверьте заполнение полей", p.Title)
	assert.Equal(t, "Дата окончания должна быть позже даты начала", p.Detail)
	require.Len(t, p.Errors, 1)
	assert.Equal(t, "endDate", p.Errors[0].Field)
}

func TestFromWrapsUnknownAsInternal(t *testing.T) {
	e := From(errors.New("сбой соединения"))
	assert.Equal(t, http.StatusInternalServerError, e.Status)
	assert.Equal(t, CodeInternal, e.Code)

	wrapped := fmt.Errorf("слой сервиса: %w", NotFound())
	assert.Equal(t, CodeNotFound, From(wrapped).Code)
	assert.True(t, Is(wrapped, CodeNotFound))
}

func TestWriteInternalHidesDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	Write(rec, req, errors.New("секретная подробность"), "01J-test", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json; charset=utf-8", rec.Header().Get("Content-Type"))
	var p Problem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &p))
	assert.Equal(t, "Внутренняя ошибка сервера. Код запроса: 01J-test", p.Detail)
	assert.NotContains(t, rec.Body.String(), "секретная")
}

func TestWriteTooManyRequestsSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	Write(rec, req, TooManyRequests(CodeLoginLocked, "Слишком много попыток входа. Повторите через 15 мин.", 15*time.Minute), "r", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "900", rec.Header().Get("Retry-After"))
}
