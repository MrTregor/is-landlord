package reconciliation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"landlord/backend/internal/platform/date"
)

func contractOf(number, status, start, end string, tenant uuid.UUID, kind, inn, last, first, middle string) Contract {
	return Contract{ID: uuid.New(), Number: number, Status: status, StartDate: date.MustParse(start), ActualEnd: date.MustParse(end),
		TenantID: tenant, TenantKind: kind, TenantINN: inn, LastName: last, FirstName: first, MiddleName: middle}
}

func line(on, payer, inn, purpose string) Line {
	return Line{ReceivedOn: date.MustParse(on), AmountKop: 100, PayerName: payer, PayerINN: inn, Purpose: purpose}
}

var (
	smirnova = uuid.New()
	romashka = uuid.New()
	semenova = uuid.New()
)

func world() []Contract {
	return []Contract{
		contractOf("2026-001", "active", "2026-03-10", "2027-02-09", smirnova, "individual", "", "Смирнова", "Анна", "Сергеевна"),
		contractOf("2025-004", "finished", "2025-01-01", "2025-12-31", romashka, "legal", "5075123457", "", "", ""),
		contractOf("2026-005", "active", "2026-05-01", "2028-04-30", romashka, "legal", "5075123457", "", "", ""),
		contractOf("2026-010", "active", "2026-11-01", "2027-10-31", romashka, "legal", "5075123457", "", "", ""),
		contractOf("2026-012", "active", "2026-02-01", "2027-01-31", semenova, "individual", "", "Семёнова", "Алёна", "Петровна"),
	}
}

// Номер договора в назначении — по границам слова, без учёта регистра (FR-07.2).
func TestMatchContractNumber_FR_07_2(t *testing.T) {
	m := NewMatcher(world())
	c, method, ok := m.Match(line("2026-10-10", "Кто-то", "", "Оплата по договору №2026-001 за октябрь"))
	assert.True(t, ok)
	assert.Equal(t, MatchContractNumber, method)
	assert.Equal(t, "2026-001", c.Number)

	_, _, ok = m.Match(line("2026-10-10", "Кто-то", "", "Оплата по договору 2026-0011"))
	assert.False(t, ok, "номер внутри другого числа не считается")
	_, _, ok = m.Match(line("2026-10-10", "Кто-то", "", "По договорам 2026-001 и 2026-012"))
	assert.False(t, ok, "два номера — договор не определён по назначению")
}

// ИНН юрлица: договор, действовавший на дату поступления или раньше, — последний по дате начала (FR-07.2).
func TestMatchINN_FR_07_2(t *testing.T) {
	m := NewMatcher(world())
	c, method, ok := m.Match(line("2026-10-02", "ООО «Ромашка»", "5075123457", "Арендная плата"))
	assert.True(t, ok)
	assert.Equal(t, MatchPayerINN, method)
	assert.Equal(t, "2026-005", c.Number, "договор с началом 01.11.2026 ещё не действовал")

	c, _, ok = m.Match(line("2026-11-05", "ООО «Ромашка»", "5075123457", "Арендная плата"))
	assert.True(t, ok)
	assert.Equal(t, "2026-010", c.Number)

	_, _, ok = m.Match(line("2024-12-01", "ООО «Ромашка»", "5075123457", "Арендная плата"))
	assert.False(t, ok, "до начала первого договора")
}

// ФИО с «Ё», «ИП», кавычки и регистр — нормализуются (FR-07.2).
func TestMatchNameYo_FR_07_2(t *testing.T) {
	m := NewMatcher(world())
	c, method, ok := m.Match(line("2026-10-02", "ИП  СЕМЕНОВА Алена Петровна", "", "за квартиру"))
	assert.True(t, ok)
	assert.Equal(t, MatchPayerName, method)
	assert.Equal(t, "2026-012", c.Number)

	c, _, ok = m.Match(line("2026-10-02", "«Смирнова Анна»", "", "за квартиру"))
	assert.True(t, ok, "совпадение «ФАМИЛИЯ ИМЯ»")
	assert.Equal(t, "2026-001", c.Number)

	_, _, ok = m.Match(line("2026-10-02", "Смирнова А. С.", "", "за квартиру"))
	assert.False(t, ok, "инициалы не сопоставляются")
	assert.Equal(t, "СЕМЕНОВА АЛЕНА ПЕТРОВНА", NormalizeName(" ип \"Семёнова\"  алёна петровна "))
}

// Два нанимателя с одинаковым ФИО — строка не опознана (FR-07.2).
func TestMatchTwoCandidates_FR_07_2(t *testing.T) {
	list := append(world(), contractOf("2026-020", "active", "2026-06-01", "2027-05-31", uuid.New(), "individual", "",
		"Семенова", "Алена", "Петровна"))
	_, _, ok := NewMatcher(list).Match(line("2026-10-02", "Семенова Алена Петровна", "", "за квартиру"))
	assert.False(t, ok)

	twoLegal := append(world(), contractOf("2026-021", "active", "2026-06-01", "2027-05-31", uuid.New(), "legal",
		"5075123457", "", "", ""))
	_, _, ok = NewMatcher(twoLegal).Match(line("2026-10-02", "ООО", "5075123457", "Аренда"))
	assert.False(t, ok, "два юрлица с одним ИНН")
}

// Закрытый договор физлица учитывается 90 дней после окончания (FR-07.2).
func TestMatchClosedContract_FR_07_2(t *testing.T) {
	orlova := uuid.New()
	list := []Contract{contractOf("2025-004", "finished", "2025-08-01", "2026-07-31", orlova, "individual", "", "Орлова", "Мария", "Дмитриевна")}
	m := NewMatcher(list)
	_, _, ok := m.Match(line("2026-10-29", "Орлова Мария Дмитриевна", "", "долг"))
	assert.True(t, ok, "90 дней после 31.07")
	_, _, ok = m.Match(line("2026-10-30", "Орлова Мария Дмитриевна", "", "долг"))
	assert.False(t, ok)
}
