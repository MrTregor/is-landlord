// Package names — подписи людей и организаций (06 API: displayName — «Фамилия И. О.» или наименование).
package names

import (
	"strings"
	"unicode/utf8"
)

func initial(s *string) string {
	if s == nil {
		return ""
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(v)
	return string(r) + "."
}

func val(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// Short — «Кузнецов П. И.».
func Short(last, first, middle *string) string {
	parts := []string{val(last)}
	ini := strings.TrimSpace(initial(first) + " " + initial(middle))
	if ini != "" {
		parts = append(parts, ini)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// Full — «Кузнецов Павел Игоревич».
func Full(last, first, middle *string) string {
	return strings.Join(strings.Fields(val(last)+" "+val(first)+" "+val(middle)), " ")
}

// Display — подпись нанимателя: физлицо — «Фамилия И. О.», юрлицо — наименование.
func Display(kind string, last, first, middle, org *string) string {
	if kind == "legal" {
		return val(org)
	}
	return Short(last, first, middle)
}

// Greeting — обращение: «Имя Отчество» (без отчества — «Имя»).
func Greeting(first, middle *string) string {
	return strings.TrimSpace(val(first) + " " + val(middle))
}
