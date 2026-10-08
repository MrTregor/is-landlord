package tenant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 16 Тестирование, раздел 2 «ИНН»: контрольные суммы 10 и 12 знаков (FR-02.3, ТК-05).
func TestValidINN_FR02_3(t *testing.T) {
	for _, ok := range []string{"507500363778", "7704211201", "5075123457"} {
		assert.True(t, ValidINN(ok), ok)
	}
	for _, bad := range []string{"507500363779", "7704211202", "1234567890", "12345", "77042112011", "abcdefghij"} {
		assert.False(t, ValidINN(bad), bad)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"8 (916) 123-45-67": "+79161234567",
		"+7 916 123 45 67":  "+79161234567",
		"9161234567":        "+79161234567",
	}
	for in, want := range cases {
		got, ok := NormalizePhone(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got)
	}
	_, ok := NormalizePhone("12-34")
	assert.False(t, ok)
}

// Маскирование паспорта и адреса регистрации (12 Безопасность и ПДн, раздел 5; ТК-04).
func TestMasking_NFR_Sec_03(t *testing.T) {
	assert.Equal(t, "45 ** ****56", MaskDocNumber("45 18 123456"))
	assert.Equal(t, "45******56", MaskDocNumber("4518123456"))
	assert.Equal(t, "г. Москва, …", MaskAddress("г. Москва, ул. Примерная, д. 1, кв. 5"))
	assert.Equal(t, "г. Руза, …", MaskAddress("Московская область, г. Руза, ул. Солнечная, д. 5"))
	assert.Equal(t, "Московская область, …", MaskAddress("Московская область, Рузский округ"))
}
