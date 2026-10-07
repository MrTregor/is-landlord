package audit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ПДн не попадают в журнал в открытом виде (12 Безопасность и ПДн, раздел 5; FR-12).
func TestMaskPII_FR12_1(t *testing.T) {
	type occupant struct {
		FullName string `json:"full_name"`
		Relation string `json:"relation"`
	}
	in := map[string]any{
		"lastName":             "Кузнецов",
		"first_name":           "Павел",
		"birthDate":            "1995-04-12",
		"docNumber":            "45 18 123456",
		"doc_issued_by_enc":    "ГУ МВД",
		"registrationAddress":  "г. Москва",
		"phone":                "+79161234567",
		"email":                "k@example.ru",
		"middleName":           nil,
		"inn":                  "5075123457",
		"documentId":           "d1",
		"occupants":            []occupant{{FullName: "Кузнецова А. П.", Relation: "дочь"}},
		"nested":               map[string]any{"displayName": "Павел Игоревич", "status": "active"},
		"privacyPolicyVersion": "1.0",
	}
	b, err := Mask(in)
	require.NoError(t, err)
	s := string(b)
	for _, secret := range []string{"Кузнецов", "Павел", "1995", "45 18", "МВД", "Москва", "+7916", "k@example.ru"} {
		assert.NotContains(t, s, secret)
	}
	assert.Contains(t, s, `"inn":"5075123457"`)
	assert.Contains(t, s, `"documentId":"d1"`)
	assert.Contains(t, s, `"relation":"дочь"`)
	assert.Contains(t, s, `"middleName":null`)
	assert.Contains(t, s, `"status":"active"`)
	assert.Contains(t, s, `"lastName":"***"`)
}

func TestMaskNil(t *testing.T) {
	b, err := Mask(nil)
	require.NoError(t, err)
	assert.Nil(t, b)
}

func TestActionTitle(t *testing.T) {
	assert.Equal(t, "Вход", ActionTitle("auth.login"))
	assert.Equal(t, "Плановая задача rent_daily", ActionTitle("job.rent_daily"))
}
