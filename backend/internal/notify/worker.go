package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/riverqueue/river"

	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/files"
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
	// Files — хранилище файлов для вложений.
	Files *files.Store
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
	atts, closeAll, err := w.attachments(ctx, q, m.Attachments)
	if err != nil {
		return err
	}
	defer closeAll()
	sendErr := w.Sender.Send(ctx, mailer.Message{
		To: m.ToEmail, ReplyTo: replyTo, Subject: m.Subject, HTML: m.BodyHtml, Text: tmpl.PlainText(m.BodyHtml),
		Attachments: atts,
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

// attachments открывает файлы вложений письма; closeAll закрывает их после отправки.
func (w *EmailSendWorker) attachments(ctx context.Context, q *sqlc.Queries, raw []byte) ([]mailer.Attachment, func(), error) {
	var list []Attachment
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, func() {}, err
		}
	}
	var out []mailer.Attachment
	var opened []*os.File
	closeAll := func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}
	for _, a := range list {
		if w.Files == nil {
			return nil, closeAll, errors.New("хранилище файлов не задано — вложение не отправить")
		}
		f, err := q.GetFile(ctx, a.FileID)
		if err != nil {
			return nil, closeAll, fmt.Errorf("вложение %s: %w", a.FileID, err)
		}
		fh, err := w.Files.Open(f.StorageKey)
		if err != nil {
			return nil, closeAll, fmt.Errorf("вложение %s: %w", a.FileID, err)
		}
		opened = append(opened, fh)
		out = append(out, mailer.Attachment{Name: a.Name, ContentType: f.ContentType, Data: fh})
	}
	return out, closeAll, nil
}
