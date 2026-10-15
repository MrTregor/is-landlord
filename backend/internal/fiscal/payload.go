package fiscal

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"landlord/backend/internal/platform/atol"
)

// DataError — данные для чека некорректны: чек сразу получает статус fail с этим текстом, без повторов (07, fiscal.send).
type DataError struct{ Text string }

func (e *DataError) Error() string { return e.Text }

// Тексты ошибок данных (08 разд. 3.2).
const (
	errNoContact = "У нанимателя нет e-mail или телефона для отправки чека"
	errVat       = "Чеки с НДС не поддерживаются"
	errNpd       = "При режиме налогообложения НПД кассовый чек не формируется: выберите для объекта режим «Чек НПД (вручную)»"
	errSums      = "Сумма позиций чека не совпадает с суммой платежа"
	errCompany   = "Не заполнены ИНН или e-mail организации: Настройки → Организация"
)

// Способы расчёта и признак предмета расчёта (08 разд. 3.2).
const (
	methodFullPayment = "full_payment"
	methodAdvance     = "advance"
	objectService     = 4
	maxItemName       = 128
)

// Типы оплаты (payments.type, 08 разд. 3.2).
const (
	PaymentCash     = 0 // наличные
	PaymentCashless = 1 // онлайн и перевод на счёт
	PaymentPrepaid  = 2 // зачёт из залога (предварительная оплата, зачёт аванса)
)

// snoByRegime — company.sno по режиму налогообложения организации (08 разд. 3.2).
var snoByRegime = map[string]string{
	"usn_income": "usn_income", "usn_income_expense": "usn_income_outcome", "osn": "osn", "psn": "patent",
}

// Client — покупатель: e-mail или (если его нет) телефон — что-то одно; наименование и ИНН — только для юрлица,
// ФИО физлица не передаётся (08 разд. 3.2, 12 разд. 5).
type Client struct {
	Email, Phone string
	Legal        bool
	Name, INN    string
}

// Item — позиция чека.
type Item struct {
	Name    string
	SumKop  int64
	Advance bool // признак способа расчёта «аванс»
}

// Input — данные чека прихода или возврата прихода.
type Input struct {
	Operation      string
	ExternalID     string
	Timestamp      time.Time
	Location       *time.Location
	CallbackURL    string
	Client         Client
	CompanyEmail   string
	CompanyINN     string
	TaxRegime      string
	VatPayer       bool
	PaymentAddress string
	Items          []Item
	PaymentType    int
	TotalKop       int64
}

type clientJSON struct {
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
	Name  string `json:"name,omitempty"`
	INN   string `json:"inn,omitempty"`
}

type companyJSON struct {
	Email          string `json:"email"`
	Sno            string `json:"sno"`
	INN            string `json:"inn"`
	PaymentAddress string `json:"payment_address"`
}

type vatJSON struct {
	Type string `json:"type"`
}

type itemJSON struct {
	Name          string      `json:"name"`
	Price         json.Number `json:"price"`
	Quantity      json.Number `json:"quantity"`
	Sum           json.Number `json:"sum"`
	Measure       int         `json:"measure"`
	PaymentMethod string      `json:"payment_method"`
	PaymentObject int         `json:"payment_object"`
	Vat           vatJSON     `json:"vat"`
}

type paymentJSON struct {
	Type int         `json:"type"`
	Sum  json.Number `json:"sum"`
}

type receiptJSON struct {
	Client   clientJSON    `json:"client"`
	Company  companyJSON   `json:"company"`
	Items    []itemJSON    `json:"items"`
	Payments []paymentJSON `json:"payments"`
	Total    json.Number   `json:"total"`
}

type serviceJSON struct {
	CallbackURL string `json:"callback_url"`
}

type requestJSON struct {
	ExternalID string      `json:"external_id"`
	Timestamp  string      `json:"timestamp"`
	Service    serviceJSON `json:"service"`
	Receipt    receiptJSON `json:"receipt"`
}

// Rubles — «45000.00» из копеек (без float).
func Rubles(kop int64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", kop/100, kop%100))
}

// Phone — телефон в формате +7XXXXXXXXXX (из 10 или 11 цифр с 7/8 в начале); иначе пусто.
func Phone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case len(d) == 11 && (d[0] == '7' || d[0] == '8'):
		return "+7" + d[1:]
	case len(d) == 10:
		return "+7" + d
	}
	return ""
}

// truncate — не длиннее n символов.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Build — тело запроса sell / sell_refund (08 разд. 3.2). Ошибка — *DataError (чек не отправляется).
func Build(in Input) ([]byte, error) {
	if in.VatPayer {
		return nil, &DataError{errVat}
	}
	if in.TaxRegime == "npd" {
		return nil, &DataError{errNpd}
	}
	if strings.TrimSpace(in.CompanyINN) == "" || strings.TrimSpace(in.CompanyEmail) == "" {
		return nil, &DataError{errCompany}
	}
	sno, ok := snoByRegime[in.TaxRegime]
	if !ok {
		return nil, &DataError{"Неизвестный режим налогообложения: " + in.TaxRegime}
	}
	var client clientJSON
	switch email, phone := strings.TrimSpace(in.Client.Email), Phone(in.Client.Phone); {
	case email != "":
		client.Email = email
	case phone != "":
		client.Phone = phone
	default:
		return nil, &DataError{errNoContact}
	}
	if in.Client.Legal {
		client.Name, client.INN = in.Client.Name, in.Client.INN
	}
	var sum int64
	items := make([]itemJSON, 0, len(in.Items))
	for _, it := range in.Items {
		if it.SumKop <= 0 {
			continue
		}
		method := methodFullPayment
		if it.Advance {
			method = methodAdvance
		}
		items = append(items, itemJSON{
			Name: truncate(it.Name, maxItemName), Price: Rubles(it.SumKop), Quantity: "1.000", Sum: Rubles(it.SumKop),
			PaymentMethod: method, PaymentObject: objectService, Vat: vatJSON{Type: "none"},
		})
		sum += it.SumKop
	}
	if len(items) == 0 || sum != in.TotalKop {
		return nil, &DataError{errSums}
	}
	req := requestJSON{
		ExternalID: in.ExternalID,
		Timestamp:  in.Timestamp.In(in.Location).Format(atol.TimestampLayout),
		Service:    serviceJSON{CallbackURL: in.CallbackURL},
		Receipt: receiptJSON{
			Client:   client,
			Company:  companyJSON{Email: in.CompanyEmail, Sno: sno, INN: in.CompanyINN, PaymentAddress: in.PaymentAddress},
			Items:    items,
			Payments: []paymentJSON{{Type: in.PaymentType, Sum: Rubles(in.TotalKop)}},
			Total:    Rubles(in.TotalKop),
		},
	}
	return json.Marshal(req)
}
