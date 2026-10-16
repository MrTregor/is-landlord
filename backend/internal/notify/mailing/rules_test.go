package mailing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"landlord/backend/internal/platform/date"
)

func d(s string) date.Date { return date.MustParse(s) }

// Письмо о просрочке: первый день просрочки и затем каждые N дней (FR-10.1).
func TestOverdueDay_FR_10_1(t *testing.T) {
	first := []date.Date{d("2026-10-11")}
	assert.True(t, OverdueDay(first, d("2026-10-11"), 7), "первый день просрочки")
	assert.False(t, OverdueDay(first, d("2026-10-12"), 7))
	assert.True(t, OverdueDay(first, d("2026-10-18"), 7), "через 7 дней")
	assert.True(t, OverdueDay(first, d("2026-10-25"), 7))
	assert.False(t, OverdueDay(nil, d("2026-10-11"), 7))
	// Новое начисление стало просроченным — письмо в его первый день просрочки.
	two := []date.Date{d("2026-10-11"), d("2026-10-21")}
	assert.True(t, OverdueDay(two, d("2026-10-21"), 7))
	assert.False(t, OverdueDay(two, d("2026-10-22"), 7))
}

// Письмо об окончании договора: наименьшее N ≥ оставшихся дней, если договор действовал за N дней (FR-10.1).
func TestNoticeFor_FR_10_1(t *testing.T) {
	notice := []int16{90, 30}
	start, end := d("2026-01-01"), d("2026-12-31")
	n, ok := NoticeFor(notice, 90, start, end)
	assert.True(t, ok)
	assert.Equal(t, 90, n)
	n, ok = NoticeFor(notice, 45, start, end)
	assert.True(t, ok)
	assert.Equal(t, 90, n, "пропущенный день — письмо «за 90» догоняется")
	n, ok = NoticeFor(notice, 30, start, end)
	assert.True(t, ok)
	assert.Equal(t, 30, n)
	_, ok = NoticeFor(notice, 91, start, end)
	assert.False(t, ok)
	// Договор на 60 дней: за 90 дней до окончания его ещё не было — письма «за 90» нет.
	_, ok = NoticeFor(notice, 60, d("2026-11-01"), d("2026-12-31"))
	assert.False(t, ok)
	n, ok = NoticeFor(notice, 30, d("2026-11-01"), d("2026-12-31"))
	assert.True(t, ok)
	assert.Equal(t, 30, n)
}
