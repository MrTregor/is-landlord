package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"landlord/backend/internal/api"
	"landlord/backend/internal/audit"
	"landlord/backend/internal/platform/apiconv"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

// Статусы строк выписки (04, bank_statement_lines).
const (
	LineMatched           = "matched"
	LinePossibleDuplicate = "possible_duplicate"
	LineUnmatched         = "unmatched"
	LineDuplicate         = "duplicate"
	LineLinkedManual      = "linked_manual"
	LineRefunded          = "refunded"
	LineIgnored           = "ignored"
)

// PostBankLine — платёж по строке выписки (05 разд. 15.1, шаг 6; 15.2 «Привязать к договору»): source = bank_import,
// method = bank_transfer, дата поступления строки, распределение, расхождения, чек; данные плательщика из строки.
// Строка получает статус status и способ сопоставления method. Договор уже заблокирован, строка — тоже.
func (s *Service) PostBankLine(ctx context.Context, tx *db.Tx, c sqlc.Contract, l sqlc.BankStatementLine, status, method string,
) (sqlc.Payment, error) {
	if c.Status == "draft" {
		return sqlc.Payment{}, apperr.Conflict(apperr.CodeConflictState, "Платежи регистрируются по договору, введённому в действие")
	}
	res, err := s.post(ctx, tx, c, PurposeCharges, MethodBankTransfer, SourceBankImport, l.AmountKop, l.ReceivedOn, nil, nil)
	if err != nil {
		return sqlc.Payment{}, err
	}
	p, err := s.attachLine(ctx, tx, res.payment.ID, l)
	if err != nil {
		return p, err
	}
	if err := tx.Q.SetBankLineMatched(ctx, sqlc.SetBankLineMatchedParams{
		ID: l.ID, Status: status, MatchMethod: &method, ContractID: &c.ID, PaymentID: &p.ID,
	}); err != nil {
		return p, err
	}
	if err := s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "payment.create", EntityType: "payment", EntityID: p.ID.String(),
		Summary: fmt.Sprintf("Договор № %s: платёж %s из выписки банка, квитанция %s", c.Number, money.Kop(p.AmountKop).Format(),
			valStr(p.ReceiptNumber)),
		After: map[string]any{
			"purpose": p.Purpose, "amountKop": p.AmountKop, "receivedOn": l.ReceivedOn.String(), "method": p.Method,
			"source": p.Source, "receiptNumber": p.ReceiptNumber, "bankLineId": l.ID, "matchMethod": method,
			"advanceKop": int64(res.advance),
		},
	}); err != nil {
		return p, err
	}
	// Письмо собственникам о платеже из выписки (10 Уведомления, раздел 3).
	return p, s.notifyOwners(ctx, tx, c, p)
}

// attachLine — связь платежа со строкой выписки и данные плательщика (05 разд. 15.1, шаги 5 и 6).
func (s *Service) attachLine(ctx context.Context, tx *db.Tx, paymentID uuid.UUID, l sqlc.BankStatementLine) (sqlc.Payment, error) {
	return tx.Q.SetPaymentBankData(ctx, sqlc.SetPaymentBankDataParams{
		ID: paymentID, LineID: &l.ID, PayerName: l.PayerName, PayerInn: l.PayerInn, PayerAccount: l.PayerAccount,
		BankPurpose: l.Purpose,
	})
}

// LinkSamePayment — «Это тот же платёж» (05 разд. 15.1, шаг 5): строка «возможный дубль» связывается с уже
// проведённым платежом договора, в платёж записываются данные плательщика. Договор и строка заблокированы.
func (s *Service) LinkSamePayment(ctx context.Context, tx *db.Tx, c sqlc.Contract, l sqlc.BankStatementLine, paymentID uuid.UUID,
) (sqlc.Payment, error) {
	cands, err := DuplicateCandidates(ctx, tx.Q, l)
	if err != nil {
		return sqlc.Payment{}, err
	}
	found := false
	for _, p := range cands {
		found = found || p.ID == paymentID
	}
	if !found {
		return sqlc.Payment{}, apperr.Validation(apperr.FieldError{Field: "paymentId", Message: "Выберите платёж из найденных"})
	}
	if _, err := tx.Q.GetPaymentForUpdate(ctx, paymentID); err != nil {
		return sqlc.Payment{}, err
	}
	p, err := s.attachLine(ctx, tx, paymentID, l)
	if err != nil {
		return p, err
	}
	if err := tx.Q.SetBankLineMatched(ctx, sqlc.SetBankLineMatchedParams{
		ID: l.ID, Status: LineMatched, MatchMethod: l.MatchMethod, ContractID: &c.ID, PaymentID: &p.ID,
	}); err != nil {
		return p, err
	}
	return p, nil
}

// DuplicateCandidates — проведённые ручные и онлайн-платежи договора строки с той же суммой и датой поступления
// ±3 дня без привязки к выписке (05 разд. 15.1, шаг 5).
func DuplicateCandidates(ctx context.Context, q *sqlc.Queries, l sqlc.BankStatementLine) ([]sqlc.Payment, error) {
	if l.ContractID == nil {
		return []sqlc.Payment{}, nil
	}
	return q.DuplicateCandidates(ctx, sqlc.DuplicateCandidatesParams{
		ContractID: *l.ContractID, AmountKop: l.AmountKop, DateFrom: l.ReceivedOn.AddDays(-3), DateTo: l.ReceivedOn.AddDays(3),
	})
}

// BankLineDTO — строка выписки для API; для «возможного дубля» — платежи-кандидаты.
func BankLineDTO(ctx context.Context, q *sqlc.Queries, r sqlc.BankLineRowRow) (api.BankLineDTO, error) {
	d := api.BankLineDTO{
		Id: r.ID, StatementId: r.StatementID, LineNo: int(r.LineNo), DocNumber: r.DocNumber, DocDate: apiconv.FromDatePtr(r.DocDate),
		ReceivedOn: apiconv.FromDate(r.ReceivedOn), AmountKop: r.AmountKop, PayerName: r.PayerName, PayerInn: r.PayerInn,
		PayerAccount: r.PayerAccount, Purpose: r.Purpose, Status: api.BankLineStatus(r.Status), PaymentId: r.PaymentID,
		Note: r.Note, CandidatePayments: []api.CandidatePayment{},
	}
	if r.MatchMethod != nil {
		d.MatchMethod = apiconv.Ptr(api.MatchMethod(*r.MatchMethod))
	}
	if r.ContractID != nil && r.ContractNumber != nil {
		d.Contract = &api.ContractRef{Id: *r.ContractID, Number: *r.ContractNumber}
	}
	if r.Status != LinePossibleDuplicate {
		return d, nil
	}
	cands, err := DuplicateCandidates(ctx, q, sqlc.BankStatementLine{ContractID: r.ContractID, AmountKop: r.AmountKop, ReceivedOn: r.ReceivedOn})
	if err != nil {
		return d, err
	}
	for _, p := range cands {
		d.CandidatePayments = append(d.CandidatePayments, api.CandidatePayment{
			Id: p.ID, ReceiptNumber: p.ReceiptNumber, ReceivedOn: apiconv.FromDatePtr(p.ReceivedOn), AmountKop: p.AmountKop,
			Source: api.PaymentSource(p.Source),
		})
	}
	return d, nil
}

// bankLineOf — строка выписки по id для ответа API (nil — строки нет).
func bankLineOf(ctx context.Context, q *sqlc.Queries, id *uuid.UUID) (*api.BankLineDTO, error) {
	if id == nil {
		return nil, nil
	}
	r, err := q.BankLineRow(ctx, *id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d, err := BankLineDTO(ctx, q, r)
	return &d, err
}

// resolveUnidentified — разбор неопознанного платежа (05 разд. 15.2): «Привязать к договору» — платёж по строке
// (как шаг 6), строка linked_manual, решение linked; «Возвращён плательщику» — строка refunded; «Не относится к
// аренде» — строка ignored; комментарий необязателен (14 Экраны собственника, раздел 7). Договор (для привязки)
// и расхождение уже заблокированы.
func (s *Service) resolveUnidentified(ctx context.Context, tx *db.Tx, d sqlc.Discrepancy, in ResolveInput, note *string) (string, error) {
	l, err := tx.Q.GetBankLineForUpdate(ctx, *d.BankStatementLineID)
	if err != nil {
		return "", err
	}
	if l.Status != LineUnmatched {
		return "", apperr.Conflict(apperr.CodeConflictState, "Строка выписки уже разобрана")
	}
	switch in.Action {
	case "link":
		c, err := lockContract(ctx, tx, *in.ContractID)
		if err != nil {
			return "", apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Договор не найден"})
		}
		p, err := s.PostBankLine(ctx, tx, c, l, LineLinkedManual, "manual")
		if err != nil {
			return "", err
		}
		if _, err := tx.Q.LinkUnidentified(ctx, sqlc.LinkUnidentifiedParams{
			ID: d.ID, Note: note, ResolvedBy: actorID(ctx), Now: s.clock.Now(), ContractID: &c.ID, PaymentID: &p.ID,
		}); err != nil {
			return "", err
		}
		return "linked", nil
	}
	resolution, status := "refunded", LineRefunded
	if in.Action == "ignore" {
		resolution, status = "ignored", LineIgnored
	}
	if err := tx.Q.SetBankLineClosed(ctx, sqlc.SetBankLineClosedParams{ID: l.ID, Status: status, Note: note}); err != nil {
		return "", err
	}
	_, err = tx.Q.ResolveDiscrepancy(ctx, sqlc.ResolveDiscrepancyParams{
		ID: d.ID, Resolution: resolution, Note: note, ResolvedBy: actorID(ctx), Now: s.clock.Now(),
	})
	return resolution, err
}
