//go:build integration

package notify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/mailer"
	"landlord/backend/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }

type fakeSender struct {
	sent []mailer.Message
	err  error
}

func (f *fakeSender) Send(_ context.Context, m mailer.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

// Письмо invite: запись email_log с dedupe_key, задача email.send, отправка воркером (FR-10, FR-13.2).
func TestInviteEmailQueuedAndSent_FR10(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t, "2026-10-16")
	u, link, err := env.App.Users.CreateOwner(ctx, "mail@landlord.local", "Александр Васильевич")
	require.NoError(t, err)

	conn := testsupport.Owner(t)
	var id, subject, body, status, dedupe string
	require.NoError(t, conn.QueryRow(ctx, `SELECT id, subject, body_html, status, dedupe_key FROM email_log
		WHERE template_code = 'invite' AND related_entity_id = $1`, u.ID).Scan(&id, &subject, &body, &status, &dedupe))
	assert.Equal(t, "Приглашение в личный кабинет — Наймодатель", subject)
	assert.Equal(t, "queued", status)
	assert.Contains(t, dedupe, "invite:")
	assert.Contains(t, body, "Здравствуйте, Александр Васильевич!")
	assert.Contains(t, body, link)

	var jobs int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'email.send' AND args->>'emailLogId' = $1`, id).Scan(&jobs))
	assert.Equal(t, 1, jobs, "задача поставлена в той же транзакции")

	// Отправка: неудача → повтор, успех → sent.
	var job notify.EmailSendArgs
	require.NoError(t, job.EmailLogID.UnmarshalText([]byte(id)))
	sender := &fakeSender{err: errors.New("SMTP недоступен")}
	w := &notify.EmailSendWorker{DB: env.App.DB, Clock: env.Clock, Sender: sender}
	rj := &river.Job[notify.EmailSendArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 10}, Args: job}
	assert.Error(t, w.Work(ctx, rj))
	require.NoError(t, conn.QueryRow(ctx, `SELECT status FROM email_log WHERE id = $1`, id).Scan(&status))
	assert.Equal(t, "queued", status)

	sender.err = nil
	rj.Attempt = 2
	require.NoError(t, w.Work(ctx, rj))
	require.Len(t, sender.sent, 1)
	assert.Equal(t, "mail@landlord.local", sender.sent[0].To)
	assert.Contains(t, sender.sent[0].Text, "Создать пароль и войти ("+link+")")
	var attempts int
	require.NoError(t, conn.QueryRow(ctx, `SELECT status, attempts FROM email_log WHERE id = $1`, id).Scan(&status, &attempts))
	assert.Equal(t, "sent", status)
	assert.Equal(t, 2, attempts)

	// Повторная обработка не отправляет письмо второй раз.
	require.NoError(t, w.Work(ctx, rj))
	assert.Len(t, sender.sent, 1)
}
