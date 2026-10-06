// Эмулятор atol (docs/spec/08 Интеграции и эмуляторы.md). На этапе M0 — каркас:
// проверка доступности и титульная страница; протокол реализуется на этапе M8.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

const banner = "Учебный эмулятор АТОЛ Онлайн"

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html lang=\"ru\"><meta charset=\"utf-8\"><title>" + banner + "</title>" +
			"<body style=\"font-family:system-ui,sans-serif\"><div style=\"background:#fde68a;padding:8px\">" + banner +
			"</div><p>Эмулятор будет реализован на этапе M8.</p></body></html>"))
	})
	return mux
}

func main() {
	addr := os.Getenv("MOCK_ADDR")
	if addr == "" {
		addr = ":8082"
	}
	srv := &http.Server{Addr: addr, Handler: newMux(), ReadHeaderTimeout: 10 * time.Second}
	slog.Info("эмулятор запущен", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("эмулятор остановлен", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
