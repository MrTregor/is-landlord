package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

func chargeSnapshot(ch sqlc.Charge) map[string]any {
	return map[string]any{
		"kind": ch.Kind, "title": ch.Title, "amountKop": ch.AmountKop, "paidKop": ch.PaidKop, "status": ch.Status,
		"penaltyState": ch.PenaltyState, "dueDate": ch.DueDate,
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func needReason(reason string) (string, error) {
	r := strings.TrimSpace(reason)
	if r == "" {
		return "", apperr.Validation(apperr.FieldError{Field: "reason", Message: "Укажите причину"})
	}
	return r, nil
}

// ManualInput — ручное начисление (возмещение ущерба или прочее).
type ManualInput struct {
	ContractID uuid.UUID
	Kind       string
	Title      string
	AmountKop  int64
	DueDate    date.Date
	Reason     string
}

// CreateManual — ручное начисление (14 разд. 5).
func (s *Service) CreateManual(ctx context.Context, in ManualInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		var err error
		id, err = s.CreateManualTx(ctx, tx, in)
		return err
	})
	return id, err
}

// CreateManualTx — ручное начисление в транзакции вызывающего (возмещение затраты начислением, 05 разд. 14.2).
func (s *Service) CreateManualTx(ctx context.Context, tx *db.Tx, in ManualInput) (uuid.UUID, error) {
	reason, err := needReason(in.Reason)
	if err != nil {
		return uuid.Nil, err
	}
	in.Title = strings.TrimSpace(in.Title)
	var errs []apperr.FieldError
	if in.Kind != KindDamage && in.Kind != KindOther {
		errs = append(errs, apperr.FieldError{Field: "kind", Message: "Вид — возмещение ущерба или прочее"})
	}
	if in.Title == "" {
		errs = append(errs, apperr.FieldError{Field: "title", Message: "Укажите название"})
	}
	if in.AmountKop <= 0 {
		errs = append(errs, apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if len(errs) > 0 {
		return uuid.Nil, apperr.Validation(errs...)
	}
	var id uuid.UUID
	err = func() error {
		c, err := lockContract(ctx, tx, in.ContractID)
		if err != nil {
			return apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Договор не найден"})
		}
		if c.Status == "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Начисления создаются по договору, введённому в действие")
		}
		id, err = tx.Q.InsertManualCharge(ctx, sqlc.InsertManualChargeParams{
			ContractID: c.ID, Kind: in.Kind, Title: in.Title, DueDate: in.DueDate, AmountKop: in.AmountKop,
			CalcDetails: mustJSON(map[string]any{"reason": reason}), CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.Rebalance(ctx, tx, c.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "charge.create", EntityType: "charge", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: ручное начисление «%s» на %s", c.Number, in.Title, money.Kop(in.AmountKop).Format()),
			After:   map[string]any{"kind": in.Kind, "title": in.Title, "amountKop": in.AmountKop, "dueDate": in.DueDate.String(), "reason": reason},
		})
	}()
	return id, err
}

// lockCharge — начисление и его договор (порядок блокировок: договор, затем начисление).
func (s *Service) lockCharge(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Charge, sqlc.Contract, error) {
	row, err := tx.Q.GetChargeRow(ctx, id)
	if db.IsNoRows(err) {
		return sqlc.Charge{}, sqlc.Contract{}, apperr.NotFound()
	}
	if err != nil {
		return sqlc.Charge{}, sqlc.Contract{}, err
	}
	c, err := lockContract(ctx, tx, row.ContractID)
	if err != nil {
		return sqlc.Charge{}, c, err
	}
	ch, err := tx.Q.GetChargeForUpdate(ctx, id)
	return ch, c, err
}

// Adjust — корректировка суммы со знаком (не для пеней).
func (s *Service) Adjust(ctx context.Context, id uuid.UUID, delta int64, reason string) error {
	reason, err := needReason(reason)
	if err != nil {
		return err
	}
	if delta == 0 {
		return apperr.Validation(apperr.FieldError{Field: "deltaKop", Message: "Сумма изменения не может быть нулевой"})
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		ch, c, err := s.lockCharge(ctx, tx, id)
		if err != nil {
			return err
		}
		if ch.Kind == KindPenalty {
			return apperr.BusinessRule("Сумму пеней меняйте действием «Изменить сумму пеней»")
		}
		if ch.Status == "cancelled" {
			return apperr.Conflict(apperr.CodeConflictState, "Начисление отменено")
		}
		amount := ch.AmountKop + delta
		if amount < 0 {
			return apperr.Validation(apperr.FieldError{Field: "deltaKop", Message: "Сумма начисления не может стать отрицательной"})
		}
		if err := s.setAmount(ctx, tx, ch, amount, ch.PeriodEnd, "manual", reason, map[string]any{"adjustedFrom": ch.AmountKop}); err != nil {
			return err
		}
		if err := s.Rebalance(ctx, tx, c.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "charge.adjust", EntityType: "charge", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: корректировка «%s» на %s", c.Number, ch.Title, money.Kop(delta).Format()),
			Before:  chargeSnapshot(ch), After: map[string]any{"amountKop": amount, "deltaKop": delta, "reason": reason},
		})
	})
}

// Cancel — отмена начисления с причиной; распределения снимаются (деньги — аванс), начисляемые пени по нему отменяются.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID, reason string) error {
	reason, err := needReason(reason)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		ch, c, err := s.lockCharge(ctx, tx, id)
		if err != nil {
			return err
		}
		if ch.Status == "cancelled" {
			return apperr.Conflict(apperr.CodeConflictState, "Начисление уже отменено")
		}
		if err := s.cancel(ctx, tx, ch, reason); err != nil {
			return err
		}
		if pen, err := tx.Q.PenaltyOf(ctx, &ch.ID); err == nil {
			if err := s.cancel(ctx, tx, pen, reason); err != nil {
				return err
			}
		} else if !db.IsNoRows(err) {
			return err
		}
		if err := s.Rebalance(ctx, tx, c.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "charge.cancel", EntityType: "charge", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: отменено начисление «%s»", c.Number, ch.Title),
			Before:  chargeSnapshot(ch), After: map[string]any{"status": "cancelled", "reason": reason},
		})
	})
}

// SetPenalty — «Изменить сумму пеней» (penalty_manual_set) или «Списать пени» (writeOff, сумма 0, penalty_writeoff).
// Состояние становится manual — задача пеней их больше не пересчитывает.
func (s *Service) SetPenalty(ctx context.Context, id uuid.UUID, amount int64, reason string, writeOff bool) error {
	reason, err := needReason(reason)
	if err != nil {
		return err
	}
	if writeOff {
		amount = 0
	}
	if amount < 0 {
		return apperr.Validation(apperr.FieldError{Field: "amountKop", Message: "Сумма не может быть отрицательной"})
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		ch, c, err := s.lockCharge(ctx, tx, id)
		if err != nil {
			return err
		}
		if ch.Kind != KindPenalty {
			return apperr.BusinessRule("Действие доступно только для пеней")
		}
		if ch.Status == "cancelled" {
			return apperr.Conflict(apperr.CodeConflictState, "Начисление отменено")
		}
		due := s.clock.Today()
		if ch.DueDate != nil {
			due = *ch.DueDate
		}
		if err := s.trimPaid(ctx, tx, ch, amount, reason); err != nil {
			return err
		}
		if err := tx.Q.SetPenaltyManual(ctx, sqlc.SetPenaltyManualParams{ID: id, AmountKop: amount, DueDate: &due}); err != nil {
			return err
		}
		kind, action, title := "penalty_manual_set", "penalty.set", "изменена сумма пеней"
		if writeOff {
			kind, action, title = "penalty_writeoff", "penalty.write_off", "пени списаны"
		}
		if delta := amount - ch.AmountKop; delta != 0 {
			if err := tx.Q.InsertAdjustment(ctx, sqlc.InsertAdjustmentParams{
				ChargeID: id, Kind: kind, DeltaKop: delta, Reason: reason, CreatedBy: actorID(ctx), Now: s.clock.Now(),
			}); err != nil {
				return err
			}
		}
		if _, err := tx.Q.RefreshChargePaid(ctx, id); err != nil {
			return err
		}
		if err := s.Rebalance(ctx, tx, c.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: action, EntityType: "charge", EntityID: id.String(),
			Summary: fmt.Sprintf("Договор № %s: %s «%s» — %s", c.Number, title, ch.Title, money.Kop(amount).Format()),
			Before:  chargeSnapshot(ch), After: map[string]any{"amountKop": amount, "penaltyState": PenaltyManual, "reason": reason},
		})
	})
}
