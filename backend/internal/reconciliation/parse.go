// Package reconciliation — сверка (FR-07.2, FR-07.3): разбор выписок банка в форматах 1С и CSV, отпечатки строк,
// сопоставление строк с договорами, возможные дубли и неопознанные платежи. Правила — docs/spec/05 Бизнес-правила и
// расчёты.md, раздел 15; форматы — docs/spec/08 Интеграции и эмуляторы.md, раздел 7.
package reconciliation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"landlord/backend/internal/platform/date"
)

// Форматы выписок.
const (
	Format1C  = "1c"
	FormatCSV = "csv"
)

// CSVHeader — обязательная первая строка CSV (08 разд. 7.2).
const CSVHeader = "Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа"

// Line — разобранная входящая строка выписки.
type Line struct {
	No           int // номер документа в файле (1С) или строки данных (CSV), с 1
	DocNumber    string
	DocDate      *date.Date
	ReceivedOn   date.Date
	AmountKop    int64
	PayerName    string
	PayerINN     string
	PayerAccount string
	Purpose      string
}

// Statement — результат разбора файла.
type Statement struct {
	Format   string
	Account  string // РасчСчет заголовка 1С
	DateFrom *date.Date
	DateTo   *date.Date
	Total    int    // все документы файла
	Incoming []Line // входящие строки
}

// ParseError — ошибки разбора: «Строка N: …»; файл не загружается частично.
type ParseError struct{ Messages []string }

func (e *ParseError) Error() string { return strings.Join(e.Messages, "; ") }

// Parse распознаёт формат и разбирает выписку. ownAccount — расчётный счёт организации (для 1С: входящими считаются
// документы, где ПолучательСчет совпадает с ним; если реквизит не заполнен — с РасчСчет заголовка).
func Parse(data []byte, ownAccount string) (Statement, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	first := firstLine(data)
	switch {
	case first == "1CClientBankExchange":
		return parse1C(decode1C(data), ownAccount)
	case utf8.Valid(data) && strings.TrimSpace(first) == CSVHeader:
		return parseCSV(string(data))
	default:
		return Statement{}, &ParseError{Messages: []string{
			"Формат файла не распознан: нужна выписка 1С (первая строка «1CClientBankExchange») или CSV с заголовком «" +
				CSVHeader + "»",
		}}
	}
}

func firstLine(data []byte) string {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		data = data[:i]
	}
	return strings.TrimSpace(string(data))
}

// decode1C — кодировка по строке «Кодировка=»: Windows → Windows-1251, DOS → CP866, иначе UTF-8; если строки нет —
// UTF-8, а при ошибке — Windows-1251 (08 разд. 7.1).
func decode1C(data []byte) string {
	key := "Кодировка="
	for _, enc := range []struct {
		key []byte
		dec func([]byte) string
	}{
		{[]byte(key), func(b []byte) string { return string(b) }},
		{encode(charmap.Windows1251, key), func(b []byte) string { return decodeWith(charmap.Windows1251, b) }},
		{encode(charmap.CodePage866, key), func(b []byte) string { return decodeWith(charmap.CodePage866, b) }},
	} {
		i := bytes.Index(data, enc.key)
		if i < 0 {
			continue
		}
		value := strings.ToLower(firstLine(data[i+len(enc.key):]))
		switch value {
		case "windows":
			return decodeWith(charmap.Windows1251, data)
		case "dos":
			return decodeWith(charmap.CodePage866, data)
		default:
			return enc.dec(data)
		}
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return decodeWith(charmap.Windows1251, data)
}

func encode(cm *charmap.Charmap, s string) []byte {
	b, _ := cm.NewEncoder().Bytes([]byte(s))
	return b
}

func decodeWith(cm *charmap.Charmap, b []byte) string {
	out, _ := cm.NewDecoder().Bytes(b)
	return string(out)
}

func lines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// parse1C — формат 1CClientBankExchange (08 разд. 7.1).
func parse1C(text, ownAccount string) (Statement, error) {
	st := Statement{Format: Format1C}
	var (
		errs  []string
		doc   map[string]string
		start int
		docs  []struct {
			fields map[string]string
			line   int
		}
	)
	for i, raw := range lines(text) {
		n := i + 1
		s := strings.TrimSpace(raw)
		if s == "" || n == 1 {
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			if s == "КонецДокумента" && doc != nil {
				docs = append(docs, struct {
					fields map[string]string
					line   int
				}{doc, start})
				doc = nil
			}
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case key == "СекцияДокумент":
			if doc != nil {
				errs = append(errs, fmt.Sprintf("Строка %d: документ, начатый в строке %d, не закрыт строкой «КонецДокумента»", n, start))
			}
			doc, start = map[string]string{}, n
		case doc != nil:
			doc[key] = value
		default:
			switch key {
			case "РасчСчет":
				if st.Account == "" {
					st.Account = value
				}
			case "ДатаНачала":
				if d, err := parseDate(value); err == nil {
					st.DateFrom = &d
				}
			case "ДатаКонца":
				if d, err := parseDate(value); err == nil {
					st.DateTo = &d
				}
			}
		}
	}
	if doc != nil {
		errs = append(errs, fmt.Sprintf("Строка %d: документ не закрыт строкой «КонецДокумента»", start))
	}
	if ownAccount == "" {
		ownAccount = st.Account
	}
	st.Total = len(docs)
	for i, d := range docs {
		if ownAccount == "" || d.fields["ПолучательСчет"] != ownAccount {
			continue // исходящий или чужой документ: считается в lines_total, не обрабатывается
		}
		l := Line{
			No: i + 1, DocNumber: d.fields["Номер"], PayerAccount: d.fields["ПлательщикСчет"],
			PayerINN: d.fields["ПлательщикИНН"], Purpose: d.fields["НазначениеПлатежа"], PayerName: d.fields["Плательщик"],
		}
		if l.PayerName == "" {
			l.PayerName = d.fields["Плательщик1"]
		}
		docDate, err := parseDate(d.fields["Дата"])
		if err != nil {
			errs = append(errs, fmt.Sprintf("Строка %d: неверная дата документа «%s» (нужно ДД.ММ.ГГГГ)", d.line, d.fields["Дата"]))
			continue
		}
		l.DocDate, l.ReceivedOn = &docDate, docDate
		if v := d.fields["ДатаПоступило"]; v != "" {
			if l.ReceivedOn, err = parseDate(v); err != nil {
				errs = append(errs, fmt.Sprintf("Строка %d: неверная дата поступления «%s» (нужно ДД.ММ.ГГГГ)", d.line, v))
				continue
			}
		}
		if l.AmountKop, err = parseAmount(d.fields["Сумма"], false); err != nil {
			errs = append(errs, fmt.Sprintf("Строка %d: %s", d.line, err))
			continue
		}
		st.Incoming = append(st.Incoming, l)
	}
	if len(errs) > 0 {
		return Statement{}, &ParseError{Messages: errs}
	}
	return st, nil
}

// parseCSV — формат CSV (08 разд. 7.2): все строки входящие.
func parseCSV(text string) (Statement, error) {
	st := Statement{Format: FormatCSV}
	var errs []string
	for i, raw := range lines(text) {
		n := i + 1
		if n == 1 || strings.TrimSpace(raw) == "" {
			continue
		}
		f := strings.Split(raw, ";")
		if len(f) != 7 {
			errs = append(errs, fmt.Sprintf("Строка %d: ожидается 7 полей через «;», найдено %d", n, len(f)))
			continue
		}
		for j := range f {
			f[j] = strings.TrimSpace(f[j])
		}
		st.Total++
		l := Line{No: st.Total, PayerName: f[2], PayerINN: f[3], PayerAccount: f[4], Purpose: f[5], DocNumber: f[6]}
		var err error
		if l.ReceivedOn, err = parseDate(f[0]); err != nil {
			errs = append(errs, fmt.Sprintf("Строка %d: неверная дата «%s» (нужно ДД.ММ.ГГГГ)", n, f[0]))
		}
		if l.AmountKop, err = parseAmount(f[1], true); err != nil {
			errs = append(errs, fmt.Sprintf("Строка %d: %s", n, err))
		}
		if l.PayerINN != "" && !innRe.MatchString(l.PayerINN) {
			errs = append(errs, fmt.Sprintf("Строка %d: ИНН плательщика — 10 или 12 цифр", n))
		}
		st.Incoming = append(st.Incoming, l)
	}
	if len(errs) > 0 {
		return Statement{}, &ParseError{Messages: errs}
	}
	if st.Total == 0 {
		return Statement{}, &ParseError{Messages: []string{"В файле нет строк с платежами"}}
	}
	// Период выписки CSV — по датам поступления строк.
	for _, l := range st.Incoming {
		d := l.ReceivedOn
		if st.DateFrom == nil || d.Before(*st.DateFrom) {
			st.DateFrom = &d
		}
		if st.DateTo == nil || d.After(*st.DateTo) {
			st.DateTo = &d
		}
	}
	return st, nil
}

var (
	innRe    = regexp.MustCompile(`^\d{10}(\d{2})?$`)
	amountRe = regexp.MustCompile(`^(\d+)(?:\.(\d{1,2}))?$`)
)

func parseDate(s string) (date.Date, error) {
	t, err := time.Parse("02.01.2006", strings.TrimSpace(s))
	if err != nil {
		return date.Date{}, err
	}
	return date.FromTime(t), nil
}

// parseAmount — сумма в копейках без float: в 1С разделитель — точка; в CSV — запятая или точка, пробелы внутри
// допускаются («26 500,00»).
func parseAmount(s string, csv bool) (int64, error) {
	v := strings.TrimSpace(s)
	if csv {
		v = strings.NewReplacer(" ", "", " ", "", " ", "").Replace(v)
		if strings.Count(v, ",") == 1 && !strings.Contains(v, ".") {
			v = strings.Replace(v, ",", ".", 1)
		}
	}
	m := amountRe.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("неверная сумма «%s»", s)
	}
	rub, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || rub > 1e13 {
		return 0, fmt.Errorf("неверная сумма «%s»", s)
	}
	frac := (m[2] + "00")[:2]
	kop, _ := strconv.ParseInt(frac, 10, 64)
	total := rub*100 + kop
	if total <= 0 {
		return 0, fmt.Errorf("сумма должна быть больше нуля: «%s»", s)
	}
	return total, nil
}

// Fingerprint — отпечаток строки (08 разд. 7.3): sha256(received_on | amount_kop | doc_number | payer_account |
// normalized(purpose)), где normalized — нижний регистр и схлопнутые пробелы.
func Fingerprint(l Line) string {
	purpose := strings.Join(strings.Fields(strings.ToLower(l.Purpose)), " ")
	s := strings.Join([]string{l.ReceivedOn.String(), strconv.FormatInt(l.AmountKop, 10), l.DocNumber, l.PayerAccount, purpose}, "|")
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
