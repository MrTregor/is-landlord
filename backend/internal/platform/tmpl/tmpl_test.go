package tmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRender_FR04_1(t *testing.T) {
	src := `<p>{{org.short_name}}, договор № {{ contract.number }}</p>{{block:lines}}<p>{{missing}}</p>`
	out := Render(src,
		map[string]string{"org.short_name": "ИП <Гордейчик>", "contract.number": "2026-007"},
		map[string]string{"lines": "<table></table>"},
		Options{Empty: "________________"})
	assert.Equal(t, `<p>ИП &lt;Гордейчик&gt;, договор № 2026-007</p><table></table><p>________________</p>`, out)
}

func TestRenderMultiline(t *testing.T) {
	out := Render("{{a}}", map[string]string{"a": "строка 1\nстрока 2"}, nil, Options{})
	assert.Equal(t, "строка 1<br>строка 2", out)
}

func TestKeysAndUnknown(t *testing.T) {
	src := "{{a}} {{ b.c }} {{block:t}} {{a}}"
	assert.Equal(t, []string{"a", "b.c", "block:t"}, Keys(src))
	assert.Equal(t, []string{"block:t"}, Unknown(src, map[string]bool{"a": true, "b.c": true}))
}

func TestPlainText(t *testing.T) {
	h := `<p>Здравствуйте!</p><p><a href="https://x.ru/invite/1">Создать пароль</a></p><p>Ссылка&nbsp;действует 7 дней.</p>`
	assert.Equal(t, "Здравствуйте!\nСоздать пароль (https://x.ru/invite/1)\nСсылка действует 7 дней.", PlainText(h))
}
