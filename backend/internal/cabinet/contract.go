package cabinet

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/contract"
	"landlord/backend/internal/meter"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
)

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	return apiconv.Ptr(d.String())
}

// documentURL — документ в кабинете.
func documentURL(id fmt.Stringer) string { return "/api/v1/cabinet/documents/" + id.String() + "/pdf" }

// GetCabinetContract — GET /cabinet/contracts/{id} (15 разд. 7): условия без заметок собственника, проживающие,
// видимые нанимателю документы, подписанные допсоглашения, история платы.
func (h *Handler) GetCabinetContract(ctx context.Context, req api.GetCabinetContractRequestObject) (api.GetCabinetContractResponseObject, error) {
	c, err := h.contract(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	q := h.db.Queries()
	p, err := q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return nil, err
	}
	terms, err := q.ListTerms(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	occ, err := q.ListOccupants(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	docs, err := q.CabinetDocuments(ctx, &c.ID)
	if err != nil {
		return nil, err
	}
	amends, err := q.ListAmendments(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	end := c.EndDate
	if c.ActualEndDate != nil {
		end = *c.ActualEndDate
	}
	out := api.CabinetContract{
		Id: c.ID, Number: c.Number, LegalBasis: api.LegalBasis(c.LegalBasis), Status: api.ContractStatus(c.Status),
		IsShortTerm: c.IsShortTerm != nil && *c.IsShortTerm, PropertyCode: p.Code, PropertyAddress: p.Address,
		StartDate: apiconv.FromDate(c.StartDate), EndDate: apiconv.FromDate(c.EndDate), ActualEndDate: apiconv.FromDate(end),
		TermsHistory: make([]api.ContractTermsDTO, 0, len(terms)), DepositKop: c.DepositKop,
		UtilitiesMode: api.UtilitiesMode(c.UtilitiesMode), PenaltyEnabled: c.PenaltyEnabled, PenaltyRatePct: decStr(c.PenaltyRatePct),
		PenaltyGraceDays: int(c.PenaltyGraceDays), PenaltyCapPct: decStr(c.PenaltyCapPct), PenaltyOnUtilities: c.PenaltyOnUtilities,
		IndexationEnabled: c.IndexationEnabled, IndexationPct: decStr(c.IndexationPct), IndexationPeriodMonths: int(c.IndexationPeriodMonths),
		SpecialTerms: c.SpecialTerms, Occupants: make([]api.OccupantDTO, 0, len(occ)),
		Documents: make([]api.CabinetDocumentDTO, 0, len(docs)), Amendments: []api.AmendmentDTO{},
	}
	if len(terms) > 0 {
		out.CurrentTerms = contract.TermsDTO(h.contracts.CurrentTerms(c, terms))
	}
	for _, t := range terms {
		out.TermsHistory = append(out.TermsHistory, contract.TermsDTO(t))
	}
	for _, o := range occ {
		out.Occupants = append(out.Occupants, api.OccupantDTO{Id: o.ID, FullName: o.FullName, BirthDate: apiconv.FromDatePtr(o.BirthDate), Relation: o.Relation})
	}
	for _, d := range docs {
		out.Documents = append(out.Documents, api.CabinetDocumentDTO{Id: d.ID, Kind: api.DocumentKind(d.Kind), Number: d.Number,
			Title: d.Title, CreatedAt: d.CreatedAt, PdfUrl: documentURL(d.ID)})
	}
	for _, a := range amends {
		if a.Status == "signed" {
			out.Amendments = append(out.Amendments, contract.AmendmentDTO(a))
		}
	}
	return api.GetCabinetContract200JSONResponse(out), nil
}

// GetCabinetDocumentPdf — GET /cabinet/documents/{id}/pdf: документ своего договора, видимый нанимателю (иначе 404).
func (h *Handler) GetCabinetDocumentPdf(ctx context.Context, req api.GetCabinetDocumentPdfRequestObject) (api.GetCabinetDocumentPdfResponseObject, error) {
	d, err := h.db.Queries().GetDocument(ctx, req.Id)
	if db.IsNoRows(err) || (err == nil && (d.ContractID == nil || !d.VisibleToTenant)) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, err
	}
	if _, err := h.contract(ctx, *d.ContractID); err != nil {
		return nil, err
	}
	return h.documents.PDF(ctx, d, req.Params.Download != nil && *req.Params.Download == 1)
}

func readingDTO(r meter.CabinetReading) api.CabinetMeterReading {
	return api.CabinetMeterReading{PeriodMonth: apiconv.FromDate(r.PeriodMonth), Value: r.Value.String(),
		Status: api.ReadingStatus(r.Status), RejectReason: r.RejectReason}
}

func metersDTO(m meter.CabinetMeters) api.CabinetMeters {
	w := m.Window
	out := api.CabinetMeters{Meters: make([]api.CabinetMeter, 0, len(m.Meters)), Window: api.ReadingsWindow{
		PeriodMonth: apiconv.FromDate(w.PeriodMonth), Open: w.Open, From: apiconv.FromDate(w.From), To: apiconv.FromDate(w.To),
	}}
	for _, cm := range m.Meters {
		d := api.CabinetMeter{MeterId: cm.Meter.ID, Type: api.MeterType(cm.Meter.Type), SerialNumber: cm.Meter.SerialNumber,
			LastValue: decStr(cm.LastValue), LastDate: apiconv.FromDatePtr(cm.LastDate), History: make([]api.CabinetMeterReading, 0, len(cm.History))}
		if cm.Current != nil {
			d.Current = apiconv.Ptr(readingDTO(*cm.Current))
		}
		for _, r := range cm.History {
			d.History = append(d.History, readingDTO(r))
		}
		out.Meters = append(out.Meters, d)
	}
	return out
}

// GetCabinetMeters — GET /cabinet/meters (15 разд. 5).
func (h *Handler) GetCabinetMeters(ctx context.Context, req api.GetCabinetMetersRequestObject) (api.GetCabinetMetersResponseObject, error) {
	c, err := h.contract(ctx, req.Params.ContractId)
	if err != nil {
		return nil, err
	}
	m, err := h.meters.TenantMeters(ctx, c)
	if err != nil {
		return nil, err
	}
	return api.GetCabinetMeters200JSONResponse(metersDTO(m)), nil
}

// SubmitCabinetMeterReadings — POST /cabinet/meter-readings: показания по всем счётчикам объекта (05 разд. 6.2).
func (h *Handler) SubmitCabinetMeterReadings(ctx context.Context, req api.SubmitCabinetMeterReadingsRequestObject) (
	api.SubmitCabinetMeterReadingsResponseObject, error,
) {
	b := req.Body
	c, err := h.contract(ctx, b.ContractId)
	if err != nil {
		return nil, err
	}
	list := make([]meter.TenantReading, 0, len(b.Readings))
	var fields []apperr.FieldError
	for i, r := range b.Readings {
		v, err := decimal.NewFromString(r.Value)
		if err != nil {
			fields = append(fields, apperr.FieldError{Field: fmt.Sprintf("readings[%d].value", i), Message: "Введите число"})
			continue
		}
		list = append(list, meter.TenantReading{MeterID: r.MeterId, Value: v})
	}
	if len(fields) > 0 {
		return nil, apperr.Validation(fields...)
	}
	if err := h.meters.SubmitTenantReadings(ctx, c, apiconv.ToDate(b.PeriodMonth), list); err != nil {
		return nil, err
	}
	m, err := h.meters.TenantMeters(ctx, c)
	if err != nil {
		return nil, err
	}
	return api.SubmitCabinetMeterReadings201JSONResponse(metersDTO(m)), nil
}
