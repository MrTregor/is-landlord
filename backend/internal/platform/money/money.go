// Package money — денежные суммы в целых копейках.
// Дробные типы (float32/float64) для денег запрещены: умножение на ставки, тарифы и доли
// выполняется в decimal.Decimal, результат округляется функцией RoundHalfUp.
package money

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Kop — сумма в копейках.
type Kop int64

var hundred = decimal.NewFromInt(100)

// RoundHalfUp округляет сумму в рублях до копейки «половина вверх» (0,005 → 0,01; 190,295 → 190,30).
// Отрицательные суммы округляются симметрично (−0,005 → −0,01).
// Правило 05 разд. 1: округляется каждая строка расчёта, итог — сумма округлённых строк.
func RoundHalfUp(rub decimal.Decimal) Kop {
	return Kop(rub.Mul(hundred).Round(0).IntPart())
}

// RoundKopHalfUp округляет значение, уже выраженное в копейках (например, 3 000 000 × 17 / 31), до целой копейки.
func RoundKopHalfUp(kop decimal.Decimal) Kop {
	return Kop(kop.Round(0).IntPart())
}

// Decimal — сумма в рублях как decimal (для умножения на ставки и записи в XLSX).
func (k Kop) Decimal() decimal.Decimal {
	return decimal.NewFromInt(int64(k)).Div(hundred)
}

// KopDecimal — сумма в копейках как decimal.
func (k Kop) KopDecimal() decimal.Decimal { return decimal.NewFromInt(int64(k)) }

// Int64 — значение в копейках.
func (k Kop) Int64() int64 { return int64(k) }

const nbsp = " "

// Format — «28 000,00 ₽» (неразрывные пробелы между разрядами и перед знаком рубля).
func (k Kop) Format() string { return k.FormatNumber() + nbsp + "₽" }

// FormatRub — «28 000,00 руб.» (тексты документов).
func (k Kop) FormatRub() string { return k.FormatNumber() + " руб." }

// FormatNumber — «28 000,00» без обозначения валюты.
func (k Kop) FormatNumber() string {
	v := int64(k)
	sign := ""
	if v < 0 {
		sign = "−"
		v = -v
	}
	rub := strconv.FormatInt(v/100, 10)
	kop := v % 100
	return sign + groupThousands(rub) + "," + twoDigits(kop)
}

func twoDigits(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}

func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteString(nbsp)
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}
