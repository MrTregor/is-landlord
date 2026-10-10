package calc

import (
	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/money"
)

// Виды доходных операций (05 разд. 12.1).
const (
	IncomeRent        = "rent"
	IncomeUtilities   = "utilities"
	IncomePenalty     = "penalty"
	IncomeDamage      = "damage"
	IncomeOther       = "other"
	IncomeAdvance     = "advance"
	IncomeWithholding = "deposit_withholding"
	IncomeRefund      = "refund"
)

// PaymentIncome — платёж периода: сумма и активные распределения по видам начислений.
type PaymentIncome struct {
	Amount      money.Kop
	Allocations map[string]money.Kop // вид начисления → сумма
	FromLegal   bool                 // плательщик — юрлицо (для НПД)
}

// Income — доход периода по видам (кассовый метод, 05 разд. 12.2).
type Income struct {
	ByKind map[string]money.Kop
	Total  money.Kop
	// Для НПД: доходы от физлиц и от юрлиц.
	FromIndividuals money.Kop
	FromLegal       money.Kop
}

// IncomeOf — доход: платежи (распределения по видам, нераспределённый остаток — «Аванс»), плюс удержания из залога,
// минус возвраты переплаты. Внесения и возвраты залога в доход не входят.
func IncomeOf(payments []PaymentIncome, withholdings []money.Kop, refunds []money.Kop) Income {
	inc := Income{ByKind: map[string]money.Kop{}}
	for _, p := range payments {
		allocated := money.Kop(0)
		for kind, a := range p.Allocations {
			inc.ByKind[kind] += a
			allocated += a
		}
		if adv := p.Amount - allocated; adv > 0 {
			inc.ByKind[IncomeAdvance] += adv
		}
		if p.FromLegal {
			inc.FromLegal += p.Amount
		} else {
			inc.FromIndividuals += p.Amount
		}
	}
	for _, w := range withholdings {
		inc.ByKind[IncomeWithholding] += w
		inc.FromIndividuals += w
	}
	for _, r := range refunds {
		inc.ByKind[IncomeRefund] -= r
		inc.FromIndividuals -= r
	}
	for _, v := range inc.ByKind {
		inc.Total += v
	}
	return inc
}

// TaxResult — база и налог; Tax = nil — налог не рассчитывается (ПСН, ОСН), см. Note.
type TaxResult struct {
	Base money.Kop
	Tax  *money.Kop
	Note string
}

// RoundRub — округление налога до целого рубля половиной вверх (1 234,49 → 1 234; 1 234,50 → 1 235).
func RoundRub(kop decimal.Decimal) money.Kop {
	return money.Kop(kop.Div(decimal.NewFromInt(100)).Round(0).IntPart() * 100)
}

func pctOf(base money.Kop, pct decimal.Decimal) money.Kop {
	return RoundRub(base.KopDecimal().Mul(pct).Div(decimal.NewFromInt(100)))
}

// Tax — налог по режиму (05 разд. 12.3).
func Tax(regime string, ratePct decimal.Decimal, inc Income, expenses money.Kop) TaxResult {
	switch regime {
	case "usn_income":
		t := pctOf(inc.Total, ratePct)
		return TaxResult{Base: inc.Total, Tax: &t}
	case "usn_income_expense":
		base := inc.Total - expenses
		t := money.Kop(0)
		if base > 0 {
			t = pctOf(base, ratePct)
		}
		return TaxResult{Base: base, Tax: &t}
	case "npd":
		t := pctOf(inc.FromIndividuals, decimal.NewFromInt(4)) + pctOf(inc.FromLegal, decimal.NewFromInt(6))
		return TaxResult{Base: inc.Total, Tax: &t}
	case "psn":
		return TaxResult{Base: inc.Total, Note: "Патент оплачивается фиксированно"}
	default:
		return TaxResult{Base: inc.Total, Note: "Налог при ОСН в отчёте не рассчитывается"}
	}
}
