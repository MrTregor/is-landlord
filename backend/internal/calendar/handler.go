package calendar

import (
	"context"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apiconv"
)

// Handler — методы API календаря.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func toDTO(e Event) api.CalendarEventDTO {
	d := api.CalendarEventDTO{
		Id: e.ID, Kind: api.CalendarEventKind(e.Kind), Title: e.Title, Date: apiconv.FromDate(e.Date), StartTime: e.StartTime,
		EndTime: e.EndTime, Source: api.CalendarEventSource(e.Source), Done: e.Done, Notes: e.Notes, AmountKop: e.AmountKop,
		Forecast: e.Forecast,
	}
	if e.PropertyID != nil && e.PropertyCode != nil {
		d.Property = &api.PropertyRef{Id: *e.PropertyID, Code: *e.PropertyCode}
	}
	if e.ContractID != nil && e.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *e.ContractID, Number: *e.ContractNumber}
	}
	return d
}

// ListCalendarEvents — GET /calendar/events.
func (h *Handler) ListCalendarEvents(ctx context.Context, req api.ListCalendarEventsRequestObject) (api.ListCalendarEventsResponseObject, error) {
	p := req.Params
	f := Filter{From: apiconv.ToDate(p.From), To: apiconv.ToDate(p.To), PropertyID: p.PropertyId}
	if p.Kinds != nil {
		for _, k := range *p.Kinds {
			f.Kinds = append(f.Kinds, string(k))
		}
	}
	events, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make(api.ListCalendarEvents200JSONResponse, 0, len(events))
	for _, e := range events {
		out = append(out, toDTO(e))
	}
	return out, nil
}

// CreateCalendarEvent — POST /calendar/events.
func (h *Handler) CreateCalendarEvent(ctx context.Context, req api.CreateCalendarEventRequestObject) (api.CreateCalendarEventResponseObject, error) {
	b := req.Body
	e, err := h.svc.Create(ctx, Input{
		Kind: string(b.Kind), Title: b.Title, StartsOn: apiconv.ToDate(b.StartsOn), StartTime: b.StartTime, EndTime: b.EndTime,
		PropertyID: b.PropertyId, ContractID: b.ContractId, Notes: b.Notes,
	})
	if err != nil {
		return nil, err
	}
	return api.CreateCalendarEvent201JSONResponse(toDTO(e)), nil
}

// UpdateCalendarEvent — PUT /calendar/events/{id}: ручные — все поля; автоматические — только заметки.
func (h *Handler) UpdateCalendarEvent(ctx context.Context, req api.UpdateCalendarEventRequestObject) (api.UpdateCalendarEventResponseObject, error) {
	b := req.Body
	cur, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	in := Input{Kind: cur.Kind, Title: cur.Title, StartsOn: cur.Date, StartTime: cur.StartTime, EndTime: cur.EndTime,
		PropertyID: cur.PropertyID, ContractID: cur.ContractID, Notes: b.Notes}
	if b.Kind != nil {
		in.Kind = string(*b.Kind)
	}
	if b.Title != nil {
		in.Title = *b.Title
	}
	if b.StartsOn != nil {
		in.StartsOn = apiconv.ToDate(*b.StartsOn)
	}
	in.StartTime, in.EndTime, in.PropertyID, in.ContractID = b.StartTime, b.EndTime, b.PropertyId, b.ContractId
	e, err := h.svc.Update(ctx, req.Id, in, false)
	if err != nil {
		return nil, err
	}
	return api.UpdateCalendarEvent200JSONResponse(toDTO(e)), nil
}

// DeleteCalendarEvent — DELETE /calendar/events/{id}.
func (h *Handler) DeleteCalendarEvent(ctx context.Context, req api.DeleteCalendarEventRequestObject) (api.DeleteCalendarEventResponseObject, error) {
	if err := h.svc.Delete(ctx, req.Id); err != nil {
		return nil, err
	}
	return api.DeleteCalendarEvent204Response{}, nil
}

// SetCalendarEventDone — POST /calendar/events/{id}/done.
func (h *Handler) SetCalendarEventDone(ctx context.Context, req api.SetCalendarEventDoneRequestObject) (api.SetCalendarEventDoneResponseObject, error) {
	e, err := h.svc.SetDone(ctx, req.Id, req.Body.Done)
	if err != nil {
		return nil, err
	}
	return api.SetCalendarEventDone200JSONResponse(toDTO(e)), nil
}
