package payment

import (
	"context"
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

// KindTitles — виды расхождений (04, справочник значений).
var KindTitles = map[string]string{
	"underpayment": "Недоплата", "overpayment": "Переплата", "unidentified": "Неопознанный платёж",
}

// ResolutionTitles — решения по расхождениям.
var ResolutionTitles = map[string]string{
	"linked": "привязан к договору", "refunded": "возвращено", "kept_as_advance": "оставлено авансом",
	"moved_to_deposit": "перенесено в залог", "acknowledged": "принято к сведению", "ignored": "не относится к аренде",
	"auto_closed": "закрыто автоматически",
}

// DiscrepancyFilter — фильтры списка расхождений.
type DiscrepancyFilter struct {
	Status, Kind  *string
	ContractID    *uuid.UUID
	Limit, Offset int32
}

// Discrepancies — список расхождений.
func (s *Service) Discrepancies(ctx context.Context, f DiscrepancyFilter) ([]sqlc.ListDiscrepanciesRow, int64, error) {
	q := s.db.Queries()
	rows, err := q.ListDiscrepancies(ctx, sqlc.ListDiscrepanciesParams{
		Status: f.Status, Kind: f.Kind, ContractID: f.ContractID, Lim: f.Limit, Off: f.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	n, err := q.CountDiscrepancies(ctx, sqlc.CountDiscrepanciesParams{Status: f.Status, Kind: f.Kind, ContractID: f.ContractID})
	return rows, n, err
}

// ResolveInput — разбор расхождения (05 разд. 15.2).
type ResolveInput struct {
	Action     string
	ContractID *uuid.UUID
	AmountKop  *int64
	RefundedOn *date.Date
	Method     *string
	Note       *string
}

// Действия разбора и допустимые виды расхождений.
var actionKinds = map[string]string{
	"keep_advance": "overpayment", "refund": "overpayment", "move_to_deposit": "overpayment",
	"acknowledge": "underpayment",
	"link":        "unidentified", "returned_to_payer": "unidentified", "ignore": "unidentified",
}

// Resolve — разбор расхождения собственником (05 разд. 10.6, 15.2).
func (s *Service) Resolve(ctx context.Context, id uuid.UUID, in ResolveInput) (uuid.UUID, error) {
	note := trimPtr(in.Note)
	err := s.db.WithTx(ctx, func(tx *db.Tx) error {
		// Порядок блокировок, как у платежей: договор, затем расхождение.
		row, err := tx.Q.GetDiscrepancyRow(ctx, id)
		if db.IsNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if row.ContractID != nil {
			if _, err := lockContract(ctx, tx, *row.ContractID); err != nil {
				return err
			}
		}
		if in.Action == "link" && row.Kind == "unidentified" {
			if in.ContractID == nil {
				return apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Выберите договор"})
			}
			if _, err := lockContract(ctx, tx, *in.ContractID); err != nil {
				return apperr.Validation(apperr.FieldError{Field: "contractId", Message: "Договор не найден"})
			}
		}
		d, err := tx.Q.GetDiscrepancyForUpdate(ctx, id)
		if err != nil {
			return err
		}
		kind, ok := actionKinds[in.Action]
		if !ok || kind != d.Kind {
			return apperr.Validation(apperr.FieldError{Field: "action", Message: "Действие не подходит для этого расхождения"})
		}
		if d.Status != "open" {
			return apperr.Conflict(apperr.CodeConflictState, "Расхождение уже разобрано")
		}
		var resolution string
		switch in.Action {
		case "link", "returned_to_payer", "ignore":
			if resolution, err = s.resolveUnidentified(ctx, tx, d, in, note); err != nil {
				return err
			}
			return s.audit.Record(ctx, tx.Q, audit.Entry{
				Action: "discrepancy.resolve", EntityType: "discrepancy", EntityID: d.ID.String(),
				Summary: fmt.Sprintf("Расхождение «%s» на %s: %s", KindTitles[d.Kind], money.Kop(d.AmountKop).Format(),
					ResolutionTitles[resolution]),
				Before: map[string]any{"status": d.Status},
				After:  map[string]any{"status": "resolved", "resolution": resolution, "note": note, "contractId": in.ContractID},
			})
		case "keep_advance":
			resolution = "kept_as_advance"
		case "acknowledge":
			if note == nil {
				return apperr.Validation(apperr.FieldError{Field: "note", Message: "Укажите комментарий"})
			}
			resolution = "acknowledged"
		case "refund", "move_to_deposit":
			// Закрывается, только если аванс израсходован целиком; иначе уменьшается сумма (05 разд. 10.6).
			if resolution, err = s.overpaymentAction(ctx, tx, d, in, note); err != nil {
				return err
			}
			return s.settleOverpayment(ctx, tx, *d.PaymentID, resolution, note)
		}
		if _, err := tx.Q.ResolveDiscrepancy(ctx, sqlc.ResolveDiscrepancyParams{
			ID: d.ID, Resolution: resolution, Note: note, ResolvedBy: actorID(ctx), Now: s.clock.Now(),
		}); err != nil {
			return err
		}
		number := "—"
		if d.ContractID != nil {
			c, err := tx.Q.GetContract(ctx, *d.ContractID)
			if err != nil {
				return err
			}
			number = c.Number
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "discrepancy.resolve", EntityType: "discrepancy", EntityID: d.ID.String(),
			Summary: fmt.Sprintf("Расхождение «%s» на %s по договору № %s: %s", KindTitles[d.Kind], money.Kop(d.AmountKop).Format(),
				number, ResolutionTitles[resolution]),
			Before: map[string]any{"status": d.Status},
			After:  map[string]any{"status": "resolved", "resolution": resolution, "note": note},
		})
	})
	return id, err
}

// overpaymentAction — «Вернуть» или «Перенести в залог» по расхождению «переплата» (05 разд. 10.6).
func (s *Service) overpaymentAction(ctx context.Context, tx *db.Tx, d sqlc.Discrepancy, in ResolveInput, note *string) (string, error) {
	if in.AmountKop == nil {
		return "", apperr.Validation(apperr.FieldError{Field: "amountKop", Message: "Укажите сумму"})
	}
	p, err := tx.Q.GetPayment(ctx, *d.PaymentID)
	if err != nil {
		return "", err
	}
	c, err := lockContract(ctx, tx, p.ContractID)
	if err != nil {
		return "", err
	}
	if p, err = tx.Q.GetPaymentForUpdate(ctx, p.ID); err != nil {
		return "", err
	}
	if in.Action == "move_to_deposit" {
		on := s.clock.Today()
		if _, err := s.moveToDeposit(ctx, tx, p, c, *in.AmountKop, on); err != nil {
			return "", err
		}
		return "moved_to_deposit", nil
	}
	var errs []apperr.FieldError
	if in.RefundedOn == nil {
		errs = append(errs, apperr.FieldError{Field: "refundedOn", Message: "Укажите дату возврата"})
	}
	if in.Method == nil {
		errs = append(errs, apperr.FieldError{Field: "method", Message: "Укажите способ возврата"})
	}
	if len(errs) > 0 {
		return "", apperr.Validation(errs...)
	}
	reason := "Возврат переплаты"
	if note != nil && strings.TrimSpace(*note) != "" {
		reason = *note
	}
	if _, err := s.refund(ctx, tx, p, c, RefundInput{AmountKop: *in.AmountKop, RefundedOn: *in.RefundedOn, Method: *in.Method,
		Reason: reason}); err != nil {
		return "", err
	}
	return "refunded", nil
}
