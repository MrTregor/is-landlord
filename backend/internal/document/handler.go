package document

import (
	"bytes"
	"context"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API документов и шаблонов документов.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// PDFURL — адрес PDF документа для собственника.
func PDFURL(id uuid.UUID) string { return "/api/v1/documents/" + id.String() + "/pdf" }

// DTO — документ для API.
func DTO(d sqlc.Document) api.DocumentDTO {
	out := api.DocumentDTO{Id: d.ID, Kind: api.DocumentKind(d.Kind), Number: d.Number, Title: d.Title,
		ContractId: d.ContractID, CreatedAt: d.CreatedAt, FileUrl: PDFURL(d.ID), VisibleToTenant: d.VisibleToTenant,
		SupersededBy: d.SupersededBy}
	if d.SignedScanFileID != nil {
		out.SignedScanUrl = apiconv.Ptr(file.URL(*d.SignedScanFileID))
	}
	return out
}

func versionDTO(v sqlc.GetTemplateVersionRow) api.DocumentTemplateVersion {
	return api.DocumentTemplateVersion{Id: v.ID, Kind: api.DocumentKind(v.Kind), Name: v.Name, Version: int(v.Version),
		BodyHtml: v.BodyHtml, IsActive: v.IsActive, IsSystem: v.IsSystem, CreatedAt: v.CreatedAt, CreatedBy: v.Author}
}

func detailDTO(d TemplateDetail) api.DocumentTemplateDetail {
	out := api.DocumentTemplateDetail{Kind: api.DocumentKind(d.Active.Kind), Active: versionDTO(d.Active),
		Versions: make([]api.DocumentTemplateVersionSummary, 0, len(d.Versions))}
	for _, v := range d.Versions {
		out.Versions = append(out.Versions, api.DocumentTemplateVersionSummary{Id: v.ID, Version: int(v.Version),
			CreatedAt: v.CreatedAt, CreatedBy: v.Author})
	}
	return out
}

// ListDocumentTemplates — активные шаблоны по видам.
func (h *Handler) ListDocumentTemplates(ctx context.Context, _ api.ListDocumentTemplatesRequestObject) (
	api.ListDocumentTemplatesResponseObject, error,
) {
	list, err := h.svc.Templates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListDocumentTemplates200JSONResponse, 0, len(list))
	for _, t := range list {
		out = append(out, api.DocumentTemplateListItem{Kind: api.DocumentKind(t.Kind), Name: t.Name, Version: int(t.Version),
			CreatedAt: t.CreatedAt, CreatedBy: t.Author})
	}
	return out, nil
}

// GetDocumentTemplate — активная версия и список версий.
func (h *Handler) GetDocumentTemplate(ctx context.Context, req api.GetDocumentTemplateRequestObject) (
	api.GetDocumentTemplateResponseObject, error,
) {
	d, err := h.svc.Template(ctx, string(req.Kind))
	if err != nil {
		return nil, err
	}
	return api.GetDocumentTemplate200JSONResponse(detailDTO(d)), nil
}

// GetDocumentTemplateVersion — текст версии.
func (h *Handler) GetDocumentTemplateVersion(ctx context.Context, req api.GetDocumentTemplateVersionRequestObject) (
	api.GetDocumentTemplateVersionResponseObject, error,
) {
	v, err := h.svc.TemplateVersion(ctx, string(req.Kind), req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetDocumentTemplateVersion200JSONResponse(versionDTO(v)), nil
}

// UpdateDocumentTemplate — новая версия.
func (h *Handler) UpdateDocumentTemplate(ctx context.Context, req api.UpdateDocumentTemplateRequestObject) (
	api.UpdateDocumentTemplateResponseObject, error,
) {
	d, err := h.svc.SaveTemplate(ctx, string(req.Kind), req.Body.Name, req.Body.BodyHtml)
	if err != nil {
		return nil, err
	}
	return api.UpdateDocumentTemplate200JSONResponse(detailDTO(d)), nil
}

// ResetDocumentTemplate — «Сбросить к стандартному».
func (h *Handler) ResetDocumentTemplate(ctx context.Context, req api.ResetDocumentTemplateRequestObject) (
	api.ResetDocumentTemplateResponseObject, error,
) {
	d, err := h.svc.ResetTemplate(ctx, string(req.Kind))
	if err != nil {
		return nil, err
	}
	return api.ResetDocumentTemplate200JSONResponse(detailDTO(d)), nil
}

// PreviewDocumentTemplate — PDF на демо-данных.
func (h *Handler) PreviewDocumentTemplate(ctx context.Context, req api.PreviewDocumentTemplateRequestObject) (
	api.PreviewDocumentTemplateResponseObject, error,
) {
	out, err := h.svc.Preview(ctx, string(req.Kind), req.Body.BodyHtml)
	if err != nil {
		return nil, err
	}
	return api.PreviewDocumentTemplate200ApplicationpdfResponse{Body: bytes.NewReader(out), ContentLength: int64(len(out))}, nil
}

// GetTemplateVariables — переменные шаблонов документов. Переменные писем добавляются на этапе M10
// (редактор шаблонов писем), до этого — пустой список.
func (h *Handler) GetTemplateVariables(context.Context, api.GetTemplateVariablesRequestObject) (
	api.GetTemplateVariablesResponseObject, error,
) {
	out := api.TemplateVariables{Documents: map[string][]api.TemplateVariable{}, Emails: map[string][]api.TemplateVariable{}}
	for _, k := range Kinds {
		vars := Variables(k)
		list := make([]api.TemplateVariable, 0, len(vars))
		for _, v := range vars {
			list = append(list, api.TemplateVariable{Key: v.Key, Title: v.Title, Example: v.Example})
		}
		out.Documents[k] = list
	}
	return api.GetTemplateVariables200JSONResponse(out), nil
}

// GenerateContractDocument — сформировать (заново) документ договора.
func (h *Handler) GenerateContractDocument(ctx context.Context, req api.GenerateContractDocumentRequestObject) (
	api.GenerateContractDocumentResponseObject, error,
) {
	d, err := h.svc.ContractDocument(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GenerateContractDocument201JSONResponse(DTO(d)), nil
}

// GenerateReconciliationAct — акт сверки за период.
func (h *Handler) GenerateReconciliationAct(ctx context.Context, req api.GenerateReconciliationActRequestObject) (
	api.GenerateReconciliationActResponseObject, error,
) {
	d, err := h.svc.ReconciliationAct(ctx, req.Id, apiconv.ToDate(req.Body.From), apiconv.ToDate(req.Body.To))
	if err != nil {
		return nil, err
	}
	return api.GenerateReconciliationAct201JSONResponse(DTO(d)), nil
}

// ListContractDocuments — документы договора.
func (h *Handler) ListContractDocuments(ctx context.Context, req api.ListContractDocumentsRequestObject) (
	api.ListContractDocumentsResponseObject, error,
) {
	list, err := h.svc.ContractDocuments(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(api.ListContractDocuments200JSONResponse, 0, len(list))
	for _, d := range list {
		out = append(out, DTO(d))
	}
	return out, nil
}

// GenerateAmendmentDocument — PDF допсоглашения.
func (h *Handler) GenerateAmendmentDocument(ctx context.Context, req api.GenerateAmendmentDocumentRequestObject) (
	api.GenerateAmendmentDocumentResponseObject, error,
) {
	d, err := h.svc.AmendmentDocument(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GenerateAmendmentDocument201JSONResponse(DTO(d)), nil
}

// GenerateActDocument — PDF акта.
func (h *Handler) GenerateActDocument(ctx context.Context, req api.GenerateActDocumentRequestObject) (
	api.GenerateActDocumentResponseObject, error,
) {
	d, err := h.svc.ActDocument(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GenerateActDocument201JSONResponse(DTO(d)), nil
}

// GetDocument — документ.
func (h *Handler) GetDocument(ctx context.Context, req api.GetDocumentRequestObject) (api.GetDocumentResponseObject, error) {
	d, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetDocument200JSONResponse(DTO(d)), nil
}

// UpdateDocument — видимость нанимателю.
func (h *Handler) UpdateDocument(ctx context.Context, req api.UpdateDocumentRequestObject) (api.UpdateDocumentResponseObject, error) {
	d, err := h.svc.SetVisibility(ctx, req.Id, req.Body.VisibleToTenant)
	if err != nil {
		return nil, err
	}
	return api.UpdateDocument200JSONResponse(DTO(d)), nil
}

// AttachSignedScan — скан подписанного экземпляра.
func (h *Handler) AttachSignedScan(ctx context.Context, req api.AttachSignedScanRequestObject) (
	api.AttachSignedScanResponseObject, error,
) {
	d, err := h.svc.AttachSignedScan(ctx, req.Id, req.Body.FileId)
	if err != nil {
		return nil, err
	}
	return api.AttachSignedScan200JSONResponse(DTO(d)), nil
}

// GetDocumentPdf — PDF документа (inline или attachment при ?download=1).
func (h *Handler) GetDocumentPdf(ctx context.Context, req api.GetDocumentPdfRequestObject) (api.GetDocumentPdfResponseObject, error) {
	d, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	f, fh, err := h.svc.files.Open(ctx, d.FileID)
	if err != nil {
		return nil, err
	}
	download := req.Params.Download != nil && *req.Params.Download == 1
	return file.Response{File: fh, Name: f.OriginalName, ContentType: f.ContentType, Size: f.SizeBytes, Download: download}, nil
}

// CreateConsentForm — бланк согласия на обработку ПДн.
func (h *Handler) CreateConsentForm(ctx context.Context, req api.CreateConsentFormRequestObject) (
	api.CreateConsentFormResponseObject, error,
) {
	d, err := h.svc.ConsentForm(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.CreateConsentForm201JSONResponse(DTO(d)), nil
}
