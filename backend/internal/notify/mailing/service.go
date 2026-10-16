// Package mailing — шаблоны писем (FR-10.3), журнал отправки с повторной отправкой и плановые рассылки
// (FR-10.1, FR-10.2, FR-05.5): напоминания об оплате и просрочке, окончание договора, показания, утренняя сводка
// собственнику. Правила — docs/spec/10 Уведомления.md и 07 Фоновые задачи.md.
package mailing

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/calendar"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/platform/tmpl"
	"landlord/backend/internal/settings"
)

// Service — шаблоны, журнал отправки, рассылки.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	notify   *notify.Service
	settings *settings.Service
	calendar *calendar.Service
	policy   *bluemonday.Policy
}

// NewService создаёт сервис рассылок.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, nt *notify.Service, st *settings.Service,
	cal *calendar.Service,
) *Service {
	return &Service{db: database, audit: au, clock: clk, notify: nt, settings: st, calendar: cal, policy: htmlPolicy()}
}

// htmlPolicy — очистка HTML шаблона письма (12 разд. 9: без <script>, on*-атрибутов и javascript:-ссылок).
func htmlPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowElements("table", "thead", "tbody", "tr", "th", "td", "u", "s", "span", "br")
	p.AllowAttrs("style").OnElements("p", "td", "th", "table", "span")
	p.RequireNoFollowOnLinks(false)
	return p
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

// ---------------------------------------------------------------- Шаблоны писем

// Templates — список шаблонов писем.
func (s *Service) Templates(ctx context.Context) ([]sqlc.EmailTemplate, error) {
	return s.db.Queries().ListEmailTemplates(ctx)
}

// Template — шаблон письма по коду.
func (s *Service) Template(ctx context.Context, code string) (sqlc.EmailTemplate, error) {
	t, err := s.db.Queries().GetEmailTemplate(ctx, code)
	if db.IsNoRows(err) {
		return t, apperr.NotFound()
	}
	return t, err
}

// TemplateInput — изменение шаблона письма.
type TemplateInput struct {
	Subject, BodyHTML string
	Enabled           bool
}

// clean — очистка и проверка переменных: неизвестные поля → 422 «Неизвестные поля: …» (10 разд. 1).
func (s *Service) clean(code string, in *TemplateInput) error {
	in.Subject = strings.TrimSpace(in.Subject)
	in.BodyHTML = strings.TrimSpace(s.policy.Sanitize(in.BodyHTML))
	// bluemonday экранирует фигурные скобки в адресах ссылок — возвращаем переменные как были.
	in.BodyHTML = strings.NewReplacer("%7B%7B", "{{", "%7D%7D", "}}").Replace(in.BodyHTML)
	allowed := notify.Allowed(code)
	var errs []apperr.FieldError
	if in.Subject == "" {
		errs = append(errs, apperr.FieldError{Field: "subject", Message: "Укажите тему письма"})
	} else if u := tmpl.Unknown(in.Subject, allowed); len(u) > 0 {
		errs = append(errs, apperr.FieldError{Field: "subject", Message: "Неизвестные поля: " + strings.Join(u, ", ")})
	}
	if in.BodyHTML == "" {
		errs = append(errs, apperr.FieldError{Field: "bodyHtml", Message: "Текст письма не может быть пустым"})
	} else if u := tmpl.Unknown(in.BodyHTML, allowed); len(u) > 0 {
		errs = append(errs, apperr.FieldError{Field: "bodyHtml", Message: "Неизвестные поля: " + strings.Join(u, ", ")})
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// UpdateTemplate — сохранение шаблона письма (тема, текст, включён).
func (s *Service) UpdateTemplate(ctx context.Context, code string, in TemplateInput) (sqlc.EmailTemplate, error) {
	if _, err := s.Template(ctx, code); err != nil {
		return sqlc.EmailTemplate{}, err
	}
	if err := s.clean(code, &in); err != nil {
		return sqlc.EmailTemplate{}, err
	}
	return s.save(ctx, code, in, "Изменён шаблон письма")
}

// ResetTemplate — текст по умолчанию (тема и тело; признак «включён» не меняется).
func (s *Service) ResetTemplate(ctx context.Context, code string) (sqlc.EmailTemplate, error) {
	t, err := s.Template(ctx, code)
	if err != nil {
		return t, err
	}
	def, err := notify.Defaults()
	if err != nil {
		return t, err
	}
	d, ok := def[code]
	if !ok {
		return t, apperr.NotFound()
	}
	return s.save(ctx, code, TemplateInput{Subject: d.Subject, BodyHTML: d.BodyHTML, Enabled: t.Enabled},
		"Шаблон письма сброшен к стандартному")
}

func (s *Service) save(ctx context.Context, code string, in TemplateInput, what string) (sqlc.EmailTemplate, error) {
	var out sqlc.EmailTemplate
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetEmailTemplate(ctx, code)
		if err != nil {
			return err
		}
		out, err = tx.Q.UpdateEmailTemplate(ctx, sqlc.UpdateEmailTemplateParams{
			Code: code, Subject: in.Subject, BodyHtml: in.BodyHTML, Enabled: in.Enabled, UpdatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "email_template.update", EntityType: "email_template", EntityID: code,
			Summary: fmt.Sprintf("%s «%s»", what, before.Name),
			Before:  map[string]any{"subject": before.Subject, "bodyHtml": before.BodyHtml, "enabled": before.Enabled},
			After:   map[string]any{"subject": out.Subject, "bodyHtml": out.BodyHtml, "enabled": out.Enabled},
		})
	})
	return out, err
}

// Preview — письмо на примерах значений (без сохранения); неизвестные поля — 422, как при сохранении.
func (s *Service) Preview(ctx context.Context, code, subject, body string) (string, string, error) {
	if _, err := s.Template(ctx, code); err != nil {
		return "", "", err
	}
	in := TemplateInput{Subject: subject, BodyHTML: body}
	if err := s.clean(code, &in); err != nil {
		return "", "", err
	}
	return s.notify.Preview(ctx, s.db.Queries(), code, in.Subject, in.BodyHTML)
}

// ---------------------------------------------------------------- Журнал отправки

// LogFilter — фильтры журнала отправки.
type LogFilter = sqlc.ListEmailLogParams

// Log — письма по фильтрам.
func (s *Service) Log(ctx context.Context, f LogFilter) ([]sqlc.EmailLog, int64, error) {
	q := s.db.Queries()
	rows, err := q.ListEmailLog(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	n, err := q.CountEmailLog(ctx, sqlc.CountEmailLogParams{
		Status: f.Status, TemplateCode: f.TemplateCode, DateFrom: f.DateFrom, DateTo: f.DateTo, Q: f.Q,
	})
	return rows, n, err
}

// Resend — повторная отправка: новая запись с dedupe_key «<ключ>:resend:<n>» и задача email.send (06 разд. 14).
// Пропущенное письмо (нет адреса) повторно не отправляется.
func (s *Service) Resend(ctx context.Context, id uuid.UUID) (sqlc.EmailLog, error) {
	var out sqlc.EmailLog
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		m, err := tx.Q.GetEmailLog(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if m.ToEmail == "—" || !strings.Contains(m.ToEmail, "@") {
			return apperr.BusinessRule("У письма нет адреса получателя — повторная отправка невозможна")
		}
		base, _, _ := strings.Cut(m.DedupeKey, ":resend:")
		n, err := tx.Q.CountResends(ctx, base)
		if err != nil {
			return err
		}
		out, err = tx.Q.InsertResend(ctx, sqlc.InsertResendParams{
			SourceID: id, DedupeKey: fmt.Sprintf("%s:resend:%d", base, n+1), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.notify.Send(ctx, tx, out.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "email.resend", EntityType: "email_log", EntityID: out.ID.String(),
			Summary: fmt.Sprintf("Повторная отправка письма «%s»", m.TemplateCode),
			After:   map[string]any{"sourceId": id, "templateCode": m.TemplateCode},
		})
	})
	return out, err
}
