package clock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/date"
)

func moscow(t *testing.T) *time.Location {
	t.Helper()
	loc, err := LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	return loc
}

func TestFixedTodayUsesBusinessTimezone(t *testing.T) {
	loc := moscow(t)
	c := NewFixed(time.Date(2026, 10, 16, 21, 30, 0, 0, time.UTC), loc) // 00:30 17.10 по Москве
	assert.Equal(t, date.MustParse("2026-10-17"), c.Today())
	assert.Equal(t, time.UTC, c.Now().Location())

	c.Advance(-time.Hour)
	assert.Equal(t, date.MustParse("2026-10-16"), c.Today())
}

func TestFixedDate(t *testing.T) {
	c := NewFixedDate(date.MustParse("2026-11-01"), moscow(t))
	assert.Equal(t, date.MustParse("2026-11-01"), c.Today())
}

func TestRealClockIsUTC(t *testing.T) {
	c := NewReal(moscow(t))
	assert.Equal(t, time.UTC, c.Now().Location())
	assert.False(t, c.Today().IsZero())
}

// Сдвиг даты системы (демо): «сегодня» — заданная дата, время суток сохраняется.
func TestOffsetFor(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	real := time.Date(2026, 10, 19, 23, 30, 0, 0, loc)
	s := OffsetFor(real, date.MustParse("2026-11-01"), loc)
	got := real.Add(time.Duration(s) * time.Second).In(loc)
	if got.Format("2006-01-02 15:04") != "2026-11-01 23:30" {
		t.Fatalf("получено %s", got)
	}
	c := NewOffset(loc, func(context.Context) (int64, error) { return s, nil })
	if c.Today().String() == "" || c.Seconds() != s {
		t.Fatal("сдвиг не прочитан")
	}
}
