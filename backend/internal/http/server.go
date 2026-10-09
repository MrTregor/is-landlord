// Package httpserver — HTTP-слой сервера API: маршрутизатор, промежуточные обработчики,
// реализация strict-server интерфейса из internal/api.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"landlord/backend/internal/api"
	"landlord/backend/internal/audit"
	"landlord/backend/internal/auth"
	"landlord/backend/internal/calendar"
	"landlord/backend/internal/config"
	"landlord/backend/internal/contract"
	"landlord/backend/internal/file"
	"landlord/backend/internal/meter"
	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/clock"
	"landlord/backend/internal/platform/db"
	"landlord/backend/internal/platform/logging"
	"landlord/backend/internal/property"
	"landlord/backend/internal/settings"
	"landlord/backend/internal/tenant"
	"landlord/backend/internal/user"
)

// BaseURL — базовый путь API.
const BaseURL = "/api/v1"

// Pinger — зависимость, доступность которой проверяет /readyz.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthChecker — проверка доступности внешнего сервиса (Gotenberg).
type HealthChecker interface {
	Health(ctx context.Context) error
}

// Deps — зависимости сервера.
type Deps struct {
	Config    *config.Config
	Log       *slog.Logger
	Clock     clock.Clock
	DB        *db.DB
	Gotenberg HealthChecker
	// Auth и Policy — проверка сессии и версии политики ПДн (nil в тестах без БД).
	Auth    Authenticator
	Policy  PolicyChecker
	Modules Modules
}

// Modules — обработчики модулей; их методы перекрывают заглушки 501.
type Modules struct {
	Auth     *auth.Handler
	Users    *user.Handler
	Audit    *audit.Handler
	Settings *settings.Handler
	Files    *file.Handler
	Property *property.Handler
	Meters   *meter.Handler
	Tenants  *tenant.Handler
	Contract *contract.Handler
	Calendar *calendar.Handler
}

// unimplemented опускает заглушки на второй уровень встраивания: методы модулей,
// встроенные в Server напрямую, перекрывают их без конфликта имён.
type unimplemented struct{ api.StrictUnimplemented }

// Псевдонимы дают встроенным обработчикам модулей разные имена полей.
type (
	authHandler     = auth.Handler
	userHandler     = user.Handler
	auditHandler    = audit.Handler
	settingsHandler = settings.Handler
	fileHandler     = file.Handler
	propertyHandler = property.Handler
	meterHandler    = meter.Handler
	tenantHandler   = tenant.Handler
	contractHandler = contract.Handler
	calendarHandler = calendar.Handler
)

// Server реализует api.StrictServerInterface. Методы, ещё не реализованные модулями, отвечают 501.
type Server struct {
	unimplemented
	*authHandler
	*userHandler
	*auditHandler
	*settingsHandler
	*fileHandler
	*propertyHandler
	*meterHandler
	*tenantHandler
	*contractHandler
	*calendarHandler

	deps     Deps
	dbPinger Pinger
	auth     Authenticator
	policy   PolicyChecker
}

var _ api.StrictServerInterface = (*Server)(nil)

// NewServer создаёт реализацию API.
func NewServer(deps Deps) *Server {
	m := deps.Modules
	s := &Server{
		authHandler: m.Auth, userHandler: m.Users, auditHandler: m.Audit, settingsHandler: m.Settings,
		fileHandler: m.Files, propertyHandler: m.Property, meterHandler: m.Meters, tenantHandler: m.Tenants,
		contractHandler: m.Contract, calendarHandler: m.Calendar,
		deps: deps, auth: deps.Auth, policy: deps.Policy,
	}
	if deps.DB != nil {
		s.dbPinger = deps.DB
	}
	return s
}

// Handler собирает маршрутизатор: промежуточные обработчики и все методы API под /api/v1.
func (s *Server) Handler() http.Handler {
	log := s.deps.Log
	r := chi.NewRouter()
	validate, err := s.OpenAPIValidator()
	if err != nil {
		panic("спецификация API: " + err.Error()) // встроенная спецификация проверяется тестом
	}
	r.Use(RequestID, Logging(log, s.deps.Clock), Recover(log), SecurityHeaders, Timeout(60*time.Second), s.Security, validate)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		s.writeError(w, req, apperr.NotFoundDetail("Адрес не найден"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		s.writeError(w, req, apperr.BadRequest("Метод не поддерживается для этого адреса"))
	})

	strict := api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			s.writeError(w, req, apperr.BadRequest("Неверный формат запроса: "+err.Error()))
		},
		ResponseErrorHandlerFunc: s.writeError,
	})
	api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseURL:    BaseURL,
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			s.writeError(w, req, apperr.BadRequest(err.Error()))
		},
	})
	return r
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	apperr.Write(w, r, err, RequestIDFrom(r.Context()), logging.From(r.Context(), s.deps.Log))
}

// GetHealthz — процесс жив.
func (s *Server) GetHealthz(context.Context, api.GetHealthzRequestObject) (api.GetHealthzResponseObject, error) {
	return api.GetHealthz200JSONResponse{Status: api.HealthStatusStatusOk}, nil
}

// GetReadyz — доступны ли БД и Gotenberg.
func (s *Server) GetReadyz(ctx context.Context, _ api.GetReadyzRequestObject) (api.GetReadyzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	res := api.ReadyStatus{Status: api.ReadyStatusStatusOk, Database: api.CheckStatusOk, Gotenberg: api.CheckStatusOk}
	log := logging.From(ctx, s.deps.Log)
	if s.dbPinger == nil || s.dbPinger.Ping(ctx) != nil {
		res.Database = api.CheckStatusError
		log.WarnContext(ctx, "readyz: БД недоступна")
	}
	if s.deps.Gotenberg == nil {
		res.Gotenberg = api.CheckStatusError
	} else if err := s.deps.Gotenberg.Health(ctx); err != nil {
		res.Gotenberg = api.CheckStatusError
		log.WarnContext(ctx, "readyz: Gotenberg недоступен", slog.String("error", err.Error()))
	}
	if res.Database != api.CheckStatusOk || res.Gotenberg != api.CheckStatusOk {
		res.Status = api.ReadyStatusStatusUnavailable
		return api.GetReadyz503JSONResponse(res), nil
	}
	return api.GetReadyz200JSONResponse(res), nil
}

// GetAppInfo — название системы, организации и режим работы (страница входа, подвал).
func (s *Server) GetAppInfo(ctx context.Context, _ api.GetAppInfoRequestObject) (api.GetAppInfoResponseObject, error) {
	info := api.AppInfo{AppName: s.deps.Config.AppName, Env: api.AppEnv(s.deps.Config.AppEnv)}
	name, err := s.deps.DB.Queries().GetOrganizationShortName(ctx)
	switch {
	case err == nil:
		info.OrgShortName = &name
	case db.IsNoRows(err):
		// Реквизиты организации ещё не заполнены.
	default:
		return nil, err
	}
	return api.GetAppInfo200JSONResponse(info), nil
}
