//go:build integration

package audit_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

type verifyResp struct {
	Ok         bool   `json:"ok"`
	Checked    int64  `json:"checked"`
	BrokenAtID *int64 `json:"brokenAtId"`
}

// IT-10: UPDATE/DELETE/TRUNCATE audit_log под ролью landlord_app → ошибка; verify → ok;
// изменение записи суперпользователем с отключённым триггером → verify указывает номер записи (FR-12, NFR-Sec-04).
func TestAuditImmutableAndVerify_IT10_FR12_NFR_Sec_04(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t, "2026-10-16")
	owner := env.ActiveOwner(t, "audit@landlord.local", "Пароль-журнала-1")

	// Несколько действий — несколько записей.
	owner.Login("audit@landlord.local", "Пароль-журнала-1")
	require.Equal(t, http.StatusNoContent, owner.Do(http.MethodPost, "/auth/password/change",
		map[string]string{"currentPassword": "Пароль-журнала-1", "newPassword": "Пароль-журнала-2"}).Status)

	var list struct {
		Items []struct {
			Action, ActionTitle, Summary string
			ActorName                    *string
			Before, After                map[string]any
		} `json:"items"`
		Total int `json:"total"`
	}
	r := owner.Do(http.MethodGet, "/audit?pageSize=100", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &list)
	require.GreaterOrEqual(t, list.Total, 5)
	assert.Equal(t, "auth.password_changed", list.Items[0].Action)
	assert.Equal(t, "Смена пароля", list.Items[0].ActionTitle)
	for _, it := range list.Items {
		if it.Action == "user.create" {
			assert.Equal(t, "***", it.After["email"], "e-mail в журнале маскируется")
		}
	}

	r = owner.Do(http.MethodGet, "/audit?action=auth.login&q=Вход", nil)
	r.JSON(t, &list)
	assert.Equal(t, 1, list.Total)

	var v verifyResp
	owner.Do(http.MethodGet, "/audit/verify", nil).JSON(t, &v)
	assert.True(t, v.Ok)
	assert.Nil(t, v.BrokenAtID)
	assert.GreaterOrEqual(t, v.Checked, int64(5))

	// Роль приложения не может менять журнал.
	app, err := pgx.Connect(ctx, testsupport.AppURL)
	require.NoError(t, err)
	defer app.Close(ctx)
	for _, sql := range []string{
		`UPDATE audit_log SET summary = 'подделка' WHERE id = 2`,
		`DELETE FROM audit_log WHERE id = 2`,
		`TRUNCATE audit_log`,
	} {
		_, err := app.Exec(ctx, sql)
		assert.Error(t, err, sql)
	}

	// Владелец схемы упирается в триггер.
	ownerConn := testsupport.Owner(t)
	_, err = ownerConn.Exec(ctx, `UPDATE audit_log SET summary = 'подделка' WHERE id = 2`)
	assert.ErrorContains(t, err, "Журнал действий не может быть изменён")

	// Суперпользователь с отключённым триггером меняет запись — проверка её находит.
	_, err = ownerConn.Exec(ctx, `ALTER TABLE audit_log DISABLE TRIGGER audit_log_no_update`)
	require.NoError(t, err)
	_, err = ownerConn.Exec(ctx, `UPDATE audit_log SET summary = 'подделка' WHERE id = 3`)
	require.NoError(t, err)
	_, err = ownerConn.Exec(ctx, `ALTER TABLE audit_log ENABLE TRIGGER audit_log_no_update`)
	require.NoError(t, err)

	owner.Do(http.MethodGet, "/audit/verify", nil).JSON(t, &v)
	assert.False(t, v.Ok)
	require.NotNil(t, v.BrokenAtID)
	assert.Equal(t, int64(3), *v.BrokenAtID)
	assert.Equal(t, int64(3), v.Checked)

	res, err := env.App.Audit.Verify(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), *res.BrokenAtID)
}
