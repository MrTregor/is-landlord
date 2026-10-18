// Package auth — вход, сессии, CSRF, приглашения, восстановление и смена пароля (NFR-Sec-01, FR-13.2).
package auth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/config"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/settings"
)

// Ограничения входа (12 Безопасность и ПДн, раздел 1).
const (
	LockWindow        = 15 * time.Minute
	MaxEmailFailures  = 5
	MaxIPFailures     = 30
	InviteTTL         = 7 * 24 * time.Hour
	ResetTTL          = time.Hour
	TouchEvery        = time.Minute
	PurposeInvite     = "invite"
	PurposeReset      = "password_reset"
	StatusActive      = "active"
	StatusInvited     = "invited"
	StatusBlocked     = "blocked"
	invalidTokenText  = "Приглашение недействительно или истекло"
	invalidResetText  = "Ссылка для восстановления пароля недействительна или истекла. Запросите её заново"
	policyNotAccepted = "Подтвердите ознакомление с Политикой обработки персональных данных"
)

// Service — вход и учётные записи.
type Service struct {
	db       *db.DB
	cfg      *config.Config
	clock    clock.Clock
	audit    *audit.Service
	notify   *notify.Service
	settings *settings.Service
	argon    crypto.Argon2Params
	dummy    string
}

// NewService создаёт сервис входа. argon — параметры хеширования (в тестах можно ускорить).
func NewService(database *db.DB, cfg *config.Config, clk clock.Clock, au *audit.Service, nt *notify.Service,
	st *settings.Service, argon crypto.Argon2Params) *Service {
	dummy, err := crypto.HashPassword("выравнивание-времени-ответа-1", argon)
	if err != nil {
		panic(err)
	}
	return &Service{db: database, cfg: cfg, clock: clk, audit: au, notify: nt, settings: st, argon: argon, dummy: dummy}
}

// Session — открытая сессия: значение cookie и CSRF-токен.
type Session struct {
	Token string
	CSRF  string
	User  sqlc.User
}

func (s *Service) newSession(ctx context.Context, tx *db.Tx, u sqlc.User) (*Session, error) {
	token, csrf := crypto.NewToken(), crypto.NewToken()
	meta := reqctx.MetaFrom(ctx)
	now := s.clock.Now()
	p := sqlc.CreateSessionParams{
		UserID: u.ID, TokenHash: crypto.HashToken(token), CsrfToken: csrf, Now: now,
		ExpiresAt: now.Add(time.Duration(s.cfg.SessionAbsoluteDays) * 24 * time.Hour), Ip: meta.IP,
	}
	if meta.UserAgent != "" {
		ua := meta.UserAgent
		p.UserAgent = &ua
	}
	if _, err := tx.Q.CreateSession(ctx, p); err != nil {
		return nil, err
	}
	return &Session{Token: token, CSRF: csrf, User: u}, nil
}

func lockedError(until, now time.Time) error {
	minutes := int(math.Ceil(until.Sub(now).Minutes()))
	if minutes < 1 {
		minutes = 1
	}
	return apperr.TooManyRequests(apperr.CodeLoginLocked,
		fmt.Sprintf("Слишком много попыток входа. Повторите через %d мин.", minutes), until.Sub(now))
}

// Login — вход по e-mail и паролю. Неизвестный e-mail даёт тот же ответ, что и неверный пароль;
// хеш считается всегда, чтобы время ответа не выдавало наличие адреса.
func (s *Service) Login(ctx context.Context, email, password string) (*Session, error) {
	email = NormalizeEmail(email)
	now := s.clock.Now()
	since := now.Add(-LockWindow)
	q := s.db.Queries()
	meta := reqctx.MetaFrom(ctx)

	if meta.IP != nil {
		n, err := q.FailedAttemptsByIP(ctx, sqlc.FailedAttemptsByIPParams{Ip: meta.IP, At: since})
		if err != nil {
			return nil, err
		}
		if n >= MaxIPFailures {
			return nil, apperr.TooManyRequests(apperr.CodeRateLimited,
				"Слишком много попыток входа с вашего адреса. Повторите позже", LockWindow)
		}
	}
	fa, err := q.FailedAttemptsByEmail(ctx, sqlc.FailedAttemptsByEmailParams{Email: email, Since: since})
	if err != nil {
		return nil, err
	}
	if fa.Failures >= MaxEmailFailures {
		if until := fa.LastFailure.Add(LockWindow); until.After(now) {
			_, _, _ = crypto.VerifyPassword(password, s.dummy, s.argon)
			return nil, lockedError(until, now)
		}
	}

	user, err := q.GetUserByEmail(ctx, email)
	found := err == nil
	if err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	hash := s.dummy
	if found && user.PasswordHash != nil {
		hash = *user.PasswordHash
	}
	ok, rehash, err := crypto.VerifyPassword(password, hash, s.argon)
	if err != nil {
		return nil, err
	}
	ok = ok && found && user.PasswordHash != nil

	if found && ok {
		switch user.Status {
		case StatusBlocked:
			return nil, apperr.Forbidden().WithDetail("Учётная запись заблокирована")
		case StatusInvited:
			return nil, apperr.Forbidden().WithDetail("Примите приглашение по ссылке из письма")
		}
	}
	if found && !ok && user.Status == StatusInvited {
		return nil, apperr.Forbidden().WithDetail("Примите приглашение по ссылке из письма")
	}

	if !ok {
		err := s.db.WithTx(ctx, func(tx *db.Tx) error {
			if err := tx.Q.InsertLoginAttempt(ctx, sqlc.InsertLoginAttemptParams{Email: email, Ip: meta.IP, Success: false, At: now}); err != nil {
				return err
			}
			if !found {
				return nil
			}
			failures := fa.Failures + 1
			p := sqlc.RecordLoginFailureParams{ID: user.ID, FailedLoginCount: failures}
			if failures >= MaxEmailFailures {
				until := now.Add(LockWindow)
				p.LockedUntil = &until
			}
			if err := tx.Q.RecordLoginFailure(ctx, p); err != nil {
				return err
			}
			if p.LockedUntil == nil {
				return nil
			}
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: audit.ActionAuthLocked, EntityType: "user", EntityID: user.ID.String(),
				Summary:     fmt.Sprintf("Вход заблокирован на 15 минут после %d неудачных попыток", failures),
				ActorUserID: &user.ID, ActorRole: user.Role,
			})
		})
		if err != nil {
			return nil, err
		}
		return nil, apperr.InvalidCredentials()
	}

	var sess *Session
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.InsertLoginAttempt(ctx, sqlc.InsertLoginAttemptParams{Email: email, Ip: meta.IP, Success: true, At: now}); err != nil {
			return err
		}
		if err := tx.Q.RecordLoginSuccess(ctx, sqlc.RecordLoginSuccessParams{ID: user.ID, LastLoginAt: &now}); err != nil {
			return err
		}
		if rehash {
			h, err := crypto.HashPassword(password, s.argon)
			if err != nil {
				return err
			}
			if err := tx.Q.SetUserPasswordHash(ctx, sqlc.SetUserPasswordHashParams{ID: user.ID, PasswordHash: &h}); err != nil {
				return err
			}
		}
		var err error
		if sess, err = s.newSession(ctx, tx, user); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionAuthLogin, EntityType: "user", EntityID: user.ID.String(),
			Summary: "Вход в систему", ActorUserID: &user.ID, ActorRole: user.Role,
		})
	})
	if err != nil {
		return nil, err
	}
	sess.User.LastLoginAt = &now
	return sess, nil
}

// Authenticate проверяет cookie сессии: не отозвана, не истекла (простой и абсолютный срок),
// пользователь активен. Возвращает nil без ошибки, если сессии нет.
func (s *Service) Authenticate(ctx context.Context, token string) (*reqctx.Actor, error) {
	if token == "" {
		return nil, nil
	}
	q := s.db.Queries()
	row, err := q.GetSessionByTokenHash(ctx, crypto.HashToken(token))
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	idle := time.Duration(s.cfg.SessionIdleHours) * time.Hour
	if row.RevokedAt != nil || !now.Before(row.ExpiresAt) || !now.Before(row.LastSeenAt.Add(idle)) ||
		row.User.Status != StatusActive {
		return nil, nil
	}
	if now.Sub(row.LastSeenAt) >= TouchEvery {
		if err := q.TouchSession(ctx, sqlc.TouchSessionParams{ID: row.SessionID, LastSeenAt: now}); err != nil {
			return nil, err
		}
	}
	u := row.User
	return &reqctx.Actor{
		UserID: u.ID, Role: u.Role, TenantID: u.TenantID, SessionID: row.SessionID, CSRFToken: row.CsrfToken,
		DisplayName: u.DisplayName, Email: u.Email, PolicyAcceptedVersion: u.PolicyAcceptedVersion,
	}, nil
}

// Logout отзывает текущую сессию.
func (s *Service) Logout(ctx context.Context) error {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return nil
	}
	now := s.clock.Now()
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.RevokeSession(ctx, sqlc.RevokeSessionParams{ID: a.SessionID, RevokedAt: &now}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionAuthLogout, EntityType: "user", EntityID: a.UserID.String(), Summary: "Выход из системы",
		})
	})
}

// Me — текущий пользователь (свежие данные из БД).
func (s *Service) Me(ctx context.Context) (sqlc.User, error) {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return sqlc.User{}, apperr.Unauthorized()
	}
	return s.db.Queries().GetUserByID(ctx, a.UserID)
}

// MustAcceptPolicy — наниматель не ознакомлен с текущей версией политики ПДн.
func (s *Service) MustAcceptPolicy(ctx context.Context, u sqlc.User) (bool, string, error) {
	version, err := s.settings.PolicyVersion(ctx, s.db.Queries())
	if err != nil {
		return false, "", err
	}
	must := u.Role == reqctx.RoleTenant && (u.PolicyAcceptedVersion == nil || *u.PolicyAcceptedVersion != version)
	return must, version, nil
}

// ChangePassword — смена пароля; все прочие сессии пользователя отзываются.
func (s *Service) ChangePassword(ctx context.Context, current, next string) error {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return apperr.Unauthorized()
	}
	u, err := s.db.Queries().GetUserByID(ctx, a.UserID)
	if err != nil {
		return err
	}
	ok := false
	if u.PasswordHash != nil {
		if ok, _, err = crypto.VerifyPassword(current, *u.PasswordHash, s.argon); err != nil {
			return err
		}
	}
	if !ok {
		return apperr.Validation(apperr.FieldError{Field: "currentPassword", Message: "Текущий пароль указан неверно"})
	}
	if err := ValidatePassword("newPassword", next, u.Email); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(next, s.argon)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.SetUserPassword(ctx, sqlc.SetUserPasswordParams{ID: u.ID, PasswordHash: &hash, PasswordChangedAt: &now}); err != nil {
			return err
		}
		sid := a.SessionID
		if err := tx.Q.RevokeUserSessions(ctx, sqlc.RevokeUserSessionsParams{Now: &now, UserID: u.ID, ExceptID: &sid}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionAuthPasswordChanged, EntityType: "user", EntityID: u.ID.String(), Summary: "Пароль изменён",
		})
	})
}

// ForgotPassword — письмо со ссылкой восстановления (действует 1 час). Ответ не раскрывает наличие адреса.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = NormalizeEmail(email)
	u, err := s.db.Queries().GetUserByEmail(ctx, email)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if u.Status != StatusActive {
		return nil
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		token, tokenID, err := s.issueToken(ctx, tx, u.ID, PurposeReset, ResetTTL)
		if err != nil {
			return err
		}
		return s.notify.Queue(ctx, tx, notify.Email{
			Code: "password_reset", To: u.Email, DedupeKey: "password_reset:" + tokenID.String(),
			Vars:        map[string]string{"link": s.link("/reset-password/" + token)},
			RelatedType: "user", RelatedID: &u.ID,
		})
	})
}

// ResetPassword — новый пароль по ссылке; все сессии пользователя отзываются.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	row, err := s.validToken(ctx, token, PurposeReset)
	if err != nil {
		return apperr.BusinessRule(invalidResetText)
	}
	u := row.User
	if u.Status != StatusActive {
		return apperr.BusinessRule(invalidResetText)
	}
	if err := ValidatePassword("newPassword", password, u.Email); err != nil {
		return err
	}
	hash, err := crypto.HashPassword(password, s.argon)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.UseAuthToken(ctx, sqlc.UseAuthTokenParams{ID: row.TokenID, UsedAt: &now}); err != nil {
			return err
		}
		if err := tx.Q.SetUserPassword(ctx, sqlc.SetUserPasswordParams{ID: u.ID, PasswordHash: &hash, PasswordChangedAt: &now}); err != nil {
			return err
		}
		if err := tx.Q.RevokeUserSessions(ctx, sqlc.RevokeUserSessionsParams{Now: &now, UserID: u.ID}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionAuthPasswordReset, EntityType: "user", EntityID: u.ID.String(),
			Summary: "Пароль восстановлен по ссылке из письма", ActorUserID: &u.ID, ActorRole: u.Role,
		})
	})
}

// GetInvite — сведения о действующем приглашении.
func (s *Service) GetInvite(ctx context.Context, token string) (sqlc.User, error) {
	row, err := s.validToken(ctx, token, PurposeInvite)
	if err != nil || row.User.Status != StatusInvited {
		return sqlc.User{}, apperr.NotFoundDetail(invalidTokenText)
	}
	return row.User, nil
}

// AcceptInvite — задать пароль по приглашению и войти. Наниматель подтверждает ознакомление с политикой ПДн;
// согласие на обработку ПДн здесь не создаётся (12 Безопасность и ПДн, раздел 4).
func (s *Service) AcceptInvite(ctx context.Context, token, password string, policyVersion *string) (*Session, error) {
	row, err := s.validToken(ctx, token, PurposeInvite)
	if err != nil || row.User.Status != StatusInvited {
		return nil, apperr.NotFoundDetail(invalidTokenText)
	}
	u := row.User
	var accepted *string
	if u.Role == reqctx.RoleTenant {
		version, err := s.settings.PolicyVersion(ctx, s.db.Queries())
		if err != nil {
			return nil, err
		}
		if policyVersion == nil || *policyVersion != version {
			return nil, apperr.Validation(apperr.FieldError{Field: "acceptPolicyVersion", Message: policyNotAccepted})
		}
		accepted = &version
	}
	if err := ValidatePassword("password", password, u.Email); err != nil {
		return nil, err
	}
	hash, err := crypto.HashPassword(password, s.argon)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	var sess *Session
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.UseAuthToken(ctx, sqlc.UseAuthTokenParams{ID: row.TokenID, UsedAt: &now}); err != nil {
			return err
		}
		active, err := tx.Q.ActivateUser(ctx, sqlc.ActivateUserParams{ID: u.ID, PasswordHash: &hash, Now: &now, PolicyVersion: accepted})
		if err != nil {
			return err
		}
		if err := tx.Q.RecordLoginSuccess(ctx, sqlc.RecordLoginSuccessParams{ID: u.ID, LastLoginAt: &now}); err != nil {
			return err
		}
		active.LastLoginAt = &now
		if sess, err = s.newSession(ctx, tx, active); err != nil {
			return err
		}
		summary := "Приглашение принято, пароль задан"
		if accepted != nil {
			summary += ", ознакомление с политикой ПДн версии " + *accepted
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionUserUpdate, EntityType: "user", EntityID: u.ID.String(), Summary: summary,
			Before: map[string]any{"status": u.Status}, After: map[string]any{"status": active.Status},
			ActorUserID: &u.ID, ActorRole: u.Role,
		})
	})
	return sess, err
}

// AcceptPolicy — ознакомление нанимателя с текущей версией политики (ч. 2 ст. 18.1 152-ФЗ; это не согласие).
func (s *Service) AcceptPolicy(ctx context.Context, version string) error {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return apperr.Unauthorized()
	}
	current, err := s.settings.PolicyVersion(ctx, s.db.Queries())
	if err != nil {
		return err
	}
	if version != current {
		return apperr.Validation(apperr.FieldError{Field: "version", Message: "Политика обновлена. Обновите страницу и ознакомьтесь с новой версией"})
	}
	now := s.clock.Now()
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.AcceptPolicy(ctx, sqlc.AcceptPolicyParams{ID: a.UserID, PolicyAcceptedVersion: &version, PolicyAcceptedAt: &now}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: audit.ActionUserUpdate, EntityType: "user", EntityID: a.UserID.String(),
			Summary: "Ознакомление с политикой обработки ПДн версии " + version,
		})
	})
}

// Invite выдаёт приглашение пользователю в статусе invited (новый токен на 7 дней, прежние недействительны),
// ставит письмо invite и пишет user.invite в журнал. Возвращает ссылку приглашения.
func (s *Service) Invite(ctx context.Context, tx *db.Tx, u sqlc.User) (string, error) {
	token, tokenID, err := s.issueToken(ctx, tx, u.ID, PurposeInvite, InviteTTL)
	if err != nil {
		return "", err
	}
	link := s.link("/invite/" + token)
	err = s.notify.Queue(ctx, tx, notify.Email{
		Code: "invite", To: u.Email, DedupeKey: "invite:" + tokenID.String(),
		Vars:        map[string]string{"link": link, "display_name": u.DisplayName},
		RelatedType: "user", RelatedID: &u.ID,
	})
	if err != nil {
		return "", err
	}
	summary := "Отправлено приглашение собственнику"
	if u.Role == reqctx.RoleTenant {
		summary = "Отправлено приглашение нанимателю в личный кабинет"
	}
	err = s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: audit.ActionUserInvite, EntityType: "user", EntityID: u.ID.String(), Summary: summary,
	})
	return link, err
}

// RevokeAllSessions — отзыв всех сессий пользователя (блокировка, обезличивание).
func (s *Service) RevokeAllSessions(ctx context.Context, tx *db.Tx, userID uuid.UUID) error {
	now := s.clock.Now()
	return tx.Q.RevokeUserSessions(ctx, sqlc.RevokeUserSessionsParams{Now: &now, UserID: userID})
}

func (s *Service) issueToken(ctx context.Context, tx *db.Tx, userID uuid.UUID, purpose string, ttl time.Duration) (string, uuid.UUID, error) {
	now := s.clock.Now()
	if err := tx.Q.InvalidateAuthTokens(ctx, sqlc.InvalidateAuthTokensParams{UserID: userID, Purpose: purpose, UsedAt: &now}); err != nil {
		return "", uuid.Nil, err
	}
	token := crypto.NewToken()
	id, err := tx.Q.CreateAuthToken(ctx, sqlc.CreateAuthTokenParams{
		UserID: userID, Purpose: purpose, TokenHash: crypto.HashToken(token), ExpiresAt: now.Add(ttl), CreatedAt: now,
	})
	return token, id, err
}

func (s *Service) validToken(ctx context.Context, token, purpose string) (sqlc.GetAuthTokenRow, error) {
	row, err := s.db.Queries().GetAuthToken(ctx, sqlc.GetAuthTokenParams{TokenHash: crypto.HashToken(token), Purpose: purpose})
	if err != nil {
		return row, err
	}
	if row.UsedAt != nil || !s.clock.Now().Before(row.ExpiresAt) {
		return row, errors.New("токен использован или истёк")
	}
	return row, nil
}

func (s *Service) link(path string) string {
	return strings.TrimRight(s.cfg.AppPublicURL, "/") + path
}

// RandomPasswordHash — хеш случайного пароля (обезличенная учётная запись: войти по ней нельзя).
func (s *Service) RandomPasswordHash() (string, error) {
	return crypto.HashPassword(crypto.NewToken(), s.argon)
}
