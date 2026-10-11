//go:build gotenberg

package document

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"landlord/backend/internal/platform/pdf"
)

// Ручная проверка PDF в настоящем Gotenberg (go test -tags gotenberg): системные шаблоны на демо-данных
// (тексты 0003 с заменами миграции 0004).
// Каталог результата — GOTENBERG_OUT.
func TestSystemTemplatesInGotenberg(t *testing.T) {
	src, err := os.ReadFile("../../migrations/0003_seed_templates.sql")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)\('(\w+)', '[^']*', \$\$(.*?)\$\$`)
	c := pdf.New(os.Getenv("GOTENBERG_URL"))
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		body := strings.NewReplacer("к {{contract.title}}", "к {{contract.title_dative}}",
			"по {{contract.title}}", "по {{contract.title_dative}}",
			"<p>{{recon.closing_text}}</p>", "<p>{{recon.closing_text}}</p><p>{{recon.deposit_text}}</p>").Replace(m[2])
		out, err := c.Convert(context.Background(), render(m[1], body, examples(m[1])))
		if err != nil {
			t.Fatal(m[1], err)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("GOTENBERG_OUT"), m[1]+".pdf"), out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
