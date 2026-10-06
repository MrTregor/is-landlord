// Команда cli — служебные операции: миграции, демо-данные, учётные записи, проверка журнала.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // база часовых поясов для образа distroless

	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/db"
)

const usage = `Использование: cli <команда>

Команды:
  migrate up       применить миграции схемы и очереди задач
  migrate down     откатить последнюю миграцию схемы (после отката первой — и таблицы очереди)
  migrate status   состояние миграций

Команды seed, user, jobs, audit, docs добавляются на следующих этапах (docs/spec/19 План реализации.md).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return errors.New("не указана команда")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "migrate":
		if len(args) != 2 {
			fmt.Print(usage)
			return errors.New("укажите up, down или status")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		m := db.NewMigrator(cfg.MigrateURL(), os.Stdout)
		switch args[1] {
		case "up":
			return m.Up(ctx)
		case "down":
			return m.Down(ctx)
		case "status":
			return m.Status(ctx)
		default:
			return fmt.Errorf("неизвестная операция migrate %q", args[1])
		}
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("неизвестная команда %q", args[0])
	}
}
