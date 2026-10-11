package document

import (
	"fmt"
	"html"
	"strings"

	"github.com/shopspring/decimal"

	"landlord/backend/internal/platform/date"
	"landlord/backend/internal/platform/money"
)

var monthsGen = [...]string{"", "января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября",
	"октября", "ноября", "декабря"}

// dateWords — «05» марта 2026 г.
func dateWords(d date.Date) string {
	return fmt.Sprintf("«%02d» %s %d г.", d.Day(), monthsGen[d.Month()], d.Year())
}

func fmtDate(d *date.Date) string {
	if d == nil {
		return ""
	}
	return d.Format()
}

func fmtDateWords(d *date.Date) string {
	if d == nil {
		return ""
	}
	return dateWords(*d)
}

func val(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// decText — число с запятой без лишних нулей: 24.50 → «24,5», 0.1000 → «0,1».
func decText(d decimal.Decimal) string {
	s := d.String()
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return strings.ReplaceAll(s, ".", ",")
}

// amountText — «28 000,00 руб. (двадцать восемь тысяч рублей 00 копеек)».
func amountText(k int64) string {
	return money.Kop(k).FormatRub() + " (" + money.ToWordsRu(money.Kop(k)) + ")"
}

// phoneText — «+7 916 123-45-67» из «+79161234567».
func phoneText(p *string) string {
	v := val(p)
	if len(v) != 12 || !strings.HasPrefix(v, "+7") {
		return v
	}
	return fmt.Sprintf("+7 %s %s-%s-%s", v[2:5], v[5:8], v[8:10], v[10:12])
}

// joinLines — многострочное значение без пустых строк.
func joinLines(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// roles — названия сторон и платы по виду договора (09 разд. 5.2: для ch34 — «Арендатор/Арендодатель/арендная плата»).
type roles struct {
	tenant, tenantDat, tenantGen string // Наниматель / Нанимателю / Нанимателя
	landlord, landlordDat        string // Наймодатель / Наймодателю
	rentGen, rentIns, rentAcc    string // внесения платы / с платой / плату
	rentNom                      string // Размер платы
	rentPrep                     string // в плату (ЖКУ включена в …)
	tenantLower                  string // нанимателя (кому направляется чек)
}

func rolesOf(basis string) roles {
	if basis == "ch34" {
		return roles{"Арендатор", "Арендатору", "Арендатора", "Арендодатель", "Арендодателю",
			"арендной платы", "с арендной платой", "арендную плату", "Размер арендной платы", "в арендную плату", "арендатора"}
	}
	return roles{"Наниматель", "Нанимателю", "Нанимателя", "Наймодатель", "Наймодателю",
		"платы", "с платой", "плату", "Размер платы", "в плату за наём", "нанимателя"}
}

// contractTerms — данные договора для текстовых условий.
type contractTerms struct {
	basis                    string
	start, end, firstDue     date.Date
	rentKop, depositKop      int64
	utilitiesMode            string
	utilitiesFixedKop        int64
	penaltyEnabled           bool
	penaltyRate, penaltyCap  *decimal.Decimal
	penaltyGrace             int
	indexationEnabled        bool
	indexationPct            *decimal.Decimal
	indexationMonths         int
	nextIndexation           *date.Date
	registrationRequired     bool
	fiscalMode, specialTerms string
}

func depositText(t contractTerms) string {
	if t.depositKop <= 0 {
		return "Обеспечительный платёж не предусмотрен."
	}
	r := rolesOf(t.basis)
	return fmt.Sprintf("В обеспечение исполнения обязательств по договору %s вносит обеспечительный платёж в размере %s "+
		"не позднее %s (ст. 381.1 ГК РФ).", r.tenant, amountText(t.depositKop), t.firstDue.Format())
}

func utilitiesText(t contractTerms) string {
	r := rolesOf(t.basis)
	switch t.utilitiesMode {
	case "meters":
		fixed := ""
		if t.utilitiesFixedKop > 0 {
			fixed = fmt.Sprintf(", а также фиксированную часть в размере %s в месяц", money.Kop(t.utilitiesFixedKop).FormatRub())
		}
		return fmt.Sprintf("%s компенсирует %s стоимость коммунальных услуг по показаниям приборов учёта по тарифам, "+
			"установленным для ресурсоснабжающих организаций%s. Сумма компенсации за месяц вносится вместе %s "+
			"за следующий месяц.", r.tenant, r.landlordDat, fixed, r.rentIns)
	case "fixed":
		return fmt.Sprintf("%s ежемесячно компенсирует %s стоимость жилищно-коммунальных услуг в фиксированной сумме %s "+
			"одновременно %s.", r.tenant, r.landlordDat, money.Kop(t.utilitiesFixedKop).FormatRub(), r.rentIns)
	default:
		return fmt.Sprintf("Плата за жилищно-коммунальные услуги включена %s.", r.rentPrep)
	}
}

func penaltyText(t contractTerms) string {
	r := rolesOf(t.basis)
	if !t.penaltyEnabled || t.penaltyRate == nil {
		return fmt.Sprintf("Пени за просрочку внесения %s договором не установлены.", r.rentGen)
	}
	capText := ""
	if t.penaltyCap != nil {
		capText = fmt.Sprintf(", но не более %s %% от просроченной суммы", decText(*t.penaltyCap))
	}
	return fmt.Sprintf("За просрочку внесения %s %s уплачивает пени в размере %s %% от просроченной суммы за каждый день "+
		"просрочки начиная с %d-го дня после установленного срока%s.", r.rentGen, r.tenant, decText(*t.penaltyRate),
		t.penaltyGrace+1, capText)
}

func indexationText(t contractTerms) string {
	r := rolesOf(t.basis)
	if !t.indexationEnabled || t.indexationPct == nil {
		return r.rentNom + " изменяется по соглашению сторон."
	}
	months := money.Plural(int64(t.indexationMonths), "месяц", "месяца", "месяцев")
	first := ""
	if t.nextIndexation != nil {
		first = " Первое изменение — не ранее " + t.nextIndexation.Format() + "."
	}
	return fmt.Sprintf("%s может быть увеличен %s не чаще одного раза в %d %s, но не более чем на %s %% в год, "+
		"путём заключения дополнительного соглашения.%s", r.rentNom, landlordIns(t.basis), t.indexationMonths, months,
		decText(*t.indexationPct), first)
}

func landlordIns(basis string) string {
	if basis == "ch34" {
		return "Арендодателем"
	}
	return "Наймодателем"
}

func registrationText(t contractTerms) string {
	if !t.registrationRequired {
		return "Договор заключён на срок менее одного года и государственной регистрации не подлежит."
	}
	if t.basis == "ch34" {
		return "Договор подлежит государственной регистрации (ст. 651 ГК РФ)."
	}
	return "Ограничение (обременение) права собственности на помещение, возникающее на основании договора, подлежит " +
		"государственной регистрации (п. 2 ст. 674 ГК РФ)."
}

func renewalText(t contractTerms) string {
	switch {
	case t.basis == "ch34":
		return "Арендатор, надлежащим образом исполнявший обязанности, имеет преимущественное право на заключение " +
			"договора на новый срок (ст. 621 ГК РФ)."
	case t.registrationRequired:
		return "Не позднее чем за три месяца до истечения срока договора Наймодатель предлагает Нанимателю заключить " +
			"договор на тех же или иных условиях либо предупреждает Нанимателя об отказе от продления договора (ст. 684 ГК РФ)."
	default:
		return "Правила ст. 684 ГК РФ о преимущественном праве Нанимателя на заключение договора на новый срок к договору " +
			"не применяются (п. 2 ст. 683 ГК РФ)."
	}
}

func fiscalText(t contractTerms) string {
	r := rolesOf(t.basis)
	switch t.fiscalMode {
	case "cloud_kkt":
		return fmt.Sprintf("При каждом расчёте формируется кассовый чек, который направляется на электронную почту %s "+
			"(Федеральный закон № 54-ФЗ).", r.tenantLower)
	case "npd_manual":
		return "При каждом расчёте формируется чек в приложении «Мой налог» (Федеральный закон № 422-ФЗ)."
	default:
		return fmt.Sprintf("Подтверждением оплаты служит квитанция, формируемая в информационной системе %s.",
			landlordGen(t.basis))
	}
}

func landlordGen(basis string) string {
	if basis == "ch34" {
		return "Арендодателя"
	}
	return "Наймодателя"
}

// contractTitle — вид договора в именительном падеже: «Основание: договор найма жилого помещения № …» (09 разд. 4.2).
func contractTitle(basis string) string {
	if basis == "ch34" {
		return "договор аренды"
	}
	return "договор найма жилого помещения"
}

// contractTitleDative — вид договора в дательном падеже: «к договору найма жилого помещения № …», «по договору аренды»
// (docs/open-questions.md, № 34).
func contractTitleDative(basis string) string {
	if basis == "ch34" {
		return "договору аренды"
	}
	return "договору найма жилого помещения"
}

// contractName — название документа договора.
func contractName(basis string) string {
	if basis == "ch34" {
		return "Договор аренды"
	}
	return "Договор найма"
}

func descriptionText(kind string, rooms *int16) string {
	if kind != "residential" {
		return "нежилое помещение"
	}
	switch {
	case rooms == nil:
		return "квартира"
	case *rooms == 0:
		return "квартира-студия"
	default:
		return fmt.Sprintf("%d-комнатная квартира", *rooms)
	}
}

func furnishedText(f bool) string {
	if f {
		return "с мебелью и бытовой техникой согласно акту приёма-передачи"
	}
	return "без мебели"
}

func usageText(basis, propertyKind string) string {
	switch {
	case propertyKind != "residential":
		return "для использования в предпринимательской деятельности Арендатора"
	case basis == "ch34":
		return "для проживания граждан (работников Арендатора) (п. 2 ст. 671 ГК РФ)"
	default:
		return "для проживания"
	}
}

var meterTitles = map[string]string{
	"cold_water": "Холодная вода", "hot_water": "Горячая вода", "electricity": "Электроэнергия",
	"electricity_day": "Электроэнергия (день)", "electricity_night": "Электроэнергия (ночь)", "gas": "Газ", "heating": "Отопление",
}

// ---------------------------------------------------------------- Блоки-таблицы

func esc(s string) string { return html.EscapeString(s) }

func cell(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<td></td>"
	}
	return "<td>" + strings.ReplaceAll(esc(s), "\n", "<br>") + "</td>"
}

func numCell(s string) string { return `<td class="num">` + esc(s) + "</td>" }

func table(head []string, rows []string) string {
	var b strings.Builder
	b.WriteString(`<table class="grid"><thead><tr>`)
	for _, h := range head {
		b.WriteString("<th>" + esc(h) + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for _, r := range rows {
		b.WriteString("<tr>" + r + "</tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

type occupantRow struct{ name, birth, relation string }

func occupantsBlock(list []occupantRow) string {
	if len(list) == 0 {
		return "<p>Иные граждане совместно с Нанимателем не проживают.</p>"
	}
	rows := make([]string, 0, len(list))
	for _, o := range list {
		rows = append(rows, cell(o.name)+cell(o.birth)+cell(o.relation))
	}
	return table([]string{"ФИО", "Дата рождения", "Кем приходится"}, rows)
}

type inventoryRow struct {
	name      string
	quantity  int
	condition string
	note      string
}

func inventoryBlock(list []inventoryRow) string {
	if len(list) == 0 {
		return "<p>Имущество не передаётся.</p>"
	}
	rows := make([]string, 0, len(list))
	for i, it := range list {
		rows = append(rows, numCell(fmt.Sprint(i+1))+cell(it.name)+numCell(fmt.Sprint(it.quantity))+cell(it.condition)+cell(it.note))
	}
	return table([]string{"№", "Наименование", "Кол-во", "Состояние", "Примечание"}, rows)
}

type meterRow struct{ title, serial, value string }

func metersBlock(list []meterRow) string {
	if len(list) == 0 {
		return "<p>Приборы учёта отсутствуют.</p>"
	}
	rows := make([]string, 0, len(list))
	for _, m := range list {
		v := m.value
		if v == "" {
			v = "________"
		}
		rows = append(rows, cell(m.title)+cell(m.serial)+numCell(v))
	}
	return table([]string{"Прибор учёта", "Заводской номер", "Показание"}, rows)
}

type paymentLine struct {
	title, period string
	amountKop     int64
}

func paymentLinesBlock(lines []paymentLine, totalKop int64) string {
	rows := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		rows = append(rows, cell(l.title)+cell(l.period)+numCell(money.Kop(l.amountKop).FormatNumber()))
	}
	rows = append(rows, `<td colspan="2"><b>Итого</b></td>`+numCell(money.Kop(totalKop).FormatNumber()))
	return table([]string{"Назначение", "Период", "Сумма, руб."}, rows)
}

type reconRow struct {
	date, doc     string
	charged, paid int64
}

func reconBlock(list []reconRow) string {
	rows := make([]string, 0, len(list)+1)
	var charged, paid int64
	kop := func(v int64) string {
		if v == 0 {
			return ""
		}
		return money.Kop(v).FormatNumber()
	}
	for _, r := range list {
		charged += r.charged
		paid += r.paid
		rows = append(rows, cell(r.date)+cell(r.doc)+numCell(kop(r.charged))+numCell(kop(r.paid)))
	}
	rows = append(rows, `<td colspan="2"><b>Обороты за период</b></td>`+numCell(money.Kop(charged).FormatNumber())+
		numCell(money.Kop(paid).FormatNumber()))
	return table([]string{"Дата", "Документ", "Начислено, руб.", "Оплачено, руб."}, rows)
}

// saldoText — «Сальдо на 01.01.2026: задолженность Нанимателя 0,00 руб.» / «переплата Нанимателя …».
func saldoText(on date.Date, basis string, saldo int64) string {
	r := rolesOf(basis)
	if saldo < 0 {
		return fmt.Sprintf("Сальдо на %s: переплата %s %s.", on.Format(), r.tenantGen, money.Kop(-saldo).FormatRub())
	}
	return fmt.Sprintf("Сальдо на %s: задолженность %s %s.", on.Format(), r.tenantGen, money.Kop(saldo).FormatRub())
}

// baseCSS — базовые стили документа (09 разд. 3).
const baseCSS = `@page { size: A4; }
body { font-family: "Liberation Serif", "Times New Roman", serif; font-size: 12pt; line-height: 1.35; color: #000; }
h1 { font-size: 13pt; text-align: center; font-weight: bold; text-transform: uppercase; margin: 0 0 4pt; }
h2 { font-size: 12pt; font-weight: bold; margin: 10pt 0 4pt; }
p { margin: 0 0 4pt; text-align: justify; }
.center { text-align: center; } .right { text-align: right; }
.place-date { display: flex; justify-content: space-between; margin: 8pt 0 10pt; }
table { border-collapse: collapse; width: 100%; }
table.grid th, table.grid td { border: 0.5pt solid #000; padding: 2pt 4pt; font-size: 11pt; vertical-align: top; }
table.grid th { font-weight: bold; text-align: center; }
table.grid td.num { text-align: right; white-space: nowrap; }
table.sign td { width: 50%; vertical-align: top; padding: 4pt 8pt 4pt 0; font-size: 11pt; }
.sign-line { margin-top: 18pt; }`

// wrap — полный HTML-документ для Gotenberg.
func wrap(title, body string) string {
	return `<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8"><title>` + esc(title) + `</title><style>` +
		baseCSS + `</style></head><body>` + body + `</body></html>`
}

// emptyValue — пустое значение в документе: строка для заполнения от руки (09 разд. 2).
const emptyValue = "________________"
