package calc

import (
	"testing"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

var d = date.MustParse

// Эталонные примеры R1–R5 (05 разд. 5) и февраль високосного года (16 разд. 2).
func TestRentProration_FR05_1(t *testing.T) {
	cases := []struct {
		name        string
		rent        money.Kop
		start, end  date.Date
		month       date.Date
		want        money.Kop
		wantTitle   string
		contractEnd date.Date
	}{
		{"R1", 3000000, d("2026-03-15"), d("2027-03-14"), d("2026-03-01"), 1645161, "Плата за наём, 15–31 марта 2026", date.Date{}},
		{"R2", 3000000, d("2026-03-15"), d("2027-03-14"), d("2026-04-01"), 3000000, "Плата за наём, апрель 2026", date.Date{}},
		{"R3", 3000000, d("2026-03-15"), d("2027-03-14"), d("2027-03-01"), 1354839, "Плата за наём, 1–14 марта 2027", date.Date{}},
		{"R4", 2800000, d("2026-03-01"), d("2026-11-10"), d("2026-11-01"), 933333, "Плата за наём, 1–10 ноября 2026", date.Date{}},
		{"R5", 2500000, d("2027-02-20"), d("2028-01-31"), d("2027-02-01"), 803571, "Плата за наём, 20–28 февраля 2027", date.Date{}},
		{"високосный", 3000000, d("2028-02-20"), d("2029-01-31"), d("2028-02-01"), 1034483, "Плата за наём, 20–29 февраля 2028", date.Date{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := PeriodOf(c.start, c.end, c.month)
			if !ok {
				t.Fatal("месяц вне срока")
			}
			if got := ProRate(c.rent, p); got != c.want {
				t.Fatalf("сумма %d, ожидалось %d", got, c.want)
			}
			if got := RentTitle("ch35", p); got != c.wantTitle {
				t.Fatalf("название %q, ожидалось %q", got, c.wantTitle)
			}
		})
	}
}

// R1, R2: сроки оплаты и момент создания начислений (05 разд. 5.3).
func TestRentSchedule_FR05_1_DueAndCreate(t *testing.T) {
	versions := []Terms{{Version: 1, EffectiveFrom: d("2026-03-15"), RentKop: 3000000, PaymentDay: 15}}
	lines := Schedule(d("2026-03-15"), d("2027-03-14"), d("2026-03-15"), versions, false, 3)
	if len(lines) != 13 {
		t.Fatalf("строк %d, ожидалось 13", len(lines))
	}
	if lines[0].DueDate != d("2026-03-15") || lines[0].RentKop != 1645161 {
		t.Fatalf("март: %v %d", lines[0].DueDate, lines[0].RentKop)
	}
	if lines[1].DueDate != d("2026-04-15") || lines[1].CreateOn != d("2026-03-29") {
		t.Fatalf("апрель: срок %v, создание %v", lines[1].DueDate, lines[1].CreateOn)
	}
	if lines[12].RentKop != 1354839 {
		t.Fatalf("март 2027: %d", lines[12].RentKop)
	}
}

func TestTermsFor_FR05_4(t *testing.T) {
	versions := []Terms{
		{Version: 2, EffectiveFrom: d("2027-04-01"), RentKop: 2940000},
		{Version: 1, EffectiveFrom: d("2026-03-15"), RentKop: 2800000},
	}
	if v := TermsFor(versions, d("2027-03-01")); v.Version != 1 {
		t.Fatalf("март 2027 — версия %d", v.Version)
	}
	if v := TermsFor(versions, d("2027-04-01")); v.Version != 2 {
		t.Fatalf("апрель 2027 — версия %d", v.Version)
	}
}

func TestDefaultPaymentDay_FR03_1(t *testing.T) {
	if DefaultPaymentDay(d("2026-03-15")) != 15 || DefaultPaymentDay(d("2026-03-31")) != 28 {
		t.Fatal("день оплаты по умолчанию")
	}
}

// Индексация (05 разд. 8.1, 8.2).
func TestIndexation_FR05_4(t *testing.T) {
	if got := FirstIndexation(d("2026-03-15"), 12); got != d("2027-04-01") {
		t.Fatalf("15.03.2026 → %v", got)
	}
	if got := FirstIndexation(d("2026-03-01"), 12); got != d("2027-03-01") {
		t.Fatalf("01.03.2026 → %v", got)
	}
	for _, c := range []struct {
		rent money.Kop
		pct  string
		want money.Kop
	}{{2800000, "5", 2940000}, {2650000, "7", 2835500}, {2730000, "4.5", 2852900}} {
		if got := IndexedRent(c.rent, decimal.RequireFromString(c.pct)); got != c.want {
			t.Fatalf("%d × %s %%: %d, ожидалось %d", c.rent, c.pct, got, c.want)
		}
	}
}
