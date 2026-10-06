//go:build integration

package db_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"landlord/backend/internal/platform/db"
)

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("landlord"),
		tcpostgres.WithUsername("landlord_owner"),
		tcpostgres.WithPassword("owner"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Роль приложения, как в deploy/db-init/01-roles.sh.
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `CREATE ROLE landlord_app LOGIN PASSWORD 'app';
		GRANT USAGE ON SCHEMA public TO landlord_app;`)
	require.NoError(t, err)
	return url
}

func countTables(t *testing.T, url string, names ...string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer conn.Close(ctx)
	var n int
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ANY($1)`, names).Scan(&n))
	return n
}

// Миграции применяются и откатываются (критерий этапа M0, NFR-Maint-02).
func TestMigrateUpDownUp_NFR_Maint_02(t *testing.T) {
	url := startPostgres(t)
	ctx := context.Background()
	var out bytes.Buffer
	m := db.NewMigrator(url, &out)

	require.NoError(t, m.Up(ctx))
	assert.Equal(t, 4, countTables(t, url, "contracts", "charges", "audit_log", "river_job"))

	// Схема рабочая: журнал действий получает хеш-цепочку и не изменяется (FR-12).
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO audit_log (actor_role, action, entity_type, entity_id, summary)
		VALUES ('system', 'job.test', 'job', '1', 'проверка')`)
	require.NoError(t, err)
	var hashLen int
	require.NoError(t, conn.QueryRow(ctx, `SELECT length(hash) FROM audit_log WHERE id = 1`).Scan(&hashLen))
	assert.Equal(t, 32, hashLen)
	_, err = conn.Exec(ctx, `UPDATE audit_log SET summary = 'x'`)
	assert.ErrorContains(t, err, "Журнал действий не может быть изменён")
	conn.Close(ctx)

	require.NoError(t, m.Down(ctx))
	assert.Equal(t, 0, countTables(t, url, "contracts", "charges", "audit_log", "river_job"))

	require.NoError(t, m.Up(ctx))
	assert.Equal(t, 4, countTables(t, url, "contracts", "charges", "audit_log", "river_job"))
	require.NoError(t, m.Status(ctx))
}
