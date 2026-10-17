// Package report — отчёты (FR-11) и сводка (FR-14): доходы и расходы по объектам, заполняемость и простои,
// задолженность и просрочки, налоговая база, доходы для книги учёта; JSON и XLSX. Определения дохода — 05 Бизнес-
// правила и расчёты, раздел 12; оформление — 11 Отчёты.
package report

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/billing/calc"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/money"
	"landlord/backend/internal/platform/names"
)

// Op — доходная операция периода (05 разд. 12.2): часть платежа по виду начисления, аванс, удержание из залога,
// возврат переплаты или перенос переплаты в залог (последние два — со знаком минус).
type Op struct {
	Date           date.Date
	Kind           string // calc.Income*
	Amount         money.Kop
	Document       string // номер квитанции или «Удержание из залога от …»
	ChargeTitle    string // для частей платежа
	ContractID     uuid.UUID
	ContractNumber string
	PropertyID     uuid.UUID
	PropertyCode   string
	TenantID       uuid.UUID
	TenantName     string
	TenantShort    string
	Legal          bool
}

type party struct {
	kind                     string
	last, first, middle, org *string
}

func (p party) display() string { return names.Display(p.kind, p.last, p.first, p.middle, p.org) }

func (p party) short() string {
	if p.kind == "legal" && p.org != nil {
		return *p.org
	}
	return names.Short(p.last, p.first, p.middle)
}

// chargeKinds — вид начисления → вид дохода.
var chargeKinds = map[string]string{
	"rent": calc.IncomeRent, "utilities": calc.IncomeUtilities, "penalty": calc.IncomePenalty, "damage": calc.IncomeDamage,
	"other": calc.IncomeOther,
}

// LoadOps — доходные операции периода по всем объектам или по одному.
func LoadOps(ctx context.Context, q *sqlc.Queries, from, to date.Date, propertyID *uuid.UUID) ([]Op, error) {
	var ops []Op
	lines, err := q.IncomePaymentLines(ctx, sqlc.IncomePaymentLinesParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return nil, err
	}
	// Платёж — части по распределениям; нераспределённый остаток — «Аванс».
	var cur *sqlc.IncomePaymentLinesRow
	var allocated money.Kop
	flush := func() {
		if cur == nil {
			return
		}
		if adv := money.Kop(cur.PaymentKop) - allocated; adv > 0 {
			ops = append(ops, paymentOp(*cur, calc.IncomeAdvance, adv, ""))
		}
	}
	for i := range lines {
		l := lines[i]
		if cur == nil || cur.PaymentID != l.PaymentID {
			flush()
			cur, allocated = &lines[i], 0
		}
		if l.ChargeKind == nil || l.AllocatedKop == 0 {
			continue
		}
		allocated += money.Kop(l.AllocatedKop)
		ops = append(ops, paymentOp(l, chargeKinds[*l.ChargeKind], money.Kop(l.AllocatedKop), deref(l.ChargeTitle)))
	}
	flush()

	deps, err := q.IncomeDepositOps(ctx, sqlc.IncomeDepositOpsParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		var kind, doc string
		amount := money.Kop(d.AmountKop)
		switch {
		case d.Kind == "withholding":
			kind, doc = calc.IncomeWithholding, "Удержание из залога от "+d.OpDate.Format()
		case d.Kind == "receipt" && d.FromAdvance:
			kind, doc, amount = calc.IncomeToDeposit, "Перенос переплаты в залог от "+d.OpDate.Format(), -amount
		default:
			continue // внесения и возвраты залога в доход не входят
		}
		p := party{d.TenantKind, d.LastName, d.FirstName, d.MiddleName, d.OrgName}
		ops = append(ops, Op{Date: d.OpDate, Kind: kind, Amount: amount, Document: doc, ContractID: d.ContractID,
			ContractNumber: d.ContractNumber, PropertyID: d.PropertyID, PropertyCode: d.PropertyCode, TenantID: d.TenantID,
			TenantName: p.display(), TenantShort: p.short(), Legal: d.TenantKind == "legal"})
	}

	refunds, err := q.IncomeRefunds(ctx, sqlc.IncomeRefundsParams{DateFrom: from, DateTo: to, PropertyID: propertyID})
	if err != nil {
		return nil, err
	}
	for _, r := range refunds {
		p := party{r.TenantKind, r.LastName, r.FirstName, r.MiddleName, r.OrgName}
		ops = append(ops, Op{Date: r.RefundedOn, Kind: calc.IncomeRefund, Amount: -money.Kop(r.AmountKop),
			Document: "Возврат переплаты от " + r.RefundedOn.Format(), ContractID: r.ContractID, ContractNumber: r.ContractNumber,
			PropertyID: r.PropertyID, PropertyCode: r.PropertyCode, TenantID: r.TenantID, TenantName: p.display(),
			TenantShort: p.short(), Legal: r.TenantKind == "legal"})
	}
	sortOps(ops)
	return ops, nil
}

func paymentOp(l sqlc.IncomePaymentLinesRow, kind string, amount money.Kop, title string) Op {
	p := party{l.TenantKind, l.LastName, l.FirstName, l.MiddleName, l.OrgName}
	return Op{Date: l.ReceivedOn, Kind: kind, Amount: amount, Document: deref(l.ReceiptNumber), ChargeTitle: title,
		ContractID: l.ContractID, ContractNumber: l.ContractNumber, PropertyID: l.PropertyID, PropertyCode: l.PropertyCode,
		TenantID: l.TenantID, TenantName: p.display(), TenantShort: p.short(), Legal: l.TenantKind == "legal"}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sortOps — по дате, затем по документу (11 разд. 5, лист «Операции»); порядок частей платежа сохраняется.
func sortOps(ops []Op) {
	for i := 1; i < len(ops); i++ {
		for j := i; j > 0 && less(ops[j], ops[j-1]); j-- {
			ops[j], ops[j-1] = ops[j-1], ops[j]
		}
	}
}

func less(a, b Op) bool {
	if !a.Date.Equal(b.Date) {
		return a.Date.Before(b.Date)
	}
	return a.Document < b.Document
}

// Kinds — доходные операции по видам (IncomeByKinds API).
type Kinds struct {
	Rent, Utilities, Penalty, Damage, Other, Advance, Withheld, Refunds, ToDeposit money.Kop
	FromIndividuals, FromLegal                                                     money.Kop
}

// Total — поступления, всего (возвраты и переносы — со знаком минус).
func (k Kinds) Total() money.Kop {
	return k.Rent + k.Utilities + k.Penalty + k.Damage + k.Other + k.Advance + k.Withheld + k.Refunds + k.ToDeposit
}

// Add — добавить операцию.
func (k *Kinds) Add(op Op) {
	switch op.Kind {
	case calc.IncomeRent:
		k.Rent += op.Amount
	case calc.IncomeUtilities:
		k.Utilities += op.Amount
	case calc.IncomePenalty:
		k.Penalty += op.Amount
	case calc.IncomeDamage:
		k.Damage += op.Amount
	case calc.IncomeOther:
		k.Other += op.Amount
	case calc.IncomeAdvance:
		k.Advance += op.Amount
	case calc.IncomeWithholding:
		k.Withheld += op.Amount
	case calc.IncomeRefund:
		k.Refunds += op.Amount
	case calc.IncomeToDeposit:
		k.ToDeposit += op.Amount
	}
	if op.Legal {
		k.FromLegal += op.Amount
	} else {
		k.FromIndividuals += op.Amount
	}
}

// Plus — сумма по видам.
func (k Kinds) Plus(o Kinds) Kinds {
	return Kinds{Rent: k.Rent + o.Rent, Utilities: k.Utilities + o.Utilities, Penalty: k.Penalty + o.Penalty,
		Damage: k.Damage + o.Damage, Other: k.Other + o.Other, Advance: k.Advance + o.Advance, Withheld: k.Withheld + o.Withheld,
		Refunds: k.Refunds + o.Refunds, ToDeposit: k.ToDeposit + o.ToDeposit, FromIndividuals: k.FromIndividuals + o.FromIndividuals,
		FromLegal: k.FromLegal + o.FromLegal}
}

// Income — для calc.Tax.
func (k Kinds) Income() calc.Income {
	return calc.Income{Total: k.Total(), FromIndividuals: k.FromIndividuals, FromLegal: k.FromLegal, ByKind: map[string]money.Kop{
		calc.IncomeRent: k.Rent, calc.IncomeUtilities: k.Utilities, calc.IncomePenalty: k.Penalty, calc.IncomeDamage: k.Damage,
		calc.IncomeOther: k.Other, calc.IncomeAdvance: k.Advance, calc.IncomeWithholding: k.Withheld, calc.IncomeRefund: k.Refunds,
		calc.IncomeToDeposit: k.ToDeposit,
	}}
}

// KindsOf — операции, отобранные условием, по видам.
func KindsOf(ops []Op, keep func(Op) bool) Kinds {
	var k Kinds
	for _, op := range ops {
		if keep == nil || keep(op) {
			k.Add(op)
		}
	}
	return k
}

// KindTitles — названия видов доходных операций (05 разд. 12.1).
var KindTitles = map[string]string{
	calc.IncomeRent: "Плата за наём", calc.IncomeUtilities: "Компенсация ЖКУ", calc.IncomePenalty: "Пени",
	calc.IncomeDamage: "Возмещение ущерба", calc.IncomeOther: "Прочее", calc.IncomeAdvance: "Аванс",
	calc.IncomeWithholding: "Удержание из залога", calc.IncomeRefund: "Возврат переплаты",
	calc.IncomeToDeposit: "Перенос переплаты в залог",
}

// Description — содержание операции для книги учёта: «Плата за наём за октябрь 2026 по договору № 2026-007,
// Кузнецов П. И.» (11 разд. 6).
func (op Op) Description() string {
	what := KindTitles[op.Kind]
	if op.ChargeTitle != "" {
		what = op.ChargeTitle
		if head, tail, ok := strings.Cut(op.ChargeTitle, ", "); ok {
			what = head + " за " + tail
		}
	}
	switch op.Kind {
	case calc.IncomeWithholding:
		what = "Удержание из обеспечительного платежа"
	case calc.IncomeToDeposit:
		what = "Перенос переплаты в обеспечительный платёж"
	}
	return what + " по договору № " + op.ContractNumber + ", " + op.TenantShort
}
