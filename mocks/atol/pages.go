package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"
)

const banner = "Учебный эмулятор АТОЛ Онлайн. Чеки не передаются в налоговую"

// moscow — часовой пояс отметок времени (tzdata встроена в бинарник, main.go).
var moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}()

// money — «45 000,00».
func money(kop int64) string {
	rub := strconv.FormatInt(kop/100, 10)
	var b strings.Builder
	for i, r := range rub {
		if i > 0 && (len(rub)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String() + "," + strconv.FormatInt(kop%100/10, 10) + strconv.FormatInt(kop%10, 10)
}

var snoTitles = map[string]string{
	"osn": "ОСН", "usn_income": "УСН доход", "usn_income_outcome": "УСН доход − расход", "esn": "ЕСХН", "patent": "ПСН",
}

var paymentMethodTitles = map[string]string{
	"full_prepayment": "Предоплата 100 %", "prepayment": "Предоплата", "advance": "Аванс", "full_payment": "Полный расчёт",
	"partial_payment": "Частичный расчёт и кредит", "credit": "Передача в кредит", "credit_payment": "Оплата кредита",
}

var paymentTypeTitles = map[string]string{
	"0": "Наличными", "1": "Безналичными", "2": "Предварительная оплата (зачёт аванса)", "3": "Последующая оплата (кредит)",
	"4": "Иная форма оплаты",
}

var funcs = template.FuncMap{
	"money": money,
	"inc":   func(i int) int { return i + 1 },
	"dt": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.In(moscow).Format("02.01.2006 15:04:05")
	},
	"statusTitle": func(s string) string {
		switch s {
		case StatusWait:
			return "Обрабатывается"
		case StatusDone:
			return "Сформирован"
		case StatusFail:
			return "Ошибка"
		}
		return s
	},
	"opTitle": func(op string) string {
		if op == "sell_refund" {
			return "Возврат прихода"
		}
		return "Приход"
	},
	"callbackState": func(d Doc) string {
		switch {
		case d.Status == StatusWait:
			return "—"
		case d.CallbackOff:
			return "не отправлялся"
		case len(d.Callbacks) == 0:
			return "отправляется"
		}
		last := d.Callbacks[len(d.Callbacks)-1]
		return "попыток " + strconv.Itoa(len(d.Callbacks)) + ", " + last.Result
	},
}

const layout = `{{define "top"}}<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title>
<style>
body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;color:#1f2937;background:#f4f6f8}
.banner{background:#fde68a;padding:8px 16px;font-weight:600;text-align:center}
main{max-width:1100px;margin:0 auto;padding:16px}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:8px;padding:16px;margin-bottom:16px}
h1{font-size:22px;margin:0 0 8px}.muted{color:#6b7280}
label{margin-right:16px;font-size:14px}input[type=number]{padding:6px;border:1px solid #d1d5db;border-radius:6px;width:110px}
button{padding:8px 14px;border:0;border-radius:6px;font-size:14px;cursor:pointer;background:#e30613;color:#fff;font-weight:600}
table{border-collapse:collapse;width:100%;background:#fff;font-size:14px}th,td{border-bottom:1px solid #e5e7eb;padding:6px 8px;text-align:left;vertical-align:top}
.st-done{color:#047857;font-weight:600}.st-fail{color:#b91c1c;font-weight:600}.st-wait{color:#92400e;font-weight:600}
a.link{color:#2563eb}
.receipt{max-width:380px;margin:0 auto;background:#fff;border:1px solid #d1d5db;padding:20px;font-family:"Courier New",monospace;font-size:13px}
.receipt h2{text-align:center;font-size:15px;margin:8px 0}.receipt .center{text-align:center;overflow-wrap:anywhere}
.receipt .row{display:flex;justify-content:space-between;gap:8px}.receipt hr{border:0;border-top:1px dashed #9ca3af;margin:8px 0}
.receipt .total{font-size:16px;font-weight:700}.receipt img{display:block;margin:12px auto;width:180px;height:180px;image-rendering:pixelated}
</style></head><body><div class="banner">` + banner + `</div><main>{{end}}
{{define "bottom"}}</main></body></html>{{end}}`

var listTmpl = template.Must(template.New("list").Funcs(funcs).Parse(layout + `{{template "top" .}}
<h1>Документы эмулятора</h1>
<form class="card" method="post" action="/admin/settings">
  <label><input type="checkbox" name="failNext" value="1"{{if .Settings.FailNext}} checked{{end}}> Следующий документ завершится ошибкой</label>
  <label>Задержка обработки, мс <input type="number" name="delayMs" min="0" max="600000" value="{{.Settings.DelayMs}}"></label>
  <label><input type="checkbox" name="noCallbacks" value="1"{{if .Settings.NoCallbacks}} checked{{end}}> Не отправлять колбэки</label>
  <button type="submit">Сохранить</button>
</form>
<table>
<thead><tr><th>Создан</th><th>uuid</th><th>external_id</th><th>Операция</th><th>Сумма, ₽</th><th>Статус</th><th>ФД / ФП</th><th>Колбэк</th></tr></thead>
<tbody>
{{range .Docs}}
<tr>
  <td>{{dt .CreatedAt}}</td>
  <td>{{if eq .Status "done"}}<a class="link" href="/receipt/{{.UUID}}">{{.UUID}}</a>{{else}}{{.UUID}}{{end}}</td>
  <td>{{.ExternalID}}</td><td>{{opTitle .Operation}}</td><td>{{money .TotalKop}}</td>
  <td class="st-{{.Status}}">{{statusTitle .Status}}{{with .Error}}<div class="muted">{{.Code}}: {{.Text}}</div>{{end}}</td>
  <td>{{with .Requisites}}{{.FiscalDocumentNumber}} / {{.FiscalDocumentAttribute}}{{else}}—{{end}}</td>
  <td>{{callbackState .}}</td>
</tr>
{{else}}<tr><td colspan="8" class="muted">Документов нет</td></tr>{{end}}
</tbody></table>
{{template "bottom"}}`))

var receiptTmpl = template.Must(template.New("receipt").Funcs(funcs).Parse(layout + `{{template "top" .}}
{{with .Receipt}}
<div class="receipt">
  <div class="center">Пользователь ККТ<br>ИНН {{.INN}}</div>
  <div class="center">Место расчётов: {{.Address}}</div>
  <h2>КАССОВЫЙ ЧЕК<br>{{.OperationTitle}}</h2>
  <hr>
  {{range $i, $it := .Items}}
  <div>{{inc $i}}. {{$it.Name}}</div>
  <div class="row"><span>{{$it.Qty}} × {{$it.Price}}</span><span>={{$it.Sum}}</span></div>
  <div class="muted">{{$it.Method}}, услуга, без НДС</div>
  {{end}}
  <hr>
  <div class="row total"><span>ИТОГ</span><span>={{.Total}}</span></div>
  {{range .Payments}}<div class="row"><span>{{.Title}}</span><span>={{.Sum}}</span></div>{{end}}
  <hr>
  <div class="row"><span>СНО</span><span>{{.Sno}}</span></div>
  <div class="row"><span>Эл. адрес покупателя</span><span>{{.Client}}</span></div>
  <div class="row"><span>Сайт ФНС</span><span>www.nalog.gov.ru</span></div>
  <div class="row"><span>Смена № {{.Shift}}</span><span>Чек № {{.Number}}</span></div>
  <div class="row"><span>Дата и время</span><span>{{.Datetime}}</span></div>
  <div class="row"><span>РН ККТ</span><span>{{.Ecr}}</span></div>
  <div class="row"><span>ФН</span><span>{{.Fn}}</span></div>
  <div class="row"><span>ФД</span><span>{{.Fd}}</span></div>
  <div class="row"><span>ФП</span><span>{{.Fp}}</span></div>
  {{if .QR}}<img src="{{.QR}}" alt="QR-код чека">{{end}}
  <div class="center muted">{{.QRText}}</div>
</div>
{{end}}
{{template "bottom"}}`))

var msgTmpl = template.Must(template.New("msg").Funcs(funcs).Parse(layout + `{{template "top" .}}
<div class="card"><p>{{.Message}}</p><p><a class="link" href="/">К списку документов</a></p></div>{{template "bottom"}}`))

type listSettings struct {
	FailNext    bool
	DelayMs     int
	NoCallbacks bool
}

type receiptItem struct{ Name, Qty, Price, Sum, Method string }

type receiptPayment struct{ Title, Sum string }

// receiptView — данные страницы «кассового чека».
type receiptView struct {
	INN, Address, OperationTitle, Total, Sno, Client string
	Items                                            []receiptItem
	Payments                                         []receiptPayment
	Shift, Number                                    int
	Datetime, Ecr, Fn                                string
	Fd, Fp                                           int64
	QR                                               template.URL
	QRText                                           string
}

type pageData struct {
	Title    string
	Message  string
	Docs     []Doc
	Settings listSettings
	Receipt  receiptView
}

func render(w http.ResponseWriter, status int, t *template.Template, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, d)
}

func renderMessage(w http.ResponseWriter, status int, msg string) {
	render(w, status, msgTmpl, pageData{Title: banner, Message: msg})
}

func renderList(w http.ResponseWriter, docs []Doc, s listSettings) {
	render(w, http.StatusOK, listTmpl, pageData{Title: "Документы — " + banner, Docs: docs, Settings: s})
}

// fixedText — число из запроса в виде «45 000,00» (копейки) или «1,000» (количество).
func fixedText(v any, scale int) string {
	n, ok := parseFixed(v, scale)
	if !ok {
		return "?"
	}
	if scale == 2 {
		return money(n)
	}
	return strconv.FormatInt(n/1000, 10) + "," + strconv.FormatInt(1000+n%1000, 10)[1:]
}

// QRText — строка QR-кода чека: t=ГГГГММДДTЧЧММ&s=сумма&fn=…&i=ФД&fp=ФП&n=1 (n=2 — возврат прихода).
func QRText(d Doc) string {
	r := d.Requisites
	n := "1"
	if d.Operation == "sell_refund" {
		n = "2"
	}
	return "t=" + r.ReceiptDatetime.In(moscow).Format("20060102T1504") + "&s=" + string(kopNumber(d.TotalKop)) +
		"&fn=" + r.FnNumber + "&i=" + strconv.FormatInt(r.FiscalDocumentNumber, 10) +
		"&fp=" + strconv.FormatInt(r.FiscalDocumentAttribute, 10) + "&n=" + n
}

func renderReceipt(w http.ResponseWriter, d Doc) {
	dec := json.NewDecoder(bytes.NewReader(d.Request))
	dec.UseNumber()
	var m map[string]any
	_ = dec.Decode(&m)
	rcpt, _ := obj(m, "receipt")
	company, _ := obj(rcpt, "company")
	client, _ := obj(rcpt, "client")
	v := receiptView{
		INN: str(company, "inn"), Address: str(company, "payment_address"), OperationTitle: "ПРИХОД",
		Total: money(d.TotalKop), Sno: snoTitles[str(company, "sno")], Client: str(client, "email"),
		Shift: d.Requisites.ShiftNumber, Number: d.Requisites.FiscalReceiptNumber,
		Datetime: d.Requisites.ReceiptDatetime.In(moscow).Format("02.01.2006 15:04"), Ecr: d.Requisites.EcrRegistrationNumber,
		Fn: d.Requisites.FnNumber, Fd: d.Requisites.FiscalDocumentNumber, Fp: d.Requisites.FiscalDocumentAttribute,
		QRText: QRText(d),
	}
	if d.Operation == "sell_refund" {
		v.OperationTitle = "ВОЗВРАТ ПРИХОДА"
	}
	if v.Client == "" {
		v.Client = str(client, "phone")
	}
	items, _ := rcpt["items"].([]any)
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		v.Items = append(v.Items, receiptItem{
			Name: str(it, "name"), Qty: fixedText(it["quantity"], 3), Price: fixedText(it["price"], 2), Sum: fixedText(it["sum"], 2),
			Method: paymentMethodTitles[str(it, "payment_method")],
		})
	}
	payments, _ := rcpt["payments"].([]any)
	for _, raw := range payments {
		p, _ := raw.(map[string]any)
		t, _ := p["type"].(json.Number)
		v.Payments = append(v.Payments, receiptPayment{Title: paymentTypeTitles[string(t)], Sum: fixedText(p["sum"], 2)})
	}
	if code, err := qr.Encode(v.QRText, qr.M); err == nil {
		v.QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()))
	}
	render(w, http.StatusOK, receiptTmpl, pageData{Title: "Кассовый чек — " + banner, Receipt: v})
}
