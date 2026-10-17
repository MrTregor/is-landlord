package meter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"landlord/backend/internal/platform/date"
)

// Окно приёма показаний от нанимателя: с readings_reminder_day числа месяца M по 5-е число M+1 (FR-05.5, 05 разд. 6.2).
func TestCurrentWindow_FR05_5(t *testing.T) {
	d := date.MustParse
	cases := []struct {
		today, month, from, to string
		open                   bool
	}{
		{"2026-10-19", "2026-10-01", "2026-10-20", "2026-11-05", false},
		{"2026-10-20", "2026-10-01", "2026-10-20", "2026-11-05", true},
		{"2026-10-31", "2026-10-01", "2026-10-20", "2026-11-05", true},
		{"2026-11-05", "2026-10-01", "2026-10-20", "2026-11-05", true},
		{"2026-11-06", "2026-11-01", "2026-11-20", "2026-12-05", false},
		{"2027-01-03", "2026-12-01", "2026-12-20", "2027-01-05", true},
	}
	for _, c := range cases {
		w := CurrentWindow(d(c.today), 20)
		assert.Equal(t, Window{PeriodMonth: d(c.month), From: d(c.from), To: d(c.to), Open: c.open}, w, c.today)
	}
}
