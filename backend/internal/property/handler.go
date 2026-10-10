package property

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/api"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db/sqlc"
)

// Handler — методы API объектов.
type Handler struct {
	svc *Service
}

// NewHandler создаёт обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func toInput(code string, kind api.PropertyKind, address string, area api.Decimal, floor, rooms *int,
	cadastral, ownership *string, furnished bool, description *string, rent int64, fiscal api.FiscalMode) (Input, error) {
	a, err := decimal.NewFromString(area)
	if err != nil {
		return Input{}, apperr.Validation(apperr.FieldError{Field: "areaSqm", Message: "Укажите площадь числом"})
	}
	return Input{
		Code: code, Kind: string(kind), Address: address, AreaSqm: a, Floor: i16(floor), Rooms: i16(rooms),
		CadastralNumber: cadastral, OwnershipBasis: ownership, Furnished: furnished, Description: description,
		ExpectedRentKop: rent, FiscalMode: string(fiscal),
	}, nil
}

func i16(v *int) *int16 {
	if v == nil {
		return nil
	}
	x := int16(*v) //nolint:gosec // проверено схемой: −5…200
	return &x
}

func i(v *int16) *int {
	if v == nil {
		return nil
	}
	x := int(*v)
	return &x
}

// ListProperties — список объектов.
func (h *Handler) ListProperties(ctx context.Context, req api.ListPropertiesRequestObject) (api.ListPropertiesResponseObject, error) {
	p := req.Params
	paging := apiconv.NewPaging(p.Page, p.PageSize)
	statuses := []string{}
	if p.Status != nil {
		for _, s := range *p.Status {
			statuses = append(statuses, string(s))
		}
	}
	params := sqlc.ListPropertiesParams{
		Statuses: statuses, Archived: p.Archived != nil && *p.Archived, Q: apiconv.TrimPtr(p.Q),
		Lim: paging.Limit(), Off: paging.Offset(),
	}
	if p.Kind != nil {
		params.Kind = apiconv.Ptr(string(*p.Kind))
	}
	if p.Sort != nil {
		params.Sort = string(*p.Sort)
	}
	q := h.svc.db.Queries()
	rows, err := q.ListProperties(ctx, params)
	if err != nil {
		return nil, err
	}
	total, err := q.CountProperties(ctx, sqlc.CountPropertiesParams{Statuses: params.Statuses, Kind: params.Kind, Archived: params.Archived, Q: params.Q})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	current, err := h.svc.CurrentContracts(ctx, ids)
	if err != nil {
		return nil, err
	}
	debtRows, err := q.DebtByProperty(ctx, ids)
	if err != nil {
		return nil, err
	}
	debts := map[uuid.UUID]int64{}
	for _, d := range debtRows {
		debts[d.ID] = d.DebtKop
	}
	items := make([]api.PropertyListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, api.PropertyListItem{
			Id: r.ID, Code: r.Code, Kind: api.PropertyKind(r.Kind), Address: r.Address, AreaSqm: r.AreaSqm.StringFixed(2),
			Status: api.PropertyStatus(r.Status), FiscalMode: api.FiscalMode(r.FiscalMode), Archived: r.ArchivedAt != nil,
			CurrentContract: contractRef(current, r.ID), DebtKop: debts[r.ID],
		})
	}
	return api.ListProperties200JSONResponse{Items: items, Total: int(total), Page: paging.Page, PageSize: paging.PageSize}, nil
}

func contractRef(m map[uuid.UUID]CurrentContract, id uuid.UUID) *api.CurrentContractRef {
	c, ok := m[id]
	if !ok {
		return nil
	}
	return &api.CurrentContractRef{Id: c.ID, Number: c.Number, TenantName: c.TenantName, EndDate: apiconv.FromDate(c.EndDate)}
}

func (h *Handler) detail(ctx context.Context, p sqlc.Property) (api.PropertyDetail, error) {
	q := h.svc.db.Queries()
	since, err := h.svc.StatusSince(ctx, q, p.ID)
	if err != nil {
		return api.PropertyDetail{}, err
	}
	photos, err := q.ListPropertyPhotos(ctx, p.ID)
	if err != nil {
		return api.PropertyDetail{}, err
	}
	current, err := h.svc.CurrentContracts(ctx, []uuid.UUID{p.ID})
	if err != nil {
		return api.PropertyDetail{}, err
	}
	ph := make([]api.PropertyPhoto, 0, len(photos))
	for _, f := range photos {
		ph = append(ph, api.PropertyPhoto{FileId: f.FileID, Url: file.URL(f.FileID), Caption: f.Caption})
	}
	return api.PropertyDetail{
		Id: p.ID, Code: p.Code, Kind: api.PropertyKind(p.Kind), Address: p.Address, AreaSqm: p.AreaSqm.StringFixed(2),
		Floor: i(p.Floor), Rooms: i(p.Rooms), CadastralNumber: p.CadastralNumber, OwnershipBasis: p.OwnershipBasis,
		Furnished: p.Furnished, Description: p.Description, ExpectedRentKop: p.ExpectedRentKop,
		FiscalMode: api.FiscalMode(p.FiscalMode), Status: api.PropertyStatus(p.Status),
		StatusSince: apiconv.FromDate(since), ArchivedAt: p.ArchivedAt, Photos: ph, CurrentContract: contractRef(current, p.ID),
		// Показатели с начала года считаются по платежам, затратам и заявкам (этапы M6, M11, M12).
		Stats: api.PropertyStats{},
	}, nil
}

// CreateProperty — добавить объект.
func (h *Handler) CreateProperty(ctx context.Context, req api.CreatePropertyRequestObject) (api.CreatePropertyResponseObject, error) {
	b := req.Body
	in, err := toInput(b.Code, b.Kind, b.Address, b.AreaSqm, b.Floor, b.Rooms, b.CadastralNumber, b.OwnershipBasis,
		b.Furnished, b.Description, b.ExpectedRentKop, b.FiscalMode)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Create(ctx, in, string(b.InitialStatus), apiconv.ToDate(b.StatusSince))
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.CreateProperty201JSONResponse(d), nil
}

// GetProperty — карточка объекта.
func (h *Handler) GetProperty(ctx context.Context, req api.GetPropertyRequestObject) (api.GetPropertyResponseObject, error) {
	p, err := h.svc.Get(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.GetProperty200JSONResponse(d), nil
}

// UpdateProperty — изменить объект.
func (h *Handler) UpdateProperty(ctx context.Context, req api.UpdatePropertyRequestObject) (api.UpdatePropertyResponseObject, error) {
	b := req.Body
	in, err := toInput(b.Code, b.Kind, b.Address, b.AreaSqm, b.Floor, b.Rooms, b.CadastralNumber, b.OwnershipBasis,
		b.Furnished, b.Description, b.ExpectedRentKop, b.FiscalMode)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Update(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.UpdateProperty200JSONResponse(d), nil
}

// ArchiveProperty — в архив.
func (h *Handler) ArchiveProperty(ctx context.Context, req api.ArchivePropertyRequestObject) (api.ArchivePropertyResponseObject, error) {
	p, err := h.svc.SetArchived(ctx, req.Id, true)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.ArchiveProperty200JSONResponse(d), nil
}

// UnarchiveProperty — вернуть из архива.
func (h *Handler) UnarchiveProperty(ctx context.Context, req api.UnarchivePropertyRequestObject) (api.UnarchivePropertyResponseObject, error) {
	p, err := h.svc.SetArchived(ctx, req.Id, false)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.UnarchiveProperty200JSONResponse(d), nil
}

// ChangePropertyStatus — сменить статус вручную.
func (h *Handler) ChangePropertyStatus(ctx context.Context, req api.ChangePropertyStatusRequestObject) (api.ChangePropertyStatusResponseObject, error) {
	p, err := h.svc.ChangeStatus(ctx, req.Id, string(req.Body.Status), apiconv.ToDate(req.Body.Since), req.Body.Reason)
	if err != nil {
		return nil, err
	}
	d, err := h.detail(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.ChangePropertyStatus200JSONResponse(d), nil
}

// GetPropertyStatusHistory — история статусов, новые сверху.
func (h *Handler) GetPropertyStatusHistory(ctx context.Context, req api.GetPropertyStatusHistoryRequestObject) (api.GetPropertyStatusHistoryResponseObject, error) {
	if _, err := h.svc.Get(ctx, req.Id); err != nil {
		return nil, err
	}
	rows, err := h.svc.db.Queries().ListStatusHistory(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	out := make(api.GetPropertyStatusHistory200JSONResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, api.PropertyStatusHistoryItem{
			Id: r.ID, Status: api.PropertyStatus(r.Status), ValidFrom: apiconv.FromDate(r.ValidFrom),
			ValidTo: apiconv.FromDatePtr(r.ValidTo), ContractId: r.ContractID, ContractNumber: r.ContractNumber,
			Reason: r.Reason, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func inventoryDTO(rows []sqlc.PropertyInventoryItem) api.PropertyInventory {
	items := make([]api.InventoryItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, api.InventoryItem{Id: r.ID, Name: r.Name, Quantity: int(r.Quantity), Condition: r.Condition, Note: r.Note})
	}
	return api.PropertyInventory{Items: items}
}

// GetPropertyInventory — опись имущества.
func (h *Handler) GetPropertyInventory(ctx context.Context, req api.GetPropertyInventoryRequestObject) (api.GetPropertyInventoryResponseObject, error) {
	if _, err := h.svc.Get(ctx, req.Id); err != nil {
		return nil, err
	}
	rows, err := h.svc.db.Queries().ListInventory(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return api.GetPropertyInventory200JSONResponse(inventoryDTO(rows)), nil
}

// UpdatePropertyInventory — заменить опись целиком.
func (h *Handler) UpdatePropertyInventory(ctx context.Context, req api.UpdatePropertyInventoryRequestObject) (api.UpdatePropertyInventoryResponseObject, error) {
	items := make([]InventoryItem, 0, len(req.Body.Items))
	for n, it := range req.Body.Items {
		if strings.TrimSpace(it.Name) == "" || strings.TrimSpace(it.Condition) == "" {
			return nil, apperr.Validation(apperr.FieldError{Field: "items", Message: "Строка " + itoa(n+1) + ": укажите наименование и состояние"})
		}
		items = append(items, InventoryItem{ID: it.Id, Name: it.Name, Quantity: int32(it.Quantity), Condition: it.Condition, Note: it.Note}) //nolint:gosec // количество мало
	}
	rows, err := h.svc.ReplaceInventory(ctx, req.Id, items)
	if err != nil {
		return nil, err
	}
	return api.UpdatePropertyInventory200JSONResponse(inventoryDTO(rows)), nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// AddPropertyPhoto — прикрепить фото.
func (h *Handler) AddPropertyPhoto(ctx context.Context, req api.AddPropertyPhotoRequestObject) (api.AddPropertyPhotoResponseObject, error) {
	ph, err := h.svc.AddPhoto(ctx, req.Id, req.Body.FileId, req.Body.Caption)
	if err != nil {
		return nil, err
	}
	return api.AddPropertyPhoto201JSONResponse{FileId: ph.FileID, Url: file.URL(ph.FileID), Caption: ph.Caption}, nil
}

// ReorderPropertyPhotos — порядок фото.
func (h *Handler) ReorderPropertyPhotos(ctx context.Context, req api.ReorderPropertyPhotosRequestObject) (api.ReorderPropertyPhotosResponseObject, error) {
	if err := h.svc.ReorderPhotos(ctx, req.Id, req.Body.FileIds); err != nil {
		return nil, err
	}
	return api.ReorderPropertyPhotos204Response{}, nil
}

// DeletePropertyPhoto — удалить фото.
func (h *Handler) DeletePropertyPhoto(ctx context.Context, req api.DeletePropertyPhotoRequestObject) (api.DeletePropertyPhotoResponseObject, error) {
	if err := h.svc.DeletePhoto(ctx, req.Id, req.FileId); err != nil {
		return nil, err
	}
	return api.DeletePropertyPhoto204Response{}, nil
}
