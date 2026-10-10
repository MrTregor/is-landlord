package meter

import (
	"context"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API счётчиков и тарифов.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func input(b api.MeterInput) (Input, error) {
	v, err := decimal.NewFromString(b.InitialValue)
	if err != nil {
		return Input{}, apperr.Validation(apperr.FieldError{Field: "initialValue", Message: "Укажите показание числом"})
	}
	return Input{
		Type: string(b.Type), SerialNumber: b.SerialNumber, InitialValue: v, InitialDate: apiconv.ToDate(b.InitialDate),
		VerificationDue: apiconv.ToDatePtr(b.VerificationDue), Note: b.Note,
	}, nil
}

func (h *Handler) dto(ctx context.Context, q *sqlc.Queries, m sqlc.Meter) (api.MeterDTO, error) {
	d := api.MeterDTO{
		Id: m.ID, PropertyId: m.PropertyID, Type: api.MeterType(m.Type), SerialNumber: m.SerialNumber,
		InitialValue: m.InitialValue.StringFixed(3), InitialDate: apiconv.FromDate(m.InitialDate),
		VerificationDue: apiconv.FromDatePtr(m.VerificationDue), Note: m.Note, ArchivedAt: m.ArchivedAt,
	}
	r, err := q.LastAcceptedReading(ctx, m.ID)
	switch {
	case err == nil:
		d.LastReading = &api.MeterLastReading{Value: r.Value.StringFixed(3), ReadingDate: apiconv.FromDate(r.ReadingDate),
			PeriodMonth: apiconv.FromDate(r.PeriodMonth)}
	case db.IsNoRows(err):
	default:
		return d, err
	}
	return d, nil
}

// ListPropertyMeters — счётчики объекта с последним принятым показанием.
func (h *Handler) ListPropertyMeters(ctx context.Context, req api.ListPropertyMetersRequestObject) (api.ListPropertyMetersResponseObject, error) {
	q := h.svc.db.Queries()
	if _, err := q.GetProperty(ctx, req.Id); db.IsNoRows(err) {
		return nil, apperr.NotFound()
	} else if err != nil {
		return nil, err
	}
	rows, err := q.ListMeters(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(api.ListPropertyMeters200JSONResponse, 0, len(rows))
	for _, m := range rows {
		d, err := h.dto(ctx, q, m)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// CreateMeter — добавить счётчик.
func (h *Handler) CreateMeter(ctx context.Context, req api.CreateMeterRequestObject) (api.CreateMeterResponseObject, error) {
	in, err := input(*req.Body)
	if err != nil {
		return nil, err
	}
	m, err := h.svc.Create(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	d, err := h.dto(ctx, h.svc.db.Queries(), m)
	if err != nil {
		return nil, err
	}
	return api.CreateMeter201JSONResponse(d), nil
}

// UpdateMeter — изменить счётчик.
func (h *Handler) UpdateMeter(ctx context.Context, req api.UpdateMeterRequestObject) (api.UpdateMeterResponseObject, error) {
	in, err := input(*req.Body)
	if err != nil {
		return nil, err
	}
	m, err := h.svc.Update(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	d, err := h.dto(ctx, h.svc.db.Queries(), m)
	if err != nil {
		return nil, err
	}
	return api.UpdateMeter200JSONResponse(d), nil
}

// ArchiveMeter — счётчик в архив.
func (h *Handler) ArchiveMeter(ctx context.Context, req api.ArchiveMeterRequestObject) (api.ArchiveMeterResponseObject, error) {
	m, err := h.svc.Archive(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := h.dto(ctx, h.svc.db.Queries(), m)
	if err != nil {
		return nil, err
	}
	return api.ArchiveMeter200JSONResponse(d), nil
}

// ListTariffs — тарифы.
func (h *Handler) ListTariffs(ctx context.Context, req api.ListTariffsRequestObject) (api.ListTariffsResponseObject, error) {
	p := sqlc.ListTariffsParams{PropertyID: req.Params.PropertyId}
	if req.Params.MeterType != nil {
		p.MeterType = apiconv.Ptr(string(*req.Params.MeterType))
	}
	rows, err := h.svc.db.Queries().ListTariffs(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make(api.ListTariffs200JSONResponse, 0, len(rows))
	for _, t := range rows {
		out = append(out, api.TariffDTO{
			Id: t.ID, MeterType: api.MeterType(t.MeterType), PropertyId: t.PropertyID, PropertyCode: t.PropertyCode,
			Price: t.Price.StringFixed(4), EffectiveFrom: apiconv.FromDate(t.EffectiveFrom), Note: t.Note, CreatedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// CreateTariff — добавить тариф.
func (h *Handler) CreateTariff(ctx context.Context, req api.CreateTariffRequestObject) (api.CreateTariffResponseObject, error) {
	b := req.Body
	price, err := decimal.NewFromString(b.Price)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "price", Message: "Укажите цену числом"})
	}
	t, err := h.svc.CreateTariff(ctx, TariffInput{
		MeterType: string(b.MeterType), PropertyID: b.PropertyId, Price: price, EffectiveFrom: apiconv.ToDate(b.EffectiveFrom), Note: b.Note,
	})
	if err != nil {
		return nil, err
	}
	var code *string
	if t.PropertyID != nil {
		if p, err := h.svc.db.Queries().GetProperty(ctx, *t.PropertyID); err == nil {
			code = &p.Code
		}
	}
	return api.CreateTariff201JSONResponse{
		Id: t.ID, MeterType: api.MeterType(t.MeterType), PropertyId: t.PropertyID, PropertyCode: code,
		Price: t.Price.StringFixed(4), EffectiveFrom: apiconv.FromDate(t.EffectiveFrom), Note: t.Note, CreatedAt: t.CreatedAt,
	}, nil
}

// DeleteTariff — удалить тариф.
func (h *Handler) DeleteTariff(ctx context.Context, req api.DeleteTariffRequestObject) (api.DeleteTariffResponseObject, error) {
	if err := h.svc.DeleteTariff(ctx, req.Id); err != nil {
		return nil, err
	}
	return api.DeleteTariff204Response{}, nil
}

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func readingDTO(v ReadingView) api.MeterReadingDTO {
	r := v.Row
	d := api.MeterReadingDTO{
		Id: r.ID, MeterId: r.MeterID, MeterType: api.MeterType(r.MeterType), SerialNumber: r.SerialNumber,
		Property: api.PropertyRef{Id: r.PropertyID, Code: r.PropertyCode}, PeriodMonth: apiconv.FromDate(r.PeriodMonth),
		ReadingDate: apiconv.FromDate(r.ReadingDate), Value: r.Value.String(), PreviousValue: decStr(v.PrevValue),
		PreviousDate: apiconv.FromDatePtr(v.PrevDate), Consumption: decStr(v.Consumption), HighConsumption: v.High,
		Source: api.ReadingSource(r.Source), Status: api.ReadingStatus(r.Status), SubmittedBy: r.SubmittedByName,
		SubmittedAt: r.SubmittedAt, DecidedAt: r.DecidedAt, RejectReason: r.RejectReason,
	}
	if r.ContractID != nil && r.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *r.ContractID, Number: *r.ContractNumber}
	}
	return d
}

func (h *Handler) decision(ctx context.Context, d Decision) (api.MeterReadingDecision, error) {
	out := api.MeterReadingDecision{Reading: readingDTO(d.Reading)}
	if d.Utilities != nil {
		ch, err := h.svc.billing.DTO(ctx, h.svc.db.Queries(), d.Utilities.ChargeID)
		if err != nil {
			return out, err
		}
		out.UtilitiesCharge = &ch
	}
	return out, nil
}

// ListMeterReadings — GET /meter-readings.
func (h *Handler) ListMeterReadings(ctx context.Context, req api.ListMeterReadingsRequestObject) (api.ListMeterReadingsResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	f := ReadingFilter{PropertyID: p.PropertyId, PeriodMonth: apiconv.ToDatePtr(p.PeriodMonth), Limit: paging.Limit(), Offset: paging.Offset()}
	if p.Status != nil {
		f.Status = apiconv.Ptr(string(*p.Status))
	}
	views, total, err := h.svc.Readings(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]api.MeterReadingDTO, 0, len(views))
	for _, v := range views {
		items = append(items, readingDTO(v))
	}
	return api.ListMeterReadings200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

// CreateMeterReading — POST /meter-readings (показание собственника, принимается сразу).
func (h *Handler) CreateMeterReading(ctx context.Context, req api.CreateMeterReadingRequestObject) (api.CreateMeterReadingResponseObject, error) {
	b := req.Body
	v, err := decimal.NewFromString(b.Value)
	if err != nil {
		return nil, apperr.Validation(apperr.FieldError{Field: "value", Message: "Введите число"})
	}
	d, err := h.svc.CreateReading(ctx, ReadingInput{
		MeterID: b.MeterId, PeriodMonth: apiconv.ToDate(b.PeriodMonth), ReadingDate: apiconv.ToDate(b.ReadingDate), Value: v,
	})
	if err != nil {
		return nil, err
	}
	out, err := h.decision(ctx, d)
	return api.CreateMeterReading201JSONResponse(out), err
}

// AcceptMeterReading — POST /meter-readings/{id}/accept.
func (h *Handler) AcceptMeterReading(ctx context.Context, req api.AcceptMeterReadingRequestObject) (api.AcceptMeterReadingResponseObject, error) {
	d, err := h.svc.AcceptReading(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := h.decision(ctx, d)
	return api.AcceptMeterReading200JSONResponse(out), err
}

// RejectMeterReading — POST /meter-readings/{id}/reject.
func (h *Handler) RejectMeterReading(ctx context.Context, req api.RejectMeterReadingRequestObject) (api.RejectMeterReadingResponseObject, error) {
	v, err := h.svc.RejectReading(ctx, req.Id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return api.RejectMeterReading200JSONResponse(readingDTO(v)), nil
}
