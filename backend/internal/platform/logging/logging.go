// Package logging — журнал приложения в формате JSON (log/slog).
// Персональные данные (ФИО, паспорт, адрес, телефон, e-mail, дата рождения) в журнал не пишутся никогда:
// в записях — только идентификаторы.
package logging

import (
	"context"
	"io"
	"log/slog"
)

type ctxKey struct{}

// New создаёт JSON-журнал с полем service.
func New(w io.Writer, level slog.Level, service string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h).With(slog.String("service", service))
}

// With кладёт журнал с полями запроса в контекст.
func With(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From возвращает журнал из контекста или fallback.
func From(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return fallback
}
