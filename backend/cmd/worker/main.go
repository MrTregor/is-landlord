// Команда worker — фоновые задачи River (плановые и по событиям).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // база часовых поясов для образа distroless

	"landlord/backend/internal/app"
	"landlord/backend/internal/config"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/logging"
	"landlord/backend/internal/platform/mailer"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker остановлен с ошибкой", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.SlogLevel(), "worker")
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	a, err := app.New(cfg, log, clock.NewReal(cfg.Location), database, app.Options{})
	if err != nil {
		return err
	}
	reg := a.Workers(mailer.NewSMTP(cfg))
	if reg.Len() == 0 {
		log.Info("worker запущен: зарегистрированных задач пока нет, ожидание остановки")
		<-ctx.Done()
		return nil
	}

	client, err := jobs.NewWorkerClient(database.Pool, reg, log)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return err
	}
	log.Info("worker запущен", slog.Int("workers", reg.Len()))
	<-ctx.Done()

	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.Stop(stopCtx)
}
