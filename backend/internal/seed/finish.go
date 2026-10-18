package seed

import (
	"fmt"
	"net/http"
	"time"

	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
)

// finish — после загрузки: письма истории — «отправлены» (без реальной отправки), очередь задач очищена, чеки
// ООО «Ромашка» — «сформированы» с условными реквизитами (17 разд. 5, 6); затем приглашение Лебедева — настоящее
// письмо в Mailpit.
func (w *world) finish() error {
	now := clock.NewReal(w.loc).Now()
	if date.In(now, w.loc) != w.today {
		now = time.Date(w.today.Year(), w.today.Month(), w.today.Day(), 12, 0, 0, 0, w.loc)
	}
	w.clk.Set(now)
	w.day, w.owner.loggedOn = w.today, date.Date{}
	stmts := []string{
		`UPDATE email_log SET status = 'sent', sent_at = created_at, attempts = 1 WHERE status = 'queued'`,
		`DELETE FROM river_job`,
		`WITH r AS (SELECT id, row_number() OVER (ORDER BY created_at, id) AS n FROM fiscal_receipts WHERE status IN ('queued', 'wait'))
		 UPDATE fiscal_receipts f SET status = 'done', external_uuid = gen_random_uuid()::text, fiscal_receipt_number = r.n,
		   shift_number = 1 + r.n / 30, receipt_datetime = f.created_at, fn_number = '9999078902012345',
		   fiscal_document_number = 1000 + r.n, fiscal_document_attribute = 1000000000 + r.n * 7919, attempts = 1,
		   ofd_receipt_url = NULL, error_code = NULL, error_text = NULL
		 FROM r WHERE f.id = r.id`,
	}
	for _, s := range stmts {
		if _, err := w.a.DB.Pool.Exec(w.ctx, s); err != nil {
			return fmt.Errorf("завершение загрузки: %w", err)
		}
	}
	return w.owner.do(http.MethodPost, "/tenants/"+w.ids["lebedev"]+"/invite", nil, nil)
}
