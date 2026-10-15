package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Коды ошибок эмулятора (08 разд. 4).
const (
	codeTokenExpired = 11
	codeBadLogin     = 12
	codeBadFormat    = 30
	codeSums         = 32
	codeDuplicate    = 33
	codeNoGroup      = 34
	codeDriver       = 50
)

// Допустимые значения полей чека (ФФД, АТОЛ Онлайн v5).
var (
	snoValues                  = []string{"osn", "usn_income", "usn_income_outcome", "esn", "patent"}
	paymentMethodValues        = []string{"full_prepayment", "prepayment", "advance", "full_payment", "partial_payment", "credit", "credit_payment"}
	vatValues                  = []string{"none", "vat0", "vat5", "vat7", "vat10", "vat20", "vat105", "vat107", "vat110", "vat120"}
	timestampLayout            = "02.01.2006 15:04:05"
	maxItemNameRunes           = 128
	maxExternalIDRunes         = 128
	maxPaymentTypeNumber int64 = 9
)

func formatError(field string) *APIError {
	return &APIError{Code: codeBadFormat, Text: "Неверный формат запроса: " + field, Type: "agent"}
}

// parseFixed — неотрицательное десятичное число с не более чем scale знаками после точки → целое в единицах
// 10^-scale (рубли → копейки при scale = 2, количество → тысячные при scale = 3). Без float.
func parseFixed(v any, scale int) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	s := string(n)
	if s == "" || strings.ContainsAny(s, "eE+-") {
		return 0, false
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > scale {
		return 0, false
	}
	frac += strings.Repeat("0", scale-len(frac))
	out, err := strconv.ParseInt(whole+frac, 10, 64)
	return out, err == nil
}

func obj(m map[string]any, k string) (map[string]any, bool) {
	v, ok := m[k].(map[string]any)
	return v, ok
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// receiptInfo — данные запроса, нужные эмулятору после проверки.
type receiptInfo struct {
	ExternalID  string
	CallbackURL string
	TotalKop    int64
}

// validateRequest — проверки чека (08 разд. 4): обязательные поля и форматы (код 30), суммы (код 32).
func validateRequest(m map[string]any) (receiptInfo, *APIError) {
	var info receiptInfo
	info.ExternalID = str(m, "external_id")
	if n := utf8.RuneCountInString(info.ExternalID); n == 0 || n > maxExternalIDRunes {
		return info, formatError("external_id")
	}
	if _, err := time.Parse(timestampLayout, str(m, "timestamp")); err != nil {
		return info, formatError("timestamp")
	}
	if svc, ok := obj(m, "service"); ok {
		info.CallbackURL = str(svc, "callback_url")
	}
	r, ok := obj(m, "receipt")
	if !ok {
		return info, formatError("receipt")
	}
	client, ok := obj(r, "client")
	if !ok {
		return info, formatError("receipt.client")
	}
	if str(client, "email") == "" && str(client, "phone") == "" {
		return info, formatError("client.email")
	}
	company, ok := obj(r, "company")
	if !ok {
		return info, formatError("receipt.company")
	}
	if str(company, "email") == "" {
		return info, formatError("company.email")
	}
	if !slices.Contains(snoValues, str(company, "sno")) {
		return info, formatError("company.sno")
	}
	if inn := str(company, "inn"); !digits(inn) || (len(inn) != 10 && len(inn) != 12) {
		return info, formatError("company.inn")
	}
	if str(company, "payment_address") == "" {
		return info, formatError("company.payment_address")
	}
	items, ok := r["items"].([]any)
	if !ok || len(items) == 0 {
		return info, formatError("receipt.items")
	}
	var itemsSum int64
	for i, raw := range items {
		it, ok := raw.(map[string]any)
		field := func(name string) *APIError { return formatError(fmt.Sprintf("items[%d].%s", i, name)) }
		if !ok {
			return info, formatError(fmt.Sprintf("items[%d]", i))
		}
		if n := utf8.RuneCountInString(str(it, "name")); n == 0 || n > maxItemNameRunes {
			return info, field("name")
		}
		price, ok := parseFixed(it["price"], 2)
		if !ok {
			return info, field("price")
		}
		qty, ok := parseFixed(it["quantity"], 3)
		if !ok || qty == 0 {
			return info, field("quantity")
		}
		sum, ok := parseFixed(it["sum"], 2)
		if !ok {
			return info, field("sum")
		}
		if !slices.Contains(paymentMethodValues, str(it, "payment_method")) {
			return info, field("payment_method")
		}
		if po, ok := parseFixed(it["payment_object"], 0); !ok || po < 1 || po > 33 {
			return info, field("payment_object")
		}
		vat, ok := obj(it, "vat")
		if !ok || !slices.Contains(vatValues, str(vat, "type")) {
			return info, field("vat.type")
		}
		// sum = price × quantity с округлением до копейки (половина — вверх).
		if (price*qty+500)/1000 != sum {
			return info, &APIError{Code: codeSums, Text: "Ошибка проверки сумм", Type: "agent"}
		}
		itemsSum += sum
	}
	payments, ok := r["payments"].([]any)
	if !ok || len(payments) == 0 {
		return info, formatError("receipt.payments")
	}
	var paid int64
	for i, raw := range payments {
		p, ok := raw.(map[string]any)
		if !ok {
			return info, formatError(fmt.Sprintf("payments[%d]", i))
		}
		if t, ok := parseFixed(p["type"], 0); !ok || t > maxPaymentTypeNumber {
			return info, formatError(fmt.Sprintf("payments[%d].type", i))
		}
		sum, ok := parseFixed(p["sum"], 2)
		if !ok {
			return info, formatError(fmt.Sprintf("payments[%d].sum", i))
		}
		paid += sum
	}
	total, ok := parseFixed(r["total"], 2)
	if !ok || total == 0 {
		return info, formatError("receipt.total")
	}
	if total != itemsSum || total != paid {
		return info, &APIError{Code: codeSums, Text: "Ошибка проверки сумм", Type: "agent"}
	}
	info.TotalKop = total
	return info, nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// canonical — тело запроса в каноническом виде (ключи по порядку, числа как в запросе): повтор external_id с тем
// же телом выдаёт тот же документ, даже если отправитель переставил ключи или пробелы.
func canonical(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}
