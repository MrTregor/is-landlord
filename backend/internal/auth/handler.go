package auth

import (
	"context"
	"net/http"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
)

// CookieName — cookie сессии (06 API, раздел 1).
const CookieName = "ll_session"

// Handler — методы API входа и учётной записи.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// sessionCookie — HttpOnly, Secure (кроме dev), SameSite=Strict, Path=/, без Expires: срок контролирует сервер.
func (h *Handler) sessionCookie(value string, remove bool) *string {
	c := &http.Cookie{ //nolint:gosec // Secure задаётся COOKIE_SECURE (false только в dev, проверяется config)
		Name: CookieName, Value: value, Path: "/", HttpOnly: true, Secure: h.svc.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	if remove {
		c.MaxAge = -1
	}
	s := c.String()
	return &s
}

func (h *Handler) authResponse(ctx context.Context, u sqlc.User, csrf string) (api.AuthResponse, error) {
	must, version, err := h.svc.MustAcceptPolicy(ctx, u)
	if err != nil {
		return api.AuthResponse{}, err
	}
	return api.AuthResponse{User: UserDTO(u), CsrfToken: csrf, MustAcceptPolicy: must, PolicyVersion: version}, nil
}

// Login — вход.
func (h *Handler) Login(ctx context.Context, req api.LoginRequestObject) (api.LoginResponseObject, error) {
	sess, err := h.svc.Login(ctx, string(req.Body.Email), req.Body.Password)
	if err != nil {
		return nil, err
	}
	body, err := h.authResponse(ctx, sess.User, sess.CSRF)
	if err != nil {
		return nil, err
	}
	return api.Login200JSONResponse{Body: body, Headers: api.Login200ResponseHeaders{SetCookie: h.sessionCookie(sess.Token, false)}}, nil
}

// Logout — выход: сессия отзывается, cookie удаляется.
func (h *Handler) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	if err := h.svc.Logout(ctx); err != nil {
		return nil, err
	}
	return api.Logout204Response{Headers: api.Logout204ResponseHeaders{SetCookie: h.sessionCookie("", true)}}, nil
}

// GetMe — текущий пользователь и CSRF-токен сессии.
func (h *Handler) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return nil, apperr.Unauthorized()
	}
	u, err := h.svc.Me(ctx)
	if err != nil {
		return nil, err
	}
	body, err := h.authResponse(ctx, u, a.CSRFToken)
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(body), nil
}

// ChangePassword — смена пароля.
func (h *Handler) ChangePassword(ctx context.Context, req api.ChangePasswordRequestObject) (api.ChangePasswordResponseObject, error) {
	if err := h.svc.ChangePassword(ctx, req.Body.CurrentPassword, req.Body.NewPassword); err != nil {
		return nil, err
	}
	return api.ChangePassword204Response{}, nil
}

// ForgotPassword — запрос восстановления; всегда 204.
func (h *Handler) ForgotPassword(ctx context.Context, req api.ForgotPasswordRequestObject) (api.ForgotPasswordResponseObject, error) {
	if err := h.svc.ForgotPassword(ctx, string(req.Body.Email)); err != nil {
		return nil, err
	}
	return api.ForgotPassword204Response{}, nil
}

// ResetPassword — новый пароль по ссылке.
func (h *Handler) ResetPassword(ctx context.Context, req api.ResetPasswordRequestObject) (api.ResetPasswordResponseObject, error) {
	if err := h.svc.ResetPassword(ctx, req.Body.Token, req.Body.NewPassword); err != nil {
		return nil, err
	}
	return api.ResetPassword204Response{}, nil
}

// GetInvite — сведения о приглашении.
func (h *Handler) GetInvite(ctx context.Context, req api.GetInviteRequestObject) (api.GetInviteResponseObject, error) {
	u, err := h.svc.GetInvite(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return api.GetInvite200JSONResponse{Email: u.Email, DisplayName: u.DisplayName, Role: api.UserRole(u.Role)}, nil
}

// AcceptInvite — принять приглашение, задать пароль и войти.
func (h *Handler) AcceptInvite(ctx context.Context, req api.AcceptInviteRequestObject) (api.AcceptInviteResponseObject, error) {
	sess, err := h.svc.AcceptInvite(ctx, req.Body.Token, req.Body.Password, req.Body.AcceptPolicyVersion)
	if err != nil {
		return nil, err
	}
	body, err := h.authResponse(ctx, sess.User, sess.CSRF)
	if err != nil {
		return nil, err
	}
	return api.AcceptInvite200JSONResponse{Body: body, Headers: api.AcceptInvite200ResponseHeaders{SetCookie: h.sessionCookie(sess.Token, false)}}, nil
}

// AcceptPolicy — ознакомление нанимателя с текущей версией политики.
func (h *Handler) AcceptPolicy(ctx context.Context, req api.AcceptPolicyRequestObject) (api.AcceptPolicyResponseObject, error) {
	if err := h.svc.AcceptPolicy(ctx, req.Body.Version); err != nil {
		return nil, err
	}
	return api.AcceptPolicy204Response{}, nil
}

// UserDTO — пользователь для API.
func UserDTO(u sqlc.User) api.UserDTO {
	return api.UserDTO{
		Id: u.ID, Email: u.Email, Role: api.UserRole(u.Role), Status: api.UserStatus(u.Status),
		DisplayName: u.DisplayName, TenantId: u.TenantID, LastLoginAt: u.LastLoginAt,
		CreatedAt: u.CreatedAt,
	}
}
