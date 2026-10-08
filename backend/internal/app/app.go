// Package app — сборка модулей (корень композиции) для server, worker и cli.
package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/config"
	"landlord/backend/internal/file"
	httpserver "landlord/backend/internal/http"
	"landlord/backend/internal/meter"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/mailer"
	"landlord/backend/internal/property"
	"landlord/backend/internal/settings"
	"landlord/backend/internal/tenant"
	"landlord/backend/internal/user"
)

// App — сервисы модулей.
type App struct {
	Config   *config.Config
	Log      *slog.Logger
	Clock    clock.Clock
	DB       *db.DB
	Jobs     *river.Client[pgx.Tx]
	Audit    *audit.Service
	Notify   *notify.Service
	Settings *settings.Service
	Auth     *auth.Service
	Users    *user.Service
	Files    *file.Service
	Property *property.Service
	Meters   *meter.Service
	Tenants  *tenant.Service
}

// Options — необязательные параметры (тесты).
type Options struct {
	// Argon — параметры хеширования паролей; по умолчанию — из спецификации.
	Argon *crypto.Argon2Params
}

// New собирает сервисы. Клиент River — только для постановки задач (InsertTx).
func New(cfg *config.Config, log *slog.Logger, clk clock.Clock, database *db.DB, opt Options) (*App, error) {
	client, err := jobs.NewInsertClient(database.Pool, log)
	if err != nil {
		return nil, err
	}
	argon := crypto.DefaultArgon2
	if opt.Argon != nil {
		argon = *opt.Argon
	}
	a := &App{Config: cfg, Log: log, Clock: clk, DB: database, Jobs: client}
	a.Audit = audit.NewService(database, clk)
	a.Notify = notify.NewService(cfg, clk, client)
	a.Settings = settings.NewService(database, a.Audit, clk)
	a.Auth = auth.NewService(database, cfg, clk, a.Audit, a.Notify, a.Settings, argon)
	a.Users = user.NewService(database, clk, a.Audit, a.Auth)
	a.Files = file.NewService(database, cfg, clk)
	a.Property = property.NewService(database, a.Audit, clk, a.Files)
	a.Meters = meter.NewService(database, a.Audit, clk)
	cipher, err := crypto.NewCipher(cfg.PDNKey)
	if err != nil {
		return nil, err
	}
	a.Tenants = tenant.NewService(database, a.Audit, clk, cipher, a.Auth, a.Settings, a.Files)
	return a, nil
}

// Server — HTTP API со всеми модулями.
func (a *App) Server(gotenberg httpserver.HealthChecker) *httpserver.Server {
	return httpserver.NewServer(httpserver.Deps{
		Config: a.Config, Log: a.Log, Clock: a.Clock, DB: a.DB, Gotenberg: gotenberg,
		Auth: a.Auth, Policy: a.Settings,
		Modules: httpserver.Modules{
			Auth:     auth.NewHandler(a.Auth),
			Users:    user.NewHandler(a.Users),
			Audit:    audit.NewHandler(a.Audit, a.Clock),
			Settings: settings.NewHandler(a.Settings),
			Files:    file.NewHandler(a.Files),
			Property: property.NewHandler(a.Property),
			Meters:   meter.NewHandler(a.Meters),
			Tenants:  tenant.NewHandler(a.Tenants),
		},
	})
}

// Workers — воркеры фоновых задач процесса worker.
func (a *App) Workers(sender mailer.Sender) *jobs.Registry {
	reg := jobs.NewRegistry()
	jobs.Add(reg, &notify.EmailSendWorker{DB: a.DB, Clock: a.Clock, Sender: sender})
	return reg
}
