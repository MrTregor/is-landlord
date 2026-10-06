# Команды разработки «Наймодателя» (docs/spec/03 Архитектура и репозиторий.md, «Команды Makefile»).
# Требуются: Go ≥ 1.26, Node.js ≥ 22, Docker с Compose v2, git, POSIX-оболочка (на Windows — Git Bash).

# Переменные окружения из .env загружает оболочка рецепта (LOADENV), а не make: так значения
# с кириллицей и пробелами одинаково передаются процессам в Linux и Windows. .env создаётся из .env.example.
LOADENV = set -a && . ./.env && set +a &&
VERSION ?= dev

GO      ?= go
GOTOOL  := $(GO) tool -modfile=tools/go.mod
GOEXE   := $(shell $(GO) env GOEXE)
COMPOSE ?= docker compose

GENERATED := backend/internal/api backend/internal/platform/db/sqlc frontend/src/api/schema.d.ts

.PHONY: help dev up down migrate migrate-down migrate-status seed gen gen-api gen-db check-gen \
        test test-backend test-mocks test-frontend cover bench lint vuln check build e2e load

help:
	@echo "make dev      — окружение разработки: контейнеры, миграции, server, worker, Vite"
	@echo "make gen      — генерация кода из api/openapi.yaml и SQL"
	@echo "make test     — тесты backend (с Docker), эмуляторов и frontend"
	@echo "make lint     — golangci-lint, eslint, vue-tsc, prettier"
	@echo "make check    — gen + lint + test + проверка зависимостей; после gen нет изменений в git"
	@echo "make migrate  — применить миграции; make migrate-down — откатить последнюю"

.env:
	cp .env.example .env

# ---------------------------------------------------------------- Окружение разработки
up: .env
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

dev: up migrate
	@echo "API: http://localhost:8080/api/v1 · интерфейс: http://localhost:5173 · почта: http://localhost:8025"
	@$(LOADENV) trap 'kill 0' INT TERM; \
	(cd backend && $(GOTOOL) air --tmp_dir tmp/server --build.cmd "$(GO) build -o tmp/server/server$(GOEXE) ./cmd/server" \
		--build.entrypoint "tmp/server/server$(GOEXE)" --build.exclude_dir "tmp,testdata,tools" --log.main_only true) & \
	(cd backend && $(GOTOOL) air --tmp_dir tmp/worker --build.cmd "$(GO) build -o tmp/worker/worker$(GOEXE) ./cmd/worker" \
		--build.entrypoint "tmp/worker/worker$(GOEXE)" --build.exclude_dir "tmp,testdata,tools" --log.main_only true) & \
	(cd frontend && npm run dev) & \
	wait

migrate: .env
	$(LOADENV) cd backend && $(GO) run ./cmd/cli migrate up

migrate-down: .env
	$(LOADENV) cd backend && $(GO) run ./cmd/cli migrate down

migrate-status: .env
	$(LOADENV) cd backend && $(GO) run ./cmd/cli migrate status

seed: .env
	$(LOADENV) cd backend && $(GO) run ./cmd/cli seed --demo

# ---------------------------------------------------------------- Генерация кода
gen: gen-api gen-db

gen-api:
	cd backend && $(GOTOOL) oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	cd backend && $(GO) run ./internal/api/stubgen internal/api/api.gen.go internal/api/stubs.gen.go
	cd frontend && npm run gen:api

gen-db:
	cd backend && $(GOTOOL) sqlc generate

# После make gen сгенерированные файлы не должны отличаться от закоммиченных.
check-gen:
	@git diff --exit-code --stat -- $(GENERATED) || \
		(echo "Сгенерированный код отличается от закоммиченного: выполните make gen и закоммитьте"; exit 1)
	@test -z "$$(git ls-files --others --exclude-standard -- $(GENERATED))" || \
		(echo "Есть незакоммиченные сгенерированные файлы"; exit 1)

# ---------------------------------------------------------------- Тесты и проверки
test: test-backend test-mocks test-frontend

test-backend:
	cd backend && $(GO) test -tags integration -count=1 ./...

test-mocks:
	cd mocks/tbank && $(GO) test -count=1 ./...
	cd mocks/atol && $(GO) test -count=1 ./...

test-frontend:
	cd frontend && npm run test

# Покрытие расчётной подсистемы не ниже 70 % (NFR-Maint-01).
cover:
	@if [ -d backend/internal/billing ]; then \
		cd backend && $(GO) test -coverprofile=coverage.out ./internal/billing/... && \
		total=$$($(GO) tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
		echo "Покрытие internal/billing: $$total %"; \
		awk -v t="$$total" 'BEGIN { exit (t < 70) }' || (echo "Покрытие ниже 70 %"; exit 1); \
	else echo "Пакет internal/billing появится на этапе M4"; fi

bench:
	cd backend && $(GO) test -run '^$$' -bench . ./internal/billing/...

lint:
	cd backend && $(GOTOOL) golangci-lint run ./...
	cd mocks/tbank && $(GO) vet ./...
	cd mocks/atol && $(GO) vet ./...
	cd frontend && npm run lint && npm run typecheck && npm run format:check

vuln:
	cd backend && $(GOTOOL) govulncheck ./...
	cd frontend && npm audit --omit=dev --audit-level=high

check: gen lint test vuln check-gen

# ---------------------------------------------------------------- Сборка и испытания
build:
	docker build -t landlord-backend:$(VERSION) backend
	docker build -t landlord-tbank-mock:$(VERSION) mocks/tbank
	docker build -t landlord-atol-mock:$(VERSION) mocks/atol

e2e:
	@echo "Сквозные тесты Playwright (tests/e2e) появятся на этапе M15"; exit 1

load:
	@echo "Нагрузочные сценарии k6 (tests/load) появятся на этапе M15"; exit 1
