// Эмулятор АТОЛ Онлайн v5 (docs/spec/08 Интеграции и эмуляторы.md, раздел 4): токен, чеки прихода и возврата
// прихода с проверками, отчёт о документе, колбэки, HTML «кассового чека» с QR-кодом, экран документов
// с переключателями для демонстрации.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
	_ "time/tzdata" // Europe/Moscow в образе distroless
)

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	delay, err := strconv.Atoi(getenv("PROCESS_DELAY_MS", "3000"))
	if err != nil || delay < 0 {
		log.Error(fmt.Sprintf("PROCESS_DELAY_MS: ожидается целое число миллисекунд, получено %q", os.Getenv("PROCESS_DELAY_MS")))
		os.Exit(1)
	}
	cfg := Config{
		PublicURL: getenv("MOCK_PUBLIC_URL", "http://localhost:8082"), Login: getenv("LOGIN", "demo"),
		Password: getenv("PASSWORD", "demo"), GroupCode: getenv("GROUP_CODE", "landlord-demo"),
		ProcessDelay: time.Duration(delay) * time.Millisecond, StateFile: getenv("STATE_FILE", "/data/atol-state.json"),
		CallbackAttempts: 3, CallbackInterval: 10 * time.Second,
	}
	store, err := NewStore(cfg.StateFile)
	if err != nil {
		log.Error("состояние эмулятора", slog.String("error", err.Error()))
		os.Exit(1)
	}
	// Нумерация фискальных документов не должна начинаться заново после перезапуска: файл состояния записывается
	// сразу, без записи эмулятор не запускается (как у эмулятора Т-Банка).
	if err := store.Init(time.Now()); err != nil {
		log.Error("файл состояния недоступен для записи", slog.String("file", cfg.StateFile), slog.String("error", err.Error()))
		os.Exit(1)
	}
	srv := NewServer(cfg, store, log)
	srv.Resume()
	addr := getenv("MOCK_ADDR", ":8082")
	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Info("эмулятор АТОЛ Онлайн запущен", slog.String("addr", addr), slog.String("public", cfg.PublicURL),
		slog.String("group", cfg.GroupCode))
	if err := hs.ListenAndServe(); err != nil {
		log.Error("эмулятор остановлен", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
