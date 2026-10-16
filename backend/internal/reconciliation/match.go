package reconciliation

import (
	"regexp"
	"strings"

	"github.com/google/uuid"

	"landlord/backend/internal/platform/date"
)

// Способы сопоставления строки с договором.
const (
	MatchContractNumber = "contract_number"
	MatchPayerINN       = "payer_inn"
	MatchPayerName      = "payer_name"
	MatchManual         = "manual"
)

// Contract — договор (кроме черновиков) с данными нанимателя для сопоставления.
type Contract struct {
	ID         uuid.UUID
	Number     string
	Status     string
	StartDate  date.Date
	ActualEnd  date.Date
	TenantID   uuid.UUID
	TenantKind string
	TenantINN  string
	LastName   string
	FirstName  string
	MiddleName string
}

// Matcher — сопоставление строк выписки с договорами (05 разд. 15.1, шаги 2–4).
type Matcher struct {
	contracts []Contract
	numbers   []*regexp.Regexp
}

// NewMatcher готовит сопоставление по списку договоров.
func NewMatcher(contracts []Contract) *Matcher {
	m := &Matcher{contracts: contracts}
	for _, c := range contracts {
		// Номер договора — по границам слова (буквы и цифры с обеих сторон не допускаются), без учёта регистра.
		m.numbers = append(m.numbers, regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])`+regexp.QuoteMeta(c.Number)+`($|[^\p{L}\p{N}])`))
	}
	return m
}

// Match — договор строки и способ сопоставления; ok = false — договор не найден (строка «не опознана»).
func (m *Matcher) Match(l Line) (c Contract, method string, ok bool) {
	if c, ok := m.byNumber(l.Purpose); ok {
		return c, MatchContractNumber, true
	}
	if c, ok := m.byINN(l.PayerINN, l.ReceivedOn); ok {
		return c, MatchPayerINN, true
	}
	if c, ok := m.byName(l.PayerName, l.ReceivedOn); ok {
		return c, MatchPayerName, true
	}
	return Contract{}, "", false
}

// byNumber — в назначении встречается ровно один номер договора из системы (шаг 2).
func (m *Matcher) byNumber(purpose string) (Contract, bool) {
	if strings.TrimSpace(purpose) == "" {
		return Contract{}, false
	}
	var found []Contract
	for i, re := range m.numbers {
		if re.MatchString(purpose) {
			found = append(found, m.contracts[i])
		}
	}
	if len(found) != 1 {
		return Contract{}, false
	}
	return found[0], true
}

// byINN — ровно один наниматель-юрлицо с таким ИНН и с договором, действовавшим на дату поступления или раньше;
// договор — последний по дате начала (шаг 3).
func (m *Matcher) byINN(inn string, on date.Date) (Contract, bool) {
	if inn == "" {
		return Contract{}, false
	}
	return latestOfOneTenant(m.contracts, func(c Contract) bool {
		return c.TenantKind == "legal" && c.TenantINN == inn && !c.StartDate.After(on)
	})
}

// byName — имя плательщика совпадает с «ФАМИЛИЯ ИМЯ ОТЧЕСТВО» или «ФАМИЛИЯ ИМЯ» ровно одного нанимателя-физлица
// с договором в статусах active, extended или закрытым не ранее 90 дней до даты поступления (шаг 4).
func (m *Matcher) byName(payer string, on date.Date) (Contract, bool) {
	name := NormalizeName(payer)
	if name == "" {
		return Contract{}, false
	}
	since := on.AddDays(-90)
	return latestOfOneTenant(m.contracts, func(c Contract) bool {
		if c.TenantKind != "individual" {
			return false
		}
		full := NormalizeName(strings.Join([]string{c.LastName, c.FirstName, c.MiddleName}, " "))
		short := NormalizeName(c.LastName + " " + c.FirstName)
		if name != full && name != short {
			return false
		}
		switch c.Status {
		case "active", "extended":
			return true
		case "terminated", "finished":
			return !c.ActualEnd.Before(since)
		}
		return false
	})
}

// latestOfOneTenant — договор с самой поздней датой начала, если подходящие договоры принадлежат ровно одному
// нанимателю.
func latestOfOneTenant(contracts []Contract, fit func(Contract) bool) (Contract, bool) {
	var (
		best   Contract
		tenant uuid.UUID
		found  bool
	)
	for _, c := range contracts {
		if !fit(c) {
			continue
		}
		if found && c.TenantID != tenant {
			return Contract{}, false
		}
		if !found || c.StartDate.After(best.StartDate) {
			best = c
		}
		tenant, found = c.TenantID, true
	}
	return best, found
}

var (
	quotesRe = regexp.MustCompile(`["'«»„“”‘’]`)
	ipRe     = regexp.MustCompile(`(^|\s)ИП(\s|$)`)
)

// NormalizeName — нормализация имени плательщика (05 разд. 15.1, шаг 4): верхний регистр, Ё → Е, без «ИП», кавычек
// и лишних пробелов.
func NormalizeName(s string) string {
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, "Ё", "Е")
	s = quotesRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	for ipRe.MatchString(s) {
		s = ipRe.ReplaceAllString(s, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}
