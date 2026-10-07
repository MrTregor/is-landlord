package apperr

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ProblemTypeBase — префикс поля type в ответе problem+json.
const ProblemTypeBase = "https://landlord.local/errors/"

// Problem — тело ответа application/problem+json.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Code      string       `json:"code"`
	Detail    string       `json:"detail,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
	RequestID string       `json:"requestId,omitempty"`
}

// ToProblem строит тело ответа. Для внутренних ошибок клиент получает только код запроса.
func ToProblem(e *Error, requestID string) Problem {
	detail := e.Detail
	if e.Code == CodeInternal {
		detail = "Внутренняя ошибка сервера. Код запроса: " + requestID
	}
	return Problem{
		Type:      ProblemTypeBase + e.Code,
		Title:     e.Title,
		Status:    e.Status,
		Code:      e.Code,
		Detail:    detail,
		Errors:    e.Fields,
		RequestID: requestID,
	}
}

// Write пишет ошибку в ответ. Внутренние ошибки пишутся в журнал приложения полностью
// (без персональных данных: в ошибки не попадают значения полей, только идентификаторы).
func Write(w http.ResponseWriter, r *http.Request, err error, requestID string, log *slog.Logger) {
	e := From(err)
	if e.Status >= http.StatusInternalServerError && e.Code != CodeNotImplemented && log != nil {
		log.ErrorContext(r.Context(), "ошибка обработки запроса",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()))
	}
	if e.Status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", e.RetryAfterSeconds())
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(ToProblem(e, requestID))
}
