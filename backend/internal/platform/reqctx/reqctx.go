// Package reqctx — сведения о запросе и пользователе, доступные обработчикам strict-server через context.
package reqctx

import (
	"context"
	"net/netip"

	"github.com/google/uuid"
)

// Роли.
const (
	RoleOwner  = "owner"
	RoleTenant = "tenant"
	RoleSystem = "system"
)

// Meta — сведения о HTTP-запросе.
type Meta struct {
	RequestID string
	IP        *netip.Addr
	UserAgent string
	// SessionToken — значение cookie ll_session (для выхода и смены пароля).
	SessionToken string
}

// Actor — вошедший пользователь.
type Actor struct {
	UserID      uuid.UUID
	Role        string
	TenantID    *uuid.UUID
	SessionID   uuid.UUID
	CSRFToken   string
	DisplayName string
	Email       string
	// PolicyAcceptedVersion — версия политики ПДн, с которой ознакомлен пользователь.
	PolicyAcceptedVersion *string
}

type metaKey struct{}
type actorKey struct{}

// WithMeta кладёт сведения о запросе в контекст.
func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

// MetaFrom возвращает сведения о запросе (пустые вне HTTP-запроса).
func MetaFrom(ctx context.Context) Meta {
	m, _ := ctx.Value(metaKey{}).(Meta)
	return m
}

// WithActor кладёт пользователя в контекст.
func WithActor(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom возвращает пользователя или nil (без входа, плановые задачи).
func ActorFrom(ctx context.Context) *Actor {
	a, _ := ctx.Value(actorKey{}).(*Actor)
	return a
}
