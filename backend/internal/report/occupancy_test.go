package report

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

func d(s string) date.Date { return date.MustParse(s) }

func dp(s string) *date.Date {
	v := d(s)
	return &v
}

// Эталон 16 Тестирование, раздел 2: 31-дневный месяц, сдан 20 дн., свободен 11 → 64,5 %; недополученный доход при
// ожидаемой плате 30 000: 30 000 × 11 / 31 = 10 645,16 (FR-05.7).
func TestOccupancyReference_FR_05_7(t *testing.T) {
	h := []Interval{
		{Status: StatusFree, From: d("2026-09-15"), To: dp("2026-10-11")},
		{Status: StatusRented, From: d("2026-10-12"), To: nil},
	}
	o := OccupancyOf(h, d("2026-10-01"), d("2026-10-31"), money.Kop(3000000))
	assert.Equal(t, 31, o.PeriodDays)
	assert.Equal(t, 20, o.Rented)
	assert.Equal(t, 11, o.Free)
	require.NotNil(t, o.Pct())
	assert.Equal(t, "64.5", *o.Pct())
	assert.Equal(t, money.Kop(1064516), o.Lost)
	require.Len(t, o.Idle, 1)
	assert.Equal(t, d("2026-10-01"), o.Idle[0].From, "обрезано началом периода")
	assert.Equal(t, 11, o.Idle[0].Days)
	assert.Equal(t, money.Kop(1064516), o.Idle[0].Lost)
}

// Простой по нескольким месяцам считается помесячно; ремонт и бронь — тоже простой.
func TestOccupancyMonths_FR_05_7(t *testing.T) {
	h := []Interval{
		{Status: StatusRepair, From: d("2026-02-20"), To: dp("2026-03-05")},
		{Status: StatusReserved, From: d("2026-03-06"), To: dp("2026-03-09")},
		{Status: StatusRented, From: d("2026-03-10"), To: nil},
	}
	o := OccupancyOf(h, d("2026-02-01"), d("2026-03-31"), money.Kop(2800000))
	assert.Equal(t, 59, o.PeriodDays)
	assert.Equal(t, 9+5, o.Repair)
	assert.Equal(t, 4, o.Reserved)
	assert.Equal(t, 22, o.Rented)
	// Февраль 28 дней: 9 дн. простоя → 28 000 × 9 / 28 = 9 000; март 31: 9 дн. → 28 000 × 9 / 31 = 8 129,03.
	assert.Equal(t, money.Kop(900000+812903), o.Lost)
	require.Len(t, o.ByMonth, 2)
	assert.Equal(t, MonthOccupancy{Month: d("2026-02-01"), Idle: 9}, o.ByMonth[0])
	assert.Equal(t, MonthOccupancy{Month: d("2026-03-01"), Rented: 22, Idle: 9}, o.ByMonth[1])
	assert.Nil(t, OccupancyOf(nil, d("2026-02-01"), d("2026-02-28"), 0).Pct(), "нет истории — заполняемость не считается")
}

func TestPctOf_FR_11_1(t *testing.T) {
	assert.Equal(t, "33.3", *PctOf(1, 3))
	assert.Equal(t, "66.7", *PctOf(2, 3))
	assert.Equal(t, "-25.0", *PctOf(-1, 4))
	assert.Nil(t, PctOf(1, 0))
}
