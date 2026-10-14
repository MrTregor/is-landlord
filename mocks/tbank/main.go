// Эмулятор Т-Банк эквайринга (docs/spec/08 Интеграции и эмуляторы.md, раздел 2): Init, GetState, страница оплаты
// (карта, СБП, отказ), уведомления с повторами, экран платежей и служебные действия для демонстрации.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Moscow в образе distroless
)

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// parseTerminals — «ключ:пароль,ключ2:пароль2».
func parseTerminals(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, pass, ok := strings.Cut(part, ":")
		if !ok || key == "" || pass == "" {
			return nil, fmt.Errorf("TERMINALS: ожидается «ключ:пароль», получено %q", part)
		}
		out[key] = pass
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("TERMINALS: не задан ни один терминал")
	}
	return out, nil
}

// parseRetry — «0s,10s,30s» → смещения попыток от первой.
func parseRetry(s string) ([]time.Duration, error) {
	var out []time.Duration
	for _, part := range strings.Split(s, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("NOTIFY_RETRY: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	terminals, err := parseTerminals(getenv("TERMINALS", "LandlordDemo:demo-terminal-password"))
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	retry, err := parseRetry(getenv("NOTIFY_RETRY", "0s,10s,30s,60s,120s,300s"))
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	cfg := Config{PublicURL: getenv("MOCK_PUBLIC_URL", "http://localhost:8081"), Terminals: terminals, Retry: retry,
		StateFile: getenv("STATE_FILE", "/data/tbank-state.json")}
	store, err := NewStore(cfg.StateFile)
	if err != nil {
		log.Error("состояние эмулятора", slog.String("error", err.Error()))
		os.Exit(1)
	}
	// Счётчик PaymentId должен пережить перезапуск контейнера (иначе система получит уже сохранённые PaymentId,
	// docs/open-questions.md, № 55): файл состояния записывается сразу, без записи эмулятор не запускается.
	if err := store.Save(); err != nil {
		log.Error("файл состояния недоступен для записи", slog.String("file", cfg.StateFile), slog.String("error", err.Error()))
		os.Exit(1)
	}
	var next int64
	store.Read(func(st *State) { next = st.Seq + 1 })
	log.Info("состояние эмулятора загружено", slog.String("file", cfg.StateFile), slog.Int64("nextPaymentId", next))
	addr := getenv("MOCK_ADDR", ":8081")
	srv := &http.Server{Addr: addr, Handler: NewServer(cfg, store, log).Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Info("эмулятор Т-Банка запущен", slog.String("addr", addr), slog.String("public", cfg.PublicURL))
	if err := srv.ListenAndServe(); err != nil {
		log.Error("эмулятор остановлен", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
