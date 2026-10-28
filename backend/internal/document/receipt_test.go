package document

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"landlord/backend/internal/platform/date"
)

// Колонка «Период» квитанции (09 разд. 4.5, FR-06.3, № 114): из названия, по месяцу начисления или «—».
func TestSplitChargeTitle_FR06_3(t *testing.T) {
	oct := date.MustParse("2026-10-01")
	cases := []struct {
		title      string
		month      *date.Date
		wantTitle  string
		wantPeriod string
	}{
		{"Плата за наём, октябрь 2026", &oct, "Плата за наём", "октябрь 2026"},
		{"Арендная плата, 15–31 октября 2026", &oct, "Арендная плата", "15–31 октября 2026"},
		{"Уборка подъезда", &oct, "Уборка подъезда", "октябрь 2026"},
		{"Пени за просрочку, договор № 2026-001", nil, "Пени за просрочку, договор № 2026-001", "—"},
	}
	for _, c := range cases {
		title, period := splitChargeTitle(c.title, c.month)
		assert.Equal(t, c.wantTitle, title, c.title)
		assert.Equal(t, c.wantPeriod, period, c.title)
	}
}
