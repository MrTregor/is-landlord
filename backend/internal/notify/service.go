// Package notify — письма (FR-10, docs/spec/10 Уведомления.md): создание записи email_log по шаблону
// и постановка задачи email.send в той же транзакции; каталог переменных и тексты по умолчанию. Плановые рассылки,
// шаблоны и журнал отправки — пакет notify/mailing.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/tmpl"
)

// Inserter — постановка задач River в транзакции (river.Client).
type Inserter interface {
	InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// EmailSendArgs — задача отправки письма.
type EmailSendArgs struct {
	EmailLogID uuid.UUID `json:"emailLogId"`
}

// Kind — вид задачи River.
func (EmailSendArgs) Kind() string { return "email.send" }

// InsertOpts — очередь email, 10 попыток (07 Фоновые задачи).
func (EmailSendArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueEmail, MaxAttempts: jobs.DefaultMaxAttempts}
}

// Email — письмо к постановке в очередь.
type Email struct {
	Code      string
	To        string
	Vars      map[string]string
	Blocks    map[string]string
	DedupeKey string
	// RelatedType и RelatedID — сущность, к которой относится письмо (журнал отправки, обезличивание).
	RelatedType string
	RelatedID   *uuid.UUID
	// Attachments — вложения (файлы системы).
	Attachments []Attachment
}

// Attachment — вложение письма: файл и имя (email_log.attachments).
type Attachment struct {
	FileID uuid.UUID `json:"fileId"`
	Name   string    `json:"name"`
}

// Service — постановка писем.
type Service struct {
	cfg   *config.Config
	clock clock.Clock
	jobs  Inserter
}

// NewService создаёт сервис писем.
func NewService(cfg *config.Config, clk clock.Clock, ins Inserter) *Service {
	return &Service{cfg: cfg, clock: clk, jobs: ins}
}

// Queue создаёт запись email_log и задачу email.send в транзакции tx. Повтор с тем же dedupe_key ничего не делает.
// Выключенный шаблон или пустой адрес → запись со статусом skipped.
func (s *Service) Queue(ctx context.Context, tx *db.Tx, e Email) error {
	_, err := s.Enqueue(ctx, tx, e)
	return err
}

// Enqueue — Queue с признаком «письмо создано» (false — письмо с этим dedupe_key уже было): для итогов рассылок.
func (s *Service) Enqueue(ctx context.Context, tx *db.Tx, e Email) (bool, error) {
	t, err := tx.Q.GetEmailTemplate(ctx, e.Code)
	if err != nil {
		return false, fmt.Errorf("шаблон письма %s: %w", e.Code, err)
	}
	vars, err := s.commonVars(ctx, tx.Q)
	if err != nil {
		return false, err
	}
	for k, v := range e.Vars {
		vars[k] = v
	}
	subject := html.UnescapeString(tmpl.Render(t.Subject, vars, nil, tmpl.Options{}))
	body := s.wrap(tmpl.Render(UnwrapBlocks(t.BodyHtml), vars, e.Blocks, tmpl.Options{}), vars)

	status := "queued"
	to := strings.TrimSpace(e.To)
	if !t.Enabled || to == "" {
		status = "skipped"
	}
	if to == "" {
		to = "—"
	}
	atts := e.Attachments
	if atts == nil {
		atts = []Attachment{}
	}
	attJSON, err := json.Marshal(atts)
	if err != nil {
		return false, err
	}
	params := sqlc.InsertEmailLogParams{
		TemplateCode: e.Code, ToEmail: to, Subject: subject, BodyHtml: body, Attachments: attJSON,
		Status: status, DedupeKey: e.DedupeKey, RelatedEntityID: e.RelatedID, CreatedAt: s.clock.Now(),
	}
	if e.RelatedType != "" {
		params.RelatedEntityType = &e.RelatedType
	}
	id, err := tx.Q.InsertEmailLog(ctx, params)
	if db.IsNoRows(err) {
		return false, nil // письмо с этим dedupe_key уже создано
	}
	if err != nil {
		return false, err
	}
	if status != "queued" {
		return true, nil
	}
	return true, s.Send(ctx, tx, id)
}

// Send ставит задачу email.send для записи email_log в транзакции tx.
func (s *Service) Send(ctx context.Context, tx *db.Tx, emailLogID uuid.UUID) error {
	_, err := s.jobs.InsertTx(ctx, tx.Tx, EmailSendArgs{EmailLogID: emailLogID}, nil)
	return err
}

// QueueOwners — письмо всем действующим собственникам: по письму на каждого, dedupe_key + «:<userId>»
// (10 Уведомления, раздел 1). Возвращает число созданных писем (повтор с тем же ключом — 0).
func (s *Service) QueueOwners(ctx context.Context, tx *db.Tx, e Email) (int, error) {
	owners, err := tx.Q.ActiveOwnerUsers(ctx)
	if err != nil {
		return 0, err
	}
	key, n := e.DedupeKey, 0
	for _, o := range owners {
		e.To, e.DedupeKey = o.Email, key+":"+o.ID.String()
		created, err := s.Enqueue(ctx, tx, e)
		if err != nil {
			return n, err
		}
		if created {
			n++
		}
	}
	return n, nil
}

// Виды расхождений для писем (04, справочник значений).
var discrepancyKinds = map[string]string{
	"underpayment": "Недоплата", "overpayment": "Переплата", "unidentified": "Неопознанный платёж",
}

// OwnerDiscrepancy — письмо собственникам owner_discrepancy о новом расхождении «неопознанный» или «переплата»
// (10 Уведомления, раздел 3); повтор по тому же расхождению письмо не создаёт. amount — сумма в рублях текстом.
func (s *Service) OwnerDiscrepancy(ctx context.Context, tx *db.Tx, id uuid.UUID, kind, amount, details string) error {
	_, err := s.QueueOwners(ctx, tx, Email{
		Code: "owner_discrepancy", DedupeKey: "owner_discrepancy:" + id.String(), RelatedType: "discrepancy", RelatedID: &id,
		Vars: map[string]string{"kind_title": discrepancyKinds[kind], "amount": amount, "details": details,
			"link": s.Link("/reconciliation")},
	})
	return err
}

func (s *Service) commonVars(ctx context.Context, q *sqlc.Queries) (map[string]string, error) {
	base := strings.TrimRight(s.cfg.AppPublicURL, "/")
	vars := map[string]string{
		"app_name":    s.cfg.AppName,
		"cabinet_url": base + "/cabinet",
		"owner_url":   base,
	}
	org, err := q.GetOrganizationSettings(ctx)
	switch {
	case err == nil:
		vars["org.short_name"] = org.ShortName
		vars["org.email"] = org.Email
		if org.Phone != nil {
			vars["org.phone"] = *org.Phone
		}
	case db.IsNoRows(err):
	default:
		return nil, err
	}
	return vars, nil
}

// wrap — обёртка письма (не редактируется): шапка с названием системы, тело, подвал (10 Уведомления, раздел 1).
func (s *Service) wrap(body string, vars map[string]string) string {
	footer := tmpl.Render("Это автоматическое письмо системы «{{app_name}}» {{org.short_name}}. Вопросы: {{org.phone}}, {{org.email}}",
		vars, nil, tmpl.Options{})
	return `<!doctype html><html lang="ru"><head><meta charset="utf-8"></head>` +
		`<body style="margin:0;background:#f4f6f8;font-family:system-ui,-apple-system,'Segoe UI',Roboto,sans-serif;color:#1f2937">` +
		`<div style="max-width:600px;margin:0 auto;background:#ffffff">` +
		`<div style="padding:16px 24px;background:#10b981;color:#ffffff;font-size:18px;font-weight:600">` +
		html.EscapeString(s.cfg.AppName) + `</div>` +
		`<div style="padding:24px;font-size:14px;line-height:1.5">` + body + `</div>` +
		`<div style="padding:16px 24px;color:#6b7280;font-size:12px;border-top:1px solid #e5e7eb">` + footer + `</div>` +
		`</div></body></html>`
}

// blockParagraph — абзац, в котором только блок {{block:…}} (так его сохраняет редактор шаблонов).
var blockParagraph = regexp.MustCompile(`<p>\s*(\{\{\s*block:[A-Za-z0-9_.]+\s*\}\})\s*</p>`)

// UnwrapBlocks — блок вместо абзаца с ним: таблица не может стоять внутри <p>.
func UnwrapBlocks(body string) string { return blockParagraph.ReplaceAllString(body, "$1") }

// Link — абсолютная ссылка на страницу системы (APP_PUBLIC_URL + path).
func (s *Service) Link(path string) string { return strings.TrimRight(s.cfg.AppPublicURL, "/") + path }
