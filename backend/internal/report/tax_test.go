package report

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Кварталы и нарастающий итог (05 разд. 12.3).
func TestTaxPeriods_FR_11_3(t *testing.T) {
	p := taxPeriods(2026)
	require.Len(t, p, 8)
	assert.Equal(t, "2026-04-01", p[1].from.String())
	assert.Equal(t, "2026-06-30", p[1].to.String())
	assert.Equal(t, "полугодие", p[5].label)
	assert.Equal(t, "2026-01-01", p[5].from.String())
	assert.Equal(t, "2026-06-30", p[5].to.String())
	assert.Equal(t, "2026-12-31", p[7].to.String())
	assert.True(t, p[7].cumulative)
}

// Налог по режимам: УСН «доходы» 6 %, «доходы минус расходы» 15 % (не меньше 0), НПД 4 % / 6 %, ПСН — без налога.
func TestTaxRowRegimes_FR_11_3(t *testing.T) {
	k := Kinds{Rent: 3000000, Advance: 200000, Withheld: 500000, Refunds: -100000, FromIndividuals: 2600000, FromLegal: 1000000}
	p := taxPeriod{label: "год", from: d("2026-01-01"), to: d("2026-12-31")}

	usn := taxRow(p, k, "usn_income", decimal.RequireFromString("6"), 0)
	assert.Equal(t, int64(3600000), usn.IncomeKop)
	require.NotNil(t, usn.TaxKop)
	assert.Equal(t, int64(216000), *usn.TaxKop) // 36 000 × 6 % = 2 160
	assert.Nil(t, usn.ExpensesKop)

	ie := taxRow(p, k, "usn_income_expense", decimal.RequireFromString("15"), 4000000)
	require.NotNil(t, ie.ExpensesKop)
	assert.Equal(t, int64(0), ie.TaxBaseKop, "база не меньше 0")
	assert.Equal(t, int64(0), *ie.TaxKop)

	npd := taxRow(p, k, "npd", decimal.Zero, 0)
	require.NotNil(t, npd.TaxIndividualsKop)
	assert.Equal(t, int64(104000), *npd.TaxIndividualsKop) // 26 000 × 4 %
	assert.Equal(t, int64(60000), *npd.TaxLegalKop)        // 10 000 × 6 %

	psn := taxRow(p, k, "psn", decimal.Zero, 0)
	assert.Nil(t, psn.TaxKop)
	assert.Equal(t, "Режим: УСН «доходы», ставка 6 %", regimeTitle("usn_income", decimal.RequireFromString("6.00")))
	assert.Equal(t, "Режим: УСН «доходы минус расходы», ставка 7,5 %", regimeTitle("usn_income_expense", decimal.RequireFromString("7.50")))
}
