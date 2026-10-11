//go:build gotenberg

package document

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"landlord/backend/internal/platform/pdf"
)

// Ручная проверка PDF в настоящем Gotenberg (go test -tags gotenberg): системные шаблоны на демо-данных.
// Каталог результата — GOTENBERG_OUT.
func TestSystemTemplatesInGotenberg(t *testing.T) {
	src, err := os.ReadFile("../../migrations/0003_seed_templates.sql")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)\('(\w+)', '[^']*', \$\$(.*?)\$\$`)
	c := pdf.New(os.Getenv("GOTENBERG_URL"))
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		out, err := c.Convert(context.Background(), render(m[1], m[2], examples(m[1])))
		if err != nil {
			t.Fatal(m[1], err)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("GOTENBERG_OUT"), m[1]+".pdf"), out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
