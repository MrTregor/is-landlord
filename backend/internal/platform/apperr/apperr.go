// Package apperr — типизированные ошибки приложения и их перевод в ответ application/problem+json
// (формат — docs/spec/06 API.md, раздел «Ошибки»).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Коды ошибок API (поле code).
const (
	CodeBadRequest          = "bad_request"
	CodeUnauthorized        = "unauthorized"
	CodeInvalidCredentials  = "invalid_credentials"
	CodeForbidden           = "forbidden"
	CodeCSRFFailed          = "csrf_failed"
	CodeNotFound            = "not_found"
	CodeConflictState       = "conflict_state"
	CodeConflictOverlap     = "conflict_overlap"
	CodeConflictExists      = "conflict_exists"
	CodeFiscalReceiptExists = "fiscal_receipt_exists"
	CodeValidationFailed    = "validation_failed"
	CodeBusinessRule        = "business_rule"
	CodeRateLimited         = "rate_limited"
	CodeLoginLocked         = "login_locked"
	CodeBadGateway          = "bad_gateway"
	CodeNotImplemented      = "not_implemented"
	CodeInternal            = "internal"
)

// FieldError — ошибка конкретного поля формы.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error — ошибка приложения с HTTP-статусом, кодом и текстом для пользователя.
type Error struct {
	Status     int
	Code       string
	Title      string
	Detail     string
	Fields     []FieldError
	RetryAfter time.Duration
	cause      error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Detail, e.cause)
	}
	return e.Code + ": " + e.Detail
}

func (e *Error) Unwrap() error { return e.cause }

// Wrap прикрепляет к ошибке исходную причину (пишется только в журнал приложения).
func (e *Error) Wrap(cause error) *Error {
	c := *e
	c.cause = cause
	return &c
}

// RetryAfterSeconds — значение заголовка Retry-After.
func (e *Error) RetryAfterSeconds() string {
	sec := int(e.RetryAfter.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}
	return strconv.Itoa(sec)
}

func newErr(status int, code, title, detail string) *Error {
	return &Error{Status: status, Code: code, Title: title, Detail: detail}
}

// BadRequest — неверный JSON, неизвестное поле, ошибка проверки по OpenAPI.
func BadRequest(detail string) *Error {
	return newErr(http.StatusBadRequest, CodeBadRequest, "Неверный запрос", detail)
}

// Unauthorized — нет сессии или она истекла.
func Unauthorized() *Error {
	return newErr(http.StatusUnauthorized, CodeUnauthorized, "Требуется вход", "Войдите в систему")
}

// InvalidCredentials — неверный e-mail или пароль.
func InvalidCredentials() *Error {
	return newErr(http.StatusUnauthorized, CodeInvalidCredentials, "Вход не выполнен", "Неверный e-mail или пароль")
}

// Forbidden — нет прав на действие.
func Forbidden() *Error {
	return newErr(http.StatusForbidden, CodeForbidden, "Доступ запрещён", "Недостаточно прав для этого действия")
}

// CSRFFailed — нет или неверный заголовок X-CSRF-Token.
func CSRFFailed() *Error {
	return newErr(http.StatusForbidden, CodeCSRFFailed, "Доступ запрещён", "Сессия устарела. Обновите страницу")
}

// NotFound — запись не найдена (наниматель получает его и для чужих записей).
func NotFound() *Error {
	return newErr(http.StatusNotFound, CodeNotFound, "Не найдено", "Запись не найдена")
}

// NotFoundDetail — запись не найдена с уточняющим текстом.
func NotFoundDetail(detail string) *Error {
	return newErr(http.StatusNotFound, CodeNotFound, "Не найдено", detail)
}

// Conflict — конфликт состояния; code — один из conflict_state, conflict_overlap, conflict_exists,
// fiscal_receipt_exists.
func Conflict(code, detail string) *Error {
	return newErr(http.StatusConflict, code, "Действие невозможно", detail)
}

// Validation — ошибки полей формы.
func Validation(fields ...FieldError) *Error {
	e := newErr(http.StatusUnprocessableEntity, CodeValidationFailed, "Проверьте заполнение полей", "")
	e.Fields = fields
	if len(fields) > 0 {
		e.Detail = fields[0].Message
	}
	return e
}

// BusinessRule — нарушение бизнес-правила; detail — текст из 05 Бизнес-правила и расчёты.
func BusinessRule(detail string) *Error {
	return newErr(http.StatusUnprocessableEntity, CodeBusinessRule, "Действие невозможно", detail)
}

// TooManyRequests — частые запросы (rate_limited) или блокировка входа (login_locked).
func TooManyRequests(code, detail string, retryAfter time.Duration) *Error {
	e := newErr(http.StatusTooManyRequests, code, "Слишком много запросов", detail)
	e.RetryAfter = retryAfter
	return e
}

// BadGateway — внешний сервис недоступен.
func BadGateway(detail string) *Error {
	return newErr(http.StatusBadGateway, CodeBadGateway, "Внешний сервис недоступен", detail)
}

// NotImplemented — метод описан в API, но ещё не реализован (заглушки этапа M0).
func NotImplemented() *Error {
	return newErr(http.StatusNotImplemented, CodeNotImplemented, "Не реализовано", "Метод ещё не реализован")
}

// Internal — непредвиденная ошибка; текст для клиента формируется при записи ответа.
func Internal(cause error) *Error {
	e := newErr(http.StatusInternalServerError, CodeInternal, "Внутренняя ошибка сервера", "")
	e.cause = cause
	return e
}

// From приводит любую ошибку к *Error; неизвестные ошибки становятся Internal.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal(err)
}

// Is сообщает, является ли err ошибкой приложения с указанным кодом.
func Is(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// WithDetail — та же ошибка с другим текстом для пользователя.
func (e *Error) WithDetail(detail string) *Error {
	c := *e
	c.Detail = detail
	return &c
}
