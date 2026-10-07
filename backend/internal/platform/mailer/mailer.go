// Package mailer — отправка писем по SMTP (08 Интеграции и эмуляторы, раздел 5).
package mailer

import (
	"context"
	"fmt"
	"io"

	"github.com/wneessen/go-mail"

	"landlord/backend/internal/config"
)

// Attachment — вложение письма.
type Attachment struct {
	Name        string
	ContentType string
	Data        io.Reader
}

// Message — письмо: HTML и текстовая версия (multipart/alternative).
type Message struct {
	To          string
	ReplyTo     string
	Subject     string
	HTML        string
	Text        string
	Attachments []Attachment
}

// Sender отправляет письма.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP — отправка через SMTP-сервер (в разработке — Mailpit).
type SMTP struct {
	cfg *config.Config
}

// NewSMTP создаёт отправителя по настройкам SMTP_*.
func NewSMTP(cfg *config.Config) *SMTP { return &SMTP{cfg: cfg} }

func (s *SMTP) client() (*mail.Client, error) {
	opts := []mail.Option{mail.WithPort(s.cfg.SMTPPort)}
	switch s.cfg.SMTPTLS {
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case "starttls":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case "tls":
		opts = append(opts, mail.WithSSL())
	}
	if s.cfg.SMTPUser != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.cfg.SMTPUser), mail.WithPassword(s.cfg.SMTPPassword))
	}
	return mail.NewClient(s.cfg.SMTPHost, opts...)
}

// Send отправляет письмо. Заголовок List-Unsubscribe не ставится (служебные письма).
func (s *SMTP) Send(ctx context.Context, m Message) error {
	msg := mail.NewMsg()
	if err := msg.From(s.cfg.MailFrom); err != nil {
		return fmt.Errorf("MAIL_FROM: %w", err)
	}
	if err := msg.To(m.To); err != nil {
		return fmt.Errorf("адрес получателя: %w", err)
	}
	if m.ReplyTo != "" {
		if err := msg.ReplyTo(m.ReplyTo); err != nil {
			return fmt.Errorf("адрес для ответа: %w", err)
		}
	}
	msg.Subject(m.Subject)
	msg.SetBodyString(mail.TypeTextPlain, m.Text)
	msg.AddAlternativeString(mail.TypeTextHTML, m.HTML)
	for _, a := range m.Attachments {
		if err := msg.AttachReader(a.Name, a.Data, mail.WithFileContentType(mail.ContentType(a.ContentType))); err != nil {
			return fmt.Errorf("вложение: %w", err)
		}
	}
	c, err := s.client()
	if err != nil {
		return err
	}
	return c.DialAndSendWithContext(ctx, msg)
}
