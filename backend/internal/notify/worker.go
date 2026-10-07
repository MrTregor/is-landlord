package notify

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/mailer"
	"landlord/backend/internal/platform/tmpl"
)

// EmailSendWorker отправляет письмо из email_log по SMTP (задача email.send, 07 Фоновые задачи).
// Неудача — повтор River; после последней попытки письмо получает статус failed.
type EmailSendWorker struct {
	river.WorkerDefaults[EmailSendArgs]
	DB     *db.DB
	Clock  clock.Clock
	Sender mailer.Sender
}

// Work отправляет письмо.
func (w *EmailSendWorker) Work(ctx context.Context, job *river.Job[EmailSendArgs]) error {
	q := w.DB.Queries()
	m, err := q.GetEmailLog(ctx, job.Args.EmailLogID)
	if db.IsNoRows(err) {
		return river.JobCancel(fmt.Errorf("письмо %s не найдено", job.Args.EmailLogID))
	}
	if err != nil {
		return err
	}
	if m.Status != "queued" && m.Status != "failed" {
		return nil // уже отправлено или пропущено
	}
	replyTo := ""
	if org, err := q.GetOrganizationSettings(ctx); err == nil {
		replyTo = org.Email
	}
	sendErr := w.Sender.Send(ctx, mailer.Message{
		To: m.ToEmail, ReplyTo: replyTo, Subject: m.Subject, HTML: m.BodyHtml, Text: tmpl.PlainText(m.BodyHtml),
	})
	if sendErr == nil {
		now := w.Clock.Now()
		return q.MarkEmailSent(ctx, sqlc.MarkEmailSentParams{ID: m.ID, SentAt: &now})
	}
	status := "queued"
	if job.Attempt >= job.MaxAttempts {
		status = "failed"
	}
	msg := sendErr.Error()
	if err := q.MarkEmailAttemptFailed(ctx, sqlc.MarkEmailAttemptFailedParams{ID: m.ID, Status: status, LastError: &msg}); err != nil {
		return err
	}
	return sendErr
}
