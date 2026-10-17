// Package expense — затраты по объектам (FR-05.6): учёт, возмещение за счёт нанимателя удержанием из залога или
// начислением «возмещение ущерба». Правила — docs/spec/05 Бизнес-правила и расчёты.md, раздел 14.2.
package expense

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/file"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/reqctx"
)

// Возмещение затраты за счёт нанимателя.
const (
	RecoveryNone    = "none"
	RecoveryDeposit = "deposit"
	RecoveryCharge  = "charge"
)

// Service — затраты.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	billing  *billing.Service
	payments *payment.Service
}

// NewService создаёт сервис затрат.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, bl *billing.Service, ps *payment.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, billing: bl, payments: ps}
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

// Input — поля затраты (14 Экраны собственника, раздел 10).
type Input struct {
	PropertyID           uuid.UUID
	Category             string
	AmountKop            int64
	PaidOn               date.Date
	PeriodMonth          date.Date
	Description          string
	Counterparty         *string
	FileID               *uuid.UUID
	MaintenanceRequestID *uuid.UUID
}

// CreateInput — новая затрата с возмещением.
type CreateInput struct {
	Input
	Recovery      string
	ContractID    *uuid.UUID
	ChargeDueDate *date.Date
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

// check — проверка полей и связей (объект, документ, заявка того же объекта).
func (s *Service) check(ctx context.Context, q *sqlc.Queries, in *Input) error {
	in.Description = strings.TrimSpace(in.Description)
	in.Counterparty = trimPtr(in.Counterparty)
	in.PeriodMonth = in.PeriodMonth.FirstOfMonth()
	var errs []apperr.FieldError
	if in.AmountKop <= 0 {
		errs = append(errs, apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if in.Description == "" {
		errs = append(errs, apperr.FieldError{Field: "description", Message: "Укажите описание"})
	}
	if _, err := q.GetProperty(ctx, in.PropertyID); err != nil {
		if !db.IsNoRows(err) {
			return err
		}
		errs = append(errs, apperr.FieldError{Field: "propertyId", Message: "Объект не найден"})
	}
	if in.FileID != nil {
		f, err := q.GetFile(ctx, *in.FileID)
		switch {
		case db.IsNoRows(err) || (err == nil && (f.DeletedAt != nil || f.Category != file.CategoryExpenseDoc)):
			errs = append(errs, apperr.FieldError{Field: "fileId", Message: "Документ не найден"})
		case err != nil:
			return err
		}
	}
	if in.MaintenanceRequestID != nil {
		r, err := q.GetRequest(ctx, *in.MaintenanceRequestID)
		switch {
		case db.IsNoRows(err):
			errs = append(errs, apperr.FieldError{Field: "maintenanceRequestId", Message: "Заявка не найдена"})
		case err != nil:
			return err
		case r.PropertyID != in.PropertyID:
			errs = append(errs, apperr.FieldError{Field: "maintenanceRequestId", Message: "Заявка относится к другому объекту"})
		}
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

func snapshot(e sqlc.Expense) map[string]any {
	return map[string]any{
		"propertyId": e.PropertyID, "category": e.Category, "amountKop": e.AmountKop, "paidOn": e.PaidOn.String(),
		"periodMonth": e.PeriodMonth.String(), "description": e.Description, "counterparty": e.Counterparty, "fileId": e.FileID,
		"maintenanceRequestId": e.MaintenanceRequestID, "recovery": e.Recovery, "depositTransactionId": e.DepositTransactionID,
		"chargeId": e.ChargeID,
	}
}

// Create — новая затрата; «за счёт нанимателя» — удержание из залога или начисление «возмещение ущерба»
// по действующему или последнему договору объекта (05 разд. 14.2) в той же транзакции.
func (s *Service) Create(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := s.check(ctx, tx.Q, &in.Input); err != nil {
			return err
		}
		var contract sqlc.Contract
		if in.Recovery != RecoveryNone {
			var err error
			if contract, err = s.recoveryContract(ctx, tx.Q, in); err != nil {
				return err
			}
		}
		e, err := tx.Q.InsertExpense(ctx, sqlc.InsertExpenseParams{
			PropertyID: in.PropertyID, Category: in.Category, AmountKop: in.AmountKop, PaidOn: in.PaidOn, PeriodMonth: in.PeriodMonth,
			Description: in.Description, Counterparty: in.Counterparty, FileID: in.FileID, MaintenanceRequestID: in.MaintenanceRequestID,
			CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		id = e.ID
		summary := fmt.Sprintf("Затрата %s: %s", money.Kop(e.AmountKop).Format(), e.Description)
		switch in.Recovery {
		case RecoveryDeposit:
			op, err := s.payments.WithholdTx(ctx, tx, contract.ID, payment.OpInput{
				AmountKop: e.AmountKop, OpDate: s.clock.Today(), Reason: e.Description, MaintenanceRequestID: e.MaintenanceRequestID,
				ExpenseID: &e.ID,
			})
			if err != nil {
				return err
			}
			if e, err = tx.Q.SetExpenseRecovery(ctx, sqlc.SetExpenseRecoveryParams{ID: e.ID, Recovery: RecoveryDeposit, DepositTransactionID: &op.ID}); err != nil {
				return err
			}
			summary += fmt.Sprintf(", удержано из залога по договору № %s", contract.Number)
		case RecoveryCharge:
			due := s.clock.Today().AddDays(10)
			if in.ChargeDueDate != nil {
				due = *in.ChargeDueDate
			}
			chID, err := s.billing.CreateManualTx(ctx, tx, billing.ManualInput{
				ContractID: contract.ID, Kind: billing.KindDamage, Title: "Возмещение: " + e.Description, AmountKop: e.AmountKop,
				DueDate: due, Reason: "Затрата за счёт нанимателя",
			})
			if err != nil {
				return err
			}
			if e, err = tx.Q.SetExpenseRecovery(ctx, sqlc.SetExpenseRecoveryParams{ID: e.ID, Recovery: RecoveryCharge, ChargeID: &chID}); err != nil {
				return err
			}
			summary += fmt.Sprintf(", выставлено начисление по договору № %s", contract.Number)
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "expense.create", EntityType: "expense", EntityID: e.ID.String(), Summary: summary, After: snapshot(e),
		})
	})
	return id, err
}

// recoveryContract — договор возмещения: указанный (того же объекта) или действующий / последний по объекту.
func (s *Service) recoveryContract(ctx context.Context, q *sqlc.Queries, in CreateInput) (sqlc.Contract, error) {
	if in.Recovery != RecoveryDeposit && in.Recovery != RecoveryCharge {
		return sqlc.Contract{}, apperr.Validation(apperr.FieldError{Field: "recovery", Message: "Неизвестный способ возмещения"})
	}
	var (
		c   sqlc.Contract
		err error
	)
	if in.ContractID != nil {
		c, err = q.GetContract(ctx, *in.ContractID)
	} else {
		c, err = q.LastContractOfProperty(ctx, in.PropertyID)
	}
	if db.IsNoRows(err) || (err == nil && (c.PropertyID != in.PropertyID || c.Status == "draft")) {
		return c, apperr.Validation(apperr.FieldError{Field: "contractId", Message: "У объекта нет договора, по которому можно возместить затрату"})
	}
	return c, err
}

// Update — изменение затраты (без способа возмещения). У возмещённой затраты нельзя менять сумму и объект
// (docs/open-questions.md, № 86).
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		before, err := tx.Q.GetExpenseForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if err := s.check(ctx, tx.Q, &in); err != nil {
			return err
		}
		if before.Recovery != RecoveryNone && (before.AmountKop != in.AmountKop || before.PropertyID != in.PropertyID) {
			return apperr.BusinessRule("Затрата возмещена нанимателем — сумму и объект изменить нельзя")
		}
		after, err := tx.Q.UpdateExpense(ctx, sqlc.UpdateExpenseParams{
			ID: id, PropertyID: in.PropertyID, Category: in.Category, AmountKop: in.AmountKop, PaidOn: in.PaidOn,
			PeriodMonth: in.PeriodMonth, Description: in.Description, Counterparty: in.Counterparty, FileID: in.FileID,
			MaintenanceRequestID: in.MaintenanceRequestID, Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "expense.update", EntityType: "expense", EntityID: id.String(),
			Summary: fmt.Sprintf("Изменена затрата %s: %s", money.Kop(after.AmountKop).Format(), after.Description),
			Before:  snapshot(before), After: snapshot(after),
		})
	})
}

// Delete — удаление затраты без возмещения (запись в журнал с полным снимком, 06 разд. 13).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		e, err := tx.Q.GetExpenseForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if e.Recovery != RecoveryNone {
			return apperr.BusinessRule("Затрата возмещена нанимателем — удалить её нельзя")
		}
		if err := tx.Q.DeleteExpense(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "expense.delete", EntityType: "expense", EntityID: id.String(),
			Summary: fmt.Sprintf("Удалена затрата %s: %s", money.Kop(e.AmountKop).Format(), e.Description), Before: snapshot(e),
		})
	})
}
