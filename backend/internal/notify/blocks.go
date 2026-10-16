package notify

import (
	"context"
	"html"
	"strings"

	"landlord/backend/internal/platform/db/sqlc"
	"landlord/backend/internal/platform/tmpl"
)

// Line — строка таблицы начислений письма нанимателю: «Начисление / Срок / К оплате» (10 Уведомления, раздел 4).
type Line struct {
	Title, Due, Amount string
}

// LinesTable — блок {{block:lines}} писем об оплате.
func LinesTable(lines []Line) string {
	var b strings.Builder
	b.WriteString(`<table style="border-collapse:collapse;width:100%;margin:8px 0">` +
		`<tr><th style="text-align:left;border-bottom:1px solid #d1d5db;padding:4px">Начисление</th>` +
		`<th style="text-align:left;border-bottom:1px solid #d1d5db;padding:4px">Срок</th>` +
		`<th style="text-align:right;border-bottom:1px solid #d1d5db;padding:4px">К оплате</th></tr>`)
	for _, l := range lines {
		b.WriteString(`<tr><td style="padding:4px">` + html.EscapeString(l.Title) + `</td>` +
			`<td style="padding:4px">` + html.EscapeString(l.Due) + `</td>` +
			`<td style="padding:4px;text-align:right;white-space:nowrap">` + html.EscapeString(l.Amount) + `</td></tr>`)
	}
	b.WriteString(`</table>`)
	return b.String()
}

// Item — пункт раздела утренней сводки: текст и ссылка (пустая — без ссылки).
type Item struct {
	Text, Link string
}

// Section — раздел утренней сводки с заголовком и списком.
type Section struct {
	Title string
	Items []Item
}

// DigestBlock — блок {{block:lines}} утренней сводки: разделы с заголовками и списками ссылок.
func DigestBlock(sections []Section) string {
	var b strings.Builder
	for _, s := range sections {
		if len(s.Items) == 0 {
			continue
		}
		b.WriteString(`<h3 style="font-size:15px;margin:16px 0 4px">` + html.EscapeString(s.Title) + `</h3><ul style="margin:0;padding-left:20px">`)
		for _, it := range s.Items {
			text := html.EscapeString(it.Text)
			if it.Link != "" {
				text = `<a href="` + html.EscapeString(it.Link) + `">` + text + `</a>`
			}
			b.WriteString(`<li>` + text + `</li>`)
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}

// sampleBlocks — блоки для предпросмотра шаблона.
func sampleBlocks(code string) map[string]string {
	if code == "owner_daily_digest" {
		return map[string]string{"lines": DigestBlock([]Section{
			{"Сегодня", []Item{{"Срок оплаты: договор № 2026-001, Студия 1 — 28 000,00 ₽", "#"}}},
			{"Просрочки", []Item{{"Договор № 2026-002, Студия 2 — 11 500,00 ₽", "#"}}},
			{"Расхождения", []Item{{"Неопознанный платёж: 1 на 12 000,00 ₽", "#"}}},
		})}
	}
	return map[string]string{"lines": LinesTable([]Line{
		{"Плата за наём, ноябрь 2026", "10.11.2026", "26 500,00 ₽"},
		{"Компенсация ЖКУ, ноябрь 2026", "10.11.2026", "1 500,00 ₽"},
	})}
}

// Preview — тема и HTML письма code на примерах значений, в обёртке (предпросмотр шаблона, 06 разд. 4).
func (s *Service) Preview(ctx context.Context, q *sqlc.Queries, code, subject, body string) (string, string, error) {
	vars, err := s.commonVars(ctx, q)
	if err != nil {
		return "", "", err
	}
	for k, v := range Examples(code) {
		if _, ok := vars[k]; !ok || vars[k] == "" {
			vars[k] = v
		}
	}
	blocks := sampleBlocks(code)
	return html.UnescapeString(tmpl.Render(subject, vars, nil, tmpl.Options{})),
		s.wrap(tmpl.Render(UnwrapBlocks(body), vars, blocks, tmpl.Options{}), vars), nil
}
