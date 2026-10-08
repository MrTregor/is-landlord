package tenant

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	w10 = []int{2, 4, 10, 3, 5, 9, 4, 6, 8}
	w11 = []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}
	w12 = []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}
)

func checkDigit(d []int, w []int) int {
	sum := 0
	for i, k := range w {
		sum += k * d[i]
	}
	return sum % 11 % 10
}

var innRe = regexp.MustCompile(`^\d{10}(\d{2})?$`)

// ValidINN — ИНН из 10 (организация) или 12 (физлицо, ИП) цифр с верными контрольными числами (алгоритм ФНС).
func ValidINN(inn string) bool {
	if !innRe.MatchString(inn) {
		return false
	}
	d := make([]int, len(inn))
	for i, r := range inn {
		d[i] = int(r - '0')
	}
	if len(d) == 10 {
		return checkDigit(d, w10) == d[9]
	}
	return checkDigit(d, w11) == d[10] && checkDigit(d, w12) == d[11]
}

// NormalizePhone — телефон в любом виде → +7XXXXXXXXXX; ok = false, если это не 10 цифр российского номера.
func NormalizePhone(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 11 && (d[0] == '7' || d[0] == '8') {
		d = d[1:]
	}
	if len(d) != 10 {
		return "", false
	}
	return "+7" + d, true
}

// MaskDocNumber — «45 ** ****56»: видны первые 2 и последние 2 цифры, разделители сохраняются (12 разд. 5).
func MaskDocNumber(s string) string {
	digits := 0
	for _, r := range s {
		if unicode.IsDigit(r) || unicode.IsLetter(r) {
			digits++
		}
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsDigit(r) || unicode.IsLetter(r) {
			n++
			if n <= 2 || n > digits-2 {
				b.WriteRune(r)
			} else {
				b.WriteRune('*')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var settlement = regexp.MustCompile(`(?i)^(г\.|город|пос\.|посёлок|поселок|п\.|пгт|с\.|село|д\.|дер\.|деревня|рп)\s*\S`)

// MaskAddress — только населённый пункт и «…» (12 разд. 5): «г. Москва, …».
func MaskAddress(s string) string {
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if settlement.MatchString(p) {
			return p + ", …"
		}
	}
	first := strings.TrimSpace(parts[0])
	if first == "" {
		return "…"
	}
	return first + ", …"
}
