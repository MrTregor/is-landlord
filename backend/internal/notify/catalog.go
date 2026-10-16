package notify

import (
	"sort"
	"strings"
)

// Variable — переменная шаблона письма: ключ, название для «Вставить поле», пример для предпросмотра.
type Variable struct {
	Key, Title, Example string
}

// Общие переменные писем (10 Уведомления, раздел 1).
var commonVars = []Variable{
	{"app_name", "Название системы", "Наймодатель"},
	{"org.short_name", "Организация (кратко)", "ИП Гордейчик А.В."},
	{"org.phone", "Телефон организации", "+7 900 123-45-67"},
	{"org.email", "E-mail организации", "owner@landlord.local"},
	{"cabinet_url", "Адрес личного кабинета", "https://landlord.example/cabinet"},
	{"owner_url", "Адрес рабочего места собственника", "https://landlord.example"},
}

// Переменные договора — во всех письмах, кроме приглашения, восстановления пароля и сводки.
var contractVars = []Variable{
	{"tenant.greeting", "Обращение к нанимателю", "Павел Игоревич"},
	{"contract.number", "Номер договора", "2026-002"},
	{"property.address", "Адрес объекта", "г. Руза, ул. Солнечная, д. 5, кв. 31"},
	{"property.code", "Код объекта", "Студия 2"},
}

// codeVars — переменные писем по кодам (10 Уведомления, разделы 2 и 3).
var codeVars = map[string][]Variable{
	"invite": {
		{"link", "Ссылка для входа (7 дней)", "https://landlord.example/invite/…"},
		{"display_name", "Имя получателя", "Павел Игоревич"},
	},
	"password_reset": {{"link", "Ссылка для смены пароля (1 час)", "https://landlord.example/reset/…"}},
	"payment_reminder": {
		{"due_date", "Срок оплаты", "10.11.2026"},
		{"total", "Итого к оплате", "28 000,00 ₽"},
		{"block:lines", "Таблица начислений", ""},
	},
	"payment_overdue": {
		{"overdue_total", "Просроченный долг", "11 500,00 ₽"},
		{"penalties_accruing", "Начисленные пени", "57,50 ₽"},
		{"block:lines", "Таблица начислений", ""},
	},
	"payment_receipt": {
		{"receipt_number", "Номер квитанции", "КВ-2026-00048"},
		{"amount", "Сумма", "26 500,00 ₽"},
		{"received_on", "Дата поступления", "09.10.2026"},
	},
	"fiscal_receipt": {
		{"receipt_url", "Ссылка на чек", "https://ofd.example/check/…"},
		{"amount", "Сумма", "45 000,00 ₽"},
	},
	"contract_ending": {
		{"end_date", "Дата окончания договора", "28.02.2027"},
		{"days_left", "Дней до окончания", "30"},
	},
	"rent_changed": {
		{"new_rent", "Новая плата", "27 825,00 ₽"},
		{"effective_from", "Действует с", "01.04.2027"},
	},
	"readings_reminder": {
		{"period", "Месяц показаний", "октябрь 2026"},
		{"window_to", "Передать до", "05.11.2026"},
	},
	"readings_rejected": {
		{"meter", "Счётчик", "Холодная вода, № 123456"},
		{"value", "Показание", "105,320"},
		{"reason", "Причина", "Значение не совпадает с фото"},
	},
	"request_status_changed": {
		{"request.number", "Номер заявки", "З-2026-014"},
		{"request.title", "Заголовок заявки", "Течёт смеситель на кухне"},
		{"status_title", "Новый статус", "В работе"},
		{"comment", "Комментарий", "Мастер придёт завтра с 10 до 12"},
	},
	"deposit_withheld": {
		{"amount", "Сумма удержания", "3 000,00 ₽"},
		{"reason", "Основание", "Сломана дверца шкафа"},
		{"balance", "Остаток залога", "24 000,00 ₽"},
	},
	"deposit_refunded": {
		{"amount", "Сумма возврата", "24 000,00 ₽"},
		{"op_date", "Дата возврата", "05.10.2026"},
	},
	"owner_payment_received": {
		{"tenant.short_name", "Наниматель (кратко)", "Кузнецов П. И."},
		{"amount", "Сумма", "26 500,00 ₽"},
		{"received_on", "Дата поступления", "09.10.2026"},
		{"method_title", "Способ", "перевод на счёт"},
		{"link", "Ссылка на платёж", "https://landlord.example/payments/…"},
	},
	"owner_bank_refund": {
		{"tenant.short_name", "Наниматель (кратко)", "Смирнова А. С."},
		{"amount", "Сумма", "28 000,00 ₽"},
		{"received_on", "Дата поступления", "10.10.2026"},
		{"receipt_number", "Номер квитанции", "КВ-2026-00051"},
		{"status_title", "Статус банка", "возврат платежа"},
		{"link", "Ссылка на платёж", "https://landlord.example/payments/…"},
	},
	"owner_discrepancy": {
		{"kind_title", "Вид расхождения", "Неопознанный платёж"},
		{"amount", "Сумма", "12 000,00 ₽"},
		{"details", "Подробности", "12.10.2026, плательщик Иванова Т. В., назначение «за квартиру»."},
		{"link", "Ссылка на сверку", "https://landlord.example/reconciliation"},
	},
	"owner_request_created": {
		{"tenant.short_name", "Наниматель (кратко)", "Кузнецов П. И."},
		{"request.number", "Номер заявки", "З-2026-014"},
		{"request.title", "Заголовок заявки", "Течёт смеситель на кухне"},
		{"request.category_title", "Категория заявки", "Сантехника"},
		{"link", "Ссылка на заявку", "https://landlord.example/requests/…"},
	},
	"owner_readings_submitted": {
		{"period", "Месяц показаний", "октябрь 2026"},
		{"link", "Ссылка на показания", "https://landlord.example/readings"},
	},
	"owner_indexation_due": {
		{"effective_from", "Индексация с", "01.04.2027"},
		{"current_rent", "Текущая плата", "26 500,00 ₽"},
		{"new_rent", "Новая плата", "27 825,00 ₽"},
		{"link", "Ссылка на договор", "https://landlord.example/contracts/…"},
	},
	"owner_fiscal_error": {
		{"amount", "Сумма", "45 000,00 ₽"},
		{"error_text", "Текст ошибки", "Неверный ИНН покупателя"},
		{"link", "Ссылка на чеки", "https://landlord.example/fiscal-receipts"},
	},
	"owner_daily_digest": {
		{"today", "Дата сводки", "18.10.2026"},
		{"block:lines", "Разделы сводки", ""},
	},
}

// withoutContract — письма без переменных договора.
var withoutContract = map[string]bool{"invite": true, "password_reset": true, "owner_daily_digest": true}

// Codes — коды писем (по алфавиту).
func Codes() []string {
	out := make([]string, 0, len(codeVars))
	for c := range codeVars {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Variables — переменные письма code: общие, договора (если есть) и свои.
func Variables(code string) []Variable {
	own, ok := codeVars[code]
	if !ok {
		return nil
	}
	out := append([]Variable{}, commonVars...)
	if !withoutContract[code] {
		out = append(out, contractVars...)
	}
	for _, v := range own {
		dup := false
		for _, e := range out {
			dup = dup || e.Key == v.Key
		}
		if !dup {
			out = append(out, v)
		}
	}
	return out
}

// Allowed — допустимые ключи шаблона письма (для проверки при сохранении).
func Allowed(code string) map[string]bool {
	m := map[string]bool{}
	for _, v := range Variables(code) {
		m[v.Key] = true
	}
	return m
}

// Examples — значения переменных для предпросмотра (без блоков).
func Examples(code string) map[string]string {
	m := map[string]string{}
	for _, v := range Variables(code) {
		if !strings.HasPrefix(v.Key, "block:") {
			m[v.Key] = v.Example
		}
	}
	return m
}
