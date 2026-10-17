// Package meter — приборы учёта объектов, тарифы и показания (FR-01.1, FR-05.5).
package meter

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
	"landlord/backend/internal/settings"
)

// TypeTitles — виды ресурсов (04 Модель данных, справочник).
var TypeTitles = map[string]string{
	"cold_water":        "Холодная вода",
	"hot_water":         "Горячая вода",
	"electricity":       "Электроэнергия",
	"electricity_day":   "Электроэнергия (день)",
	"electricity_night": "Электроэнергия (ночь)",
	"gas":               "Газ",
	"heating":           "Отопление",
}

// Service — счётчики и тарифы.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	billing  *billing.Service
	notify   *notify.Service
	settings *settings.Service
}

// NewService создаёт сервис.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, bl *billing.Service, nt *notify.Service,
	st *settings.Service,
) *Service {
	return &Service{db: database, audit: au, clock: clk, billing: bl, notify: nt, settings: st}
}

// Input — поля счётчика.
type Input struct {
	Type            string
	SerialNumber    *string
	InitialValue    decimal.Decimal
	InitialDate     date.Date
	VerificationDue *date.Date
	Note            *string
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

// validValue — показание: не меньше 0, не больше 6 знаков до запятой и 3 после (05 разд. 6.2).
func validValue(v decimal.Decimal) bool {
	return !v.IsNegative() && v.LessThan(decimal.NewFromInt(1_000_000)) && v.Equal(v.Round(3))
}

func (in *Input) normalize() error {
	in.SerialNumber = trim(in.SerialNumber)
	in.Note = trim(in.Note)
	if _, ok := TypeTitles[in.Type]; !ok {
		return apperr.Validation(apperr.FieldError{Field: "type", Message: "Укажите вид счётчика"})
	}
	if !validValue(in.InitialValue) {
		return apperr.Validation(apperr.FieldError{Field: "initialValue",
			Message: "Показание — неотрицательное число: не более 6 знаков до запятой и 3 после"})
	}
	return nil
}

func snapshot(m sqlc.Meter) map[string]any {
	return map[string]any{
		"type": m.Type, "serialNumber": m.SerialNumber, "initialValue": m.InitialValue.StringFixed(3),
		"initialDate": m.InitialDate.String(), "verificationDue": m.VerificationDue, "note": m.Note,
		"archived": m.ArchivedAt != nil,
	}
}

// syncVerificationEvent — событие календаря «Поверка счётчика» (05 разд. 13.1): создаётся или обновляется
// при создании и изменении счётчика, удаляется, если дата поверки не задана или счётчик в архиве.
func (s *Service) syncVerificationEvent(ctx context.Context, tx *db.Tx, m sqlc.Meter, propertyCode string) error {
	key := "meter_verification:" + m.ID.String()
	if m.VerificationDue == nil || m.ArchivedAt != nil {
		return tx.Q.DeleteAutoEvent(ctx, &key)
	}
	return tx.Q.UpsertAutoEvent(ctx, sqlc.UpsertAutoEventParams{
		Kind: "meter_verification", Title: fmt.Sprintf("Поверка счётчика: %s, %s", strings.ToLower(TypeTitles[m.Type]), propertyCode),
		StartsOn: *m.VerificationDue, PropertyID: &m.PropertyID, AutoKey: &key, Now: s.clock.Now(),
	})
}

// Create — добавить счётчик объекта.
func (s *Service) Create(ctx context.Context, propertyID uuid.UUID, in Input) (sqlc.Meter, error) {
	if err := in.normalize(); err != nil {
		return sqlc.Meter{}, err
	}
	var m sqlc.Meter
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, err := tx.Q.GetPropertyForUpdate(ctx, propertyID)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return apperr.BusinessRule("Объект в архиве")
		}
		m, err = tx.Q.CreateMeter(ctx, sqlc.CreateMeterParams{
			PropertyID: propertyID, Type: in.Type, SerialNumber: in.SerialNumber, InitialValue: in.InitialValue,
			InitialDate: in.InitialDate, VerificationDue: in.VerificationDue, Note: in.Note, Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.syncVerificationEvent(ctx, tx, m, p.Code); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "meter.create", EntityType: "meter", EntityID: m.ID.String(),
			Summary: fmt.Sprintf("Объект «%s»: добавлен счётчик «%s»", p.Code, TypeTitles[m.Type]), After: snapshot(m),
		})
	})
	return m, err
}

// Update — изменить счётчик; вид менять нельзя, если есть показания.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (sqlc.Meter, error) {
	if err := in.normalize(); err != nil {
		return sqlc.Meter{}, err
	}
	var m sqlc.Meter
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetMeter(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if before.Type != in.Type {
			has, err := tx.Q.MeterHasReadings(ctx, id)
			if err != nil {
				return err
			}
			if has {
				return apperr.Validation(apperr.FieldError{Field: "type", Message: "Вид счётчика нельзя изменить: по нему есть показания"})
			}
		}
		p, err := tx.Q.GetPropertyForUpdate(ctx, before.PropertyID)
		if err != nil {
			return err
		}
		m, err = tx.Q.UpdateMeter(ctx, sqlc.UpdateMeterParams{
			ID: id, Type: in.Type, SerialNumber: in.SerialNumber, InitialValue: in.InitialValue,
			InitialDate: in.InitialDate, VerificationDue: in.VerificationDue, Note: in.Note,
		})
		if err != nil {
			return err
		}
		if err := s.syncVerificationEvent(ctx, tx, m, p.Code); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "meter.update", EntityType: "meter", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: изменён счётчик «%s»", p.Code, TypeTitles[m.Type]),
			Before:  snapshot(before), After: snapshot(m),
		})
	})
	return m, err
}

// Archive — перевести счётчик в архив (показания по нему больше не принимаются).
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (sqlc.Meter, error) {
	var m sqlc.Meter
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetMeter(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if before.ArchivedAt != nil {
			m = before
			return nil
		}
		p, err := tx.Q.GetPropertyForUpdate(ctx, before.PropertyID)
		if err != nil {
			return err
		}
		if m, err = tx.Q.ArchiveMeter(ctx, sqlc.ArchiveMeterParams{ID: id, ArchivedAt: ptr(s.clock.Now())}); err != nil {
			return err
		}
		if err := s.syncVerificationEvent(ctx, tx, m, p.Code); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "meter.archive", EntityType: "meter", EntityID: id.String(),
			Summary: fmt.Sprintf("Объект «%s»: счётчик «%s» переведён в архив", p.Code, TypeTitles[m.Type]),
		})
	})
	return m, err
}

// TariffInput — новый тариф.
type TariffInput struct {
	MeterType     string
	PropertyID    *uuid.UUID
	Price         decimal.Decimal
	EffectiveFrom date.Date
	Note          *string
}

// CreateTariff — добавить тариф (цена за единицу > 0, до 4 знаков после запятой).
func (s *Service) CreateTariff(ctx context.Context, in TariffInput) (sqlc.Tariff, error) {
	if _, ok := TypeTitles[in.MeterType]; !ok {
		return sqlc.Tariff{}, apperr.Validation(apperr.FieldError{Field: "meterType", Message: "Укажите ресурс"})
	}
	if !in.Price.IsPositive() || !in.Price.Equal(in.Price.Round(4)) || in.Price.GreaterThanOrEqual(decimal.NewFromInt(100_000_000)) {
		return sqlc.Tariff{}, apperr.Validation(apperr.FieldError{Field: "price", Message: "Цена — положительное число, не более 4 знаков после запятой"})
	}
	var t sqlc.Tariff
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		scope := "все объекты"
		if in.PropertyID != nil {
			p, err := tx.Q.GetProperty(ctx, *in.PropertyID)
			if db.IsNoRows(err) {
				return apperr.Validation(apperr.FieldError{Field: "propertyId", Message: "Объект не найден"})
			}
			if err != nil {
				return err
			}
			scope = "объект «" + p.Code + "»"
		}
		var err error
		t, err = tx.Q.CreateTariff(ctx, sqlc.CreateTariffParams{
			MeterType: in.MeterType, PropertyID: in.PropertyID, Price: in.Price, EffectiveFrom: in.EffectiveFrom,
			Note: trim(in.Note), CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if code, _, ok := db.ConstraintError(err); ok && code == db.CodeUniqueViolation {
			return apperr.Conflict(apperr.CodeConflictExists, "Тариф на этот ресурс и объект с этой даты уже есть")
		}
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tariff.create", EntityType: "tariff", EntityID: t.ID.String(),
			Summary: fmt.Sprintf("Тариф «%s» (%s) с %s: %s руб.", TypeTitles[t.MeterType], scope, t.EffectiveFrom.Format(), t.Price.String()),
		})
	})
	return t, err
}

// DeleteTariff — удалить тариф, если по нему ещё не начислялись ЖКУ (06 API, раздел 4).
func (s *Service) DeleteTariff(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		t, err := tx.Q.GetTariff(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		used, err := tx.Q.TariffInUse(ctx, sqlc.TariffInUseParams{EffectiveFrom: t.EffectiveFrom, PropertyID: t.PropertyID})
		if err != nil {
			return err
		}
		if used {
			return apperr.BusinessRule("Тариф нельзя удалить: по нему уже есть начисления ЖКУ")
		}
		if err := tx.Q.DeleteTariff(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "tariff.delete", EntityType: "tariff", EntityID: id.String(),
			Summary: fmt.Sprintf("Удалён тариф «%s» с %s", TypeTitles[t.MeterType], t.EffectiveFrom.Format()),
			Before:  map[string]any{"meterType": t.MeterType, "propertyId": t.PropertyID, "price": t.Price.String(), "effectiveFrom": t.EffectiveFrom.String()},
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
