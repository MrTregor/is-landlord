package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
)

// InventoryItem — строка описи в акте (contract_acts.inventory).
type InventoryItem struct {
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Condition string  `json:"condition"`
	Note      *string `json:"note"`
}

// ActReading — показание счётчика в акте.
type ActReading struct {
	MeterID uuid.UUID
	Value   decimal.Decimal
}

// ActInput — содержимое акта (PUT /acts/{id}).
type ActInput struct {
	ActDate        date.Date
	Inventory      []InventoryItem
	KeysCount      int
	ConditionNotes *string
	Damages        *string
	Readings       []ActReading
}

// ActMeter — счётчик объекта в акте: значение в акте и предыдущее принятое показание.
type ActMeter struct {
	Meter        sqlc.Meter
	Value        *decimal.Decimal
	PreviousVal  *decimal.Decimal
	PreviousDate *date.Date
}

// ActView — акт с описью и показаниями.
type ActView struct {
	Act       sqlc.ContractAct
	Inventory []InventoryItem
	Meters    []ActMeter
}

var readingMax = decimal.NewFromInt(1_000_000_000)

func actTitle(kind string) string {
	if kind == ActReturn {
		return "акт возврата"
	}
	return "акт приёма-передачи"
}

// Acts — акты договора с показаниями всех неархивных счётчиков объекта.
func (s *Service) Acts(ctx context.Context, contractID uuid.UUID) ([]ActView, error) {
	q := s.db.Queries()
	c, err := s.Get(ctx, contractID)
	if err != nil {
		return nil, err
	}
	acts, err := q.ListActs(ctx, contractID)
	if err != nil {
		return nil, err
	}
	out := make([]ActView, 0, len(acts))
	for _, a := range acts {
		v, err := s.actView(ctx, q, c, a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) actView(ctx context.Context, q *sqlc.Queries, c sqlc.Contract, a sqlc.ContractAct) (ActView, error) {
	v := ActView{Act: a, Inventory: []InventoryItem{}}
	if err := json.Unmarshal(a.Inventory, &v.Inventory); err != nil {
		return v, fmt.Errorf("опись акта: %w", err)
	}
	readings, err := q.ActReadings(ctx, &a.ID)
	if err != nil {
		return v, err
	}
	byMeter := map[uuid.UUID]decimal.Decimal{}
	for _, r := range readings {
		byMeter[r.MeterID] = r.Value
	}
	meters, err := q.ActivePropertyMeters(ctx, c.PropertyID)
	if err != nil {
		return v, err
	}
	for _, m := range meters {
		am := ActMeter{Meter: m}
		if val, ok := byMeter[m.ID]; ok {
			am.Value = &val
		}
		prev, err := s.previousReading(ctx, q, m, a)
		if err != nil {
			return v, err
		}
		if prev != nil {
			am.PreviousVal, am.PreviousDate = &prev.Value, &prev.ReadingDate
		}
		v.Meters = append(v.Meters, am)
	}
	return v, nil
}

// previousReading — последнее принятое показание до даты акта, а если его нет — начальное показание счётчика.
func (s *Service) previousReading(ctx context.Context, q *sqlc.Queries, m sqlc.Meter, a sqlc.ContractAct) (*sqlc.PreviousReadingRow, error) {
	r, err := q.PreviousReading(ctx, sqlc.PreviousReadingParams{MeterID: m.ID, OnDate: a.ActDate, ActID: &a.ID})
	if err == nil {
		return &r, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}
	if m.InitialDate.After(a.ActDate) {
		return nil, nil
	}
	return &sqlc.PreviousReadingRow{Value: m.InitialValue, ReadingDate: m.InitialDate}, nil
}

// CreateReturnAct — акт возврата: опись копируется из акта приёма-передачи (05 разд. 4.10).
func (s *Service) CreateReturnAct(ctx context.Context, contractID uuid.UUID) (ActView, error) {
	var v ActView
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := s.lock(ctx, tx, contractID)
		if err != nil {
			return err
		}
		if c.Status == StatusDraft {
			return apperr.Conflict(apperr.CodeConflictState, "Акт возврата создаётся для действующего или закрытого договора")
		}
		acts, err := tx.Q.ListActs(ctx, contractID)
		if err != nil {
			return err
		}
		inventory := []byte("[]")
		for _, a := range acts {
			switch a.Kind {
			case ActReturn:
				return apperr.Conflict(apperr.CodeConflictExists, "Акт возврата уже создан")
			case ActHandover:
				inventory = a.Inventory
			}
		}
		a, err := tx.Q.CreateAct(ctx, sqlc.CreateActParams{
			ContractID: contractID, Kind: ActReturn, ActDate: date.Min(actualEnd(c), s.clock.Today()), Inventory: inventory,
			CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if v, err = s.actView(ctx, tx.Q, c, a); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "act.update", EntityType: "act", EntityID: a.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: создан акт возврата", c.Number),
		})
	})
	return v, err
}

func (s *Service) lockAct(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.ContractAct, sqlc.Contract, error) {
	a, err := tx.Q.GetActForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return a, sqlc.Contract{}, apperr.NotFound()
	}
	if err != nil {
		return a, sqlc.Contract{}, err
	}
	c, err := s.lock(ctx, tx, a.ContractID)
	return a, c, err
}

// UpdateAct — заполнить черновой акт: дата, опись, ключи, состояние, повреждения, показания счётчиков.
func (s *Service) UpdateAct(ctx context.Context, id uuid.UUID, in ActInput) (ActView, error) {
	var v ActView
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		a, c, err := s.lockAct(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Signed {
			return apperr.Conflict(apperr.CodeConflictState, "Акт подписан и не редактируется")
		}
		var errs []apperr.FieldError
		if in.ActDate.Before(c.StartDate.AddDays(-30)) || in.ActDate.After(s.clock.Today().AddDays(365)) {
			errs = append(errs, apperr.FieldError{Field: "actDate", Message: "Проверьте дату акта"})
		}
		if in.KeysCount < 0 {
			errs = append(errs, apperr.FieldError{Field: "keysCount", Message: "Число ключей не может быть отрицательным"})
		}
		for i := range in.Inventory {
			it := &in.Inventory[i]
			it.Name, it.Condition, it.Note = strings.TrimSpace(it.Name), strings.TrimSpace(it.Condition), trim(it.Note)
			if it.Name == "" || it.Condition == "" || it.Quantity < 1 {
				errs = append(errs, apperr.FieldError{Field: fmt.Sprintf("inventory[%d]", i), Message: "Заполните наименование, количество и состояние"})
			}
		}
		meters, err := tx.Q.ActivePropertyMeters(ctx, c.PropertyID)
		if err != nil {
			return err
		}
		known := map[uuid.UUID]sqlc.Meter{}
		for _, m := range meters {
			known[m.ID] = m
		}
		a.ActDate = in.ActDate
		seen := map[uuid.UUID]bool{}
		for i, r := range in.Readings {
			field := fmt.Sprintf("readings[%d].value", i)
			m, ok := known[r.MeterID]
			if !ok || seen[r.MeterID] {
				errs = append(errs, apperr.FieldError{Field: fmt.Sprintf("readings[%d].meterId", i), Message: "Счётчик не относится к объекту"})
				continue
			}
			seen[r.MeterID] = true
			if r.Value.IsNegative() || r.Value.GreaterThanOrEqual(readingMax) || !r.Value.Equal(r.Value.Round(3)) {
				errs = append(errs, apperr.FieldError{Field: field, Message: "Показание — неотрицательное число, не более 3 знаков после запятой"})
				continue
			}
			prev, err := s.previousReading(ctx, tx.Q, m, a)
			if err != nil {
				return err
			}
			if prev != nil && r.Value.LessThan(prev.Value) {
				errs = append(errs, apperr.FieldError{Field: field,
					Message: "Показание меньше предыдущего (" + strings.Replace(prev.Value.String(), ".", ",", 1) + " на " + prev.ReadingDate.Format() + ")"})
			}
		}
		if len(errs) > 0 {
			return apperr.Validation(errs...)
		}
		damages := trim(in.Damages)
		if a.Kind != ActReturn {
			damages = nil
		}
		raw, err := json.Marshal(in.Inventory)
		if err != nil {
			return err
		}
		a, err = tx.Q.UpdateAct(ctx, sqlc.UpdateActParams{
			ID: id, ActDate: in.ActDate, Inventory: raw, KeysCount: int16(in.KeysCount), //nolint:gosec // проверено схемой
			ConditionNotes: trim(in.ConditionNotes), Damages: damages,
		})
		if err != nil {
			return err
		}
		if err := tx.Q.DeleteActReadings(ctx, &id); err != nil {
			return err
		}
		source := "act_" + a.Kind
		for _, r := range in.Readings {
			if err := tx.Q.InsertActReading(ctx, sqlc.InsertActReadingParams{
				MeterID: r.MeterID, ContractID: &c.ID, ActID: &id, PeriodMonth: in.ActDate.FirstOfMonth(), ReadingDate: in.ActDate,
				Value: r.Value, Source: source, SubmittedBy: actorID(ctx), Now: s.clock.Now(),
			}); err != nil {
				return err
			}
		}
		if v, err = s.actView(ctx, tx.Q, c, a); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "act.update", EntityType: "act", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: заполнен %s от %s", c.Number, actTitle(a.Kind), in.ActDate.Format()),
			After:   map[string]any{"actDate": in.ActDate.String(), "inventoryRows": len(in.Inventory), "keysCount": in.KeysCount, "readings": len(in.Readings)},
		})
	})
	return v, err
}

// SignAct — отметка «Подписан»: показания принимаются (source act_*, status accepted), акт больше не редактируется.
// По акту возврата при ЖКУ «По счётчикам» создаётся итоговое начисление ЖКУ; PDF — на этапе M5.
func (s *Service) SignAct(ctx context.Context, id uuid.UUID, signedOn date.Date) (ActView, error) {
	if signedOn.After(s.clock.Today()) {
		return ActView{}, apperr.Validation(apperr.FieldError{Field: "signedOn", Message: "Дата подписания не может быть позже сегодняшней"})
	}
	var v ActView
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		a, c, err := s.lockAct(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Signed {
			return apperr.Conflict(apperr.CodeConflictState, "Акт уже подписан")
		}
		cur, err := s.actView(ctx, tx.Q, c, a)
		if err != nil {
			return err
		}
		for _, m := range cur.Meters {
			if m.Value == nil {
				return apperr.BusinessRule("Внесите в акт показания всех счётчиков объекта")
			}
		}
		if a, err = tx.Q.SignAct(ctx, sqlc.SignActParams{ID: id, SignedOn: &signedOn}); err != nil {
			return err
		}
		if err := tx.Q.AcceptActReadings(ctx, sqlc.AcceptActReadingsParams{ActID: &id, DecidedBy: actorID(ctx), Now: ptr(s.clock.Now())}); err != nil {
			return err
		}
		if a.Kind == ActReturn {
			res, err := s.billing.FinalUtilities(ctx, tx, c, a)
			if err != nil {
				return err
			}
			if err := s.billing.RecordCreated(ctx, tx, c, res, "по акту возврата"); err != nil {
				return err
			}
		}
		// При подписании формируется PDF акта (05 разд. 4.10).
		if _, err := s.docs.ActDocumentTx(ctx, tx, id); err != nil {
			return err
		}
		if a, err = tx.Q.GetAct(ctx, id); err != nil {
			return err
		}
		if v, err = s.actView(ctx, tx.Q, c, a); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "act.sign", EntityType: "act", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: %s подписан %s", c.Number, actTitle(a.Kind), signedOn.Format()),
			After:   map[string]any{"signedOn": signedOn.String(), "readings": len(cur.Meters)},
		})
	})
	return v, err
}
