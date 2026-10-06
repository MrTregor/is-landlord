package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // драйвер database/sql для goose
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"landlord/backend/migrations"
)

// Migrator применяет миграции схемы (goose) и очереди задач (River).
// Выполняется под ролью владельца схемы (DATABASE_MIGRATE_URL).
type Migrator struct {
	url string
	out io.Writer
}

// NewMigrator создаёт мигратор; out — куда печатать ход выполнения.
func NewMigrator(url string, out io.Writer) *Migrator { return &Migrator{url: url, out: out} }

func (m *Migrator) open(ctx context.Context) (*sql.DB, *goose.Provider, *pgxpool.Pool, error) {
	sqlDB, err := sql.Open("pgx", m.url)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("подключение к БД: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, nil, fmt.Errorf("миграции goose: %w", err)
	}
	pool, err := pgxpool.New(ctx, m.url)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, nil, fmt.Errorf("подключение к БД: %w", err)
	}
	return sqlDB, provider, pool, nil
}

// Up применяет все миграции: сначала схема (goose), затем таблицы River.
func (m *Migrator) Up(ctx context.Context) error {
	sqlDB, provider, pool, err := m.open(ctx)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	defer pool.Close()

	results, err := provider.Up(ctx)
	for _, r := range results {
		fmt.Fprintf(m.out, "goose: применена %s (%s)\n", r.Source.Path, r.Duration)
	}
	if err != nil {
		return fmt.Errorf("goose up: %w", err)
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("миграции River: %w", err)
	}
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river up: %w", err)
	}
	for _, v := range res.Versions {
		fmt.Fprintf(m.out, "river: применена версия %d\n", v.Version)
	}
	return nil
}

// Down откатывает последнюю миграцию goose. Когда откатана и первая миграция (схема пуста),
// откатываются и таблицы River — так down симметричен up.
func (m *Migrator) Down(ctx context.Context) error {
	sqlDB, provider, pool, err := m.open(ctx)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	defer pool.Close()

	r, err := provider.Down(ctx)
	if err != nil && !errors.Is(err, goose.ErrNoNextVersion) {
		return fmt.Errorf("goose down: %w", err)
	}
	if r != nil {
		fmt.Fprintf(m.out, "goose: откатена %s (%s)\n", r.Source.Path, r.Duration)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("версия схемы: %w", err)
	}
	if version > 0 {
		return nil
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("миграции River: %w", err)
	}
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionDown, &rivermigrate.MigrateOpts{TargetVersion: -1})
	if err != nil {
		return fmt.Errorf("river down: %w", err)
	}
	for _, v := range res.Versions {
		fmt.Fprintf(m.out, "river: откатена версия %d\n", v.Version)
	}
	return nil
}

// Status печатает состояние миграций схемы.
func (m *Migrator) Status(ctx context.Context) error {
	sqlDB, provider, pool, err := m.open(ctx)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	defer pool.Close()

	statuses, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("goose status: %w", err)
	}
	for _, s := range statuses {
		state := "не применена"
		if s.State == goose.StateApplied {
			state = "применена " + s.AppliedAt.Format("02.01.2006 15:04:05")
		}
		fmt.Fprintf(m.out, "goose: %-40s %s\n", s.Source.Path, state)
	}
	return nil
}
