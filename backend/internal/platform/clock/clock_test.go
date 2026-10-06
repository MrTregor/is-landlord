package clock

import (
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
