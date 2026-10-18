package dbdoc

import (
	"context"
	"strings"
	"testing"

	"landlord/backend/internal/platform/db/sqlc"
)

type fakeCatalog struct{}

func (fakeCatalog) DocTables(context.Context) ([]sqlc.DocTablesRow, error) {
	return []sqlc.DocTablesRow{{Name: "files", Comment: "Загруженные файлы; содержимое — в FILES_DIR"}}, nil
}

func (fakeCatalog) DocColumns(context.Context) ([]sqlc.DocColumnsRow, error) {
	return []sqlc.DocColumnsRow{
		{TableName: "files", Name: "id", Type: "uuid", NotNull: true, DefaultValue: "gen_random_uuid()"},
		{TableName: "files", Name: "note", Type: "text", Comment: "Примечание | с чертой"},
		{TableName: "river_job", Name: "id", Type: "bigint", NotNull: true},
	}, nil
}

func (fakeCatalog) DocConstraints(context.Context) ([]sqlc.DocConstraintsRow, error) {
	return []sqlc.DocConstraintsRow{
		{TableName: "files", Name: "files_pkey", Kind: "p", Definition: "PRIMARY KEY (id)"},
		{TableName: "files", Name: "files_id_not_null", Kind: "n", Definition: "NOT NULL id"},
	}, nil
}

func (fakeCatalog) DocIndexes(context.Context) ([]sqlc.DocIndexesRow, error) {
	return []sqlc.DocIndexesRow{{TableName: "files", Name: "files_sha_idx", Definition: "CREATE INDEX files_sha_idx ON public.files USING btree (sha256)"}}, nil
}

// Описание БД (18 разд. 10, NFR-Maint-02): таблица, поля, ограничения, индексы; служебные таблицы не выводятся.
func TestGenerate_NFR_Maint_02(t *testing.T) {
	var b strings.Builder
	if err := Generate(context.Background(), fakeCatalog{}, &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"Таблиц: 1.",
		"- [files](#files) — Загруженные файлы\n",
		"## files\nЗагруженные файлы; содержимое — в FILES_DIR",
		"| `id` | uuid | да | `gen_random_uuid()` |  |",
		`| ` + "`note`" + ` | text |  |  | Примечание \| с чертой |`,
		"- `files_pkey` (первичный ключ): `PRIMARY KEY (id)`",
		"- `files_sha_idx`: `CREATE INDEX",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("нет %q в\n%s", want, out)
		}
	}
	if strings.Contains(out, "river_job") || strings.Contains(out, "files_id_not_null") {
		t.Errorf("лишнее в описании:\n%s", out)
	}
}
