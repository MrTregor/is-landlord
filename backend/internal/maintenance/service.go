// Package maintenance — заявки на обслуживание (FR-08): создание собственником и нанимателем, фото, переходы
// статусов, лента событий и комментарии, письма. Правила — docs/spec/05 Бизнес-правила и расчёты.md, раздел 14.1.
package maintenance

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/file"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/reqctx"
)

// Статусы заявки.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusClosed     = "closed"
)

// StatusTitles — подписи статусов (04, справочник значений).
var StatusTitles = map[string]string{
	StatusNew: "Новая", StatusInProgress: "В работе", StatusDone: "Выполнена", StatusClosed: "Закрыта",
}

// CategoryTitles — подписи категорий (04, справочник значений).
var CategoryTitles = map[string]string{
	"plumbing": "Сантехника", "electrical": "Электрика", "heating": "Отопление", "appliance": "Бытовая техника",
	"furniture": "Мебель", "doors_windows": "Двери и окна", "other": "Другое",
}

// Service — заявки.
type Service struct {
	db     *db.DB
	audit  *audit.Service
	clock  clock.Clock
	notify *notify.Service
}

// NewService создаёт сервис заявок.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, nt *notify.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, notify: nt}
}

func actor(ctx context.Context) (*reqctx.Actor, error) {
	a := reqctx.ActorFrom(ctx)
	if a == nil {
		return nil, apperr.Unauthorized()
	}
	return a, nil
}

// requestNumber — «З-2026-014»: год создания и сквозной номер в году.
func requestNumber(ctx context.Context, q *sqlc.Queries, on date.Date) (string, error) {
	n, err := q.NextSequenceValue(ctx, sqlc.NextSequenceValueParams{Name: "request", Year: int32(on.Year())}) //nolint:gosec // год — четыре цифры
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("З-%d-%03d", on.Year(), n), nil
}

// CreateInput — новая заявка.
type CreateInput struct {
	PropertyID  uuid.UUID
	ContractID  *uuid.UUID
	Category    string
	Title       string
	Description string
	Priority    string
	PhotoIDs    []uuid.UUID
}

func (in *CreateInput) clean() error {
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	var errs []apperr.FieldError
	if n := len([]rune(in.Title)); n < 3 || n > 120 {
		errs = append(errs, apperr.FieldError{Field: "title", Message: "Заголовок — от 3 до 120 символов"})
	}
	if in.Description == "" {
		errs = append(errs, apperr.FieldError{Field: "description", Message: "Опишите проблему"})
	}
	if len(in.PhotoIDs) > 5 {
		errs = append(errs, apperr.FieldError{Field: "photoFileIds", Message: "Не больше 5 фото"})
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// checkPhotos — фото заявки: файлы категории request_photo; наниматель прикладывает только загруженные им.
func checkPhotos(ctx context.Context, q *sqlc.Queries, a *reqctx.Actor, ids []uuid.UUID) error {
	for _, id := range ids {
		f, err := q.GetFile(ctx, id)
		if db.IsNoRows(err) || (err == nil && (f.DeletedAt != nil || f.Category != file.CategoryRequestPhoto ||
			(a.Role == reqctx.RoleTenant && (f.UploadedBy == nil || *f.UploadedBy != a.UserID)))) {
			return apperr.Validation(apperr.FieldError{Field: "photoFileIds", Message: "Фото не найдено — загрузите его заново"})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Create — заявка собственника по любому объекту; договор — указанный (того же объекта) или действующий.
func (s *Service) Create(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	a, err := actor(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := in.clean(); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		if _, err := tx.Q.GetProperty(ctx, in.PropertyID); err != nil {
			if db.IsNoRows(err) {
				return apperr.Validation(apperr.FieldError{Field: "propertyId", Message: "Объект не найден"})
			}
			return err
		}
		if in.ContractID != nil {
			c, err := tx.Q.GetContract(ctx, *in.ContractID)
			if db.IsNoRows(err) || (err == nil && (c.PropertyID != in.PropertyID || c.Status == "draft")) {
				return apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Договор относится к другому объекту"})
			}
			if err != nil {
				return err
			}
		} else if c, err := tx.Q.ActiveContractOfProperty(ctx, sqlc.ActiveContractOfPropertyParams{
			PropertyID: in.PropertyID, OnDate: s.clock.Today(),
		}); err == nil {
			in.ContractID = &c.ID
		} else if !db.IsNoRows(err) {
			return err
		}
		r, err := s.insert(ctx, tx, a, in)
		id = r.ID
		return err
	})
	return id, err
}

// CreateByTenant — заявка нанимателя по объекту его действующего договора (05 разд. 14.1); письмо собственникам.
func (s *Service) CreateByTenant(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	a, err := actor(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := in.clean(); err != nil {
		return uuid.Nil, err
	}
	in.Priority = "normal"
	var id uuid.UUID
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := payment.TenantContract(ctx, tx.Q, *in.ContractID)
		if err != nil {
			return err
		}
		if c.Status != "active" && c.Status != "extended" {
			return apperr.BusinessRule("Заявку можно подать только по действующему договору")
		}
		in.PropertyID = c.PropertyID
		r, err := s.insert(ctx, tx, a, in)
		if err != nil {
			return err
		}
		id = r.ID
		return s.notifyCreated(ctx, tx, r, c)
	})
	return id, err
}

func (s *Service) insert(ctx context.Context, tx *db.Tx, a *reqctx.Actor, in CreateInput) (sqlc.MaintenanceRequest, error) {
	if err := checkPhotos(ctx, tx.Q, a, in.PhotoIDs); err != nil {
		return sqlc.MaintenanceRequest{}, err
	}
	number, err := requestNumber(ctx, tx.Q, s.clock.Today())
	if err != nil {
		return sqlc.MaintenanceRequest{}, err
	}
	now := s.clock.Now()
	r, err := tx.Q.InsertRequest(ctx, sqlc.InsertRequestParams{
		Number: number, PropertyID: in.PropertyID, ContractID: in.ContractID, AuthorID: a.UserID, AuthorRole: a.Role,
		Category: in.Category, Title: in.Title, Description: in.Description, Priority: in.Priority, Now: now,
	})
	if err != nil {
		return r, err
	}
	for _, f := range in.PhotoIDs {
		if err := tx.Q.InsertRequestPhoto(ctx, sqlc.InsertRequestPhotoParams{RequestID: r.ID, FileID: f}); err != nil {
			return r, err
		}
	}
	if _, err := tx.Q.InsertRequestEvent(ctx, sqlc.InsertRequestEventParams{
		RequestID: r.ID, AuthorID: &a.UserID, Kind: "status", ToStatus: ptr(StatusNew), Now: now,
	}); err != nil {
		return r, err
	}
	who := "собственником"
	if a.Role == reqctx.RoleTenant {
		who = "нанимателем"
	}
	return r, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "request.transition", EntityType: "maintenance_request", EntityID: r.ID.String(),
		Summary: fmt.Sprintf("Заявка %s создана %s: «%s»", r.Number, who, r.Title),
		After:   map[string]any{"status": StatusNew, "category": r.Category, "priority": r.Priority, "photos": len(in.PhotoIDs)},
	})
}

func ptr[T any](v T) *T { return &v }

// mailVars — адрес нанимателя договора и общие переменные письма (10 Уведомления, раздел 1).
func mailVars(ctx context.Context, q *sqlc.Queries, contractID uuid.UUID) (string, map[string]string, error) {
	c, err := q.ContractMailInfo(ctx, contractID)
	if err != nil {
		return "", nil, err
	}
	contract, err := q.GetContract(ctx, contractID)
	if err != nil {
		return "", nil, err
	}
	t, err := q.GetTenant(ctx, contract.TenantID)
	if err != nil {
		return "", nil, err
	}
	to := ""
	if c.Email != nil && c.AnonymizedAt == nil {
		to = *c.Email
	}
	greeting, short := "Уважаемые коллеги", names.Short(t.LastName, t.FirstName, t.MiddleName)
	if t.Kind == "legal" {
		if t.OrgName != nil {
			short = *t.OrgName
		}
	} else {
		greeting = names.Greeting(t.FirstName, t.MiddleName)
	}
	return to, map[string]string{
		"tenant.greeting": greeting, "tenant.short_name": short, "contract.number": c.Number,
		"property.address": c.PropertyAddress, "property.code": c.PropertyCode,
	}, nil
}

// notifyCreated — письмо собственникам owner_request_created.
func (s *Service) notifyCreated(ctx context.Context, tx *db.Tx, r sqlc.MaintenanceRequest, c sqlc.Contract) error {
	_, vars, err := mailVars(ctx, tx.Q, c.ID)
	if err != nil {
		return err
	}
	vars["request.number"], vars["request.title"] = r.Number, r.Title
	vars["request.category_title"] = CategoryTitles[r.Category]
	vars["link"] = s.notify.Link("/requests/" + r.ID.String())
	_, err = s.notify.QueueOwners(ctx, tx, notify.Email{
		Code: "owner_request_created", Vars: vars, DedupeKey: "owner_request:" + r.ID.String(),
		RelatedType: "maintenance_request", RelatedID: &r.ID,
	})
	return err
}

// lock — заявка под блокировкой.
func (s *Service) lock(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.MaintenanceRequest, error) {
	r, err := tx.Q.GetRequestForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return r, apperr.NotFound()
	}
	return r, err
}

// PatchInput — приоритет, исполнитель, плановая дата, вина нанимателя (сохраняются сразу, 14 разд. 9.2).
// Исполнитель и плановая дата берутся как есть (null — очистка), приоритет и вина — если переданы
// (docs/open-questions.md, № 87).
type PatchInput struct {
	Priority    *string
	Contractor  *string
	PlannedDate *date.Date
	TenantFault *bool
}

// Update — изменение полей управления заявкой собственником.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in PatchInput) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Status == StatusClosed {
			return apperr.Conflict(apperr.CodeConflictState, "Заявка закрыта")
		}
		p := sqlc.UpdateRequestFieldsParams{ID: r.ID, Priority: r.Priority, Contractor: trimPtr(in.Contractor), PlannedDate: in.PlannedDate,
			TenantFault: r.TenantFault, Now: s.clock.Now()}
		if in.Priority != nil {
			p.Priority = *in.Priority
		}
		if in.TenantFault != nil {
			p.TenantFault = *in.TenantFault
		}
		after, err := tx.Q.UpdateRequestFields(ctx, p)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "request.update", EntityType: "maintenance_request", EntityID: r.ID.String(),
			Summary: fmt.Sprintf("Заявка %s: изменены приоритет, исполнитель, плановая дата или вина нанимателя", r.Number),
			Before:  map[string]any{"priority": r.Priority, "contractor": r.Contractor, "plannedDate": r.PlannedDate, "tenantFault": r.TenantFault},
			After: map[string]any{"priority": after.Priority, "contractor": after.Contractor, "plannedDate": after.PlannedDate,
				"tenantFault": after.TenantFault},
		})
	})
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

// rule — допустимый переход (05 разд. 14.1): кто выполняет и что обязательно.
type rule struct {
	owner, tenant bool
	needComment   bool
	needResult    bool
}

var transitions = map[[2]string]rule{
	{StatusNew, StatusInProgress}:  {owner: true},
	{StatusNew, StatusClosed}:      {owner: true, needComment: true},
	{StatusInProgress, StatusDone}: {owner: true, needResult: true},
	{StatusDone, StatusClosed}:     {owner: true, tenant: true},
	{StatusDone, StatusInProgress}: {owner: true, tenant: true, needComment: true},
}

// TransitionInput — смена статуса.
type TransitionInput struct {
	To         string
	Comment    *string
	ResultNote *string
}

// Transition — смена статуса собственником; письмо нанимателю request_status_changed.
func (s *Service) Transition(ctx context.Context, id uuid.UUID, in TransitionInput) error {
	return s.transition(ctx, id, in, nil)
}

// TransitionByTenant — «Подтверждаю выполнение» и «Не устранено» по заявке своего договора.
func (s *Service) TransitionByTenant(ctx context.Context, id uuid.UUID, in TransitionInput) error {
	return s.transition(ctx, id, in, func(r sqlc.MaintenanceRequest, q *sqlc.Queries) error {
		return tenantOwns(ctx, q, r)
	})
}

// tenantOwns — заявка относится к договору нанимателя; иначе 404.
func tenantOwns(ctx context.Context, q *sqlc.Queries, r sqlc.MaintenanceRequest) error {
	if r.ContractID == nil {
		return apperr.NotFound()
	}
	_, err := payment.TenantContract(ctx, q, *r.ContractID)
	return err
}

func (s *Service) transition(ctx context.Context, id uuid.UUID, in TransitionInput, check func(sqlc.MaintenanceRequest, *sqlc.Queries) error) error {
	a, err := actor(ctx)
	if err != nil {
		return err
	}
	comment, result := trimPtr(in.Comment), trimPtr(in.ResultNote)
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if check != nil {
			if err := check(r, tx.Q); err != nil {
				return err
			}
		}
		ru, ok := transitions[[2]string{r.Status, in.To}]
		if !ok || (a.Role == reqctx.RoleTenant && !ru.tenant) || (a.Role != reqctx.RoleTenant && !ru.owner) {
			return apperr.Conflict(apperr.CodeConflictState,
				fmt.Sprintf("Переход «%s» → «%s» недоступен", StatusTitles[r.Status], StatusTitles[in.To]))
		}
		if ru.needComment && comment == nil {
			return apperr.Validation(apperr.FieldError{Field: "comment", Message: "Укажите комментарий"})
		}
		if ru.needResult && result == nil {
			return apperr.Validation(apperr.FieldError{Field: "resultNote", Message: "Опишите результат работ"})
		}
		now := s.clock.Now()
		if _, err := tx.Q.SetRequestStatus(ctx, sqlc.SetRequestStatusParams{ID: r.ID, Status: in.To, ResultNote: result, Now: now}); err != nil {
			return err
		}
		text := comment
		if ru.needResult {
			text = result
		}
		ev, err := tx.Q.InsertRequestEvent(ctx, sqlc.InsertRequestEventParams{
			RequestID: r.ID, AuthorID: &a.UserID, Kind: "status", FromStatus: &r.Status, ToStatus: &in.To, Text: text, Now: now,
		})
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "request.transition", EntityType: "maintenance_request", EntityID: r.ID.String(),
			Summary: fmt.Sprintf("Заявка %s: «%s» → «%s»", r.Number, StatusTitles[r.Status], StatusTitles[in.To]),
			Before:  map[string]any{"status": r.Status}, After: map[string]any{"status": in.To},
		}); err != nil {
			return err
		}
		if a.Role == reqctx.RoleTenant || r.ContractID == nil {
			return nil
		}
		to, vars, err := mailVars(ctx, tx.Q, *r.ContractID)
		if err != nil {
			return err
		}
		vars["request.number"], vars["request.title"], vars["status_title"] = r.Number, r.Title, StatusTitles[in.To]
		if text != nil {
			vars["comment"] = *text
		}
		return s.notify.Queue(ctx, tx, notify.Email{
			Code: "request_status_changed", To: to, Vars: vars, DedupeKey: "request_status:" + ev.ID.String(),
			RelatedType: "maintenance_request", RelatedID: &r.ID,
		})
	})
}

// Comment — комментарий в ленте заявки (любая сторона до закрытия, 05 разд. 14.1).
func (s *Service) Comment(ctx context.Context, id uuid.UUID, text string, byTenant bool) (sqlc.MaintenanceRequestEvent, error) {
	a, err := actor(ctx)
	if err != nil {
		return sqlc.MaintenanceRequestEvent{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return sqlc.MaintenanceRequestEvent{}, apperr.Validation(apperr.FieldError{Field: "text", Message: "Введите комментарий"})
	}
	var ev sqlc.MaintenanceRequestEvent
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if byTenant {
			if err := tenantOwns(ctx, tx.Q, r); err != nil {
				return err
			}
		}
		if r.Status == StatusClosed {
			return apperr.Conflict(apperr.CodeConflictState, "Заявка закрыта — комментарии не принимаются")
		}
		now := s.clock.Now()
		if ev, err = tx.Q.InsertRequestEvent(ctx, sqlc.InsertRequestEventParams{
			RequestID: r.ID, AuthorID: &a.UserID, Kind: "comment", Text: &text, Now: now,
		}); err != nil {
			return err
		}
		if err := tx.Q.TouchRequest(ctx, sqlc.TouchRequestParams{ID: r.ID, Now: now}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "request.comment", EntityType: "maintenance_request", EntityID: r.ID.String(),
			Summary: fmt.Sprintf("Заявка %s: комментарий", r.Number), After: map[string]any{"eventId": ev.ID},
		})
	})
	return ev, err
}
