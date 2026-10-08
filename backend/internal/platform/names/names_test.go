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
