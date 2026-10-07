// Команда cli — служебные операции: миграции, учётные записи, проверка журнала действий.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata" // база часовых поясов для образа distroless

	"landlord/backend/internal/app"
	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/logging"
)

const usage = `Использование: cli <команда>

Команды:
  migrate up                                      применить миграции схемы и очереди задач
  migrate down                                    откатить последнюю миграцию схемы (после первой — и очередь)
  migrate status                                  состояние миграций
  user create-owner --email <e-mail> --name <имя> создать собственника: приглашение по e-mail и ссылка в выводе
  audit verify                                    проверить цепочку хешей журнала действий

Команды seed, jobs, docs добавляются на следующих этапах (docs/spec/19 План реализации.md).
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return errors.New("не указана команда")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "migrate":
		if len(args) != 2 {
			fmt.Fprint(out, usage)
			return errors.New("укажите up, down или status")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		m := db.NewMigrator(cfg.MigrateURL(), out)
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
	case "user":
		if len(args) < 2 || args[1] != "create-owner" {
			fmt.Fprint(out, usage)
			return errors.New("укажите user create-owner")
		}
		return createOwner(ctx, args[2:], out)
	case "audit":
		if len(args) != 2 || args[1] != "verify" {
			fmt.Fprint(out, usage)
			return errors.New("укажите audit verify")
		}
		return auditVerify(ctx, out)
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	default:
		fmt.Fprint(out, usage)
		return fmt.Errorf("неизвестная команда %q", args[0])
	}
}

func openApp(ctx context.Context) (*app.App, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	log := logging.New(os.Stderr, slog.LevelWarn, "cli")
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	a, err := app.New(cfg, log, clock.NewReal(cfg.Location), database, app.Options{})
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return a, database.Close, nil
}

// createOwner — первый собственник при развёртывании (18 Развёртывание, раздел 6, шаг 5).
func createOwner(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("user create-owner", flag.ContinueOnError)
	email := fs.String("email", "", "e-mail собственника")
	name := fs.String("name", "", "имя для обращения («Александр Васильевич»)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*email) == "" || strings.TrimSpace(*name) == "" {
		return errors.New("укажите --email и --name")
	}
	a, closeDB, err := openApp(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	u, link, err := a.Users.CreateOwner(ctx, *email, *name)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Создан собственник %s (статус «приглашён»).\nПисьмо с приглашением поставлено в очередь.\nСсылка приглашения (действует 7 дней): %s\n", u.Email, link)
	return nil
}

// auditVerify — проверка цепочки хешей журнала (FR-12, NFR-Sec-04).
func auditVerify(ctx context.Context, out io.Writer) error {
	a, closeDB, err := openApp(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	res, err := a.Audit.Verify(ctx)
	if err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("нарушение целостности в записи № %d (проверено записей: %d)", *res.BrokenAtID, res.Checked)
	}
	fmt.Fprintf(out, "Цепочка записей не нарушена, проверено %d записей\n", res.Checked)
	return nil
}
