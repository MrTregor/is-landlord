package audit

// Коды действий журнала (12 Безопасность и ПДн, раздел 7) и их названия в интерфейсе.
const (
	ActionAuthLogin           = "auth.login"
	ActionAuthLogout          = "auth.logout"
	ActionAuthLocked          = "auth.locked"
	ActionAuthPasswordChanged = "auth.password_changed"
	ActionAuthPasswordReset   = "auth.password_reset"
	ActionUserCreate          = "user.create"
	ActionUserUpdate          = "user.update"
	ActionUserBlock           = "user.block"
	ActionUserInvite          = "user.invite"
	ActionSettingsUpdate      = "settings.update"
)

var actionTitles = map[string]string{
	"auth.login":            "Вход",
	"auth.logout":           "Выход",
	"auth.locked":           "Блокировка входа",
	"auth.password_changed": "Смена пароля",
	"auth.password_reset":   "Восстановление пароля",
	"user.create":           "Создание пользователя",
	"user.update":           "Изменение пользователя",
	"user.block":            "Блокировка пользователя",
	"user.invite":           "Приглашение пользователя",
	"settings.update":       "Изменение настроек",
	"tariff.create":         "Добавление тарифа",
	"tariff.delete":         "Удаление тарифа",
	"template.update":       "Изменение шаблона документа",
	"template.reset":        "Сброс шаблона документа",
	"email_template.update": "Изменение шаблона письма",
	"property.create":       "Создание объекта",
	"property.update":       "Изменение объекта",
	"property.status":       "Смена статуса объекта",
	"property.archive":      "Архивирование объекта",
	"meter.create":          "Добавление счётчика",
	"meter.update":          "Изменение счётчика",
	"meter.archive":         "Архивирование счётчика",
	"tenant.create":         "Создание нанимателя",
	"tenant.update":         "Изменение нанимателя",
	"tenant.pdn_view":       "Просмотр паспортных данных",
	"tenant.consent":        "Согласие на обработку ПДн",
	"tenant.consent_revoke": "Отзыв согласия на обработку ПДн",
	"tenant.anonymize":      "Обезличивание нанимателя",
	"contract.create":       "Создание договора",
	"contract.update":       "Изменение договора",
	"contract.delete_draft": "Удаление черновика договора",
	"contract.activate":     "Ввод договора в действие",
	"contract.terminate":    "Расторжение договора",
	"contract.finish":       "Завершение договора",
	"contract.occupants":    "Изменение проживающих",
	"amendment.create":      "Создание допсоглашения",
	"amendment.sign":        "Подписание допсоглашения",
	"amendment.cancel":      "Отмена допсоглашения",
	"act.update":            "Изменение акта",
	"act.sign":              "Подписание акта",
	"document.generate":     "Формирование документа",
	"charge.create":         "Создание начисления",
	"charge.adjust":         "Корректировка начисления",
	"charge.cancel":         "Отмена начисления",
	"penalty.set":           "Изменение пеней",
	"penalty.write_off":     "Списание пеней",
	"payment.create":        "Регистрация платежа",
	"payment.succeed":       "Проведение платежа",
	"payment.fail":          "Платёж не прошёл",
	"payment.cancel":        "Отмена платежа",
	"payment.refund":        "Возврат переплаты",
	"allocation.reverse":    "Отмена распределения",
	"deposit.receipt":       "Внесение залога",
	"deposit.withhold":      "Удержание из залога",
	"deposit.offset":        "Зачёт залога",
	"deposit.refund":        "Возврат залога",
	"deposit.cancel":        "Отмена операции с залогом",
	"fiscal.done":           "Чек сформирован",
	"fiscal.fail":           "Ошибка чека",
	"fiscal.manual":         "Чек НПД указан вручную",
	"statement.upload":      "Загрузка выписки",
	"discrepancy.resolve":   "Разбор расхождения",
	"reading.accept":        "Принятие показания",
	"reading.reject":        "Отклонение показания",
	"request.transition":    "Смена статуса заявки",
	"expense.create":        "Добавление затраты",
	"expense.update":        "Изменение затраты",
	"expense.delete":        "Удаление затраты",
	"dev.clock":             "Сдвиг даты (демо)",
}

// ActionTitle — русское название действия; для итогов задач job.<name> — «Плановая задача».
func ActionTitle(code string) string {
	if t, ok := actionTitles[code]; ok {
		return t
	}
	if len(code) > 4 && code[:4] == "job." {
		return "Плановая задача " + code[4:]
	}
	return code
}

// ActionCodes — все известные коды (фильтр «Действие» на экране журнала).
func ActionCodes() map[string]string { return actionTitles }
