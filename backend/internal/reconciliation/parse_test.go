package reconciliation

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

const ownAccount = "40802810900000012345"

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	require.NoError(t, err)
	return b
}

// 1С в Windows-1251: 5 документов → 4 входящих (FR-07.2).
func TestParse1CWin1251_FR_07_2(t *testing.T) {
	st, err := Parse(readTestdata(t, "statement_1c_win1251.txt"), ownAccount)
	require.NoError(t, err)
	assert.Equal(t, Format1C, st.Format)
	assert.Equal(t, 5, st.Total)
	require.Len(t, st.Incoming, 4)
	assert.Equal(t, ownAccount, st.Account)
	assert.Equal(t, "2026-10-01", st.DateFrom.String())
	assert.Equal(t, "2026-10-14", st.DateTo.String())

	l := st.Incoming[1]
	assert.Equal(t, 2, l.No)
	assert.Equal(t, "ООО «Ромашка»", l.PayerName)
	assert.Equal(t, "5075123457", l.PayerINN)
	assert.Equal(t, int64(4500000), l.AmountKop)
	assert.Equal(t, "2026-10-01", l.DocDate.String())
	assert.Equal(t, "2026-10-02", l.ReceivedOn.String(), "дата поступления — ДатаПоступило")
	assert.Equal(t, "318", l.DocNumber)
}

// 1С в UTF-8 (строки «Кодировка=» нет) и в CP866 разбираются так же (FR-07.2).
func TestParse1CEncodings_FR_07_2(t *testing.T) {
	win := readTestdata(t, "statement_1c_win1251.txt")
	text, err := charmap.Windows1251.NewDecoder().Bytes(win)
	require.NoError(t, err)
	utf := strings.Replace(string(text), "Кодировка=Windows\r\n", "", 1)
	st, err := Parse([]byte(utf), ownAccount)
	require.NoError(t, err)
	assert.Equal(t, 5, st.Total)
	require.Len(t, st.Incoming, 4)
	assert.Equal(t, "Смирнова Анна Сергеевна", st.Incoming[0].PayerName)

	// В CP866 нет кавычек-ёлочек и знака номера.
	dosText := strings.NewReplacer("«", `"`, "»", `"`, "№", "N", "Кодировка=Windows", "Кодировка=DOS").Replace(string(text))
	dos, err := charmap.CodePage866.NewEncoder().Bytes([]byte(dosText))
	require.NoError(t, err)
	st, err = Parse(dos, ownAccount)
	require.NoError(t, err)
	require.Len(t, st.Incoming, 4)
	assert.Equal(t, "Иванова Т. В.", st.Incoming[3].PayerName)
}

// Реквизит счёта организации не заполнен — входящие по РасчСчет заголовка.
func TestParse1CHeaderAccount_FR_07_2(t *testing.T) {
	st, err := Parse(readTestdata(t, "statement_1c_win1251.txt"), "")
	require.NoError(t, err)
	assert.Len(t, st.Incoming, 4)
}

// CSV: «26 500,00» → 2650000, все строки входящие (FR-07.2).
func TestParseCSV_FR_07_2(t *testing.T) {
	st, err := Parse(readTestdata(t, "statement.csv"), ownAccount)
	require.NoError(t, err)
	assert.Equal(t, FormatCSV, st.Format)
	assert.Equal(t, 4, st.Total)
	require.Len(t, st.Incoming, 4)
	assert.Equal(t, int64(2800000), st.Incoming[0].AmountKop)
	assert.Equal(t, int64(2650000), st.Incoming[2].AmountKop)
	assert.Equal(t, int64(1200000), st.Incoming[3].AmountKop)
	assert.Equal(t, "2026-10-02", st.DateFrom.String())
	assert.Equal(t, "2026-10-12", st.DateTo.String())
	assert.Nil(t, st.Incoming[0].DocDate)
}

func TestParseCSVBOM_FR_07_2(t *testing.T) {
	st, err := Parse(append([]byte("\xEF\xBB\xBF"), readTestdata(t, "statement.csv")...), "")
	require.NoError(t, err)
	assert.Len(t, st.Incoming, 4)
}

// Ошибки формата — с номером строки; файл не загружается частично (FR-07.2).
func TestParseCSVErrors_FR_07_2(t *testing.T) {
	body := CSVHeader + "\n" +
		"10.10.2026;28 000,00;Смирнова;;;Оплата;1\n" +
		"32.10.2026;100;Иванов;;;Оплата;2\n" +
		"11.10.2026;1.234,5;Петров;;;Оплата;3\n" +
		"11.10.2026;0;Петров;;;Оплата;4\n" +
		"11.10.2026;100;Петров;123;;Оплата;5\n" +
		"11.10.2026;100;Петров\n"
	_, err := Parse([]byte(body), "")
	var pe *ParseError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, []string{
		"Строка 3: неверная дата «32.10.2026» (нужно ДД.ММ.ГГГГ)",
		"Строка 4: неверная сумма «1.234,5»",
		"Строка 5: сумма должна быть больше нуля: «0»",
		"Строка 6: ИНН плательщика — 10 или 12 цифр",
		"Строка 7: ожидается 7 полей через «;», найдено 3",
	}, pe.Messages)
}

func TestParseUnknownFormat_FR_07_2(t *testing.T) {
	_, err := Parse([]byte("Дата,Сумма\n01.10.2026,100\n"), "")
	var pe *ParseError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, pe.Messages[0], "Формат файла не распознан")
}

func TestParse1CErrors_FR_07_2(t *testing.T) {
	body := "1CClientBankExchange\nКодировка=UTF8\nРасчСчет=" + ownAccount + "\n" +
		"СекцияДокумент=Платежное поручение\nНомер=1\nДата=01.10.2026\nСумма=10,5\nПолучательСчет=" + ownAccount + "\nКонецДокумента\n" +
		"СекцияДокумент=Платежное поручение\nНомер=2\nДата=2026-10-01\nСумма=10\nПолучательСчет=" + ownAccount + "\nКонецДокумента\n"
	_, err := Parse([]byte(body), "")
	var pe *ParseError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, []string{
		"Строка 4: неверная сумма «10,5»",
		"Строка 10: неверная дата документа «2026-10-01» (нужно ДД.ММ.ГГГГ)",
	}, pe.Messages)
}

func TestParseAmount_FR_07_2(t *testing.T) {
	for in, want := range map[string]int64{"26 500,00": 2650000, "26500.5": 2650050, "0,01": 1, "45000": 4500000, "1 000,10": 100010} {
		got, err := parseAmount(in, true)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"1.234,5", "-100", "10,123", "abc", "", "1,000,00"} {
		_, err := parseAmount(in, true)
		assert.Error(t, err, in)
	}
	_, err := parseAmount("10,50", false)
	assert.Error(t, err, "в 1С разделитель — точка")
}

// Отпечаток одинаков для одинаковых строк (регистр и пробелы назначения не важны) и различается по сумме.
func TestFingerprint_FR_07_2(t *testing.T) {
	a, err := Parse(readTestdata(t, "statement_1c_win1251.txt"), ownAccount)
	require.NoError(t, err)
	b, err := Parse(readTestdata(t, "statement.csv"), ownAccount)
	require.NoError(t, err)
	for i := range a.Incoming {
		assert.Equal(t, Fingerprint(a.Incoming[i]), Fingerprint(b.Incoming[i]), "строка %d в 1С и CSV", i+1)
	}
	l := a.Incoming[0]
	l2 := l
	l2.Purpose = "  ОПЛАТА по   договору № 2026-001 за октябрь 2026. без ндс "
	assert.Equal(t, Fingerprint(l), Fingerprint(l2))
	l2.AmountKop++
	assert.NotEqual(t, Fingerprint(l), Fingerprint(l2))
	assert.Len(t, Fingerprint(l), 64)
}
