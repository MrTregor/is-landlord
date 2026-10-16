package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"landlord/backend/internal/platform/tmpl"
)

// Тексты по умолчанию — все 21 письмо из миграций, и в них только известные переменные (FR-10.3).
func TestDefaultsMatchCatalog_FR_10_3(t *testing.T) {
	def, err := Defaults()
	require.NoError(t, err)
	assert.Len(t, def, 21)
	assert.ElementsMatch(t, Codes(), keys(def))
	for code, d := range def {
		allowed := Allowed(code)
		assert.Empty(t, tmpl.Unknown(d.Subject, allowed), "тема %s", code)
		assert.Empty(t, tmpl.Unknown(d.BodyHTML, allowed), "текст %s", code)
	}
	assert.Equal(t, "Напоминание об оплате до {{due_date}}", def["payment_reminder"].Subject)
	assert.Contains(t, def["payment_reminder"].BodyHTML, "{{block:lines}}")
	assert.Equal(t, "Банк сообщил о возврате платежа {{amount}} — {{tenant.short_name}}", def["owner_bank_refund"].Subject)
}

func TestVariables_FR_10_3(t *testing.T) {
	vars := Allowed("invite")
	assert.True(t, vars["link"])
	assert.True(t, vars["org.short_name"])
	assert.False(t, vars["contract.number"], "в приглашении нет договора")
	assert.True(t, Allowed("payment_overdue")["block:lines"])
	assert.True(t, Allowed("owner_daily_digest")["today"])
	assert.Nil(t, Variables("unknown"))
	assert.NotContains(t, Examples("payment_reminder"), "block:lines")
}

func keys(m map[string]Default) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestUnwrapBlocks_FR_10_3(t *testing.T) {
	assert.Equal(t, "<p>a</p>{{block:lines}}<p>b</p>", UnwrapBlocks("<p>a</p><p>{{block:lines}}</p><p>b</p>"))
	assert.Equal(t, "<p>a</p>{{ block:lines }}", UnwrapBlocks("<p>a</p><p> {{ block:lines }} </p>"))
	assert.Equal(t, "<p>Итого {{block:lines}}</p>", UnwrapBlocks("<p>Итого {{block:lines}}</p>"))
}
