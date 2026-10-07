package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"landlord/backend/internal/platform/apperr"
	"landlord/backend/internal/platform/crypto"
	"landlord/backend/internal/platform/reqctx"
)

// Authenticator проверяет cookie сессии.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*reqctx.Actor, error)
}

// PolicyChecker сообщает текущую версию политики ПДн.
type PolicyChecker interface {
	CurrentPolicyVersion(ctx context.Context) (string, error)
}

const sessionCookie = "ll_session"

// access — кто может вызывать путь API (12 Безопасность и ПДн, раздел 3).
type access int

const (
	accessPublic access = iota // без входа
	accessAny                  // собственник и наниматель
	accessOwner
	accessTenant
)

// classify определяет права по пути внутри /api/v1.
func classify(path string) access {
	switch {
	case path == "/healthz", path == "/readyz",
		strings.HasPrefix(path, "/public/"),
		path == "/auth/login", path == "/auth/password/forgot", path == "/auth/password/reset",
		strings.HasPrefix(path, "/auth/invite/"),
		strings.HasPrefix(path, "/integrations/"):
		return accessPublic
	case path == "/auth/accept-policy", strings.HasPrefix(path, "/cabinet/"):
		return accessTenant
	case strings.HasPrefix(path, "/auth/"), path == "/files", strings.HasPrefix(path, "/files/"):
		return accessAny
	default:
		return accessOwner
	}
}

// csrfExempt — методы без проверки CSRF (06 API, раздел 1).
func csrfExempt(path string) bool {
	return path == "/auth/login" || path == "/auth/invite/accept" || path == "/auth/password/forgot" ||
		path == "/auth/password/reset" || strings.HasPrefix(path, "/integrations/")
}

// clientIP — адрес клиента. За Caddy — последний адрес X-Forwarded-For (его ставит прокси), иначе RemoteAddr.
func clientIP(r *http.Request) *netip.Addr {
	raw := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		raw = strings.TrimSpace(parts[len(parts)-1])
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}
	addr = addr.Unmap()
	return &addr
}

// Security — сведения о запросе, сессия, права по пути, CSRF и ограничение частоты.
func (s *Server) Security(next http.Handler) http.Handler {
	general := newLimiter(300, time.Minute)
	uploads := newLimiter(30, time.Minute)
	integrations := newLimiter(120, time.Minute)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		path := strings.TrimPrefix(r.URL.Path, BaseURL)
		ip := clientIP(r)
		meta := reqctx.Meta{RequestID: RequestIDFrom(ctx), IP: ip, UserAgent: r.UserAgent()}
		if c, err := r.Cookie(sessionCookie); err == nil {
			meta.SessionToken = c.Value
		}
		ctx = reqctx.WithMeta(ctx, meta)

		ipKey := "ip:"
		if ip != nil {
			ipKey += ip.String()
		}
		if strings.HasPrefix(path, "/integrations/") && !integrations.allow(ipKey) {
			s.writeError(w, r, apperr.TooManyRequests(apperr.CodeRateLimited, "Слишком много запросов", time.Minute))
			return
		}

		var actor *reqctx.Actor
		if meta.SessionToken != "" && s.auth != nil {
			a, err := s.auth.Authenticate(ctx, meta.SessionToken)
			if err != nil {
				s.writeError(w, r, err)
				return
			}
			actor = a
		}
		if actor != nil {
			ctx = reqctx.WithActor(ctx, actor)
		}

		key := ipKey
		if actor != nil {
			key = "s:" + actor.SessionID.String()
		}
		if !general.allow(key) {
			s.writeError(w, r, apperr.TooManyRequests(apperr.CodeRateLimited, "Слишком много запросов. Повторите через минуту", time.Minute))
			return
		}
		if r.Method == http.MethodPost && path == "/files" && !uploads.allow(key) {
			s.writeError(w, r, apperr.TooManyRequests(apperr.CodeRateLimited, "Слишком много загрузок файлов. Повторите через минуту", time.Minute))
			return
		}

		switch classify(path) {
		case accessPublic:
		case accessAny:
			if actor == nil {
				s.writeError(w, r, apperr.Unauthorized())
				return
			}
		case accessOwner:
			if actor == nil {
				s.writeError(w, r, apperr.Unauthorized())
				return
			}
			if actor.Role != reqctx.RoleOwner {
				s.writeError(w, r, apperr.Forbidden())
				return
			}
		case accessTenant:
			if actor == nil {
				s.writeError(w, r, apperr.Unauthorized())
				return
			}
			if actor.Role != reqctx.RoleTenant {
				s.writeError(w, r, apperr.Forbidden())
				return
			}
			// Без ознакомления с текущей версией политики кабинет недоступен (12 Безопасность и ПДн, раздел 4).
			if strings.HasPrefix(path, "/cabinet/") && s.policy != nil {
				version, err := s.policy.CurrentPolicyVersion(ctx)
				if err != nil {
					s.writeError(w, r, err)
					return
				}
				if actor.PolicyAcceptedVersion == nil || *actor.PolicyAcceptedVersion != version {
					s.writeError(w, r, apperr.Forbidden().WithDetail("Ознакомьтесь с Политикой обработки персональных данных"))
					return
				}
			}
		}

		if isMutating(r.Method) && !csrfExempt(path) && actor != nil &&
			!crypto.EqualConstantTime(r.Header.Get("X-CSRF-Token"), actor.CSRFToken) {
			s.writeError(w, r, apperr.CSRFFailed())
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutating(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

// limiter — ограничение частоты по ключу (сессия или адрес) в памяти процесса.
type limiter struct {
	mu      sync.Mutex
	perMin  int
	window  time.Duration
	buckets map[string]*rate.Limiter
	calls   int
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{perMin: n, window: window, buckets: map[string]*rate.Limiter{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls%10000 == 0 {
		// Простая очистка: заполненные (неактивные) корзины удаляются.
		for k, b := range l.buckets {
			if b.Tokens() >= float64(l.perMin) { //nolint:forbidigo // не деньги: число токенов ограничителя
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[key]
	if !ok {
		b = rate.NewLimiter(rate.Every(l.window/time.Duration(l.perMin)), l.perMin)
		l.buckets[key] = b
	}
	return b.Allow()
}
