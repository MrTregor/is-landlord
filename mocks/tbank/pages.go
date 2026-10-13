package main

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const banner = "Учебный эмулятор Т-Банк эквайринга. Деньги не списываются"

// moscow — часовой пояс отображения сроков (tzdata встроена в бинарник, main.go).
var moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}()

// money — «15 032,50 ₽».
func money(kop int64) string {
	sign := ""
	if kop < 0 {
		sign, kop = "−", -kop
	}
	rub := strconv.FormatInt(kop/100, 10)
	var b strings.Builder
	for i, r := range rub {
		if i > 0 && (len(rub)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return sign + b.String() + "," + strconv.FormatInt(kop%100/10, 10) + strconv.FormatInt(kop%10, 10) + " ₽"
}

var funcs = template.FuncMap{
	"money": money,
	"dt": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.In(moscow).Format("02.01.2006 15:04:05")
	},
	"statusTitle": func(s string) string {
		switch s {
		case StatusNew:
			return "Создан"
		case StatusFormShowed:
			return "Открыта страница оплаты"
		case StatusAuthorized:
			return "Авторизован"
		case StatusConfirmed:
			return "Оплачен"
		case StatusRejected:
			return "Отклонён"
		case StatusDeadlineExpired:
			return "Истёк срок"
		}
		return s
	},
	"lastAttempt": func(n *Notice) string {
		switch {
		case len(n.Attempts) == 0:
			return "ожидает отправки"
		case n.Delivered:
			return "доставлено"
		case n.GaveUp:
			return "не доставлено (" + n.Attempts[len(n.Attempts)-1].Result + ")"
		}
		return n.Attempts[len(n.Attempts)-1].Result + ", повтор по расписанию"
	},
}

const layout = `{{define "top"}}<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title>
<style>
body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;color:#1f2937;background:#f4f6f8}
.banner{background:#fde68a;padding:8px 16px;font-weight:600;text-align:center}
main{max-width:960px;margin:0 auto;padding:16px}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:8px;padding:16px;margin-bottom:16px}
h1{font-size:22px;margin:0 0 8px}h2{font-size:17px;margin:0 0 12px}
.sum{font-size:28px;font-weight:700;margin:8px 0}.muted{color:#6b7280}
label{display:block;margin:8px 0 4px;font-size:14px}
input[type=text],input[type=number]{padding:8px;border:1px solid #d1d5db;border-radius:6px;font-size:15px;width:100%;box-sizing:border-box}
.row{display:flex;gap:12px}.row>div{flex:1}
button{padding:10px 16px;border:0;border-radius:6px;font-size:15px;cursor:pointer;background:#ffdd2d;color:#111;font-weight:600}
button.secondary{background:#e5e7eb;font-weight:500}button.danger{background:#fee2e2;color:#991b1b}
.qr{width:140px;height:140px;border:2px dashed #9ca3af;display:flex;align-items:center;justify-content:center;text-align:center;margin:8px 0;color:#4b5563}
table{border-collapse:collapse;width:100%;background:#fff;font-size:14px}th,td{border-bottom:1px solid #e5e7eb;padding:6px 8px;text-align:left;vertical-align:top}
td form{display:inline}td button{padding:4px 8px;font-size:13px;margin:2px}
.st-CONFIRMED{color:#047857;font-weight:600}.st-REJECTED,.st-DEADLINE_EXPIRED{color:#b91c1c;font-weight:600}
a.link{color:#2563eb}
</style></head><body><div class="banner">` + banner + `</div><main>{{end}}
{{define "bottom"}}</main></body></html>{{end}}`

var payTmpl = template.Must(template.New("pay").Funcs(funcs).Parse(layout + `{{template "top" .}}
{{with .Payment}}
<div class="card">
  <h1>Оплата заказа</h1>
  <div>{{.Description}}</div>
  <div class="sum">{{money .Amount}}</div>
  <div class="muted">Заказ № {{.OrderID}} · ссылка действует до {{dt .RedirectDueDate}}</div>
</div>
{{if $.Open}}
<form method="post" action="/pay/{{.PaymentID}}/action">
<div class="card">
  <h2>Карта</h2>
  <label for="pan">Номер карты</label><input id="pan" type="text" name="pan" value="2200 0000 0000 0004">
  <div class="row"><div><label for="exp">Срок</label><input id="exp" type="text" name="exp" value="12/30"></div>
  <div><label for="cvc">CVC</label><input id="cvc" type="text" name="cvc" value="123"></div></div>
  <p><button type="submit" name="action" value="card_success">Оплатить {{money .Amount}}</button></p>
</div>
<div class="card">
  <h2>СБП</h2>
  <div class="qr">СБП, эмулятор</div>
  <button type="submit" name="action" value="sbp_success">Я оплатил в приложении банка</button>
</div>
<div class="card">
  <button type="submit" name="action" value="reject" class="danger">Отклонить: недостаточно средств</button>
  <button type="submit" name="action" value="back" class="secondary">Вернуться в магазин</button>
  <details style="margin-top:12px"><summary>Настройки эмуляции</summary>
    <label><input type="checkbox" name="notifyTwice" value="1"> Отправить уведомление дважды</label>
    <label for="delay">Задержка уведомления, с (0–120)</label>
    <input id="delay" type="number" name="notifyDelaySec" min="0" max="120" value="0" style="max-width:120px">
  </details>
</div>
</form>
{{else}}
<div class="card">
  {{if eq .Status "DEADLINE_EXPIRED"}}<h2>Срок оплаты истёк</h2><p>Вернитесь в личный кабинет и начните оплату заново.</p>
  {{else}}<h2>Платёж уже обработан</h2><p>Статус: <b class="st-{{.Status}}">{{statusTitle .Status}}</b></p>{{end}}
  <p><a class="link" href="{{.FailURL}}">Вернуться в магазин</a></p>
</div>
{{end}}
{{end}}
{{template "bottom"}}`))

var listTmpl = template.Must(template.New("list").Funcs(funcs).Parse(layout + `{{template "top" .}}
<h1>Платежи эмулятора</h1>
<p class="muted">Повтор уведомления по оплаченному платежу показывает идемпотентность: в системе дубль не появляется.</p>
<form method="post" action="/admin/reset" onsubmit="return confirm('Очистить все платежи эмулятора?')">
  <button class="danger" type="submit">Очистить всё</button></form>
<p></p>
<table>
<thead><tr><th>PaymentId</th><th>OrderId</th><th>Сумма</th><th>Статус</th><th>Создан</th><th>Уведомления</th><th>Действия</th></tr></thead>
<tbody>
{{range .Payments}}
<tr>
  <td><a class="link" href="/pay/{{.PaymentID}}">{{.PaymentID}}</a></td>
  <td>{{.OrderID}}</td><td>{{money .Amount}}</td>
  <td class="st-{{.Status}}">{{statusTitle .Status}}<div class="muted">{{.Status}}</div></td>
  <td>{{dt .CreatedAt}}</td>
  <td>{{range .Notices}}<div>{{.Status}}: попыток {{len .Attempts}} — {{lastAttempt .}}</div>{{else}}<span class="muted">нет</span>{{end}}</td>
  <td>
    <form method="post" action="/admin/{{.PaymentID}}/resend"><button type="submit">Повторить уведомление</button></form>
    <form method="post" action="/admin/{{.PaymentID}}/status"><input type="hidden" name="status" value="CONFIRMED"><button type="submit">CONFIRMED</button></form>
    <form method="post" action="/admin/{{.PaymentID}}/status"><input type="hidden" name="status" value="REJECTED"><button type="submit">REJECTED</button></form>
    <form method="post" action="/admin/{{.PaymentID}}/status"><input type="hidden" name="status" value="DEADLINE_EXPIRED"><button type="submit">Истёк срок</button></form>
  </td>
</tr>
{{else}}<tr><td colspan="7" class="muted">Платежей нет</td></tr>{{end}}
</tbody></table>
{{template "bottom"}}`))

var msgTmpl = template.Must(template.New("msg").Funcs(funcs).Parse(layout + `{{template "top" .}}
<div class="card"><p>{{.Message}}</p></div>{{template "bottom"}}`))

type pageData struct {
	Title    string
	Message  string
	Payment  Payment
	Open     bool
	Payments []Payment
}

func render(w http.ResponseWriter, status int, t *template.Template, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = t.Execute(w, d)
}

func renderPage(w http.ResponseWriter, status int, title string, d pageData) {
	d.Title = title
	render(w, status, msgTmpl, d)
}

func renderPayPage(w http.ResponseWriter, p Payment) {
	open := p.Status == StatusNew || p.Status == StatusFormShowed
	render(w, http.StatusOK, payTmpl, pageData{Title: "Оплата заказа — " + banner, Payment: p, Open: open})
}

func renderList(w http.ResponseWriter, list []Payment) {
	render(w, http.StatusOK, listTmpl, pageData{Title: "Платежи — " + banner, Payments: list})
}
