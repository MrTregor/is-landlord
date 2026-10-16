package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"landlord/backend/internal/document"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/money"
)

// ReceiptArgs — задача document.payment_receipt (07 Фоновые задачи): квитанция PDF и письмо нанимателю.
type ReceiptArgs struct {
	PaymentID uuid.UUID `json:"paymentId"`
}

// Kind — вид задачи River.
func (ReceiptArgs) Kind() string { return "document.payment_receipt" }

// InsertOpts — очередь default, 5 попыток (07 Фоновые задачи).
func (ReceiptArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueDefault, MaxAttempts: 5}
}

// ReceiptWorker формирует квитанцию по проведённому платежу.
type ReceiptWorker struct {
	river.WorkerDefaults[ReceiptArgs]
	Svc *Service
}

// Work — квитанция и письмо; повтор после успеха ничего не создаёт.
func (w *ReceiptWorker) Work(ctx context.Context, job *river.Job[ReceiptArgs]) error {
	_, err := w.Svc.Receipt(ctx, job.Args.PaymentID)
	if apperr.Is(err, apperr.CodeNotFound) {
		return river.JobCancel(err)
	}
	return err
}

// Receipt — квитанция платежа (09 разд. 5.6): сформировать, если её ещё нет, и поставить письмо нанимателю
// payment_receipt с квитанцией во вложении (10 Уведомления, раздел 2). Одна транзакция.
func (s *Service) Receipt(ctx context.Context, paymentID uuid.UUID) (sqlc.Document, error) {
	var doc sqlc.Document
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, err := tx.Q.GetPayment(ctx, paymentID)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if p.Status != "succeeded" {
			return apperr.BusinessRule("Квитанция формируется по проведённому платежу")
		}
		var created bool
		doc, created, err = s.documents.PaymentReceiptTx(ctx, tx, paymentID)
		if err != nil || !created {
			return err
		}
		c, err := tx.Q.GetContract(ctx, p.ContractID)
		if err != nil {
			return err
		}
		to, vars, err := tenantMail(ctx, tx.Q, c)
		if err != nil {
			return err
		}
		vars["receipt_number"] = *p.ReceiptNumber
		vars["amount"] = money.Kop(p.AmountKop).Format()
		vars["received_on"] = p.ReceivedOn.Format()
		return s.notify.Queue(ctx, tx, notify.Email{
			Code: "payment_receipt", To: to, Vars: vars, DedupeKey: "payment_receipt:" + p.ID.String(),
			RelatedType: "payment", RelatedID: &p.ID,
			Attachments: []notify.Attachment{{FileID: doc.FileID, Name: fmt.Sprintf("Квитанция %s.pdf", *p.ReceiptNumber)}},
		})
	})
	if document.IsUnavailable(err) {
		return doc, apperr.BadGateway("Сервис формирования PDF недоступен. Повторите попытку позже").Wrap(err)
	}
	return doc, err
}
