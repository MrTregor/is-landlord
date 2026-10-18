//go:build integration

package audit_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/testsupport"
)

// Гонка двух транзакций (№ 109): запись A получила id и ждёт блокировку цепочки, запись B вставлена позже,
// но зафиксирована раньше. Цепочка в порядке id не должна разрываться (FR-12, NFR-Sec-04).
func TestAuditChainConcurrentInserts_FR12_NFR_Sec_04(t *testing.T) {
	ctx := context.Background()
	testsupport.NewEnv(t, "2026-10-16") // миграции базы пакета
	const insert = `INSERT INTO audit_log (actor_role, action, entity_type, entity_id, summary)
		VALUES ('system', $1, 'test', 'chain', 'гонка') RETURNING id`

	b := testsupport.Owner(t)
	txB, err := b.Begin(ctx)
	require.NoError(t, err)
	_, err = txB.Exec(ctx, `SELECT pg_advisory_xact_lock(7428001)`) // B первой держит блокировку цепочки
	require.NoError(t, err)

	idA := make(chan int64, 1)
	errA := make(chan error, 1)
	go func() {
		a := testsupport.Owner(t)
		var id int64
		err := a.QueryRow(ctx, insert, "test.race.a").Scan(&id) // id выдан, ждёт блокировку
		idA <- id
		errA <- err
	}()
	time.Sleep(500 * time.Millisecond)

	var idB int64
	require.NoError(t, txB.QueryRow(ctx, insert, "test.race.b").Scan(&idB))
	require.NoError(t, txB.Commit(ctx))
	require.NoError(t, <-errA)
	gotA := <-idA
	assert.Greater(t, gotA, idB, "запись, зафиксированная позже, получает больший id")

	type row struct {
		id         int64
		prev, hash []byte
	}
	rows, err := b.Query(ctx, `SELECT id, prev_hash, hash FROM audit_log WHERE id >= $1 ORDER BY id`, idB)
	require.NoError(t, err)
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.id, &r.prev, &r.hash))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)
	assert.Equal(t, []int64{idB, gotA}, []int64{got[0].id, got[1].id})
	assert.True(t, bytes.Equal(got[1].prev, got[0].hash), "prev_hash записи A — hash записи B")
}
