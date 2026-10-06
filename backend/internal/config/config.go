// Package config — настройки процессов из переменных окружения (полный список — .env.example,
// docs/spec/03 Архитектура и репозиторий.md).
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Режимы работы.
const (
	EnvDev  = "dev"
	EnvDemo = "demo"
	EnvProd = "prod"
)

// Config — все переменные окружения backend.
type Config struct {
	AppEnv       string `env:"APP_ENV" envDefault:"dev"`
	AppName      string `env:"APP_NAME" envDefault:"Наймодатель"`
	AppPublicURL string `env:"APP_PUBLIC_URL" envDefault:"http://localhost:5173"`
	HTTPAddr     string `env:"HTTP_ADDR" envDefault:":8080"`

	DatabaseURL        string `env:"DATABASE_URL,required"`
	DatabaseMigrateURL string `env:"DATABASE_MIGRATE_URL"`

	TZBusiness  string `env:"TZ_BUSINESS" envDefault:"Europe/Moscow"`
	FilesDir    string `env:"FILES_DIR" envDefault:"/data/files"`
	MaxUploadMB int    `env:"MAX_UPLOAD_MB" envDefault:"10"`
	BackupsDir  string `env:"BACKUPS_DIR" envDefault:"/backups"`

	PDNEncKey string `env:"PDN_ENC_KEY,required"`

	SessionIdleHours    int  `env:"SESSION_IDLE_HOURS" envDefault:"8"`
	SessionAbsoluteDays int  `env:"SESSION_ABSOLUTE_DAYS" envDefault:"30"`
	CookieSecure        bool `env:"COOKIE_SECURE" envDefault:"true"`

	SMTPHost     string `env:"SMTP_HOST" envDefault:"localhost"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"1025"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPTLS      string `env:"SMTP_TLS" envDefault:"none"`
	MailFrom     string `env:"MAIL_FROM" envDefault:"Наймодатель <no-reply@landlord.local>"`

	GotenbergURL string `env:"GOTENBERG_URL" envDefault:"http://localhost:3000"`

	TbankAPIURL          string `env:"TBANK_API_URL" envDefault:"http://localhost:8081/v2"`
	TbankTerminalKey     string `env:"TBANK_TERMINAL_KEY" envDefault:"LandlordDemo"`
	TbankPassword        string `env:"TBANK_PASSWORD"`
	TbankNotificationURL string `env:"TBANK_NOTIFICATION_URL"`

	AtolAPIURL      string `env:"ATOL_API_URL" envDefault:"http://localhost:8082/possystem/v5"`
	AtolLogin       string `env:"ATOL_LOGIN"`
	AtolPassword    string `env:"ATOL_PASSWORD"`
	AtolGroupCode   string `env:"ATOL_GROUP_CODE"`
	AtolCallbackURL string `env:"ATOL_CALLBACK_URL"`

	DemoClockEnabled bool   `env:"DEMO_CLOCK_ENABLED" envDefault:"false"`
	LogLevel         string `env:"LOG_LEVEL" envDefault:"info"`

	// Вычисляемые при загрузке значения.
	Location *time.Location `env:"-"`
	PDNKey   []byte         `env:"-"`
}

// Load читает переменные окружения и проверяет их.
func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("переменные окружения: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	var errs []error
	switch c.AppEnv {
	case EnvDev, EnvDemo, EnvProd:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV: допустимо dev, demo или prod, получено %q", c.AppEnv))
	}

	loc, err := time.LoadLocation(c.TZBusiness)
	if err != nil {
		errs = append(errs, fmt.Errorf("TZ_BUSINESS: %w", err))
	}
	c.Location = loc

	key, err := base64.StdEncoding.DecodeString(c.PDNEncKey)
	if err != nil || len(key) != 32 {
		errs = append(errs, errors.New("PDN_ENC_KEY: ожидается 32 байта в base64 (openssl rand -base64 32)"))
	}
	c.PDNKey = key

	switch c.SMTPTLS {
	case "none", "starttls", "tls":
	default:
		errs = append(errs, fmt.Errorf("SMTP_TLS: допустимо none, starttls или tls, получено %q", c.SMTPTLS))
	}
	if c.MaxUploadMB < 1 {
		errs = append(errs, errors.New("MAX_UPLOAD_MB: должно быть не меньше 1"))
	}
	if c.SessionIdleHours < 1 || c.SessionAbsoluteDays < 1 {
		errs = append(errs, errors.New("SESSION_IDLE_HOURS и SESSION_ABSOLUTE_DAYS: должны быть не меньше 1"))
	}
	if c.AppEnv == EnvProd && !c.CookieSecure {
		errs = append(errs, errors.New("COOKIE_SECURE=false допустимо только в режиме dev"))
	}
	if c.DemoClockEnabled && c.AppEnv != EnvDemo {
		errs = append(errs, errors.New("DEMO_CLOCK_ENABLED=true допустимо только при APP_ENV=demo"))
	}
	return errors.Join(errs...)
}

// MigrateURL — подключение для миграций (владелец схемы); если не задано — DATABASE_URL.
func (c *Config) MigrateURL() string {
	if c.DatabaseMigrateURL != "" {
		return c.DatabaseMigrateURL
	}
	return c.DatabaseURL
}

// IsDemo — включён демонстрационный режим.
func (c *Config) IsDemo() bool { return c.AppEnv == EnvDemo }

// SlogLevel — уровень журнала из LOG_LEVEL.
func (c *Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
