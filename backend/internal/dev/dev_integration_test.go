//go:build integration

package dev_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

const password = "Пароль-демо-режима-1"

type devClock struct {
	Today         string `json:"today"`
	OffsetSeconds int64  `json:"offsetSeconds"`
}

// Дата системы и «Выполнить ежедневные задачи» в режиме demo (07 «Порядок выполнения на защите», 14 разд. 16).
func TestDemoClockAndRunDaily(t *testing.T) {
	backups := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(backups, "landlord-2026-10-18.dump"), []byte("старая"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(backups, "landlord-2026-10-19.dump"), []byte("новая копия"), 0o600))
	old := time.Now().Add(-24 * time.Hour) //nolint:forbidigo // время файла на диске, не бизнес-дата
	require.NoError(t, os.Chtimes(filepath.Join(backups, "landlord-2026-10-18.dump"), old, old))

	env := testsupport.NewDemoEnv(t, backups)
	owner := env.ActiveOwner(t, "demo-clock@landlord.local", password)
	t.Cleanup(func() {
		_, _ = testsupport.Owner(t).Exec(context.Background(), `UPDATE demo_clock SET offset_seconds = 0`)
	})

	var c devClock
	r := owner.Do(http.MethodPut, "/dev/clock", map[string]any{"today": "2026-11-01"})
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &c)
	assert.Equal(t, "2026-11-01", c.Today)
	assert.Equal(t, "2026-11-01", env.App.Clock.Today().String(), "часы приложения сдвинуты")
	owner.Login("demo-clock@landlord.local", password)
	r = owner.Do(http.MethodGet, "/dev/clock", nil)
	r.JSON(t, &c)
	assert.Equal(t, "2026-11-01", c.Today)

	r = owner.Do(http.MethodPost, "/dev/run-daily", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var res struct {
		Today string `json:"today"`
		Jobs  []struct {
			JobName      string `json:"jobName"`
			BusinessDate string `json:"businessDate"`
			Status       string `json:"status"`
		} `json:"jobs"`
	}
	r.JSON(t, &res)
	assert.Equal(t, "2026-11-01", res.Today)
	names := make([]string, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		names = append(names, j.JobName)
		assert.Equal(t, "2026-11-01", j.BusinessDate, j.JobName)
		assert.NotEqual(t, "running", j.Status, j.JobName)
	}
	assert.Equal(t, []string{"contracts.daily", "billing.rent_daily", "billing.penalties_daily", "billing.indexation_daily",
		"pdn.retention_daily", "maintenance.cleanup", "notify.owner_digest", "notify.payment_reminders", "notify.contract_end",
		"notify.readings_reminder"}, names)

	var backupsList []struct {
		FileName  string `json:"fileName"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	r = owner.Do(http.MethodGet, "/settings/backups", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &backupsList)
	require.Len(t, backupsList, 2)
	assert.Equal(t, "landlord-2026-10-19.dump", backupsList[0].FileName, "новые первыми")
	assert.Equal(t, int64(len("новая копия")), backupsList[0].SizeBytes)

	r = owner.Do(http.MethodDelete, "/dev/clock", nil)
	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	r.JSON(t, &c)
	assert.Zero(t, c.OffsetSeconds)
}

// Вне режима demo методы /dev/* отвечают 404.
func TestDevOutsideDemo(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-15")
	owner := env.ActiveOwner(t, "dev-off@landlord.local", password)
	assert.Equal(t, http.StatusNotFound, owner.Do(http.MethodGet, "/dev/clock", nil).Status)
	assert.Equal(t, http.StatusNotFound, owner.Do(http.MethodPost, "/dev/run-daily", nil).Status)
}

// maintenance.cleanup: файлы без привязки дольше 24 ч, старые попытки входа, отозванные сессии (NFR-Sec-01).
func TestCleanup_NFR_Sec_01(t *testing.T) {
	env := testsupport.NewEnv(t, "2026-10-15")
	owner := env.ActiveOwner(t, "cleanup@landlord.local", password)
	orphan := owner.Upload("request_photo", "забытое.png", testsupport.PNG).ID(t)
	fresh := owner.Upload("request_photo", "свежее.png", testsupport.PNG).ID(t)
	conn := testsupport.Owner(t)
	ctx := context.Background()
	_, err := conn.Exec(ctx, `UPDATE files SET created_at = created_at - interval '2 days' WHERE id = $1`, orphan)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO login_attempts (email, success, at) VALUES ('old@landlord.local', false, $1)`,
		env.Clock.Now().Add(-31*24*time.Hour))
	require.NoError(t, err)
	var key string
	require.NoError(t, conn.QueryRow(ctx, `SELECT storage_key FROM files WHERE id = $1`, orphan).Scan(&key))

	st, err := env.App.Cleanup.RunCleanup(ctx, jobs.DailyArgs{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, st.Counts["files"], 1)
	assert.GreaterOrEqual(t, st.Counts["loginAttempts"], 1)
	var deleted, freshDeleted bool
	require.NoError(t, conn.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM files WHERE id = $1`, orphan).Scan(&deleted))
	require.NoError(t, conn.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM files WHERE id = $1`, fresh).Scan(&freshDeleted))
	assert.True(t, deleted)
	assert.False(t, freshDeleted, "файл моложе 24 ч остаётся")
	_, err = os.Stat(filepath.Join(env.App.Config.FilesDir, key))
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, http.StatusOK, owner.Do(http.MethodGet, "/auth/me", nil).Status, "действующая сессия не тронута")
}
