package money

import (
	"strings"
)

var (
	unitsMale   = []string{"", "один", "два", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	unitsFemale = []string{"", "одна", "две", "три", "четыре", "пять", "шесть", "семь", "восемь", "девять"}
	teens       = []string{"десять", "одиннадцать", "двенадцать", "тринадцать", "четырнадцать", "пятнадцать",
		"шестнадцать", "семнадцать", "восемнадцать", "девятнадцать"}
	tens     = []string{"", "", "двадцать", "тридцать", "сорок", "пятьдесят", "шестьдесят", "семьдесят", "восемьдесят", "девяносто"}
	hundreds = []string{"", "сто", "двести", "триста", "четыреста", "пятьсот", "шестьсот", "семьсот", "восемьсот", "девятьсот"}
)

// scale — разряд: формы «один / два / пять» и род числительного.
type scale struct {
	forms  [3]string
	female bool
}

var scales = []scale{
	{forms: [3]string{"", "", ""}},
	{forms: [3]string{"тысяча", "тысячи", "тысяч"}, female: true},
	{forms: [3]string{"миллион", "миллиона", "миллионов"}},
	{forms: [3]string{"миллиард", "миллиарда", "миллиардов"}},
	{forms: [3]string{"триллион", "триллиона", "триллионов"}},
}

// Plural выбирает форму слова для числа n: one — 1, 21; few — 2–4, 22–24; many — 0, 5–20, 25–30.
func Plural(n int64, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	n100, n10 := n%100, n%10
	switch {
	case n100 >= 11 && n100 <= 14:
		return many
	case n10 == 1:
		return one
	case n10 >= 2 && n10 <= 4:
		return few
	default:
		return many
	}
}

// triad — число 1–999 прописью.
func triad(n int64, female bool) []string {
	var out []string
	if h := n / 100; h > 0 {
		out = append(out, hundreds[h])
	}
	rest := n % 100
	switch {
	case rest >= 10 && rest < 20:
		out = append(out, teens[rest-10])
	default:
		if t := rest / 10; t > 0 {
			out = append(out, tens[t])
		}
		if u := rest % 10; u > 0 {
			if female {
				out = append(out, unitsFemale[u])
			} else {
				out = append(out, unitsMale[u])
			}
		}
	}
	return out
}

// IntWords — целое неотрицательное число прописью (мужской род: «двадцать один»).
func IntWords(n int64) string {
	if n == 0 {
		return "ноль"
	}
	var groups []int64
	for v := n; v > 0; v /= 1000 {
		groups = append(groups, v%1000)
	}
	var words []string
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		if g == 0 {
			continue
		}
		sc := scales[i]
		words = append(words, triad(g, sc.female)...)
		if i > 0 {
			words = append(words, Plural(g, sc.forms[0], sc.forms[1], sc.forms[2]))
		}
	}
	return strings.Join(words, " ")
}

// ToWordsRu — сумма прописью для документов (09 разд. 4.6):
// 2 800 000 → «двадцать восемь тысяч рублей 00 копеек», 1 → «ноль рублей 01 копейка».
func ToWordsRu(k Kop) string {
	v := int64(k)
	sign := ""
	if v < 0 {
		sign = "минус "
		v = -v
	}
	rub, kop := v/100, v%100
	return sign + IntWords(rub) + " " + Plural(rub, "рубль", "рубля", "рублей") + " " +
		twoDigits(kop) + " " + Plural(kop, "копейка", "копейки", "копеек")
}
