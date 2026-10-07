// Package tmpl — подстановка переменных в шаблоны писем и документов (09 Документы и шаблоны, раздел 2):
// `{{ключ}}` (пробелы внутри скобок допускаются) и `{{block:имя}}` — готовый HTML-фрагмент.
// Значения экранируются как HTML, переводы строк → <br>. Других конструкций нет.
package tmpl

import (
	"html"
	"regexp"
	"sort"
	"strings"
)

var placeholder = regexp.MustCompile(`\{\{\s*(block:)?([A-Za-z0-9_.]+)\s*\}\}`)

// Options — правила подстановки.
type Options struct {
	// Empty — что подставить вместо пустого или отсутствующего значения
	// (для документов — «________________», для писем — пустая строка).
	Empty string
}

// Render подставляет значения и блоки.
func Render(src string, vars, blocks map[string]string, opt Options) string {
	return placeholder.ReplaceAllStringFunc(src, func(m string) string {
		sub := placeholder.FindStringSubmatch(m)
		isBlock, key := sub[1] != "", sub[2]
		if isBlock {
			if b, ok := blocks[key]; ok {
				return b
			}
			return opt.Empty
		}
		v := strings.TrimSpace(vars[key])
		if v == "" {
			return opt.Empty
		}
		return strings.ReplaceAll(html.EscapeString(v), "\n", "<br>")
	})
}

// Keys возвращает ключи переменных и блоков шаблона (блоки — с префиксом «block:»), без повторов, по алфавиту.
func Keys(src string) []string {
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(src, -1) {
		seen[m[1]+m[2]] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Unknown возвращает ключи шаблона, которых нет среди допустимых (проверка при сохранении шаблона).
func Unknown(src string, allowed map[string]bool) []string {
	var out []string
	for _, k := range Keys(src) {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	return out
}

var tags = regexp.MustCompile(`(?s)<[^>]*>`)
var lineBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</tr>|</h[1-6]>|</li>`)
var links = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
var blankLines = regexp.MustCompile(`\n{3,}`)

// PlainText — текстовая версия HTML-письма (08 разд. 5): теги удаляются, <br> и </p> → перевод строки,
// ссылки → «текст (адрес)».
func PlainText(htmlSrc string) string {
	s := links.ReplaceAllString(htmlSrc, "$2 ($1)")
	s = lineBreaks.ReplaceAllString(s, "\n")
	s = tags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
