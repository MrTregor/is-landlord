package notify

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"landlord/backend/migrations"
)

// Default — текст письма по умолчанию («Сбросить к стандартному»).
type Default struct {
	Subject, BodyHTML string
}

// Тексты по умолчанию — те, что загружены миграциями данных (10 Уведомления, раздел 4): миграции встроены
// в бинарник, поэтому отдельная копия текстов не нужна (docs/open-questions.md, № 76).
var (
	defaultsOnce sync.Once
	defaults     map[string]Default
	defaultsErr  error
	defaultRow   = regexp.MustCompile(`(?s)\('([a-z_]+)',\s*'(?:[^']|'')*',\s*'[a-z]+',\s*'((?:[^']|'')*)',\s*\$\$(.*?)\$\$\)`)
)

var defaultFiles = []string{"0002_seed_email_templates.sql", "0005_email_owner_bank_refund.sql"}

// Defaults — тексты писем по умолчанию по кодам.
func Defaults() (map[string]Default, error) {
	defaultsOnce.Do(func() {
		defaults = map[string]Default{}
		for _, name := range defaultFiles {
			b, err := fs.ReadFile(migrations.FS, name)
			if err != nil {
				defaultsErr = err
				return
			}
			up, _, _ := strings.Cut(string(b), "-- +goose Down")
			for _, m := range defaultRow.FindAllStringSubmatch(up, -1) {
				defaults[m[1]] = Default{Subject: strings.ReplaceAll(m[2], "''", "'"), BodyHTML: m[3]}
			}
		}
		if len(defaults) == 0 {
			defaultsErr = fmt.Errorf("тексты писем по умолчанию не найдены в миграциях")
		}
	})
	return defaults, defaultsErr
}
