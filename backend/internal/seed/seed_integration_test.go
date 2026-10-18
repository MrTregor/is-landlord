//go:build integration

package seed_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/seed"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

func count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, testsupport.Owner(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// seed --demo на пустой БД без ошибок и повторно (идемпотентно — очищает и загружает заново), 17 Демо-данные.
func TestSeedDemoTwice(t *testing.T) {
	cfg, log, today := seedConfig(t)
	for run := 1; run <= 2; run++ {
		var out bytes.Buffer
		err := seed.Run(context.Background(), cfg, log, &out, seed.Options{PDF: &testsupport.FakePDF{}, Today: &today})
		require.NoError(t, err, "запуск %d: %s", run, out.String())
	}
	checkDemo(t)
}

// --load (17 разд. 7): долгий прогон, только при SEED_LOAD=1.
func TestSeedLoad(t *testing.T) {
	if os.Getenv("SEED_LOAD") != "1" {
		t.Skip("SEED_LOAD=1 — прогон режима --load")
	}
	cfg, log, today := seedConfig(t)
	var out bytes.Buffer
	require.NoError(t, seed.Run(context.Background(), cfg, log, &out, seed.Options{PDF: &testsupport.FakePDF{}, Today: &today, Load: true}))
	checkDemo(t)
	assert.Equal(t, 105, count(t, `SELECT count(*) FROM contracts`))
	assert.Positive(t, count(t, `SELECT count(*) FROM payments p JOIN contracts k ON k.id = p.contract_id JOIN properties o ON o.id = k.property_id
		WHERE o.code LIKE 'НТ-%' AND p.status = 'succeeded'`))
}

func seedConfig(t *testing.T) (*config.Config, *slog.Logger, date.Date) {
	today := date.MustParse("2026-10-19")
	cfg := &config.Config{
		AppEnv: config.EnvDev, AppName: "Наймодатель", AppPublicURL: "http://localhost:5173", SessionIdleHours: 8,
		SessionAbsoluteDays: 30, Location: testsupport.Moscow(), FilesDir: t.TempDir(), MaxUploadMB: 10,
		PDNKey: bytes.Repeat([]byte{42}, 32), DatabaseURL: testsupport.AppURL, DatabaseMigrateURL: testsupport.OwnerURL,
	}
	return cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), today
}

// checkDemo — состав демо-данных 17 разд. 1–6.
func checkDemo(t *testing.T) {
	t.Helper()
	assert.Equal(t, 5, count(t, `SELECT count(*) FROM properties WHERE code NOT LIKE 'НТ-%'`))
	assert.Equal(t, 5, count(t, `SELECT count(*) FROM contracts c JOIN properties p ON p.id = c.property_id WHERE p.code NOT LIKE 'НТ-%'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM users WHERE role = 'owner'`))
	assert.Equal(t, 3, count(t, `SELECT count(*) FROM users WHERE role = 'tenant' AND status = 'active'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM users WHERE role = 'tenant' AND status = 'invited'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM contracts WHERE number = '2025-004' AND status = 'finished'`))
	for _, n := range []string{"2026-001", "2026-002", "2026-003", "2026-005"} {
		assert.Equal(t, 1, count(t, `SELECT count(*) FROM contracts WHERE number = $1 AND status = 'active'`, n), n)
	}
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM properties WHERE code = 'Студия 3' AND status = 'free'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM discrepancies WHERE kind = 'overpayment' AND status = 'open'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM discrepancies WHERE kind = 'unidentified' AND status = 'open'`))
	assert.Positive(t, count(t, `SELECT count(*) FROM payments WHERE method = 'online' AND status = 'succeeded'`))
	assert.Positive(t, count(t, `SELECT count(*) FROM payments WHERE source = 'bank_import' AND status = 'succeeded'`))
	assert.Positive(t, count(t, `SELECT count(*) FROM charges WHERE kind = 'penalty'`))
	assert.Positive(t, count(t, `SELECT count(*) FROM charges WHERE kind = 'utilities'`))
	assert.Zero(t, count(t, `SELECT count(*) FROM fiscal_receipts WHERE status <> 'done'`))
	assert.Positive(t, count(t, `SELECT count(*) FROM fiscal_receipts`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM email_log WHERE status = 'queued'`), "в очереди — только приглашение Лебедева")
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM meter_readings WHERE status = 'submitted'`)/3, "показания Смирновой за прошлый месяц на проверке")
	assert.Equal(t, 3, count(t, `SELECT count(*) FROM maintenance_requests`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM deposit_transactions WHERE kind = 'refund'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM deposit_transactions WHERE kind = 'withholding'`))
	assert.Equal(t, 2, count(t, `SELECT count(*) FROM calendar_events WHERE source = 'manual'`))
	assert.Equal(t, 1, count(t, `SELECT count(*) FROM bank_statements`))
	assert.Equal(t, 10, count(t, `SELECT count(*) FROM property_photos`))
}
