package mailing

import (
	"context"
	"encoding/json"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API шаблонов писем и журнала отправки (06 разд. 4, 14).
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func templateItem(t sqlc.EmailTemplate) api.EmailTemplateListItem {
	return api.EmailTemplateListItem{Code: api.EmailTemplateCode(t.Code), Name: t.Name, Recipient: api.EmailRecipient(t.Recipient),
		Subject: t.Subject, Enabled: t.Enabled, UpdatedAt: t.UpdatedAt}
}

func templateDTO(t sqlc.EmailTemplate) api.EmailTemplateDTO {
	i := templateItem(t)
	return api.EmailTemplateDTO{Code: i.Code, Name: i.Name, Recipient: i.Recipient, Subject: i.Subject, Enabled: i.Enabled,
		UpdatedAt: i.UpdatedAt, BodyHtml: t.BodyHtml}
}

// ListEmailTemplates — GET /settings/email-templates.
func (h *Handler) ListEmailTemplates(ctx context.Context, _ api.ListEmailTemplatesRequestObject) (api.ListEmailTemplatesResponseObject, error) {
	list, err := h.svc.Templates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListEmailTemplates200JSONResponse, 0, len(list))
	for _, t := range list {
		out = append(out, templateItem(t))
	}
	return out, nil
}

// GetEmailTemplate — GET /settings/email-templates/{code}.
func (h *Handler) GetEmailTemplate(ctx context.Context, req api.GetEmailTemplateRequestObject) (api.GetEmailTemplateResponseObject, error) {
	t, err := h.svc.Template(ctx, string(req.Code))
	if err != nil {
		return nil, err
	}
	return api.GetEmailTemplate200JSONResponse(templateDTO(t)), nil
}

// UpdateEmailTemplate — PUT /settings/email-templates/{code}.
func (h *Handler) UpdateEmailTemplate(ctx context.Context, req api.UpdateEmailTemplateRequestObject) (api.UpdateEmailTemplateResponseObject, error) {
	b := req.Body
	t, err := h.svc.UpdateTemplate(ctx, string(req.Code), TemplateInput{Subject: b.Subject, BodyHTML: b.BodyHtml, Enabled: b.Enabled})
	if err != nil {
		return nil, err
	}
	return api.UpdateEmailTemplate200JSONResponse(templateDTO(t)), nil
}

// ResetEmailTemplate — POST /settings/email-templates/{code}/reset.
func (h *Handler) ResetEmailTemplate(ctx context.Context, req api.ResetEmailTemplateRequestObject) (api.ResetEmailTemplateResponseObject, error) {
	t, err := h.svc.ResetTemplate(ctx, string(req.Code))
	if err != nil {
		return nil, err
	}
	return api.ResetEmailTemplate200JSONResponse(templateDTO(t)), nil
}

// PreviewEmailTemplate — POST /settings/email-templates/{code}/preview.
func (h *Handler) PreviewEmailTemplate(ctx context.Context, req api.PreviewEmailTemplateRequestObject) (
	api.PreviewEmailTemplateResponseObject, error,
) {
	subject, html, err := h.svc.Preview(ctx, string(req.Code), req.Body.Subject, req.Body.BodyHtml)
	if err != nil {
		return nil, err
	}
	return api.PreviewEmailTemplate200JSONResponse{Subject: subject, Html: html}, nil
}

func logDTO(m sqlc.EmailLog) api.EmailLogDTO {
	atts := []api.EmailAttachment{}
	if len(m.Attachments) > 0 {
		_ = json.Unmarshal(m.Attachments, &atts)
	}
	return api.EmailLogDTO{
		Id: m.ID, TemplateCode: api.EmailTemplateCode(m.TemplateCode), ToEmail: m.ToEmail, Subject: m.Subject, BodyHtml: m.BodyHtml,
		Attachments: atts, Status: api.EmailStatus(m.Status), Attempts: int(m.Attempts), LastError: m.LastError,
		RelatedEntityType: m.RelatedEntityType, RelatedEntityId: m.RelatedEntityID, CreatedAt: m.CreatedAt, SentAt: m.SentAt,
	}
}

// ListEmailLog — GET /email-log.
func (h *Handler) ListEmailLog(ctx context.Context, req api.ListEmailLogRequestObject) (api.ListEmailLogResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := LogFilter{DateFrom: apiconv.ToDatePtr(p.From), DateTo: apiconv.ToDatePtr(p.To), Q: apiconv.TrimPtr(p.Q),
		Lim: paging.Limit(), Off: paging.Offset()}
	if p.Status != nil {
		f.Status = apiconv.Ptr(string(*p.Status))
	}
	if p.TemplateCode != nil {
		f.TemplateCode = apiconv.Ptr(string(*p.TemplateCode))
	}
	rows, n, err := h.svc.Log(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]api.EmailLogDTO, 0, len(rows))
	for _, m := range rows {
		items = append(items, logDTO(m))
	}
	return api.ListEmailLog200JSONResponse{Items: items, Total: int(n), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// ResendEmail — POST /email-log/{id}/resend.
func (h *Handler) ResendEmail(ctx context.Context, req api.ResendEmailRequestObject) (api.ResendEmailResponseObject, error) {
	m, err := h.svc.Resend(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.ResendEmail201JSONResponse(logDTO(m)), nil
}
