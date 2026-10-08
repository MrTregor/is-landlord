package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"landlord/backend/internal/api"
	"landlord/backend/internal/platform/apperr"
)

// OpenAPIValidator проверяет параметры и тело запроса по api/openapi.yaml (06 API: ошибка проверки — 400 bad_request).
// Права и сессия проверяются отдельно (Security), поэтому схемы безопасности здесь не проверяются.
func (s *Server) OpenAPIValidator() (func(http.Handler) http.Handler, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}
	spec.Servers = openapi3.Servers{{URL: BaseURL}}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	opts := &openapi3filter.Options{
		AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil },
		MultiError:         false,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, params, err := router.FindRoute(r)
			if err != nil {
				// Неизвестный путь или метод — ответит маршрутизатор (404 или 400).
				next.ServeHTTP(w, r)
				return
			}
			o := *opts
			// Тела multipart (файлы) проверяет обработчик: тип по сигнатуре, размер.
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
				o.ExcludeRequestBody = true
			}
			in := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: &o}
			if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
				s.writeError(w, r, validationProblem(err))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// validationProblem — ошибка проверки по схеме на русском с указанием поля.
func validationProblem(err error) error {
	field := ""
	var re *openapi3filter.RequestError
	if errors.As(err, &re) && re.Parameter != nil {
		field = re.Parameter.Name
	}
	var se *openapi3.SchemaError
	if field == "" && errors.As(err, &se) {
		if p := se.JSONPointer(); len(p) > 0 {
			field = strings.Join(p, ".")
		}
	}
	msg := "Неверный формат запроса"
	if field != "" {
		msg = "Недопустимое значение поля «" + field + "»"
	}
	e := apperr.BadRequest(msg)
	if field != "" {
		e.Fields = []apperr.FieldError{{Field: field, Message: "Недопустимое значение"}}
	}
	return e.Wrap(err)
}
