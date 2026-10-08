// Package property — объекты недвижимости: характеристики, статусы и их история, архив, фото, опись (FR-01).
package property

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/file"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/reqctx"
)

// Статусы объекта.
const (
	StatusFree     = "free"
	StatusReserved = "reserved"
	StatusRented   = "rented"
	StatusRepair   = "repair"
)

var statusTitles = map[string]string{
	StatusFree: "Свободен", StatusReserved: "Забронирован", StatusRented: "Сдан", StatusRepair: "На ремонте",
}

// Service — объекты.
type Service struct {
	db    *db.DB
	audit *audit.Service
	clock clock.Clock
	files *file.Service
}

// NewService создаёт сервис объектов.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, fs *file.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, files: fs}
}

// Input — характеристики объекта.
type Input struct {
	Code            string
	Kind            string
	Address         string
	AreaSqm         decimal.Decimal
	Floor           *int16
	Rooms           *int16
	CadastralNumber *string
	OwnershipBasis  *string
	Furnished       bool
	Description     *string
	ExpectedRentKop int64
	FiscalMode      string
}

func (in *Input) normalize() error {
	in.Code = strings.TrimSpace(in.Code)
	in.Address = strings.TrimSpace(in.Address)
	in.CadastralNumber = trim(in.CadastralNumber)
	in.OwnershipBasis = trim(in.OwnershipBasis)
	in.Description = trim(in.Description)
	var errs []apperr.FieldError
	if n := len([]rune(in.Code)); n < 2 || n > 40 {
		errs = append(errs, apperr.FieldError{Field: "code", Message: "Короткое название — от 2 до 40 символов"})
	}
	if in.Address == "" {
		errs = append(errs, apperr.FieldError{Field: "address", Message: "Укажите адрес"})
	}
	if !in.AreaSqm.IsPositive() || in.AreaSqm.GreaterThanOrEqual(decimal.NewFromInt(100000)) ||
		!in.AreaSqm.Equal(in.AreaSqm.Round(2)) {
		errs = append(errs, apperr.FieldError{Field: "areaSqm", Message: "Площадь — положительное число, не более двух знаков после запятой"})
	}
	if in.Kind != "residential" && in.Rooms != nil {
		errs = append(errs, apperr.FieldError{Field: "rooms", Message: "Число комнат указывается только для жилых помещений"})
	}
	if in.ExpectedRentKop < 0 {
		errs = append(errs, apperr.FieldError{Field: "expectedRentKop", Message: "Ожидаемая плата не может быть отрицательной"})
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

func trim(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func uniqueCodeError(err error) error {
	if code, constraint, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation && strings.Contains(constraint, "code") {
		return apperr.Validation(apperr.FieldError{Field: "code", Message: "Объект с таким коротким названием уже есть"})
	}
	return err
}

// snapshot — объект для журнала.
func snapshot(p sqlc.Property) map[string]any {
	return map[string]any{
		"code": p.Code, "kind": p.Kind, "address": p.Address, "areaSqm": p.AreaSqm.String(), "floor": p.Floor,
		"rooms": p.Rooms, "cadastralNumber": p.CadastralNumber, "ownershipBasis": p.OwnershipBasis,
		"furnished": p.Furnished, "expectedRentKop": p.ExpectedRentKop, "fiscalMode": p.FiscalMode,
		"status": p.Status, "archived": p.ArchivedAt != nil,
	}
}

// Create — новый объект с начальным статусом (free или repair) с даты statusSince (можно в прошлом).
func (s *Service) Create(ctx context.Context, in Input, initialStatus string, since date.Date) (sqlc.Property, error) {
	if err := in.normalize(); err != nil {
		return sqlc.Property{}, err
	}
	if initialStatus != StatusFree && initialStatus != StatusRepair {
		return sqlc.Property{}, apperr.Validation(apperr.FieldError{Field: "initialStatus", Message: "Начальный статус — «Свободен» или «На ремонте»"})
	}
	if since.After(s.clock.Today()) {
		return sqlc.Property{}, apperr.Validation(apperr.FieldError{Field: "statusSince", Message: "Дата не может быть позже сегодняшней"})
	}
	var p sqlc.Property
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		p, err = tx.Q.CreateProperty(ctx, sqlc.CreatePropertyParams{
			Code: in.Code, Kind: in.Kind, Address: in.Address, AreaSqm: in.AreaSqm, Floor: in.Floor, Rooms: in.Rooms,
			CadastralNumber: in.CadastralNumber, OwnershipBasis: in.OwnershipBasis, Furnished: in.Furnished,
			Description: in.Description, ExpectedRentKop: in.ExpectedRentKop, FiscalMode: in.FiscalMode,
			Status: initialStatus, Now: s.clock.Now(),
		})
		if err != nil {
			return uniqueCodeError(err)
		}
		if err := tx.Q.InsertStatusHistory(ctx, sqlc.InsertStatusHistoryParams{
			PropertyID: p.ID, Status: initialStatus, ValidFrom: since, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.create", EntityType: "property", EntityID: p.ID.String(),
			Summary: fmt.Sprintf("Добавлен объект «%s»", p.Code), After: snapshot(p),
		})
	})
	return p, err
}

// Update — изменить характеристики.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (sqlc.Property, error) {
	if err := in.normalize(); err != nil {
		return sqlc.Property{}, err
	}
	var p sqlc.Property
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		p, err = tx.Q.UpdateProperty(ctx, sqlc.UpdatePropertyParams{
			ID: id, Code: in.Code, Kind: in.Kind, Address: in.Address, AreaSqm: in.AreaSqm, Floor: in.Floor, Rooms: in.Rooms,
			CadastralNumber: in.CadastralNumber, OwnershipBasis: in.OwnershipBasis, Furnished: in.Furnished,
			Description: in.Description, ExpectedRentKop: in.ExpectedRentKop, FiscalMode: in.FiscalMode,
		})
		if err != nil {
			return uniqueCodeError(err)
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.update", EntityType: "property", EntityID: id.String(),
			Summary: fmt.Sprintf("Изменён объект «%s»", p.Code), Before: snapshot(before), After: snapshot(p),
		})
	})
	return p, err
}

func (s *Service) lock(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Property, error) {
	p, err := tx.Q.GetPropertyForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return p, apperr.NotFound()
	}
	return p, err
}

// Get — объект.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (sqlc.Property, error) {
	p, err := s.db.Queries().GetProperty(ctx, id)
	if db.IsNoRows(err) {
		return p, apperr.NotFound()
	}
	return p, err
}

// SetArchived — перевести в архив (только без договоров «Черновик», «Действует», «Продлён») или вернуть.
func (s *Service) SetArchived(ctx context.Context, id uuid.UUID, archive bool) (sqlc.Property, error) {
	var p sqlc.Property
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if archive == (before.ArchivedAt != nil) {
			p = before
			return nil
		}
		var at = s.clock.Now()
		params := sqlc.SetPropertyArchivedParams{ID: id}
		summary := fmt.Sprintf("Объект «%s» возвращён из архива", before.Code)
		if archive {
			n, err := tx.Q.CountOpenContractsByProperty(ctx, id)
			if err != nil {
				return err
			}
			if n > 0 {
				return apperr.BusinessRule("Нельзя перевести в архив: по объекту есть договоры в статусе «Черновик», «Действует» или «Продлён»")
			}
			params.ArchivedAt = &at
			summary = fmt.Sprintf("Объект «%s» переведён в архив", before.Code)
		}
		if p, err = tx.Q.SetPropertyArchived(ctx, params); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.archive", EntityType: "property", EntityID: id.String(), Summary: summary,
			Before: map[string]any{"archived": before.ArchivedAt != nil}, After: map[string]any{"archived": archive},
		})
	})
	return p, err
}

// ChangeStatus — ручная смена статуса (05 разд. 2.1): «Сдан» ставит и снимает только договор; «На ремонте» —
// только без действующего договора на сегодня; дата — не позже сегодня и не раньше начала текущей записи истории.
func (s *Service) ChangeStatus(ctx context.Context, id uuid.UUID, status string, since date.Date, reason *string) (sqlc.Property, error) {
	reason = trim(reason)
	today := s.clock.Today()
	if since.After(today) {
		return sqlc.Property{}, apperr.Validation(apperr.FieldError{Field: "since", Message: "Дата не может быть позже сегодняшней"})
	}
	if status == StatusRented {
		return sqlc.Property{}, apperr.BusinessRule("Статус «Сдан» ставится и снимается только договором")
	}
	var p sqlc.Property
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.ArchivedAt != nil {
			return apperr.BusinessRule("Объект в архиве. Сначала верните его из архива")
		}
		if before.Status == StatusRented {
			return apperr.BusinessRule("Статус «Сдан» ставится и снимается только договором")
		}
		if status == StatusRepair {
			active, err := tx.Q.HasActiveContractOn(ctx, sqlc.HasActiveContractOnParams{PropertyID: id, OnDate: today})
			if err != nil {
				return err
			}
			if active {
				return apperr.BusinessRule("На сегодня по объекту действует договор — статус «На ремонте» поставить нельзя")
			}
		}
		cur, err := tx.Q.CurrentStatusRow(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status == status {
			return apperr.BusinessRule("Объект уже в статусе «" + statusTitles[status] + "»")
		}
		if since.Before(cur.ValidFrom) {
			return apperr.Validation(apperr.FieldError{Field: "since",
				Message: "Дата не может быть раньше начала текущего статуса (" + cur.ValidFrom.Format() + ")"})
		}
		if since.Equal(cur.ValidFrom) {
			err = tx.Q.ReplaceStatusHistory(ctx, sqlc.ReplaceStatusHistoryParams{ID: cur.ID, Status: status, Reason: reason, CreatedBy: actorID(ctx)})
		} else {
			if err = tx.Q.CloseStatusHistory(ctx, sqlc.CloseStatusHistoryParams{ID: cur.ID, ValidTo: ptr(since.AddDays(-1))}); err == nil {
				err = tx.Q.InsertStatusHistory(ctx, sqlc.InsertStatusHistoryParams{
					PropertyID: id, Status: status, ValidFrom: since, Reason: reason, CreatedBy: actorID(ctx), Now: s.clock.Now(),
				})
			}
		}
		if err != nil {
			return err
		}
		if err := tx.Q.SetPropertyStatus(ctx, sqlc.SetPropertyStatusParams{ID: id, Status: status}); err != nil {
			return err
		}
		p = before
		p.Status = status
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.status", EntityType: "property", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: статус «%s» с %s", before.Code, statusTitles[status], since.Format()),
			Before:  map[string]any{"status": before.Status}, After: map[string]any{"status": status, "since": since.String(), "reason": reason},
		})
	})
	return p, err
}

// StatusSince — дата начала текущего статуса.
func (s *Service) StatusSince(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (date.Date, error) {
	cur, err := q.CurrentStatusRow(ctx, id)
	if db.IsNoRows(err) {
		return date.Date{}, nil
	}
	return cur.ValidFrom, err
}

// CurrentContract — действующий на сегодня договор объекта.
type CurrentContract struct {
	ID         uuid.UUID
	Number     string
	TenantName string
	EndDate    date.Date
}

// CurrentContracts — действующие договоры объектов на сегодня.
func (s *Service) CurrentContracts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]CurrentContract, error) {
	rows, err := s.db.Queries().CurrentContracts(ctx, sqlc.CurrentContractsParams{PropertyIds: ids, OnDate: s.clock.Today()})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]CurrentContract, len(rows))
	for _, r := range rows {
		out[r.PropertyID] = CurrentContract{
			ID: r.ID, Number: r.Number, EndDate: r.EndDate,
			TenantName: names.Display(r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName),
		}
	}
	return out, nil
}

// InventoryItem — строка описи.
type InventoryItem struct {
	ID        *uuid.UUID
	Name      string
	Quantity  int32
	Condition string
	Note      *string
}

// ReplaceInventory — заменить опись целиком; порядок строк — порядок массива.
func (s *Service) ReplaceInventory(ctx context.Context, id uuid.UUID, items []InventoryItem) ([]sqlc.PropertyInventoryItem, error) {
	var out []sqlc.PropertyInventoryItem
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := tx.Q.DeleteInventory(ctx, id); err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for i, it := range items {
			itemID := uuid.Must(uuid.NewV7())
			if it.ID != nil && !seen[*it.ID] {
				itemID = *it.ID
			}
			seen[itemID] = true
			row, err := tx.Q.InsertInventoryItem(ctx, sqlc.InsertInventoryItemParams{
				ID: itemID, PropertyID: id, Name: strings.TrimSpace(it.Name), Quantity: it.Quantity,
				Condition: strings.TrimSpace(it.Condition), Note: trim(it.Note), Sort: int32(i), //nolint:gosec // строк описи немного
			})
			if err != nil {
				return err
			}
			out = append(out, row)
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.update", EntityType: "property", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: изменена опись имущества (%d строк)", p.Code, len(items)),
		})
	})
	return out, err
}

// AddPhoto — прикрепить загруженное изображение к объекту.
func (s *Service) AddPhoto(ctx context.Context, id, fileID uuid.UUID, caption *string) (sqlc.PropertyPhoto, error) {
	var ph sqlc.PropertyPhoto
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		f, err := s.files.Get(ctx, tx.Q, fileID)
		if err != nil {
			return err
		}
		if f.Category != file.CategoryPropertyPhoto {
			return apperr.Validation(apperr.FieldError{Field: "fileId", Message: "Файл загружен не как фото объекта"})
		}
		ph, err = tx.Q.AddPropertyPhoto(ctx, sqlc.AddPropertyPhotoParams{PropertyID: id, FileID: fileID, Caption: trim(caption)})
		if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
			return apperr.Conflict(apperr.CodeConflictExists, "Это фото уже прикреплено")
		}
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.update", EntityType: "property", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: добавлено фото", p.Code),
		})
	})
	return ph, err
}

// ReorderPhotos — порядок фото: список должен совпадать с прикреплёнными фото.
func (s *Service) ReorderPhotos(ctx context.Context, id uuid.UUID, fileIDs []uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		if _, err := s.lock(ctx, tx, id); err != nil {
			return err
		}
		current, err := tx.Q.ListPropertyPhotos(ctx, id)
		if err != nil {
			return err
		}
		have := map[uuid.UUID]bool{}
		for _, ph := range current {
			have[ph.FileID] = true
		}
		if len(fileIDs) != len(current) {
			return apperr.Validation(apperr.FieldError{Field: "fileIds", Message: "Укажите все фото объекта"})
		}
		for i, f := range fileIDs {
			if !have[f] {
				return apperr.Validation(apperr.FieldError{Field: "fileIds", Message: "Фото не относится к объекту"})
			}
			if err := tx.Q.SetPropertyPhotoSort(ctx, sqlc.SetPropertyPhotoSortParams{PropertyID: id, FileID: f, Sort: int16(i)}); err != nil { //nolint:gosec // фото немного
				return err
			}
		}
		return nil
	})
}

// DeletePhoto — открепить фото (файл удаляется плановой очисткой).
func (s *Service) DeletePhoto(ctx context.Context, id, fileID uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		n, err := tx.Q.DeletePropertyPhoto(ctx, sqlc.DeletePropertyPhotoParams{PropertyID: id, FileID: fileID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "property.update", EntityType: "property", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: удалено фото", p.Code),
		})
	})
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
