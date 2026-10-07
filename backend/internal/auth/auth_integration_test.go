//go:build integration

package auth_test

import (
	"context"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

type authResp struct {
	CsrfToken        string `json:"csrfToken"`
	MustAcceptPolicy bool   `json:"mustAcceptPolicy"`
	PolicyVersion    string `json:"policyVersion"`
	User             struct {
		ID     string `json:"id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
		Status string `json:"status"`
	} `json:"user"`
}

func countAudit(t *testing.T, action, entityID string) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND entity_id = $2`, action, entityID).Scan(&n))
	return n
}

// Приглашение собственника, вход, выход (FR-14.1, FR-13.2).
func TestOwnerInviteLoginLogout_FR14_1(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	ctx := context.Background()
	u, link, err := env.App.Users.CreateOwner(ctx, "Owner1@Landlord.local", "Александр Васильевич")
	require.NoError(t, err)
	assert.Equal(t, "owner1@landlord.local", u.Email)
	assert.Equal(t, "invited", u.Status)
	assert.Contains(t, link, "http://localhost:5173/invite/")

	// Письмо invite поставлено в очередь со ссылкой.
	var body string
	require.NoError(t, testsupport.Owner(t).QueryRow(ctx,
		`SELECT body_html FROM email_log WHERE template_code = 'invite' AND related_entity_id = $1`, u.ID).Scan(&body))
	assert.Contains(t, body, link)

	c := env.NewClient(t)
	token := testsupport.TokenFromLink(link)
	r := c.Do(http.MethodGet, "/auth/invite/"+token, nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.JSONEq(t, `{"email":"owner1@landlord.local","displayName":"Александр Васильевич","role":"owner"}`, string(r.Body))

	r = c.Do(http.MethodPost, "/auth/invite/accept", map[string]any{"token": token, "password": "короткий1"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	r = c.Do(http.MethodPost, "/auth/invite/accept", map[string]any{"token": token, "password": "Demo-2026-owner"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var ar authResp
	r.JSON(t, &ar)
	assert.Equal(t, "active", ar.User.Status)
	assert.False(t, ar.MustAcceptPolicy)
	assert.Contains(t, r.Header.Get("Set-Cookie"), "ll_session=")
	assert.Contains(t, r.Header.Get("Set-Cookie"), "HttpOnly")

	// Приглашение одноразовое.
	assert.Equal(t, http.StatusNotFound, c.Do(http.MethodGet, "/auth/invite/"+token, nil).Status)

	r = c.Do(http.MethodGet, "/auth/me", nil)
	require.Equal(t, http.StatusOK, r.Status)

	// Выход без CSRF-токена отклоняется.
	csrf := c.CSRF
	c.CSRF = ""
	r = c.Do(http.MethodPost, "/auth/logout", nil)
	assert.Equal(t, http.StatusForbidden, r.Status)
	assert.Equal(t, "csrf_failed", r.Code())
	c.CSRF = csrf

	assert.Equal(t, http.StatusNoContent, c.Do(http.MethodPost, "/auth/logout", nil).Status)
	assert.Equal(t, http.StatusUnauthorized, c.Do(http.MethodGet, "/auth/me", nil).Status)
	assert.Equal(t, 1, countAudit(t, "auth.logout", u.ID.String()))
	assert.Equal(t, 1, countAudit(t, "user.create", u.ID.String()))
	assert.Equal(t, 1, countAudit(t, "user.invite", u.ID.String()))

	c.Login("owner1@landlord.local", "Demo-2026-owner")
	assert.Equal(t, http.StatusOK, c.Do(http.MethodGet, "/auth/me", nil).Status)
}

// IT-09: 5 неверных паролей → 6-я попытка 429 login_locked даже с верным паролем; через 15 мин — вход успешен.
func TestLoginLockout_IT09_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	env.ActiveOwner(t, "lock@landlord.local", "Верный-пароль-1")
	c := env.NewClient(t)

	for i := 1; i <= 5; i++ {
		r := c.Do(http.MethodPost, "/auth/login", map[string]string{"email": "lock@landlord.local", "password": "неверный-1"})
		require.Equal(t, http.StatusUnauthorized, r.Status, "попытка %d", i)
		assert.Equal(t, "invalid_credentials", r.Code())
		env.Clock.Advance(10 * time.Second)
	}
	r := c.Do(http.MethodPost, "/auth/login", map[string]string{"email": "lock@landlord.local", "password": "Верный-пароль-1"})
	require.Equal(t, http.StatusTooManyRequests, r.Status)
	assert.Equal(t, "login_locked", r.Code())
	assert.NotEmpty(t, r.Header.Get("Retry-After"))
	assert.Regexp(t, regexp.MustCompile(`Повторите через \d+ мин\.`), string(r.Body))

	var locked int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log a JOIN users u ON u.id::text = a.entity_id WHERE a.action = 'auth.locked' AND u.email = 'lock@landlord.local'`).Scan(&locked))
	assert.Equal(t, 1, locked)

	env.Clock.Advance(15 * time.Minute)
	c.Login("lock@landlord.local", "Верный-пароль-1")
}

// Неизвестный e-mail — тот же ответ, что и неверный пароль.
func TestUnknownEmailSameAnswer_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	r := env.NewClient(t).Do(http.MethodPost, "/auth/login", map[string]string{"email": "nobody@landlord.local", "password": "что-то-123"})
	assert.Equal(t, http.StatusUnauthorized, r.Status)
	assert.Equal(t, "invalid_credentials", r.Code())
	assert.Contains(t, string(r.Body), "Неверный e-mail или пароль")
}

// Восстановление пароля: письмо со ссылкой, новый пароль, старые сессии отозваны; ответ не раскрывает адрес.
func TestForgotAndResetPassword_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	old := env.ActiveOwner(t, "reset@landlord.local", "Старый-пароль-1")
	c := env.NewClient(t)

	assert.Equal(t, http.StatusNoContent, c.Do(http.MethodPost, "/auth/password/forgot", map[string]string{"email": "nobody@x.ru"}).Status)
	assert.Equal(t, http.StatusNoContent, c.Do(http.MethodPost, "/auth/password/forgot", map[string]string{"email": "reset@landlord.local"}).Status)

	var body string
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(),
		`SELECT body_html FROM email_log WHERE template_code = 'password_reset' AND to_email = 'reset@landlord.local'`).Scan(&body))
	m := regexp.MustCompile(`/reset-password/([A-Za-z0-9_-]+)`).FindStringSubmatch(body)
	require.Len(t, m, 2)

	r := c.Do(http.MethodPost, "/auth/password/reset", map[string]string{"token": m[1], "newPassword": "Новый-пароль-2"})
	require.Equal(t, http.StatusNoContent, r.Status, string(r.Body))
	assert.Equal(t, http.StatusUnauthorized, old.Do(http.MethodGet, "/auth/me", nil).Status, "старая сессия отозвана")
	c.Login("reset@landlord.local", "Новый-пароль-2")

	// Ссылка одноразовая.
	r = c.Do(http.MethodPost, "/auth/password/reset", map[string]string{"token": m[1], "newPassword": "Ещё-пароль-3"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

// Смена пароля отзывает прочие сессии; текущая продолжает работать.
func TestChangePassword_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	a := env.ActiveOwner(t, "change@landlord.local", "Первый-пароль-1")
	b := env.NewClient(t)
	b.Login("change@landlord.local", "Первый-пароль-1")

	r := a.Do(http.MethodPost, "/auth/password/change", map[string]string{"currentPassword": "неверный", "newPassword": "Второй-пароль-2"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	r = a.Do(http.MethodPost, "/auth/password/change", map[string]string{"currentPassword": "Первый-пароль-1", "newPassword": "Второй-пароль-2"})
	require.Equal(t, http.StatusNoContent, r.Status, string(r.Body))
	assert.Equal(t, http.StatusOK, a.Do(http.MethodGet, "/auth/me", nil).Status)
	assert.Equal(t, http.StatusUnauthorized, b.Do(http.MethodGet, "/auth/me", nil).Status)
}

// Простой сессии дольше 8 часов — выход.
func TestSessionIdleTimeout_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	c := env.ActiveOwner(t, "idle@landlord.local", "Пароль-простоя-1")
	env.Clock.Advance(7 * time.Hour)
	assert.Equal(t, http.StatusOK, c.Do(http.MethodGet, "/auth/me", nil).Status)
	env.Clock.Advance(8*time.Hour + time.Minute)
	assert.Equal(t, http.StatusUnauthorized, c.Do(http.MethodGet, "/auth/me", nil).Status)
}

// Блокировка пользователя отзывает его сессии; себя заблокировать нельзя.
func TestBlockUser_FR14_1(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	admin := env.ActiveOwner(t, "admin@landlord.local", "Админ-пароль-1")
	other := env.ActiveOwner(t, "second@landlord.local", "Второй-пароль-1")

	var me, second authResp
	admin.Do(http.MethodGet, "/auth/me", nil).JSON(t, &me)
	other.Do(http.MethodGet, "/auth/me", nil).JSON(t, &second)

	r := admin.Do(http.MethodPatch, "/users/"+me.User.ID, map[string]string{"status": "blocked"})
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	r = admin.Do(http.MethodPatch, "/users/"+second.User.ID, map[string]string{"status": "blocked"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	assert.Equal(t, http.StatusUnauthorized, other.Do(http.MethodGet, "/auth/me", nil).Status)
	assert.Equal(t, 1, countAudit(t, "user.block", second.User.ID))

	r = env.NewClient(t).Do(http.MethodPost, "/auth/login", map[string]string{"email": "second@landlord.local", "password": "Второй-пароль-1"})
	assert.Equal(t, http.StatusForbidden, r.Status)
	assert.Contains(t, string(r.Body), "Учётная запись заблокирована")

	// Повторный e-mail — 409 conflict_exists.
	r = admin.Do(http.MethodPost, "/users", map[string]string{"email": "second@landlord.local", "displayName": "Дубль"})
	assert.Equal(t, http.StatusConflict, r.Status)
	assert.Equal(t, "conflict_exists", r.Code())
}

// Наниматель: доступ только к своим разделам, ознакомление с новой версией политики (12 разд. 3–4).
func TestTenantAccessAndPolicy_NFR_Sec_02(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-16")
	owner := env.ActiveOwner(t, "pol-owner@landlord.local", "Пароль-собств-1")
	tenant := env.ActiveTenant(t, "tenant@landlord.local", "Пароль-наним-1")

	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/users", nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/audit", nil).Status)
	assert.Equal(t, http.StatusForbidden, tenant.Do(http.MethodGet, "/settings/organization", nil).Status)
	assert.Equal(t, http.StatusForbidden, owner.Do(http.MethodGet, "/cabinet/summary", nil).Status)

	var ar authResp
	tenant.Do(http.MethodGet, "/auth/me", nil).JSON(t, &ar)
	assert.False(t, ar.MustAcceptPolicy)

	// Собственник публикует новую версию политики.
	var s map[string]any
	owner.Do(http.MethodGet, "/settings/organization", nil).JSON(t, &s)
	s["fullName"], s["shortName"], s["inn"], s["address"] = "Индивидуальный предприниматель Гордейчик Александр Васильевич", "ИП Гордейчик А.В.", "507500363778", "Московская область, г. Руза"
	s["email"], s["signatoryName"], s["privacyPolicyVersion"] = "owner@landlord.local", "А.В. Гордейчик", "1.1"
	delete(s, "updatedAt")
	delete(s, "updatedBy")
	r := owner.Do(http.MethodPut, "/settings/organization", s)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))

	tenant.Do(http.MethodGet, "/auth/me", nil).JSON(t, &ar)
	assert.True(t, ar.MustAcceptPolicy)
	assert.Equal(t, "1.1", ar.PolicyVersion)
	r = tenant.Do(http.MethodGet, "/cabinet/summary", nil)
	assert.Equal(t, http.StatusForbidden, r.Status, "кабинет недоступен до ознакомления")

	assert.Equal(t, http.StatusUnprocessableEntity, tenant.Do(http.MethodPost, "/auth/accept-policy", map[string]string{"version": "1.0"}).Status)
	assert.Equal(t, http.StatusNoContent, tenant.Do(http.MethodPost, "/auth/accept-policy", map[string]string{"version": "1.1"}).Status)
	tenant.Do(http.MethodGet, "/auth/me", nil).JSON(t, &ar)
	assert.False(t, ar.MustAcceptPolicy)

	// Политика на странице /privacy — с реквизитами организации.
	var p struct{ Version, Html string }
	env.NewClient(t).Do(http.MethodGet, "/public/privacy-policy", nil).JSON(t, &p)
	assert.Equal(t, "1.1", p.Version)
	assert.Contains(t, p.Html, "ИНН 507500363778")
	assert.NotContains(t, p.Html, "{{")
}
