package money

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// Эталоны 16 Тестирование разд. 2 «Деньги» и 05 разд. 1.
func TestRoundHalfUp_Money(t *testing.T) {
	cases := []struct {
		rub  string
		want Kop
	}{
		{"190.295", 19030}, // U1: 3,500 × 54,37
		{"0.005", 1},       // половина вверх
		{"0.004", 0},
		{"1099.53", 109953},     // U1: 171 × 6,43
		{"16451.6129", 1645161}, // R1
		{"13548.387", 1354839},  // R3
		{"-0.005", -1},
	}
	for _, c := range cases {
		t.Run(c.rub, func(t *testing.T) {
			assert.Equal(t, c.want, RoundHalfUp(decimal.RequireFromString(c.rub)))
		})
	}
}

func TestRoundKopHalfUp_FR05_1_R1(t *testing.T) {
	// 30 000,00 × 17 / 31 в копейках.
	v := decimal.NewFromInt(3000000).Mul(decimal.NewFromInt(17)).Div(decimal.NewFromInt(31))
	assert.Equal(t, Kop(1645161), RoundKopHalfUp(v))
}

func TestFormat(t *testing.T) {
	assert.Equal(t, "28 000,00 ₽", Kop(2800000).Format())
	assert.Equal(t, "0,05 ₽", Kop(5).Format())
	assert.Equal(t, "1 503 250,00", Kop(150325000).FormatNumber())
	assert.Equal(t, "999,99", Kop(99999).FormatNumber())
	assert.Equal(t, "−1 000,00", Kop(-100000).FormatNumber())
	assert.Equal(t, "28 000,00 руб.", Kop(2800000).FormatRub())
}

func TestDecimal(t *testing.T) {
	assert.Equal(t, "28000.5", Kop(2800050).Decimal().String())
	assert.Equal(t, "2800050", Kop(2800050).KopDecimal().String())
}
