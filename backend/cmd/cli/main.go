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
	"landlord/backend/internal/dbdoc"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/logging"
	"landlord/backend/internal/seed"
)

const usage = `Использование: cli <команда>

Команды:
  migrate up                                      применить миграции схемы и очереди задач
  migrate down                                    откатить последнюю миграцию схемы (после первой — и очередь)
  migrate status                                  состояние миграций
  user create-owner --email <e-mail> --name <имя> создать собственника: приглашение по e-mail и ссылка в выводе
  audit verify                                    проверить цепочку хешей журнала действий
  seed --demo [--load]                            очистить бизнес-данные и загрузить демо (--load — ещё 100 объектов
                                                  для нагрузочного теста), docs/spec/17 Демо-данные.md
  jobs run <имя>                                  выполнить ежедневную задачу за сегодня (например, billing.rent_daily)
  docs db                                         описание БД в Markdown (docs/database.md) в стандартный вывод
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
	case "seed":
		return runSeed(ctx, args[1:], out)
	case "jobs":
		if len(args) != 3 || args[1] != "run" {
			fmt.Fprint(out, usage)
			return errors.New("укажите jobs run <имя>")
		}
		return runJob(ctx, args[2], out)
	case "docs":
		if len(args) != 2 || args[1] != "db" {
			fmt.Fprint(out, usage)
			return errors.New("укажите docs db")
		}
		return docsDB(ctx, out)
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
	a, err := app.New(cfg, log, app.NewClock(cfg, database), database, app.Options{})
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

// runSeed — cli seed --demo [--load] (17 Демо-данные): очистка и загрузка заново, повторный запуск идемпотентен.
func runSeed(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	demo := fs.Bool("demo", false, "загрузить демо-данные")
	load := fs.Bool("load", false, "добавить данные нагрузочного теста")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*demo {
		return errors.New("укажите seed --demo")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(os.Stderr, slog.LevelWarn, "cli")
	return seed.Run(ctx, cfg, log, out, seed.Options{Load: *load})
}

// runJob — cli jobs run <имя>: ежедневная задача за сегодня синхронно (trigger = manual).
func runJob(ctx context.Context, name string, out io.Writer) error {
	a, closeDB, err := openApp(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	for _, r := range a.DailyRunners() {
		if r.Name != name {
			continue
		}
		st, err := r.Run(ctx, jobs.DailyArgs{BusinessDate: a.Clock.Today(), Trigger: jobs.TriggerManual})
		if st != nil {
			fmt.Fprintf(out, "%s за %s: %s\n", name, a.Clock.Today().Format(), st.JSON())
		}
		return err
	}
	return fmt.Errorf("неизвестная ежедневная задача %q", name)
}

// docsDB — cli docs db: описание БД из каталога PostgreSQL (18 Развёртывание, раздел 10).
func docsDB(ctx context.Context, out io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	return dbdoc.Generate(ctx, database.Queries(), out)
}
