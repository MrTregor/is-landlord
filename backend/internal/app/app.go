// Package app — сборка модулей (корень композиции) для server, worker и cli.
package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"landlord/backend/internal/audit"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/billing"
	"landlord/backend/internal/cabinet"
	"landlord/backend/internal/calendar"
	"landlord/backend/internal/config"
	"landlord/backend/internal/contract"
	"landlord/backend/internal/document"
	"landlord/backend/internal/expense"
	"landlord/backend/internal/file"
	"landlord/backend/internal/fiscal"
	httpserver "landlord/backend/internal/http"
	"landlord/backend/internal/jobrun"
	"landlord/backend/internal/maintenance"
	"landlord/backend/internal/meter"
	"landlord/backend/internal/notify"
	"landlord/backend/internal/notify/mailing"
	"landlord/backend/internal/payment"
	"landlord/backend/internal/platform/atol"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/jobs"
	"landlord/backend/internal/platform/mailer"
	"landlord/backend/internal/platform/pdf"
	"landlord/backend/internal/platform/tbank"
	"landlord/backend/internal/property"
	"landlord/backend/internal/reconciliation"
	"landlord/backend/internal/report"
	"landlord/backend/internal/settings"
	"landlord/backend/internal/tenant"
	"landlord/backend/internal/user"
)

// App — сервисы модулей.
type App struct {
	Config    *config.Config
	Log       *slog.Logger
	Clock     clock.Clock
	DB        *db.DB
	Jobs      *river.Client[pgx.Tx]
	Audit     *audit.Service
	Notify    *notify.Service
	Settings  *settings.Service
	Auth      *auth.Service
	Users     *user.Service
	Files     *file.Service
	Property  *property.Service
	Meters    *meter.Service
	Tenants   *tenant.Service
	Billing   *billing.Service
	Documents *document.Service
	Contract  *contract.Service
	Calendar  *calendar.Service
	Payments  *payment.Service
	Fiscal    *fiscal.Service
	Reconcile *reconciliation.Service
	Mailing   *mailing.Service
	Requests  *maintenance.Service
	Expenses  *expense.Service
	Reports   *report.Service
}

// Options — необязательные параметры (тесты).
type Options struct {
	// Argon — параметры хеширования паролей; по умолчанию — из спецификации.
	Argon *crypto.Argon2Params
	// PDF — формирование PDF; по умолчанию — Gotenberg по адресу GOTENBERG_URL.
	PDF pdf.Converter
	// Kassa — API кассы; по умолчанию — АТОЛ Онлайн по адресу ATOL_API_URL.
	Kassa fiscal.Sender
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
	cipher, err := crypto.NewCipher(cfg.PDNKey)
	if err != nil {
		return nil, err
	}
	a.Tenants = tenant.NewService(database, a.Audit, clk, cipher, a.Auth, a.Settings, a.Files)
	a.Billing = billing.NewService(database, a.Audit, clk, a.Settings, a.Notify)
	a.Meters = meter.NewService(database, a.Audit, clk, a.Billing, a.Notify)
	conv := opt.PDF
	if conv == nil {
		conv = pdf.New(cfg.GotenbergURL)
	}
	a.Documents = document.NewService(database, a.Audit, clk, a.Settings, a.Tenants, a.Files, conv, cfg.AppPublicURL)
	a.Contract = contract.NewService(database, a.Audit, clk, a.Tenants, a.Settings, a.Notify, a.Billing, a.Documents)
	a.Calendar = calendar.NewService(database, a.Audit, clk)
	kassa := opt.Kassa
	if kassa == nil {
		kassa = atol.NewClient(cfg.AtolAPIURL, cfg.AtolLogin, cfg.AtolPassword, cfg.AtolGroupCode, clk)
	}
	a.Fiscal = fiscal.NewService(database, a.Audit, clk, a.Notify, client, kassa,
		fiscal.Config{CallbackURL: cfg.AtolCallbackURL, PublicURL: cfg.AppPublicURL})
	a.Payments = payment.NewService(database, a.Audit, clk, a.Billing, a.Documents, a.Notify, client, payment.Online{
		Client:          tbank.NewClient(cfg.TbankAPIURL, cfg.TbankTerminalKey, cfg.TbankPassword),
		NotificationURL: cfg.TbankNotificationURL, PublicURL: cfg.AppPublicURL, Log: log,
	}, a.Fiscal)
	a.Reconcile = reconciliation.NewService(database, a.Audit, clk, a.Files, a.Payments, a.Notify)
	a.Requests = maintenance.NewService(database, a.Audit, clk, a.Notify)
	a.Expenses = expense.NewService(database, a.Audit, clk, a.Billing, a.Payments)
	a.Reports = report.NewService(database, clk, a.Settings, a.Calendar)
	a.Mailing = mailing.NewService(database, a.Audit, clk, a.Notify, a.Settings, a.Calendar)
	return a, nil
}

// Server — HTTP API со всеми модулями.
func (a *App) Server(gotenberg httpserver.HealthChecker) *httpserver.Server {
	return httpserver.NewServer(httpserver.Deps{
		Config: a.Config, Log: a.Log, Clock: a.Clock, DB: a.DB, Gotenberg: gotenberg,
		Auth: a.Auth, Policy: a.Settings,
		Modules: httpserver.Modules{
			Auth:      auth.NewHandler(a.Auth),
			Users:     user.NewHandler(a.Users),
			Audit:     audit.NewHandler(a.Audit, a.Clock),
			Settings:  settings.NewHandler(a.Settings),
			Files:     file.NewHandler(a.Files),
			Property:  property.NewHandler(a.Property, a.Reports.YearToDate),
			Meters:    meter.NewHandler(a.Meters),
			Tenants:   tenant.NewHandler(a.Tenants),
			Contract:  contract.NewHandler(a.Contract),
			Calendar:  calendar.NewHandler(a.Calendar),
			Document:  document.NewHandler(a.Documents),
			Billing:   billing.NewHandler(a.Billing),
			Payments:  payment.NewHandler(a.Payments),
			Cabinet:   cabinet.NewHandler(a.DB, a.Clock, a.Payments, a.Documents),
			Jobs:      jobrun.NewHandler(a.DB, a.Jobs, a.Clock, a.Audit, append(Daily(), Periodic()...)),
			Fiscal:    fiscal.NewHandler(a.Fiscal),
			Reconcile: reconciliation.NewHandler(a.Reconcile),
			Mailing:   mailing.NewHandler(a.Mailing),
			Requests:  maintenance.NewHandler(a.Requests),
			Expenses:  expense.NewHandler(a.Expenses),
			Reports:   report.NewHandler(a.Reports),
		},
	})
}

// Daily — реализованные ежедневные задачи в порядке выполнения (07 Фоновые задачи).
func Daily() []jobs.Daily {
	return append([]jobs.Daily{contract.Daily(), billing.RentDaily(), billing.PenaltiesDaily(), contract.IndexationDaily()},
		mailing.Schedules()...)
}

// Periodic — реализованные плановые задачи не ежедневного расписания (07 Фоновые задачи).
func Periodic() []jobs.Daily {
	return []jobs.Daily{fiscal.ReconcileSchedule(), payment.ExpireHourly()}
}

// Workers — воркеры фоновых задач процесса worker.
func (a *App) Workers(sender mailer.Sender) *jobs.Registry {
	reg := jobs.NewRegistry()
	jobs.Add(reg, &notify.EmailSendWorker{DB: a.DB, Clock: a.Clock, Sender: sender, Files: a.Files.Store()})
	jobs.Add(reg, &contract.DailyWorker{Svc: a.Contract})
	jobs.Add(reg, &billing.RentWorker{Svc: a.Billing})
	jobs.Add(reg, &billing.PenaltiesWorker{Svc: a.Billing})
	jobs.Add(reg, &contract.IndexationWorker{Svc: a.Contract})
	jobs.Add(reg, &payment.ReceiptWorker{Svc: a.Payments})
	jobs.Add(reg, &payment.NotificationWorker{Svc: a.Payments})
	jobs.Add(reg, &payment.ExpireWorker{Svc: a.Payments})
	jobs.Add(reg, &fiscal.SendWorker{Svc: a.Fiscal})
	jobs.Add(reg, &fiscal.PollWorker{Svc: a.Fiscal})
	jobs.Add(reg, &fiscal.ReconcileWorker{Svc: a.Fiscal})
	jobs.Add(reg, &mailing.DigestWorker{Svc: a.Mailing})
	jobs.Add(reg, &mailing.RemindersWorker{Svc: a.Mailing})
	jobs.Add(reg, &mailing.ContractEndWorker{Svc: a.Mailing})
	jobs.Add(reg, &mailing.ReadingsWorker{Svc: a.Mailing})
	for _, d := range append(Daily(), Periodic()...) {
		reg.AddDaily(d, a.Clock)
	}
	return reg
}
