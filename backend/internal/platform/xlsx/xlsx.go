// Package xlsx — выгрузки XLSX по правилам 11 Отчёты, раздел 1 (NFR-Comp-01): строка 1 — название (полужирный,
// 14 pt), строка 2 — период, время формирования и организация, затем примечания, пустая строка, заголовки
// (полужирный, серая заливка, границы, перенос), данные, «Итого» (суммы сервера) и «Итого по видимым строкам»
// (SUBTOTAL(9, …)); закрепление под заголовками, автофильтр, ширина колонок, деньги — «#,##0.00 "₽"», даты —
// настоящие даты Excel «dd.mm.yyyy», проценты — «0.0" %"».
//
// Это единственное место, где копейки переводятся в число с плавающей точкой — только для записи значения ячейки
// (decimal → число).
package xlsx

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

// Kind — тип колонки.
type Kind int

// Типы колонок.
const (
	Text Kind = iota
	Money
	Date
	Int
	Pct
	DateTime
)

// Column — колонка таблицы.
type Column struct {
	Title string
	Width float64
	Kind  Kind
}

// Sheet — лист с таблицей.
type Sheet struct {
	Name     string // ≤ 31 символа
	Title    string
	Subtitle string   // «Период: … · Сформирован … · Организация»
	Notes    []string // строки над таблицей (справочно)
	Columns  []Column
	Rows     [][]any // значения: string, money.Kop, date.Date, *date.Date, int, int64, *string (процент), time.Time, nil
	// Bold — строки данных, выделяемые полужирным (итоги кварталов и т. п.), по индексу в Rows.
	Bold map[int]bool
	// Totals — строка «Итого» (nil — без итогов); первая ячейка — подпись.
	Totals []any
}

// Subtitle — строка 2: «Период: 01.01.2026 – 31.12.2026 · Сформирован 16.10.2026 19:30 · ИП Гордейчик А.В.».
func Subtitle(period string, at time.Time, org string) string {
	parts := []string{}
	if period != "" {
		parts = append(parts, period)
	}
	parts = append(parts, "Сформирован "+at.Format("02.01.2006 15:04"))
	if org != "" {
		parts = append(parts, org)
	}
	return strings.Join(parts, " · ")
}

// Period — «Период: 01.01.2026 – 31.12.2026».
func Period(from, to date.Date) string {
	return "Период: " + from.Format() + " – " + to.Format()
}

type styles struct {
	title, header, money, date, dateTime, pct, text, int         int
	boldText, boldMoney, boldPct, boldInt, totalText, totalMoney int
}

func newStyles(f *excelize.File) (styles, error) {
	var s styles
	border := []excelize.Border{
		{Type: "left", Color: "A0A4AB", Style: 1}, {Type: "right", Color: "A0A4AB", Style: 1},
		{Type: "top", Color: "A0A4AB", Style: 1}, {Type: "bottom", Color: "A0A4AB", Style: 1},
	}
	moneyFmt, pctFmt := `#,##0.00 "₽"`, `0.0" %"`
	defs := []struct {
		dst *int
		st  excelize.Style
	}{
		{&s.title, excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}}},
		{&s.header, excelize.Style{Font: &excelize.Font{Bold: true}, Border: border,
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E7E9EC"}},
			Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}}},
		{&s.money, excelize.Style{CustomNumFmt: &moneyFmt}},
		{&s.date, excelize.Style{CustomNumFmt: ptrS("dd.mm.yyyy")}},
		{&s.dateTime, excelize.Style{CustomNumFmt: ptrS("dd.mm.yyyy hh:mm")}},
		{&s.pct, excelize.Style{CustomNumFmt: &pctFmt}},
		{&s.text, excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}}},
		{&s.int, excelize.Style{NumFmt: 1}},
		{&s.boldText, excelize.Style{Font: &excelize.Font{Bold: true}}},
		{&s.boldMoney, excelize.Style{Font: &excelize.Font{Bold: true}, CustomNumFmt: &moneyFmt}},
		{&s.boldPct, excelize.Style{Font: &excelize.Font{Bold: true}, CustomNumFmt: &pctFmt}},
		{&s.boldInt, excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 1}},
		{&s.totalText, excelize.Style{Font: &excelize.Font{Bold: true}, Border: []excelize.Border{{Type: "top", Color: "000000", Style: 1}}}},
		{&s.totalMoney, excelize.Style{Font: &excelize.Font{Bold: true}, CustomNumFmt: &moneyFmt,
			Border: []excelize.Border{{Type: "top", Color: "000000", Style: 1}}}},
	}
	for _, d := range defs {
		st := d.st
		id, err := f.NewStyle(&st)
		if err != nil {
			return s, err
		}
		*d.dst = id
	}
	return s, nil
}

func ptrS(s string) *string { return &s }

func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// rub — копейки → рубли числом для ячейки (через decimal, без накопления ошибок).
func rub(k money.Kop) float64 {
	v, _ := decimal.NewFromInt(int64(k)).Div(decimal.NewFromInt(100)).Float64()
	return v
}

func pctValue(s string) (float64, bool) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return 0, false
	}
	v, _ := d.Float64()
	return v, true
}

// excelDate — дата как настоящая дата Excel (полночь UTC без сдвига часового пояса).
func excelDate(d date.Date) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func (s styles) styleOf(k Kind, bold bool) int {
	switch k {
	case Money:
		if bold {
			return s.boldMoney
		}
		return s.money
	case Date:
		return s.date
	case DateTime:
		return s.dateTime
	case Pct:
		if bold {
			return s.boldPct
		}
		return s.pct
	case Int:
		if bold {
			return s.boldInt
		}
		return s.int
	default:
		if bold {
			return s.boldText
		}
		return s.text
	}
}

func setValue(f *excelize.File, sheet, ref string, v any) error {
	switch x := v.(type) {
	case nil:
		return nil
	case money.Kop:
		return f.SetCellValue(sheet, ref, rub(x))
	case date.Date:
		if x.IsZero() {
			return nil
		}
		return f.SetCellValue(sheet, ref, excelDate(x))
	case *date.Date:
		if x == nil {
			return nil
		}
		return f.SetCellValue(sheet, ref, excelDate(*x))
	case *string:
		if x == nil {
			return nil
		}
		if p, ok := pctValue(*x); ok {
			return f.SetCellValue(sheet, ref, p)
		}
		return f.SetCellValue(sheet, ref, *x)
	case *int64:
		if x == nil {
			return nil
		}
		return f.SetCellValue(sheet, ref, *x)
	case time.Time:
		return f.SetCellValue(sheet, ref, time.Date(x.Year(), x.Month(), x.Day(), x.Hour(), x.Minute(), x.Second(), 0, time.UTC))
	default:
		return f.SetCellValue(sheet, ref, x)
	}
}

func writeSheet(f *excelize.File, st styles, sh Sheet, first bool) error {
	name := sh.Name
	if len([]rune(name)) > 31 {
		name = string([]rune(name)[:31])
	}
	if first {
		if err := f.SetSheetName("Sheet1", name); err != nil {
			return err
		}
	} else if _, err := f.NewSheet(name); err != nil {
		return err
	}
	if err := f.SetCellValue(name, "A1", sh.Title); err != nil {
		return err
	}
	_ = f.SetCellStyle(name, "A1", "A1", st.title)
	if err := f.SetCellValue(name, "A2", sh.Subtitle); err != nil {
		return err
	}
	row := 3
	for _, n := range sh.Notes {
		if err := f.SetCellValue(name, cell(1, row), n); err != nil {
			return err
		}
		row++
	}
	row++ // пустая строка
	head := row
	cols := len(sh.Columns)
	for i, c := range sh.Columns {
		if err := f.SetCellValue(name, cell(i+1, head), c.Title); err != nil {
			return err
		}
		colName, _ := excelize.ColumnNumberToName(i + 1)
		w := c.Width
		if w == 0 {
			w = 14
		}
		if err := f.SetColWidth(name, colName, colName, w); err != nil {
			return err
		}
	}
	_ = f.SetCellStyle(name, cell(1, head), cell(cols, head), st.header)
	for ri, r := range sh.Rows {
		y := head + 1 + ri
		for ci, v := range r {
			if ci >= cols {
				break
			}
			if err := setValue(f, name, cell(ci+1, y), v); err != nil {
				return err
			}
			_ = f.SetCellStyle(name, cell(ci+1, y), cell(ci+1, y), st.styleOf(sh.Columns[ci].Kind, sh.Bold[ri]))
		}
	}
	last := head + len(sh.Rows)
	if err := f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: head, TopLeftCell: cell(1, head+1), ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if len(sh.Rows) > 0 {
		if err := f.AutoFilter(name, cell(1, head)+":"+cell(cols, last), nil); err != nil {
			return err
		}
	}
	if sh.Totals == nil {
		return nil
	}
	tot := last + 1
	for ci, v := range sh.Totals {
		if ci >= cols {
			break
		}
		if err := setValue(f, name, cell(ci+1, tot), v); err != nil {
			return err
		}
		style := st.totalText
		if sh.Columns[ci].Kind == Money {
			style = st.totalMoney
		}
		_ = f.SetCellStyle(name, cell(ci+1, tot), cell(ci+1, tot), style)
	}
	// «Итого по видимым строкам» — формула SUBTOTAL по каждой денежной колонке (пересчитывается при фильтре).
	vis := tot + 1
	_ = f.SetCellValue(name, cell(1, vis), "Итого по видимым строкам")
	_ = f.SetCellStyle(name, cell(1, vis), cell(1, vis), st.boldText)
	for ci, c := range sh.Columns {
		if c.Kind != Money || ci == 0 {
			continue
		}
		colName, _ := excelize.ColumnNumberToName(ci + 1)
		formula := fmt.Sprintf("SUBTOTAL(9,%s%d:%s%d)", colName, head+1, colName, last)
		if len(sh.Rows) == 0 {
			formula = "0"
		}
		if err := f.SetCellFormula(name, cell(ci+1, vis), formula); err != nil {
			return err
		}
		_ = f.SetCellStyle(name, cell(ci+1, vis), cell(ci+1, vis), st.boldMoney)
	}
	return nil
}

// Build — книга из листов.
func Build(sheets ...Sheet) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	st, err := newStyles(f)
	if err != nil {
		return nil, err
	}
	for i, sh := range sheets {
		if err := writeSheet(f, st, sh, i == 0); err != nil {
			return nil, err
		}
	}
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileName — имя файла «{Название} {параметры}.xlsx» без недопустимых символов.
func FileName(parts ...string) string {
	name := strings.Join(parts, " ")
	name = strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "").Replace(name)
	return strings.TrimSpace(name) + ".xlsx"
}

// Response — ответ API с файлом XLSX для скачивания (имя на русском, RFC 5987).
func Response(name string, data []byte) api.XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse {
	disp := fmt.Sprintf(`attachment; filename="report.xlsx"; filename*=UTF-8''%s`, url.PathEscape(name))
	return api.XlsxApplicationvndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		Body: bytes.NewReader(data), ContentLength: int64(len(data)), Headers: api.XlsxResponseHeaders{ContentDisposition: &disp},
	}
}
