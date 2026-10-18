package httpserver

import (
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"

	"landlord/backend/internal/api"
	"landlord/backend/internal/config"
)

// Страница описания API (18 Развёртывание, раздел 10; NFR-Comp-02): Redoc, скрипт встроен в бинарник (redoc 2.4.0,
// MIT, docs/REDOC-LICENSE). Только в режимах dev и demo; права — как у методов собственника (security.go).

//go:embed docs/redoc.standalone.js
var redocJS []byte

const docsPage = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API — Наймодатель</title>
</head>
<body>
<redoc spec-url="` + BaseURL + `/docs/openapi.json" hide-download-button></redoc>
<script src="` + BaseURL + `/docs/redoc.js"></script>
</body>
</html>
`

func (s *Server) mountDocs(r chi.Router) {
	if s.deps.Config == nil || s.deps.Config.AppEnv == config.EnvProd {
		return
	}
	r.Get(BaseURL+"/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	})
	r.Get(BaseURL+"/docs/redoc.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(redocJS)
	})
	r.Get(BaseURL+"/docs/openapi.json", func(w http.ResponseWriter, req *http.Request) {
		spec, err := api.GetSpecJSON()
		if err != nil {
			s.writeError(w, req, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	})
}
