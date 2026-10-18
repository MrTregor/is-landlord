// Package dbdoc — описание БД в Markdown (docs/database.md) из каталога PostgreSQL и комментариев COMMENT ON
// (cli docs db, docs/spec/18 Развёртывание и эксплуатация.md, раздел 10).
package dbdoc

import (
	"context"
	"fmt"
	"io"
	"strings"

	"landlord/backend/internal/platform/db/sqlc"
)

// Catalog — запросы к каталогу схемы (queries/dbdoc.sql).
type Catalog interface {
	DocTables(ctx context.Context) ([]sqlc.DocTablesRow, error)
	DocColumns(ctx context.Context) ([]sqlc.DocColumnsRow, error)
	DocConstraints(ctx context.Context) ([]sqlc.DocConstraintsRow, error)
	DocIndexes(ctx context.Context) ([]sqlc.DocIndexesRow, error)
}

var constraintKinds = map[string]string{
	"p": "первичный ключ",
	"u": "уникальность",
	"f": "внешний ключ",
	"c": "проверка",
	"x": "исключение",
	"t": "триггер",
	"n": "не NULL",
}

// Generate пишет описание всех таблиц схемы public (кроме служебных таблиц River и goose).
func Generate(ctx context.Context, q Catalog, out io.Writer) error {
	tables, err := q.DocTables(ctx)
	if err != nil {
		return fmt.Errorf("таблицы: %w", err)
	}
	cols, err := q.DocColumns(ctx)
	if err != nil {
		return fmt.Errorf("столбцы: %w", err)
	}
	cons, err := q.DocConstraints(ctx)
	if err != nil {
		return fmt.Errorf("ограничения: %w", err)
	}
	idx, err := q.DocIndexes(ctx)
	if err != nil {
		return fmt.Errorf("индексы: %w", err)
	}
	colsOf := map[string][]sqlc.DocColumnsRow{}
	for _, c := range cols {
		colsOf[c.TableName] = append(colsOf[c.TableName], c)
	}
	consOf := map[string][]sqlc.DocConstraintsRow{}
	for _, c := range cons {
		if c.Kind == "n" {
			continue // NOT NULL видно в колонке «Обяз.»
		}
		consOf[c.TableName] = append(consOf[c.TableName], c)
	}
	idxOf := map[string][]sqlc.DocIndexesRow{}
	for _, i := range idx {
		idxOf[i.TableName] = append(idxOf[i.TableName], i)
	}

	var b strings.Builder
	b.WriteString("# Описание базы данных\n\n")
	b.WriteString("Файл сформирован командой `cli docs db` (`make docs-db`) из схемы PostgreSQL и комментариев " +
		"`COMMENT ON` в миграциях `backend/migrations`; руками не править. Полный DDL — " +
		"`docs/spec/04 Модель данных.md` и миграции. Служебные таблицы очереди задач River и goose не описываются.\n\n")
	fmt.Fprintf(&b, "Таблиц: %d.\n\n## Содержание\n", len(tables))
	for _, t := range tables {
		fmt.Fprintf(&b, "- [%s](#%s)", t.Name, t.Name)
		if t.Comment != "" {
			fmt.Fprintf(&b, " — %s", firstSentence(t.Comment))
		}
		b.WriteString("\n")
	}
	for _, t := range tables {
		fmt.Fprintf(&b, "\n## %s\n", t.Name)
		if t.Comment != "" {
			fmt.Fprintf(&b, "%s\n", t.Comment)
		}
		b.WriteString("\n| Поле | Тип | Обяз. | По умолчанию | Описание |\n|---|---|---|---|---|\n")
		for _, c := range colsOf[t.Name] {
			req := ""
			if c.NotNull {
				req = "да"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", c.Name, cell(c.Type), req, code(c.DefaultValue), cell(c.Comment))
		}
		if cs := consOf[t.Name]; len(cs) > 0 {
			b.WriteString("\nОграничения:\n")
			for _, c := range cs {
				fmt.Fprintf(&b, "- `%s` (%s): `%s`\n", c.Name, constraintKinds[c.Kind], c.Definition)
			}
		}
		if is := idxOf[t.Name]; len(is) > 0 {
			b.WriteString("\nИндексы:\n")
			for _, i := range is {
				fmt.Fprintf(&b, "- `%s`: `%s`\n", i.Name, i.Definition)
			}
		}
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// cell — текст для ячейки таблицы Markdown.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + cell(s) + "`"
}

func firstSentence(s string) string {
	if i := strings.Index(s, ";"); i > 0 {
		return s[:i]
	}
	return s
}
