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

// IsFemale — женский род по отчеству (…вна, …чна, …кызы) — для «гражданин/гражданка» в документах (09 разд. 4.2).
func IsFemale(middle *string) bool {
	m := strings.ToLower(val(middle))
	return strings.HasSuffix(m, "вна") || strings.HasSuffix(m, "чна") || strings.HasSuffix(m, "кызы")
}

// ShortOf — «Иванов И. И.» из полного ФИО одной строкой («Иванов Иван Иванович»).
func ShortOf(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return ""
	}
	var first, middle *string
	if len(parts) > 1 {
		first = &parts[1]
	}
	if len(parts) > 2 {
		middle = &parts[2]
	}
	return Short(&parts[0], first, middle)
}

// GenitiveFull — ФИО одной строкой в родительном падеже («Иванов Иван Иванович» → «Иванова Ивана Ивановича»).
// Простые правила для распространённых русских имён; несклоняемые формы остаются как есть.
func GenitiveFull(full string) string {
	parts := strings.Fields(full)
	female := len(parts) > 2 && IsFemale(&parts[2])
	for i, w := range parts {
		switch i {
		case 0:
			parts[i] = genitiveSurname(w, female)
		default:
			parts[i] = genitiveName(w, female)
		}
	}
	return strings.Join(parts, " ")
}

// GenitivePhrase — должность в родительном падеже («Генеральный директор» → «генерального директора»):
// склоняются прилагательные в начале и первое существительное, остальные слова («по развитию») не меняются.
func GenitivePhrase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		lw := strings.ToLower(w)
		switch {
		case hasAny(lw, "ый", "ой"):
			words[i] = trimLast(lw, 2) + "ого"
		case hasAny(lw, "ий"):
			words[i] = trimLast(lw, 2) + "его"
		default:
			words[i] = genitiveNoun(lw)
			return strings.Join(words, " ")
		}
	}
	return strings.Join(words, " ")
}

func genitiveNoun(w string) string {
	switch {
	case hasAny(w, "ь"):
		return trimLast(w, 1) + "я"
	case hasAny(w, "а"):
		return trimLast(w, 1) + "ы"
	case hasAny(w, "я"):
		return trimLast(w, 1) + "и"
	case hasAny(w, "о", "е", "и", "у", "ю", "э", "ы"):
		return w
	default:
		return w + "а"
	}
}

func genitiveSurname(w string, female bool) string {
	lw := strings.ToLower(w)
	if female {
		switch {
		case hasAny(lw, "ова", "ева", "ёва", "ина", "ына"):
			return trimLast(w, 1) + "ой"
		case hasAny(lw, "ая"):
			return trimLast(w, 2) + "ой"
		}
		return w
	}
	switch {
	case hasAny(lw, "ов", "ев", "ёв", "ин", "ын"):
		return w + "а"
	case hasAny(lw, "ский", "цкий"):
		return trimLast(w, 2) + "ого"
	case hasAny(lw, "ой", "ый"):
		return trimLast(w, 2) + "ого"
	case hasAny(lw, "ий"):
		return trimLast(w, 2) + "его"
	case hasAny(lw, "о", "е", "и", "у", "ю", "э", "ы", "ых", "их"):
		return w
	case hasAny(lw, "ь", "й"):
		return trimLast(w, 1) + "я"
	case hasAny(lw, "а"):
		return trimLast(w, 1) + "ы"
	case hasAny(lw, "я"):
		return trimLast(w, 1) + "и"
	}
	return w + "а"
}

func genitiveName(w string, female bool) string {
	lw := strings.ToLower(w)
	switch {
	case hasAny(lw, "вна", "чна"):
		return trimLast(w, 1) + "ы"
	case hasAny(lw, "ич"):
		return w + "а"
	case hasAny(lw, "ия"):
		return trimLast(w, 1) + "и"
	case hasAny(lw, "ка", "га", "ха", "жа", "ша", "ща", "ча"):
		return trimLast(w, 1) + "и"
	case hasAny(lw, "а"):
		return trimLast(w, 1) + "ы"
	case hasAny(lw, "я"):
		return trimLast(w, 1) + "и"
	case female:
		return w
	case hasAny(lw, "й", "ь"):
		return trimLast(w, 1) + "я"
	case hasAny(lw, "о", "е", "и", "у", "ю", "э", "ы"):
		return w
	}
	return w + "а"
}

func hasAny(w string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(w, s) {
			return true
		}
	}
	return false
}

func trimLast(w string, n int) string {
	r := []rune(w)
	if len(r) < n {
		return w
	}
	return string(r[:len(r)-n])
}
