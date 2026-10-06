package date

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAndFormat(t *testing.T) {
	d, err := Parse("2026-03-15")
	require.NoError(t, err)
	assert.Equal(t, 2026, d.Year())
	assert.Equal(t, time.March, d.Month())
	assert.Equal(t, 15, d.Day())
	assert.Equal(t, "2026-03-15", d.String())
	assert.Equal(t, "15.03.2026", d.Format())

	_, err = Parse("15.03.2026")
	assert.Error(t, err)
	_, err = Parse("2026-02-30")
	assert.Error(t, err)
}

func TestDaysIn(t *testing.T) {
	assert.Equal(t, 31, DaysIn(2026, time.March))
	assert.Equal(t, 28, DaysIn(2027, time.February))
	assert.Equal(t, 29, DaysIn(2028, time.February))
	assert.Equal(t, 30, MustParse("2026-11-10").DaysInMonth())
}

func TestArithmetic(t *testing.T) {
	d := MustParse("2026-03-15")
	assert.Equal(t, MustParse("2026-03-01"), d.FirstOfMonth())
	assert.Equal(t, MustParse("2026-03-31"), d.LastOfMonth())
	assert.Equal(t, MustParse("2026-04-01"), d.LastOfMonth().AddDays(1))
	assert.Equal(t, MustParse("2026-03-12"), d.AddDays(-3))
	// Пример 05 разд. 5: 15–31 марта — 17 дней.
	assert.Equal(t, 17, DaysInclusive(d, d.LastOfMonth()))
	assert.Equal(t, 16, d.LastOfMonth().Sub(d))
	// Месяцы: последний день, а не переполнение.
	assert.Equal(t, MustParse("2027-02-28"), MustParse("2027-01-31").AddMonths(1))
	assert.Equal(t, MustParse("2027-03-15"), d.AddMonths(12))
	assert.Equal(t, MustParse("2025-12-15"), d.AddMonths(-3))
}

func TestCompare(t *testing.T) {
	a, b := MustParse("2026-10-16"), MustParse("2026-10-17")
	assert.True(t, a.Before(b))
	assert.True(t, b.After(a))
	assert.Equal(t, -1, a.Compare(b))
	assert.Equal(t, 0, a.Compare(a))
	assert.Equal(t, a, Min(a, b))
	assert.Equal(t, b, Max(a, b))
}

func TestInMoscow(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	// 22:30 UTC 16.10 — это уже 17.10 по Москве.
	instant := time.Date(2026, 10, 16, 22, 30, 0, 0, time.UTC)
	assert.Equal(t, MustParse("2026-10-17"), In(instant, msk))
}

func TestJSON(t *testing.T) {
	type wrap struct {
		D Date  `json:"d"`
		N *Date `json:"n"`
	}
	b, err := json.Marshal(wrap{D: MustParse("2026-10-16")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"d":"2026-10-16","n":null}`, string(b))

	var w wrap
	require.NoError(t, json.Unmarshal([]byte(`{"d":"2026-01-02","n":null}`), &w))
	assert.Equal(t, MustParse("2026-01-02"), w.D)
	assert.Nil(t, w.N)
	assert.Error(t, json.Unmarshal([]byte(`{"d":"02.01.2026"}`), &w))
}

func TestScanValue(t *testing.T) {
	var d Date
	require.NoError(t, d.Scan(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, MustParse("2026-05-01"), d)
	v, err := d.Value()
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), v)

	require.NoError(t, d.Scan(nil))
	assert.True(t, d.IsZero())
	v, err = d.Value()
	require.NoError(t, err)
	assert.Nil(t, v)
}
