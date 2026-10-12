package calc

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/money"
)

var dec = decimal.RequireFromString

// U1 (05 разд. 6): вода 3,500 × 54,37 = 190,30; электроэнергия 171 × 6,43 = 1 099,53; фиксированная часть 450 → 1 739,83.
func TestUtilities_FR05_5_U1(t *testing.T) {
	water := dec("115.900")
	el := dec("9012")
	lines := []MeterLine{
		MeterAmount(uuid.New(), "cold_water", dec("112.400"), d("2026-08-31"), &water, dec("54.37")),
		MeterAmount(uuid.New(), "electricity", dec("8841"), d("2026-08-31"), &el, dec("6.43")),
	}
	if lines[0].Amount != 19030 || lines[1].Amount != 109953 {
		t.Fatalf("строки %d, %d", lines[0].Amount, lines[1].Amount)
	}
	if got := UtilitiesTotal(lines, 45000); got != 173983 {
		t.Fatalf("итого %d, ожидалось 173983", got)
	}
	if !lines[0].Consumption.Equal(dec("3.5")) {
		t.Fatalf("расход %s", lines[0].Consumption)
	}
}

// Счётчик без показания — расход 0 и отметка missing.
func TestUtilitiesMissingReading_FR05_5(t *testing.T) {
	l := MeterAmount(uuid.New(), "gas", dec("10"), d("2026-08-31"), nil, dec("8"))
	if !l.Missing || l.Amount != 0 || !l.Consumption.IsZero() {
		t.Fatalf("%+v", l)
	}
}

// Тариф объекта важнее общего; нет тарифа — ok = false.
func TestSelectTariff_FR05_5(t *testing.T) {
	prop := uuid.New()
	other := uuid.New()
	tariffs := []Tariff{
		{MeterType: "cold_water", Price: dec("50"), EffectiveFrom: d("2026-01-01")},
		{MeterType: "cold_water", Price: dec("54.37"), EffectiveFrom: d("2026-07-01")},
		{MeterType: "cold_water", PropertyID: &prop, Price: dec("60"), EffectiveFrom: d("2026-09-01")},
		{MeterType: "cold_water", PropertyID: &other, Price: dec("99"), EffectiveFrom: d("2026-01-01")},
	}
	if p, _ := SelectTariff(tariffs, "cold_water", prop, d("2026-08-31")); !p.Equal(dec("54.37")) {
		t.Fatalf("август: %s", p)
	}
	if p, _ := SelectTariff(tariffs, "cold_water", prop, d("2026-09-30")); !p.Equal(dec("60")) {
		t.Fatalf("сентябрь: %s", p)
	}
	if _, ok := SelectTariff(tariffs, "gas", prop, d("2026-09-30")); ok {
		t.Fatal("тарифа на газ нет")
	}
}

func TestUtilitiesDue_FR05_5(t *testing.T) {
	if got := UtilitiesDue(d("2026-09-01"), 10, d("2026-10-02")); got != d("2026-10-10") {
		t.Fatalf("%v", got)
	}
	if got := UtilitiesDue(d("2026-09-01"), 10, d("2026-10-09")); got != d("2026-10-12") {
		t.Fatalf("%v", got)
	}
}

func TestHighConsumption_FR05_5(t *testing.T) {
	prev := []decimal.Decimal{dec("3"), dec("4"), dec("5")}
	if !HighConsumption(dec("12.1"), prev) || HighConsumption(dec("12"), prev) || HighConsumption(dec("100"), nil) {
		t.Fatal("правило трёх средних")
	}
}

// Пени P1–P5 (05 разд. 7).
func TestPenalty_FR05_3(t *testing.T) {
	rate := dec("0.1")
	cap10 := dec("10")
	cases := []struct {
		name    string
		in      PenaltyInput
		want    money.Kop
		accrues bool
		last    string
	}{
		{"P1", PenaltyInput{Amount: 2650000, Due: d("2026-10-05"), RatePct: rate, GraceDays: 3,
			Payments: []Paid{{d("2026-10-07"), 1500000}}, Today: d("2026-10-17")}, 9200, true, "2026-10-16"},
		{"P2", PenaltyInput{Amount: 3000000, Due: d("2026-10-20"), RatePct: rate,
			Payments: []Paid{{d("2026-10-25"), 3000000}}, Today: d("2026-10-27")}, 15000, true, "2026-10-25"},
		{"P3", PenaltyInput{Amount: 2800000, Due: d("2026-11-10"), RatePct: rate,
			Payments: []Paid{{d("2026-11-20"), 1800000}, {d("2026-11-15"), 1000000}}, Today: d("2026-12-01")}, 23000, true, "2026-11-20"},
		{"P4", PenaltyInput{Amount: 3000000, Due: d("2026-10-20"), RatePct: rate, GraceDays: 3,
			Payments: []Paid{{d("2026-10-22"), 3000000}}, Today: d("2026-10-27")}, 0, false, ""},
		{"P5", PenaltyInput{Amount: 3000000, Due: d("2026-01-01"), RatePct: rate, CapPct: &cap10,
			Today: d("2026-01-01").AddDays(201)}, 300000, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Penalty(c.in)
			if r.Total != c.want || r.Accrues != c.accrues {
				t.Fatalf("пени %d (начисляются %v), ожидалось %d", r.Total, r.Accrues, c.want)
			}
			if c.last != "" && r.LastDay != d(c.last) {
				t.Fatalf("последний день %v", r.LastDay)
			}
		})
	}
	// P3 по отрезкам: 5 × 28 000 и 5 × 18 000.
	r := Penalty(cases[2].in)
	if len(r.Segments) != 2 || r.Segments[0].Amount != 14000 || r.Segments[1].Amount != 9000 || r.Segments[1].Days != 5 {
		t.Fatalf("отрезки %+v", r.Segments)
	}
	// P5 без предела — 6 000.
	in := cases[4].in
	in.CapPct = nil
	if Penalty(in).Total != 600000 {
		t.Fatal("P5 без предела")
	}
}

// Налоговая база (16 разд. 2): платёж 30 000 (28 000 плата + 2 000 аванс), удержание 5 000, возврат 1 000 → 34 000; 6 % = 2 040.
func TestIncomeAndTax_FR05_8_FR11_3(t *testing.T) {
	inc := IncomeOf([]PaymentIncome{{Amount: 3000000, Allocations: map[string]money.Kop{IncomeRent: 2800000}}},
		[]money.Kop{500000}, []money.Kop{100000}, nil)
	if inc.Total != 3400000 || inc.ByKind[IncomeAdvance] != 200000 || inc.ByKind[IncomeRefund] != -100000 {
		t.Fatalf("доход %+v", inc)
	}
	r := Tax("usn_income", dec("6"), inc, 0)
	if r.Tax == nil || *r.Tax != 204000 {
		t.Fatalf("налог %+v", r)
	}
	if RoundRub(dec("123449")) != 123400 || RoundRub(dec("123450")) != 123500 {
		t.Fatal("округление налога до рубля")
	}
	if r := Tax("usn_income_expense", dec("15"), inc, 5000000); *r.Tax != 0 || r.Base != -1600000 {
		t.Fatalf("УСН д−р при убытке: %+v", r)
	}
	legal := IncomeOf([]PaymentIncome{{Amount: 1000000, FromLegal: true}, {Amount: 1000000}}, nil, nil, nil)
	if r := Tax("npd", decimal.Zero, legal, 0); *r.Tax != 100000 {
		t.Fatalf("НПД: %+v", r)
	}
	if r := Tax("psn", decimal.Zero, inc, 0); r.Tax != nil || r.Note == "" {
		t.Fatal("ПСН")
	}
}

// Перенос переплаты в залог (05 разд. 12.2): платёж 30 000 (28 000 плата + 2 000 аванс), 2 000 перенесены в залог,
// позже залог 2 000 зачтён (платёж deposit_offset) → доход 28 000 + 2 000 = 30 000, без двойного учёта.
func TestIncomeAdvanceToDeposit_FR05_8_FR11_3(t *testing.T) {
	inc := IncomeOf([]PaymentIncome{
		{Amount: 3000000, Allocations: map[string]money.Kop{IncomeRent: 2800000}},
		{Amount: 200000, Allocations: map[string]money.Kop{IncomeRent: 200000}},
	}, nil, nil, []money.Kop{200000})
	if inc.Total != 3200000-200000 || inc.ByKind[IncomeToDeposit] != -200000 || inc.ByKind[IncomeAdvance] != 200000 {
		t.Fatalf("доход %+v", inc)
	}
}
