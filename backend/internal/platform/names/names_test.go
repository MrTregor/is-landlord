package names

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func p(s string) *string { return &s }

func TestNames(t *testing.T) {
	assert.Equal(t, "Кузнецов П. И.", Short(p("Кузнецов"), p("Павел"), p("Игоревич")))
	assert.Equal(t, "Кузнецов П.", Short(p("Кузнецов"), p("Павел"), nil))
	assert.Equal(t, "Кузнецов Павел Игоревич", Full(p("Кузнецов"), p("Павел"), p("Игоревич")))
	assert.Equal(t, "ООО «Ромашка»", Display("legal", nil, nil, nil, p("ООО «Ромашка»")))
	assert.Equal(t, "Смирнова А. С.", Display("individual", p("Смирнова"), p("Анна"), p("Сергеевна"), nil))
	assert.Equal(t, "Павел Игоревич", Greeting(p("Павел"), p("Игоревич")))
	assert.Equal(t, "Павел", Greeting(p("Павел"), nil))
}

// Склонение для «в лице …» и род для «гражданин/гражданка» (09 разд. 4.2), FR-04.1.
func TestGenitive_FR04_1(t *testing.T) {
	assert.Equal(t, "Иванова Ивана Ивановича", GenitiveFull("Иванов Иван Иванович"))
	assert.Equal(t, "Смирновой Анны Сергеевны", GenitiveFull("Смирнова Анна Сергеевна"))
	assert.Equal(t, "Соколовского Андрея Петровича", GenitiveFull("Соколовский Андрей Петрович"))
	assert.Equal(t, "Петровой Ольги Ильиничны", GenitiveFull("Петрова Ольга Ильинична"))
	assert.Equal(t, "Шевченко Игоря Николаевича", GenitiveFull("Шевченко Игорь Николаевич"))
	assert.Equal(t, "генерального директора", GenitivePhrase("Генеральный директор"))
	assert.Equal(t, "директора", GenitivePhrase("директор"))
	assert.Equal(t, "директора по развитию", GenitivePhrase("Директор по развитию"))
	assert.Equal(t, "управляющего", GenitivePhrase("Управляющий"))
	assert.True(t, IsFemale(p("Сергеевна")))
	assert.False(t, IsFemale(p("Игоревич")))
	assert.Equal(t, "Иванов И. И.", ShortOf("Иванов Иван Иванович"))
}
