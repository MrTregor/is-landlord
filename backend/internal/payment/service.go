// Package payment — платежи (FR-06.1): ручная регистрация, номер квитанции, распределение по начислениям (FR-07.1),
// авансы, отмена, возвраты переплаты; обеспечительный платёж (FR-05.2); расхождения «переплата» и «недоплата»
// (FR-07.3). Правила — docs/spec/05 Бизнес-правила и расчёты.md, разделы 9 и 10.
package payment

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/document"
	"landlord/backend/internal/fiscal"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
	"landlord/backend/internal/platform/reqctx"
)

// Назначения, способы и источники платежей.
const (
	PurposeCharges = "charges"
	PurposeDeposit = "deposit"

	MethodBankTransfer  = "bank_transfer"
	MethodCash          = "cash"
	MethodDepositOffset = "deposit_offset"

	SourceManual        = "manual"
	SourceBankImport    = "bank_import"
	SourceDepositOffset = "deposit_offset"
)

// Service — платежи, залог, расхождения.
type Service struct {
	db        *db.DB
	audit     *audit.Service
	clock     clock.Clock
	billing   *billing.Service
	documents *document.Service
	notify    *notify.Service
	jobs      notify.Inserter
	online    Online
	fiscal    *fiscal.Service
}

// NewService создаёт сервис платежей.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, bl *billing.Service, docs *document.Service,
	nt *notify.Service, ins notify.Inserter, online Online, fs *fiscal.Service,
) *Service {
	return &Service{db: database, audit: au, clock: clk, billing: bl, documents: docs, notify: nt, jobs: ins, online: online, fiscal: fs}
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func needReason(field, reason string) (string, error) {
	r := strings.TrimSpace(reason)
	if r == "" {
		return "", apperr.Validation(apperr.FieldError{Field: field, Message: "Укажите причину"})
	}
	return r, nil
}

// lockContract — договор под блокировкой (распределение блокирует строку договора, 05 разд. 10.3).
func lockContract(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Contract, error) {
	c, err := tx.Q.GetContractForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return c, apperr.NotFound()
	}
	return c, err
}

// receiptNumber — «КВ-2026-00048»: год даты поступления и сквозной номер в году (05 разд. 10.2).
func receiptNumber(ctx context.Context, q *sqlc.Queries, on date.Date) (string, error) {
	n, err := q.NextSequenceValue(ctx, sqlc.NextSequenceValueParams{Name: "receipt", Year: int32(on.Year())}) //nolint:gosec // год — четыре цифры
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("КВ-%d-%05d", on.Year(), n), nil
}

// CreateInput — ручная регистрация платежа (05 разд. 10.1).
type CreateInput struct {
	ContractID uuid.UUID
	Purpose    string
	AmountKop  int64
	ReceivedOn date.Date
	Method     string
	ChargeIDs  []uuid.UUID
	Comment    *string
}

func (s *Service) validateCreate(in CreateInput) error {
	var errs []apperr.FieldError
	if in.Purpose != PurposeCharges && in.Purpose != PurposeDeposit {
		errs = append(errs, apperr.FieldError{Field: "purpose", Message: "Выберите назначение платежа"})
	}
	if in.AmountKop <= 0 {
		errs = append(errs, apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if in.ReceivedOn.After(s.clock.Today()) {
		errs = append(errs, apperr.FieldError{Field: "receivedOn", Message: "Дата поступления не может быть позже сегодняшней"})
	}
	if in.Method != MethodBankTransfer && in.Method != MethodCash {
		errs = append(errs, apperr.FieldError{Field: "method", Message: "Способ — перевод на счёт или наличные"})
	}
	if len(errs) > 0 {
		return apperr.Validation(errs...)
	}
	return nil
}

// posted — данные проведённого платежа для журнала и последующих действий.
type posted struct {
	payment sqlc.Payment
	advance money.Kop
}

// post — проведение платежа в транзакции (05 разд. 10.1): номер квитанции, распределение (для начислений),
// внесение залога (для залога), расхождения «переплата» и «недоплата», задача формирования квитанции.
// Договор уже заблокирован.
func (s *Service) post(ctx context.Context, tx *db.Tx, c sqlc.Contract, purpose, method, source string, amount int64,
	on date.Date, requested []uuid.UUID, comment *string,
) (posted, error) {
	number, err := receiptNumber(ctx, tx.Q, on)
	if err != nil {
		return posted{}, err
	}
	if requested == nil {
		requested = []uuid.UUID{}
	}
	p, err := tx.Q.InsertPayment(ctx, sqlc.InsertPaymentParams{
		ContractID: c.ID, Purpose: purpose, AmountKop: amount, ReceivedOn: &on, Method: method, Source: source,
		RequestedChargeIds: requested, ReceiptNumber: &number, Comment: comment, CreatedBy: actorID(ctx), Now: s.clock.Now(),
	})
	if err != nil {
		return posted{}, err
	}
	return s.settle(ctx, tx, c, p)
}

// settle — последствия проведения платежа (05 разд. 10.1, 08 разд. 1.5): внесение залога или распределение
// (сначала requested_charge_ids), перераспределение авансов, расхождения «переплата» и «недоплата», задача квитанции.
// Платёж уже в статусе succeeded, договор заблокирован.
func (s *Service) settle(ctx context.Context, tx *db.Tx, c sqlc.Contract, p sqlc.Payment) (posted, error) {
	res := posted{payment: p}
	on := *p.ReceivedOn
	if p.Purpose == PurposeDeposit {
		if _, err := tx.Q.InsertDepositTx(ctx, sqlc.InsertDepositTxParams{
			ContractID: c.ID, Kind: "receipt", AmountKop: p.AmountKop, OpDate: on, PaymentID: &p.ID, CreatedBy: actorID(ctx), Now: s.clock.Now(),
		}); err != nil {
			return res, err
		}
	} else {
		open, err := s.billing.OpenCharges(ctx, tx, c.ID)
		if err != nil {
			return res, err
		}
		if res.advance, err = s.billing.Allocate(ctx, tx, p.ID, money.Kop(p.AmountKop), open, p.RequestedChargeIds); err != nil {
			return res, err
		}
		if err := s.billing.Rebalance(ctx, tx, c.ID); err != nil {
			return res, err
		}
		if err := s.billing.FlagOverpayment(ctx, tx, c.ID, p.ID, res.advance); err != nil {
			return res, err
		}
	}
	if err := s.billing.CheckUnderpayment(ctx, tx, c.ID, p.ID, on); err != nil {
		return res, err
	}
	// Чек прихода по режиму объекта (05 разд. 11).
	if err := s.fiscal.ForPayment(ctx, tx, p, c); err != nil {
		return res, err
	}
	// Квитанция PDF и письмо нанимателю — задачей document.payment_receipt (07 Фоновые задачи).
	if _, err := s.jobs.InsertTx(ctx, tx.Tx, ReceiptArgs{PaymentID: p.ID}, nil); err != nil {
		return res, err
	}
	return res, nil
}

// Create — ручная регистрация платежа собственником (05 разд. 10.1).
func (s *Service) Create(ctx context.Context, in CreateInput) (uuid.UUID, error) {
	if err := s.validateCreate(in); err != nil {
		return uuid.Nil, err
	}
	in.Comment = trimPtr(in.Comment)
	var id uuid.UUID
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		c, err := lockContract(ctx, tx, in.ContractID)
		if err != nil {
			return apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Договор не найден"})
		}
		if c.Status == "draft" {
			return apperr.Conflict(apperr.CodeConflictState, "Платежи регистрируются по договору, введённому в действие")
		}
		requested := in.ChargeIDs
		if in.Purpose == PurposeDeposit {
			requested = nil
		}
		if err := checkRequested(ctx, tx.Q, c.ID, requested); err != nil {
			return err
		}
		res, err := s.post(ctx, tx, c, in.Purpose, in.Method, SourceManual, in.AmountKop, in.ReceivedOn, requested, in.Comment)
		if err != nil {
			return err
		}
		id = res.payment.ID
		after := map[string]any{
			"purpose": in.Purpose, "amountKop": in.AmountKop, "receivedOn": in.ReceivedOn.String(), "method": in.Method,
			"receiptNumber": res.payment.ReceiptNumber, "chargeIds": requested, "advanceKop": int64(res.advance),
		}
		summary := fmt.Sprintf("Договор № %s: зарегистрирован платёж %s, квитанция %s", c.Number,
			money.Kop(in.AmountKop).Format(), *res.payment.ReceiptNumber)
		if err := s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "payment.create", EntityType: "payment", EntityID: id.String(), Summary: summary, After: after,
		}); err != nil {
			return err
		}
		if in.Purpose == PurposeDeposit {
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "deposit.receipt", EntityType: "contract", EntityID: c.ID.String(),
				Summary: fmt.Sprintf("Договор № %s: внесён залог %s", c.Number, money.Kop(in.AmountKop).Format()),
				After:   map[string]any{"amountKop": in.AmountKop, "opDate": in.ReceivedOn.String(), "paymentId": id},
			})
		}
		return nil
	})
	return id, err
}

// checkRequested — выбранные начисления относятся к договору.
func checkRequested(ctx context.Context, q *sqlc.Queries, contractID uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		ch, err := q.GetChargeRow(ctx, id)
		if err != nil || ch.ContractID != contractID {
			return apperr.Validation(apperr.FieldError{Field: "chargeIds", Message: "Начисление не относится к договору"})
		}
	}
	return nil
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

// lockPayment — платёж и его договор (порядок блокировок: договор, затем платёж).
func (s *Service) lockPayment(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.Payment, sqlc.Contract, error) {
	p, err := tx.Q.GetPayment(ctx, id)
	if db.IsNoRows(err) {
		return p, sqlc.Contract{}, apperr.NotFound()
	}
	if err != nil {
		return p, sqlc.Contract{}, err
	}
	c, err := lockContract(ctx, tx, p.ContractID)
	if err != nil {
		return p, c, err
	}
	p, err = tx.Q.GetPaymentForUpdate(ctx, id)
	return p, c, err
}

// Cancel — отмена платежа (05 разд. 10.5): распределения отменяются, начисления пересчитываются; для выписки строка
// возвращается в «не опознан»; для зачёта — отменяется операция залога; для внесения залога — его внесение.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID, reason string) error {
	reason, err := needReason("reason", reason)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, c, err := s.lockPayment(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.cancelPayment(ctx, tx, p, c, reason); err != nil {
			return err
		}
		return s.billing.Rebalance(ctx, tx, c.ID)
	})
}

// cancelPayment — отмена платежа без перераспределения авансов (вызывающий выполняет Rebalance).
func (s *Service) cancelPayment(ctx context.Context, tx *db.Tx, p sqlc.Payment, c sqlc.Contract, reason string) error {
	if p.Status != "succeeded" {
		return apperr.Conflict(apperr.CodeConflictState, "Отменить можно только проведённый платёж")
	}
	if p.Source != SourceManual && p.Source != SourceBankImport && p.Source != SourceDepositOffset {
		return apperr.BusinessRule("Онлайн-платёж не отменяется: оформите возврат")
	}
	hasFiscal, err := tx.Q.PaymentHasActiveFiscal(ctx, &p.ID)
	if err != nil {
		return err
	}
	if hasFiscal {
		return apperr.Conflict(apperr.CodeFiscalReceiptExists, "По платежу сформирован чек. Оформите возврат")
	}
	// docs/open-questions.md, № 46: возврат переплаты не отменяется; перенос в залог сначала отменяется
	// как последняя операция по залогу (без каскада).
	ret, err := tx.Q.PaymentReturns(ctx, p.ID)
	if err != nil {
		return err
	}
	if ret.HasRefunds {
		return apperr.BusinessRule("По платежу оформлен возврат переплаты — отмена невозможна")
	}
	if ret.HasTransfers {
		return apperr.BusinessRule("По платежу оформлен перенос переплаты в залог. Сначала отмените его в разделе «Залог»")
	}
	now := s.clock.Now()
	charges, err := tx.Q.ReversePaymentAllocations(ctx, sqlc.ReversePaymentAllocationsParams{PaymentID: p.ID, Now: &now, Reason: &reason})
	if err != nil {
		return err
	}
	for _, chID := range charges {
		if _, err := tx.Q.RefreshChargePaid(ctx, chID); err != nil {
			return err
		}
	}
	if p.Purpose == PurposeDeposit {
		if err := s.cancelDepositReceipt(ctx, tx, c, p, reason); err != nil {
			return err
		}
	}
	if p.Source == SourceDepositOffset {
		op, err := tx.Q.DepositOffsetOfPayment(ctx, &p.ID)
		if err == nil {
			if err := tx.Q.CancelDepositTx(ctx, sqlc.CancelDepositTxParams{ID: op.ID, Now: &now, Reason: &reason}); err != nil {
				return err
			}
		} else if !db.IsNoRows(err) {
			return err
		}
	}
	// Строка выписки возвращается в «не опознан» и для платежа, связанного с ней как «тот же платёж»
	// (docs/open-questions.md, № 72).
	if p.BankStatementLineID != nil {
		if err := tx.Q.ClearPaymentBankLink(ctx, p.ID); err != nil {
			return err
		}
		if err := tx.Q.ReturnBankLineToUnmatched(ctx, *p.BankStatementLineID); err != nil {
			return err
		}
		line, err := tx.Q.GetBankLine(ctx, *p.BankStatementLineID)
		if err != nil {
			return err
		}
		disc, err := tx.Q.ReopenUnidentified(ctx, sqlc.ReopenUnidentifiedParams{LineID: p.BankStatementLineID, AmountKop: p.AmountKop, Now: now})
		if err != nil {
			return err
		}
		if err := s.notify.OwnerDiscrepancy(ctx, tx, disc, "unidentified", money.Kop(p.AmountKop).Format(),
			UnidentifiedDetails(line)); err != nil {
			return err
		}
	}
	if err := tx.Q.CancelPayment(ctx, sqlc.CancelPaymentParams{ID: p.ID, Now: &now, CancelledBy: actorID(ctx), Reason: &reason}); err != nil {
		return err
	}
	return s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "payment.cancel", EntityType: "payment", EntityID: p.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: отменён платёж %s, квитанция %s", c.Number, money.Kop(p.AmountKop).Format(),
			valStr(p.ReceiptNumber)),
		Before: map[string]any{"status": p.Status}, After: map[string]any{"status": "cancelled", "reason": reason},
	})
}

// cancelDepositReceipt — отмена внесения залога вместе с платежом; остаток не может стать отрицательным.
func (s *Service) cancelDepositReceipt(ctx context.Context, tx *db.Tx, c sqlc.Contract, p sqlc.Payment, reason string) error {
	op, err := tx.Q.DepositReceiptOfPayment(ctx, &p.ID)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	tot, err := tx.Q.DepositTotals(ctx, c.ID)
	if err != nil {
		return err
	}
	if tot.BalanceKop < op.AmountKop {
		return apperr.BusinessRule("Сумма больше остатка залога " + money.Kop(tot.BalanceKop).Format())
	}
	now := s.clock.Now()
	return tx.Q.CancelDepositTx(ctx, sqlc.CancelDepositTxParams{ID: op.ID, Now: &now, Reason: &reason})
}

func valStr(s *string) string {
	if s == nil {
		return "—"
	}
	return *s
}

// RefundInput — возврат переплаты по платежу (05 разд. 10.6).
type RefundInput struct {
	AmountKop  int64
	RefundedOn date.Date
	Method     string
	Reason     string
}

// Refund — возврат переплаты (аванса) платежа: сумма ≤ аванса. Открытое расхождение «переплата» по платежу
// закрывается с решением «возвращено».
func (s *Service) Refund(ctx context.Context, paymentID uuid.UUID, in RefundInput) (sqlc.Refund, error) {
	var r sqlc.Refund
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, c, err := s.lockPayment(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		r, err = s.refund(ctx, tx, p, c, in)
		if err != nil {
			return err
		}
		return s.settleOverpayment(ctx, tx, p.ID, "refunded", &r.Reason)
	})
	return r, err
}

func (s *Service) refund(ctx context.Context, tx *db.Tx, p sqlc.Payment, c sqlc.Contract, in RefundInput) (sqlc.Refund, error) {
	reason, err := needReason("reason", in.Reason)
	if err != nil {
		return sqlc.Refund{}, err
	}
	var errs []apperr.FieldError
	if in.AmountKop <= 0 {
		errs = append(errs, apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if in.Method != MethodBankTransfer && in.Method != MethodCash {
		errs = append(errs, apperr.FieldError{Field: "method", Message: "Способ — перевод на счёт или наличные"})
	}
	if in.RefundedOn.After(s.clock.Today()) {
		errs = append(errs, apperr.FieldError{Field: "refundedOn", Message: "Дата возврата не может быть позже сегодняшней"})
	}
	if len(errs) > 0 {
		return sqlc.Refund{}, apperr.Validation(errs...)
	}
	advance, err := s.advanceOf(ctx, tx, p)
	if err != nil {
		return sqlc.Refund{}, err
	}
	if in.AmountKop > advance {
		return sqlc.Refund{}, apperr.BusinessRule("Сумма больше аванса по платежу " + money.Kop(advance).Format())
	}
	r, err := tx.Q.InsertRefund(ctx, sqlc.InsertRefundParams{
		PaymentID: p.ID, AmountKop: in.AmountKop, RefundedOn: in.RefundedOn, Method: in.Method, Reason: reason,
		CreatedBy: actorID(ctx), Now: s.clock.Now(),
	})
	if err != nil {
		return r, err
	}
	// Чек возврата прихода, если по платежу есть чек (05 разд. 11).
	if err := s.fiscal.ForRefund(ctx, tx, r); err != nil {
		return r, err
	}
	return r, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "payment.refund", EntityType: "payment", EntityID: p.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: возврат переплаты %s по квитанции %s", c.Number, money.Kop(in.AmountKop).Format(),
			valStr(p.ReceiptNumber)),
		After: map[string]any{"refundId": r.ID, "amountKop": in.AmountKop, "refundedOn": in.RefundedOn.String(), "method": in.Method,
			"reason": reason},
	})
}

// advanceOf — аванс проведённого платежа «за начисления» (0 для остальных).
func (s *Service) advanceOf(ctx context.Context, tx *db.Tx, p sqlc.Payment) (int64, error) {
	if p.Status != "succeeded" || p.Purpose != PurposeCharges {
		return 0, nil
	}
	return tx.Q.PaymentAdvance(ctx, p.ID)
}

// MoveToDeposit — перенос переплаты (аванса) платежа в обеспечительный платёж (05 разд. 10.6): внесение залога
// from_advance = true. Открытое расхождение «переплата» закрывается с решением «перенесено в залог».
func (s *Service) MoveToDeposit(ctx context.Context, paymentID uuid.UUID, amount int64, on date.Date) (sqlc.DepositTransaction, error) {
	var op sqlc.DepositTransaction
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		p, c, err := s.lockPayment(ctx, tx, paymentID)
		if err != nil {
			return err
		}
		op, err = s.moveToDeposit(ctx, tx, p, c, amount, on)
		if err != nil {
			return err
		}
		return s.settleOverpayment(ctx, tx, p.ID, "moved_to_deposit", nil)
	})
	return op, err
}

func (s *Service) moveToDeposit(ctx context.Context, tx *db.Tx, p sqlc.Payment, c sqlc.Contract, amount int64, on date.Date,
) (sqlc.DepositTransaction, error) {
	if amount <= 0 {
		return sqlc.DepositTransaction{}, apperr.Validation(apperr.FieldError{Field: "amountKop", Message: "Сумма должна быть больше нуля"})
	}
	if on.After(s.clock.Today()) {
		return sqlc.DepositTransaction{}, apperr.Validation(apperr.FieldError{Field: "opDate", Message: "Дата операции не может быть позже сегодняшней"})
	}
	advance, err := s.advanceOf(ctx, tx, p)
	if err != nil {
		return sqlc.DepositTransaction{}, err
	}
	if amount > advance {
		return sqlc.DepositTransaction{}, apperr.BusinessRule("Сумма больше аванса по платежу " + money.Kop(advance).Format())
	}
	op, err := tx.Q.InsertDepositTx(ctx, sqlc.InsertDepositTxParams{
		ContractID: c.ID, Kind: "receipt", AmountKop: amount, OpDate: on, Reason: ptr("Перенос переплаты по квитанции " + valStr(p.ReceiptNumber)),
		PaymentID: &p.ID, FromAdvance: true, CreatedBy: actorID(ctx), Now: s.clock.Now(),
	})
	if err != nil {
		return op, err
	}
	return op, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "deposit.receipt", EntityType: "contract", EntityID: c.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: переплата %s по квитанции %s перенесена в залог", c.Number,
			money.Kop(amount).Format(), valStr(p.ReceiptNumber)),
		After: map[string]any{"depositTxId": op.ID, "amountKop": amount, "opDate": on.String(), "paymentId": p.ID, "fromAdvance": true},
	})
}

// settleOverpayment — открытое расхождение «переплата» после возврата или переноса в залог (05 разд. 10.6):
// аванс израсходован целиком — расхождение закрывается решением resolution; остался — уменьшается сумма,
// расхождение остаётся открытым.
func (s *Service) settleOverpayment(ctx context.Context, tx *db.Tx, paymentID uuid.UUID, resolution string, note *string) error {
	list, err := tx.Q.ListDiscrepancies(ctx, sqlc.ListDiscrepanciesParams{
		Status: ptr("open"), Kind: ptr("overpayment"), PaymentID: &paymentID, Lim: 1,
	})
	if err != nil || len(list) == 0 {
		return err
	}
	d := list[0]
	advance, err := tx.Q.PaymentAdvance(ctx, paymentID)
	if err != nil {
		return err
	}
	if advance > 0 {
		return tx.Q.SetDiscrepancyAmount(ctx, sqlc.SetDiscrepancyAmountParams{ID: d.ID, AmountKop: advance})
	}
	if _, err := tx.Q.ResolveDiscrepancy(ctx, sqlc.ResolveDiscrepancyParams{
		ID: d.ID, Resolution: resolution, Note: note, ResolvedBy: actorID(ctx), Now: s.clock.Now(),
	}); err != nil {
		return err
	}
	return s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "discrepancy.resolve", EntityType: "discrepancy", EntityID: d.ID.String(),
		Summary: fmt.Sprintf("Расхождение «%s» по договору № %s: %s", KindTitles[d.Kind], valStr(d.ContractNumber), ResolutionTitles[resolution]),
		Before:  map[string]any{"status": "open"}, After: map[string]any{"status": "resolved", "resolution": resolution},
	})
}

// tenantMail — адрес нанимателя и общие переменные писем по договору (10 Уведомления, раздел 1).
func tenantMail(ctx context.Context, q *sqlc.Queries, c sqlc.Contract) (string, map[string]string, error) {
	t, err := q.GetTenant(ctx, c.TenantID)
	if err != nil {
		return "", nil, err
	}
	p, err := q.GetProperty(ctx, c.PropertyID)
	if err != nil {
		return "", nil, err
	}
	to := ""
	if t.Email != nil && t.AnonymizedAt == nil {
		to = *t.Email
	}
	greeting := "Уважаемые коллеги"
	if t.Kind != "legal" {
		greeting = names.Greeting(t.FirstName, t.MiddleName)
	}
	return to, map[string]string{
		"tenant.greeting": greeting, "contract.number": c.Number, "property.address": p.Address, "property.code": p.Code,
	}, nil
}
