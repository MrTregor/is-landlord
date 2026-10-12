package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

// Виды операций с залогом.
const (
	DepositReceipt     = "receipt"
	DepositWithholding = "withholding"
	DepositOffset      = "offset"
	DepositRefund      = "refund"
)

// Deposit — обеспечительный платёж договора (06 разд. 11).
type Deposit struct {
	ExpectedKop, ReceivedKop, BalanceKop int64
	Operations                           []sqlc.DepositOperationsRow
	LastID                               uuid.UUID
}

// Deposit — ожидается, внесено, остаток и операции.
func (s *Service) Deposit(ctx context.Context, contractID uuid.UUID) (Deposit, error) {
	q := s.db.Queries()
	c, err := q.GetContract(ctx, contractID)
	if db.IsNoRows(err) {
		return Deposit{}, apperr.NotFound()
	}
	if err != nil {
		return Deposit{}, err
	}
	tot, err := q.DepositTotals(ctx, c.ID)
	if err != nil {
		return Deposit{}, err
	}
	ops, err := q.DepositOperations(ctx, c.ID)
	if err != nil {
		return Deposit{}, err
	}
	d := Deposit{ExpectedKop: c.DepositKop, ReceivedKop: tot.ReceivedKop, BalanceKop: tot.BalanceKop, Operations: ops}
	last, err := q.LastDepositOperation(ctx, c.ID)
	switch {
	case err == nil:
		d.LastID = last.ID
	case !db.IsNoRows(err):
		return d, err
	}
	return d, nil
}

// OpInput — удержание, зачёт или возврат залога.
type OpInput struct {
	AmountKop            int64
	OpDate               date.Date
	Reason               string
	Method               string      // возврат: bank_transfer или cash
	ChargeIDs            []uuid.UUID // зачёт: выбранные начисления
	MaintenanceRequestID *uuid.UUID  // удержание: связь с заявкой
	ExpenseID            *uuid.UUID  // удержание: связь с затратой
}

// lockDeposit — договор под блокировкой, проверки общей части операции (сумма, дата, причина) и остаток.
func (s *Service) lockDeposit(ctx context.Context, tx *db.Tx, contractID uuid.UUID, in OpInput) (sqlc.Contract, int64, string, error) {
	reason, err := needReason("reason", in.Reason)
	if err != nil {
		return sqlc.Contract{}, 0, "", err
	}
	var errs []apperr.FieldError
	if in.AmountKop <= 0 {
		errs = append(errs, apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if in.OpDate.After(s.clock.Today()) {
		errs = append(errs, apperr.FieldError{Field: "opDate", Message: "Дата операции не может быть позже сегодняшней"})
	}
	if len(errs) > 0 {
		return sqlc.Contract{}, 0, "", apperr.Validation(errs...)
	}
	c, err := lockContract(ctx, tx, contractID)
	if err != nil {
		return c, 0, "", err
	}
	if c.Status == "draft" {
		return c, 0, "", apperr.Conflict(apperr.CodeConflictState, "Договор не введён в действие")
	}
	tot, err := tx.Q.DepositTotals(ctx, c.ID)
	if err != nil {
		return c, 0, "", err
	}
	// 05 разд. 9.5: остаток не может стать отрицательным.
	if in.AmountKop > tot.BalanceKop {
		return c, 0, "", apperr.BusinessRule("Сумма больше остатка залога " + money.Kop(tot.BalanceKop).Format())
	}
	return c, tot.BalanceKop, reason, nil
}

// Withhold — удержание из залога (05 разд. 9.2): доход на дату операции, письмо нанимателю deposit_withheld.
func (s *Service) Withhold(ctx context.Context, contractID uuid.UUID, in OpInput) (sqlc.DepositTransaction, error) {
	var op sqlc.DepositTransaction
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, balance, reason, err := s.lockDeposit(ctx, tx, contractID, in)
		if err != nil {
			return err
		}
		op, err = tx.Q.InsertDepositTx(ctx, sqlc.InsertDepositTxParams{
			ContractID: c.ID, Kind: DepositWithholding, AmountKop: in.AmountKop, OpDate: in.OpDate, Reason: &reason,
			MaintenanceRequestID: in.MaintenanceRequestID, ExpenseID: in.ExpenseID, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if code, _, ok := db.ConstraintError(err); ok && code == db.CodeForeignKey {
			return apperr.Validation(apperr.FieldError{Field: "maintenanceRequestId", Message: "Заявка или затрата не найдена"})
		}
		if err != nil {
			return err
		}
		rest := balance - in.AmountKop
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "deposit.withhold", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: удержано из залога %s", c.Number, money.Kop(in.AmountKop).Format()),
			After: map[string]any{"depositTxId": op.ID, "amountKop": in.AmountKop, "opDate": in.OpDate.String(), "reason": reason,
				"balanceKop": rest},
		}); err != nil {
			return err
		}
		to, vars, err := tenantMail(ctx, tx.Q, c)
		if err != nil {
			return err
		}
		vars["amount"] = money.Kop(in.AmountKop).FormatRub()
		vars["reason"] = reason
		vars["balance"] = money.Kop(rest).FormatRub()
		return s.notify.Queue(ctx, tx, notify.Email{
			Code: "deposit_withheld", To: to, Vars: vars, DedupeKey: "deposit_withheld:" + op.ID.String(),
			RelatedType: "contract", RelatedID: &c.ID,
		})
	})
	return op, err
}

// Offset — зачёт залога в счёт долга (05 разд. 9.3): платёж deposit_offset с распределением по выбранным
// начислениям (по умолчанию — все по порядку погашения) и операция offset со ссылкой на платёж.
func (s *Service) Offset(ctx context.Context, contractID uuid.UUID, in OpInput) (sqlc.DepositTransaction, error) {
	var op sqlc.DepositTransaction
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, _, reason, err := s.lockDeposit(ctx, tx, contractID, in)
		if err != nil {
			return err
		}
		if err := checkRequested(ctx, tx.Q, c.ID, in.ChargeIDs); err != nil {
			return err
		}
		debt, err := s.debtOf(ctx, tx, c.ID, in.ChargeIDs)
		if err != nil {
			return err
		}
		if in.AmountKop > debt {
			return apperr.BusinessRule("Сумма больше долга по выбранным начислениям " + money.Kop(debt).Format())
		}
		res, err := s.post(ctx, tx, c, PurposeCharges, MethodDepositOffset, SourceDepositOffset, in.AmountKop, in.OpDate, in.ChargeIDs, nil)
		if err != nil {
			return err
		}
		op, err = tx.Q.InsertDepositTx(ctx, sqlc.InsertDepositTxParams{
			ContractID: c.ID, Kind: DepositOffset, AmountKop: in.AmountKop, OpDate: in.OpDate, Reason: &reason,
			PaymentID: &res.payment.ID, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		// Чек «зачёт предоплаты» (05 разд. 11) — модуль фискализации (этап M8).
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "deposit.offset", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: зачёт залога %s в счёт долга, квитанция %s", c.Number,
				money.Kop(in.AmountKop).Format(), *res.payment.ReceiptNumber),
			After: map[string]any{"depositTxId": op.ID, "paymentId": res.payment.ID, "amountKop": in.AmountKop,
				"opDate": in.OpDate.String(), "chargeIds": in.ChargeIDs, "reason": reason},
		})
	})
	return op, err
}

// debtOf — долг по выбранным начислениям (или по всем открытым, кроме начисляемых пеней).
func (s *Service) debtOf(ctx context.Context, tx *db.Tx, contractID uuid.UUID, ids []uuid.UUID) (int64, error) {
	open, err := s.billing.OpenCharges(ctx, tx, contractID)
	if err != nil {
		return 0, err
	}
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var debt int64
	for _, ch := range open {
		if len(ids) == 0 || want[ch.ID] {
			debt += ch.AmountKop - ch.PaidKop
		}
	}
	return debt, nil
}

// methodNames — способ возврата залога в основании операции.
var methodNames = map[string]string{MethodBankTransfer: "переводом на счёт", MethodCash: "наличными"}

// RefundDeposit — возврат залога (05 разд. 9.4), письмо нанимателю deposit_refunded. Способ возврата сохраняется
// в основании операции (в deposit_transactions нет отдельного поля, docs/open-questions.md).
func (s *Service) RefundDeposit(ctx context.Context, contractID uuid.UUID, in OpInput) (sqlc.DepositTransaction, error) {
	if in.Method != MethodBankTransfer && in.Method != MethodCash {
		return sqlc.DepositTransaction{}, apperr.Validation(apperr.FieldError{Field: "method", Message: "Способ — перевод на счёт или наличные"})
	}
	var op sqlc.DepositTransaction
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, _, reason, err := s.lockDeposit(ctx, tx, contractID, in)
		if err != nil {
			return err
		}
		reason = reason + " (" + methodNames[in.Method] + ")"
		op, err = tx.Q.InsertDepositTx(ctx, sqlc.InsertDepositTxParams{
			ContractID: c.ID, Kind: DepositRefund, AmountKop: in.AmountKop, OpDate: in.OpDate, Reason: &reason,
			CreatedBy: actorID(ctx), Now: s.clock.Now(),
		})
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "deposit.refund", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: возвращён залог %s", c.Number, money.Kop(in.AmountKop).Format()),
			After: map[string]any{"depositTxId": op.ID, "amountKop": in.AmountKop, "opDate": in.OpDate.String(), "method": in.Method,
				"reason": reason},
		}); err != nil {
			return err
		}
		to, vars, err := tenantMail(ctx, tx.Q, c)
		if err != nil {
			return err
		}
		vars["amount"] = money.Kop(in.AmountKop).FormatRub()
		vars["op_date"] = in.OpDate.Format()
		return s.notify.Queue(ctx, tx, notify.Email{
			Code: "deposit_refunded", To: to, Vars: vars, DedupeKey: "deposit_refunded:" + op.ID.String(),
			RelatedType: "contract", RelatedID: &c.ID,
		})
	})
	return op, err
}

// CancelOperation — отмена последней операции по залогу (05 разд. 9.5). Отмена зачёта отменяет связанный платёж,
// отмена внесения платежом — сам платёж; отмена переноса переплаты возвращает сумму в аванс платежа.
func (s *Service) CancelOperation(ctx context.Context, id uuid.UUID, reason string) (sqlc.DepositTransaction, error) {
	reason, err := needReason("reason", reason)
	if err != nil {
		return sqlc.DepositTransaction{}, err
	}
	var op sqlc.DepositTransaction
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		cur, err := tx.Q.GetDepositTxForUpdate(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		c, err := lockContract(ctx, tx, cur.ContractID)
		if err != nil {
			return err
		}
		if cur.CancelledAt != nil {
			return apperr.Conflict(apperr.CodeConflictState, "Операция уже отменена")
		}
		last, err := tx.Q.LastDepositOperation(ctx, c.ID)
		if err != nil {
			return err
		}
		if last.ID != cur.ID {
			return apperr.BusinessRule("Отменить можно только последнюю операцию по залогу")
		}
		linked := cur.PaymentID != nil && (cur.Kind == DepositOffset || (cur.Kind == DepositReceipt && !cur.FromAdvance))
		if linked {
			p, err := tx.Q.GetPaymentForUpdate(ctx, *cur.PaymentID)
			if err != nil {
				return err
			}
			if p.Status == "succeeded" {
				// Отмена платежа отменяет и эту операцию.
				if err := s.cancelPayment(ctx, tx, p, c, reason); err != nil {
					return err
				}
			}
		}
		now := s.clock.Now()
		if err := tx.Q.CancelDepositTx(ctx, sqlc.CancelDepositTxParams{ID: cur.ID, Now: &now, Reason: &reason}); err != nil {
			return err
		}
		if err := s.billing.Rebalance(ctx, tx, c.ID); err != nil {
			return err
		}
		if op, err = tx.Q.GetDepositTxForUpdate(ctx, cur.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "deposit.cancel", EntityType: "contract", EntityID: c.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: отменена операция с залогом «%s» на %s", c.Number, DepositKindTitles[cur.Kind],
				money.Kop(cur.AmountKop).Format()),
			Before: map[string]any{"depositTxId": cur.ID, "kind": cur.Kind, "amountKop": cur.AmountKop},
			After:  map[string]any{"cancelled": true, "reason": reason},
		})
	})
	return op, err
}

// DepositKindTitles — виды операций с залогом (04, справочник значений).
var DepositKindTitles = map[string]string{
	DepositReceipt: "Внесение", DepositWithholding: "Удержание", DepositOffset: "Зачёт в счёт долга", DepositRefund: "Возврат",
}
