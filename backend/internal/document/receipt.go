package document

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
)

// MethodTitles — способ оплаты в квитанции (09 разд. 4.5).
var MethodTitles = map[string]string{
	"online":         "Онлайн-оплата (карта, СБП)",
	"bank_transfer":  "Перевод на счёт",
	"cash":           "Наличные",
	"deposit_offset": "Зачёт из обеспечительного платежа",
}

// PaymentReceiptTx — квитанция об оплате по проведённому платежу (09 разд. 5.6, задача document.payment_receipt).
// Повторный вызов возвращает уже сформированную квитанцию.
func (s *Service) PaymentReceiptTx(ctx context.Context, tx *db.Tx, paymentID uuid.UUID) (sqlc.Document, bool, error) {
	if doc, err := tx.Q.PaymentReceiptDocument(ctx, &paymentID); err == nil {
		return doc, false, nil
	} else if !db.IsNoRows(err) {
		return doc, false, err
	}
	p, err := tx.Q.GetPayment(ctx, paymentID)
	if db.IsNoRows(err) {
		return sqlc.Document{}, false, errNotFound
	}
	if err != nil {
		return sqlc.Document{}, false, err
	}
	if p.Status != "succeeded" || p.ReceiptNumber == nil || p.ReceivedOn == nil {
		return sqlc.Document{}, false, fmt.Errorf("платёж %s не проведён — квитанция не формируется", p.ID)
	}
	cc, err := s.loadContract(ctx, tx.Q, p.ContractID)
	if err != nil {
		return sqlc.Document{}, false, err
	}
	d, err := s.contractData(ctx, tx.Q, cc)
	if err != nil {
		return sqlc.Document{}, false, err
	}
	if err := s.receiptData(ctx, tx.Q, d, cc, p); err != nil {
		return sqlc.Document{}, false, err
	}
	title := "Квитанция № " + *p.ReceiptNumber + " от " + p.ReceivedOn.Format()
	doc, err := s.generate(ctx, tx, subject{kind: KindReceipt, number: p.ReceiptNumber, title: title, contractID: &cc.c.ID,
		paymentID: &p.ID, visibleToTenant: true}, d)
	return doc, true, err
}

// receiptData — переменные квитанции (09 разд. 4.5): плательщик, суммы, способ, чек и строки назначения.
func (s *Service) receiptData(ctx context.Context, q *sqlc.Queries, d Data, cc contractCtx, p sqlc.Payment) error {
	v := d.Vars
	v["payment.receipt_number"] = val(p.ReceiptNumber)
	v["payment.received_on"] = p.ReceivedOn.Format()
	v["payment.amount"] = money.Kop(p.AmountKop).FormatRub()
	v["payment.amount_words"] = money.ToWordsRu(money.Kop(p.AmountKop))
	v["payment.method_title"] = MethodTitles[p.Method]
	payer := v["tenant.full_name"]
	if val(p.PayerName) != "" {
		payer = val(p.PayerName)
	}
	v["payment.payer"] = payer
	fiscal, err := s.fiscalText(ctx, q, cc, p)
	if err != nil {
		return err
	}
	v["payment.fiscal_text"] = fiscal

	var lines []paymentLine
	if p.Purpose == "deposit" {
		lines = append(lines, paymentLine{title: "Обеспечительный платёж", amountKop: p.AmountKop})
	} else {
		allocs, err := q.PaymentAllocations(ctx, p.ID)
		if err != nil {
			return err
		}
		var allocated int64
		for _, a := range allocs {
			if a.ReversedAt != nil {
				continue
			}
			title, period := a.ChargeTitle, ""
			if a.PeriodMonth != nil {
				if i := strings.Index(title, ", "); i > 0 {
					title, period = title[:i], title[i+2:]
				}
			}
			lines = append(lines, paymentLine{title: title, period: period, amountKop: a.AmountKop})
			allocated += a.AmountKop
		}
		if rest := p.AmountKop - allocated; rest > 0 {
			lines = append(lines, paymentLine{title: "Аванс", amountKop: rest})
		}
	}
	d.Blocks["payment_lines"] = paymentLinesBlock(lines, p.AmountKop)
	return nil
}

// fiscalText — строка о кассовом чеке (09 разд. 4.5): реквизиты чека, ссылка на чек НПД, «чек не требуется»
// (режим none, внесение залога) или «формируется».
func (s *Service) fiscalText(ctx context.Context, q *sqlc.Queries, cc contractCtx, p sqlc.Payment) (string, error) {
	const notRequired = "Чек не требуется (п. 2 ст. 2 Федерального закона № 54-ФЗ)"
	if cc.p.FiscalMode == "none" || p.Purpose == "deposit" {
		return notRequired, nil
	}
	f, err := q.PaymentFiscalReceipt(ctx, &p.ID)
	if db.IsNoRows(err) {
		return "Кассовый чек формируется", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case f.Status == "done" && f.NpdReceiptUrl != nil:
		return "Чек НПД: " + *f.NpdReceiptUrl, nil
	case f.Status == "done":
		on := ""
		if f.ReceiptDatetime != nil {
			on = " от " + date.In(*f.ReceiptDatetime, s.clock.Location()).Format()
		}
		num := ""
		if f.FiscalReceiptNumber != nil {
			num = fmt.Sprintf(" № %d", *f.FiscalReceiptNumber)
		}
		text := "Кассовый чек" + num + on
		if f.FnNumber != nil {
			text += ", ФН " + *f.FnNumber
		}
		if f.FiscalDocumentNumber != nil {
			text += fmt.Sprintf(", ФД %d", *f.FiscalDocumentNumber)
		}
		if f.FiscalDocumentAttribute != nil {
			text += fmt.Sprintf(", ФП %d", *f.FiscalDocumentAttribute)
		}
		return text, nil
	}
	return "Кассовый чек формируется", nil
}
