// Package user — учётные записи собственников (экран «Настройки → Пользователи», FR-14.1).
package user

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
)

// Service — пользователи.
type Service struct {
	db    *db.DB
	clock clock.Clock
	audit *audit.Service
	auth  *auth.Service
}

// NewService создаёт сервис пользователей.
func NewService(database *db.DB, clk clock.Clock, au *audit.Service, as *auth.Service) *Service {
	return &Service{db: database, clock: clk, audit: au, auth: as}
}

// Filter — фильтры списка.
type Filter struct {
	Role, Status, Q *string
	Limit, Offset   int32
}

// List — пользователи и их общее число.
func (s *Service) List(ctx context.Context, f Filter) ([]sqlc.User, int64, error) {
	q := s.db.Queries()
	rows, err := q.ListUsers(ctx, sqlc.ListUsersParams{Role: f.Role, Status: f.Status, Q: f.Q, Lim: f.Limit, Off: f.Offset})
	if err != nil {
		return nil, 0, err
	}
	total, err := q.CountUsers(ctx, sqlc.CountUsersParams{Role: f.Role, Status: f.Status, Q: f.Q})
	return rows, total, err
}

// CreateOwner создаёт собственника в статусе invited и отправляет приглашение. Возвращает пользователя и ссылку.
func (s *Service) CreateOwner(ctx context.Context, email, displayName string) (sqlc.User, string, error) {
	email = auth.NormalizeEmail(email)
	displayName = strings.TrimSpace(displayName)
	var u sqlc.User
	var link string
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		u, err = tx.Q.CreateUser(ctx, sqlc.CreateUserParams{
			Email: email, Role: reqctx.RoleOwner, DisplayName: displayName, Now: s.clock.Now(),
		})
		if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
			return apperr.Conflict(apperr.CodeConflictExists, "Пользователь с таким e-mail уже есть")
		}
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionUserCreate, EntityType: "user", EntityID: u.ID.String(),
			Summary: "Добавлен собственник", After: userSnapshot(u),
		}); err != nil {
			return err
		}
		link, err = s.auth.Invite(ctx, tx, u)
		return err
	})
	return u, link, err
}

// Update меняет имя или статус (active/blocked). Себя заблокировать нельзя; блокировка отзывает сессии.
func (s *Service) Update(ctx context.Context, id uuid.UUID, displayName, status *string) (sqlc.User, error) {
	var out sqlc.User
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		u, err := tx.Q.GetUserByID(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		name, st := u.DisplayName, u.Status
		if displayName != nil {
			if n := strings.TrimSpace(*displayName); n != "" {
				name = n
			}
		}
		if status != nil && *status != u.Status {
			if u.Status == auth.StatusInvited {
				return apperr.Conflict(apperr.CodeConflictState, "Пользователь ещё не принял приглашение")
			}
			if a := reqctx.ActorFrom(ctx); a != nil && a.UserID == id && *status == auth.StatusBlocked {
				return apperr.BusinessRule("Нельзя заблокировать собственную учётную запись")
			}
			st = *status
		}
		out, err = tx.Q.UpdateUser(ctx, sqlc.UpdateUserParams{ID: id, DisplayName: name, Status: st})
		if err != nil {
			return err
		}
		action, summary := audit.ActionUserUpdate, "Изменён пользователь"
		switch {
		case st == auth.StatusBlocked && u.Status != auth.StatusBlocked:
			action, summary = audit.ActionUserBlock, "Пользователь заблокирован"
			if err := s.auth.RevokeAllSessions(ctx, tx, id); err != nil {
				return err
			}
		case st == auth.StatusActive && u.Status == auth.StatusBlocked:
			summary = "Пользователь разблокирован"
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: action, EntityType: "user", EntityID: id.String(), Summary: summary,
			Before: userSnapshot(u), After: userSnapshot(out),
		})
	})
	return out, err
}

// ResendInvite — новое приглашение (старое становится недействительным).
func (s *Service) ResendInvite(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		u, err := tx.Q.GetUserByID(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if u.Status != auth.StatusInvited {
			return apperr.Conflict(apperr.CodeConflictState, "Приглашение уже принято")
		}
		_, err = s.auth.Invite(ctx, tx, u)
		return err
	})
}

// userSnapshot — поля пользователя для журнала (ПДн маскируются при записи).
func userSnapshot(u sqlc.User) map[string]any {
	return map[string]any{"email": u.Email, "displayName": u.DisplayName, "role": u.Role, "status": u.Status}
}
