// Package migrations — SQL-миграции goose, встроенные в бинарник.
package migrations

import "embed"

// FS — файлы миграций (0001_init.sql, 0002_….sql).
//
//go:embed *.sql
var FS embed.FS
