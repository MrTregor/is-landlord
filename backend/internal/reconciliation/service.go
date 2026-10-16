package reconciliation

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/file"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/reqctx"
)

// Service — загрузка выписок и разбор строк.
type Service struct {
	db       *db.DB
	audit    *audit.Service
	clock    clock.Clock
	files    *file.Service
	payments *payment.Service
}

// NewService создаёт сервис сверки.
func NewService(database *db.DB, au *audit.Service, clk clock.Clock, fs *file.Service, ps *payment.Service) *Service {
	return &Service{db: database, audit: au, clock: clk, files: fs, payments: ps}
}

func actorID(ctx context.Context) *uuid.UUID {
	if a := reqctx.ActorFrom(ctx); a != nil {
		id := a.UserID
		return &id
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Upload — загрузка и синхронная обработка выписки (06 разд. 12, 05 разд. 15.1). Ошибки разбора — 422 со списком
// «Строка N: …», файл не сохраняется.
func (s *Service) Upload(ctx context.Context, name string, data []byte) (uuid.UUID, error) {
	org, err := s.db.Queries().GetOrganizationSettings(ctx)
	if err != nil && !db.IsNoRows(err) {
		return uuid.Nil, err
	}
	own := ""
	if org.BankAccount != nil {
		own = *org.BankAccount
	}
	st, err := Parse(data, own)
	var pe *ParseError
	if errors.As(err, &pe) {
		fields := make([]apperr.FieldError, 0, len(pe.Messages))
		for _, m := range pe.Messages {
			fields = append(fields, apperr.FieldError{Field: "file", Message: m})
		}
		return uuid.Nil, apperr.Validation(fields...)
	}
	if err != nil {
		return uuid.Nil, err
	}
	var (
		id uuid.UUID
		f  sqlc.File
	)
	err = s.db.WithTx(ctx, func(tx *db.Tx) error {
		if err := tx.Q.LockStatements(ctx); err != nil {
			return err
		}
		if f, err = s.files.UploadTx(ctx, tx.Q, name, file.CategoryBankStatement, bytes.NewReader(data)); err != nil {
			return err
		}
		id, err = s.process(ctx, tx, f, st)
		return err
	})
	if err != nil && f.ID != uuid.Nil {
		s.files.Discard(f)
	}
	return id, err
}

// stats — итоги обработки выписки.
type stats struct{ matched, unmatched, duplicate int32 }

// process — сохранение выписки и обработка входящих строк (05 разд. 15.1).
func (s *Service) process(ctx context.Context, tx *db.Tx, f sqlc.File, st Statement) (uuid.UUID, error) {
	bs, err := tx.Q.InsertBankStatement(ctx, sqlc.InsertBankStatementParams{
		FileID: f.ID, Format: st.Format, AccountNumber: nonEmpty(st.Account), DateFrom: st.DateFrom, DateTo: st.DateTo,
		LinesTotal: int32(st.Total), LinesIncoming: int32(len(st.Incoming)), UploadedBy: actorID(ctx), Now: s.clock.Now(), //nolint:gosec // строк в файле ≤ 2 МБ
	})
	if err != nil {
		return uuid.Nil, err
	}
	rows, err := tx.Q.MatchContracts(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	matcher := NewMatcher(contractsOf(rows))
	var sum stats
	for _, l := range st.Incoming {
		if err := s.processLine(ctx, tx, bs.ID, l, matcher, &sum); err != nil {
			return uuid.Nil, err
		}
	}
	if err := tx.Q.SetBankStatementStats(ctx, sqlc.SetBankStatementStatsParams{
		ID: bs.ID, Matched: sum.matched, Unmatched: sum.unmatched, Duplicates: sum.duplicate,
	}); err != nil {
		return uuid.Nil, err
	}
	return bs.ID, s.audit.Record(ctx, tx.Q, audit.Entry{
		Action: "statement.upload", EntityType: "bank_statement", EntityID: bs.ID.String(),
		Summary: fmt.Sprintf("Загружена выписка банка (%s): строк %d, входящих %d, сопоставлено %d, не опознано %d, дублей %d",
			formatTitle(st.Format), st.Total, len(st.Incoming), sum.matched, sum.unmatched, sum.duplicate),
		After: map[string]any{
			"fileId": f.ID, "format": st.Format, "linesTotal": st.Total, "linesIncoming": len(st.Incoming),
			"linesMatched": sum.matched, "linesUnmatched": sum.unmatched, "linesDuplicate": sum.duplicate,
		},
	})
}

func formatTitle(f string) string {
	if f == Format1C {
		return "формат 1С"
	}
	return "CSV"
}

func contractsOf(rows []sqlc.MatchContractsRow) []Contract {
	out := make([]Contract, 0, len(rows))
	for _, r := range rows {
		c := Contract{ID: r.ID, Number: r.Number, Status: r.Status, StartDate: r.StartDate, TenantID: r.TenantID,
			TenantKind: r.TenantKind, TenantINN: deref(r.TenantInn), LastName: deref(r.LastName), FirstName: deref(r.FirstName),
			MiddleName: deref(r.MiddleName)}
		if r.ActualEndDate != nil {
			c.ActualEnd = *r.ActualEndDate
		}
		out = append(out, c)
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// processLine — одна входящая строка (05 разд. 15.1, шаги 1–7). Счётчик «дублей» — дубли и возможные дубли
// (docs/open-questions.md, № 70).
func (s *Service) processLine(ctx context.Context, tx *db.Tx, statementID uuid.UUID, l Line, m *Matcher, sum *stats) error {
	fp := Fingerprint(l)
	params := sqlc.InsertBankLineParams{
		StatementID: statementID, LineNo: int32(l.No), DocNumber: nonEmpty(l.DocNumber), DocDate: l.DocDate, //nolint:gosec // номер строки файла
		ReceivedOn: l.ReceivedOn, AmountKop: l.AmountKop, PayerName: nonEmpty(l.PayerName), PayerInn: nonEmpty(l.PayerINN),
		PayerAccount: nonEmpty(l.PayerAccount), Purpose: nonEmpty(l.Purpose), Fingerprint: fp,
	}
	// 1. Отпечаток уже есть у другой строки — дубль, дальше не обрабатывается.
	dup, err := tx.Q.FingerprintExists(ctx, fp)
	if err != nil {
		return err
	}
	if dup {
		params.Status = payment.LineDuplicate
		sum.duplicate++
		_, err := tx.Q.InsertBankLine(ctx, params)
		return err
	}
	c, method, ok := m.Match(l)
	if !ok {
		// 7. Договор не найден — «не опознан» и расхождение.
		params.Status = payment.LineUnmatched
		row, err := tx.Q.InsertBankLine(ctx, params)
		if err != nil {
			return err
		}
		sum.unmatched++
		return tx.Q.ReopenUnidentified(ctx, sqlc.ReopenUnidentifiedParams{LineID: &row.ID, AmountKop: row.AmountKop, Now: s.clock.Now()})
	}
	contract, err := tx.Q.GetContractForUpdate(ctx, c.ID)
	if err != nil {
		return err
	}
	params.MatchMethod, params.ContractID = &method, &c.ID
	// 5. Возможный дубль ручного или онлайн-платежа — платёж не создаётся, решает собственник.
	cands, err := tx.Q.DuplicateCandidates(ctx, sqlc.DuplicateCandidatesParams{
		ContractID: c.ID, AmountKop: l.AmountKop, DateFrom: l.ReceivedOn.AddDays(-3), DateTo: l.ReceivedOn.AddDays(3),
	})
	if err != nil {
		return err
	}
	if len(cands) > 0 {
		params.Status = payment.LinePossibleDuplicate
		sum.duplicate++
		_, err := tx.Q.InsertBankLine(ctx, params)
		return err
	}
	// 6. Платёж из выписки.
	params.Status = payment.LineMatched
	row, err := tx.Q.InsertBankLine(ctx, params)
	if err != nil {
		return err
	}
	sum.matched++
	_, err = s.payments.PostBankLine(ctx, tx, contract, row, payment.LineMatched, method)
	return err
}

// lockDuplicateLine — строка «возможный дубль» и её договор под блокировкой (порядок: договор, затем строка).
func (s *Service) lockDuplicateLine(ctx context.Context, tx *db.Tx, id uuid.UUID) (sqlc.BankStatementLine, sqlc.Contract, error) {
	l, err := tx.Q.GetBankLine(ctx, id)
	if db.IsNoRows(err) {
		return l, sqlc.Contract{}, apperr.NotFound()
	}
	if err != nil {
		return l, sqlc.Contract{}, err
	}
	if l.Status != payment.LinePossibleDuplicate || l.ContractID == nil {
		return l, sqlc.Contract{}, apperr.Conflict(apperr.CodeConflictState, "Строка выписки уже разобрана")
	}
	c, err := tx.Q.GetContractForUpdate(ctx, *l.ContractID)
	if err != nil {
		return l, c, err
	}
	l, err = tx.Q.GetBankLineForUpdate(ctx, id)
	if err != nil {
		return l, c, err
	}
	if l.Status != payment.LinePossibleDuplicate {
		return l, c, apperr.Conflict(apperr.CodeConflictState, "Строка выписки уже разобрана")
	}
	return l, c, nil
}

// SamePayment — «Это тот же платёж» (05 разд. 15.1, шаг 5).
func (s *Service) SamePayment(ctx context.Context, lineID, paymentID uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		l, c, err := s.lockDuplicateLine(ctx, tx, lineID)
		if err != nil {
			return err
		}
		p, err := s.payments.LinkSamePayment(ctx, tx, c, l, paymentID)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "discrepancy.resolve", EntityType: "bank_statement_line", EntityID: l.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: строка выписки — тот же платёж, квитанция %s", c.Number, deref(p.ReceiptNumber)),
			Before:  map[string]any{"status": l.Status},
			After:   map[string]any{"status": payment.LineMatched, "paymentId": p.ID},
		})
	})
}

// DifferentPayment — «Это другой платёж»: платёж по строке, как шаг 6 (05 разд. 15.1).
func (s *Service) DifferentPayment(ctx context.Context, lineID uuid.UUID) error {
	return s.db.WithTx(ctx, func(tx *db.Tx) error {
		l, c, err := s.lockDuplicateLine(ctx, tx, lineID)
		if err != nil {
			return err
		}
		p, err := s.payments.PostBankLine(ctx, tx, c, l, payment.LineMatched, deref(l.MatchMethod))
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx.Q, audit.Entry{
			Action: "discrepancy.resolve", EntityType: "bank_statement_line", EntityID: l.ID.String(),
			Summary: fmt.Sprintf("Договор № %s: строка выписки — другой платёж, квитанция %s", c.Number, deref(p.ReceiptNumber)),
			Before:  map[string]any{"status": l.Status},
			After:   map[string]any{"status": payment.LineMatched, "paymentId": p.ID},
		})
	})
}
