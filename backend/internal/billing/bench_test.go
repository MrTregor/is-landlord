//go:build integration

package billing_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/testsupport"
)

// BenchmarkRentDaily100 — billing.rent_daily на 100 действующих договорах в день создания начислений нового месяца
// (NFR-Perf-02): один прогон — не дольше 10 с.
func BenchmarkRentDaily100(b *testing.B) {
	env := testsupport.NewEnv(b, "2026-10-01")
	ctx := context.Background()
	conn := testsupport.Owner(b)
	suffix := fmt.Sprint(time.Now().UnixNano()) //nolint:forbidigo // уникальные коды данных бенчмарка
	for i := range 100 {
		var prop, ten, con string
		if err := conn.QueryRow(ctx, `INSERT INTO properties (code, kind, address, area_sqm, fiscal_mode, status)
			VALUES ($1, 'residential', 'г. Руза', 30, 'none', 'rented') RETURNING id`, fmt.Sprintf("Бенч %s-%d", suffix, i)).Scan(&prop); err != nil {
			b.Fatal(err)
		}
		if err := conn.QueryRow(ctx, `INSERT INTO tenants (kind, last_name, first_name) VALUES ('individual', 'Тестов', 'Тест') RETURNING id`).Scan(&ten); err != nil {
			b.Fatal(err)
		}
		if err := conn.QueryRow(ctx, `INSERT INTO contracts (number, legal_basis, property_id, tenant_id, status, start_date, end_date,
			first_due_date, utilities_mode, activated_at) VALUES ($1, 'ch35', $2, $3, 'active', '2026-10-01', '2027-08-31', '2026-10-01', 'included', now())
			RETURNING id`, fmt.Sprintf("Б-%s-%03d", suffix, i), prop, ten).Scan(&con); err != nil {
			b.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO contract_terms (contract_id, version, effective_from, rent_kop, payment_day)
			VALUES ($1, 1, '2026-10-01', 3000000, 1)`, con); err != nil {
			b.Fatal(err)
		}
	}
	svc := env.App.Billing
	if _, err := svc.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: date.MustParse("2026-10-01")}); err != nil {
		b.Fatal(err)
	}
	day := date.MustParse("2026-10-29") // 01.11 − 3 дня: создаются начисления ноября
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now() //nolint:forbidigo // измерение длительности прогона
		st, err := svc.RunRentDaily(ctx, jobs.DailyArgs{BusinessDate: day})
		if err != nil {
			b.Fatal(err)
		}
		if i == 0 {
			if st.Counts["created"] < 100 {
				b.Fatalf("создано начислений %d, ожидалось не меньше 100", st.Counts["created"])
			}
			if d := time.Since(start); d > 10*time.Second { //nolint:forbidigo // NFR-Perf-02
				b.Fatalf("прогон %v, допускается не больше 10 с", d)
			}
		}
	}
}
