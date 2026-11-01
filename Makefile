# Команды разработки «Наймодателя» (docs/spec/03 Архитектура и репозиторий.md, «Команды Makefile»).
# Требуются: Go ≥ 1.26, Node.js 22.22.2+ или 24.15+, Docker с Compose v2, git, POSIX-оболочка (на Windows — Git Bash).

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
        test test-backend test-mocks test-frontend cover bench lint vuln check build e2e e2e-up e2e-down load docs-db

help:
	@echo "make dev      — окружение разработки: контейнеры, миграции, server, worker, Vite"
	@echo "make gen      — генерация кода из api/openapi.yaml и SQL"
	@echo "make test     — тесты backend (с Docker), эмуляторов и frontend"
	@echo "make lint     — golangci-lint, eslint, vue-tsc, prettier"
	@echo "make check    — gen + lint + test + проверка зависимостей; после gen нет изменений в git"
	@echo "make migrate  — применить миграции; make migrate-down — откатить последнюю"
	@echo "make build    — образы сервера; make e2e — стек demo и Playwright; make load — k6"
	@echo "make docs-db  — описание БД docs/database.md"

.env:
	cp .env.example .env

# ---------------------------------------------------------------- Окружение разработки
up: .env
	$(COMPOSE) up -d --build --wait

down:
	$(COMPOSE) down

# Дополнительные флаги air; deploy/run-wsl.sh включает опрос файлов (--build.poll true): из WSL изменения на диске
# Windows (/mnt/...) не приходят событиями, без опроса server и worker не пересобираются.
AIR_FLAGS ?=

dev: up migrate
	@echo "API: http://localhost:8080/api/v1 · интерфейс: http://localhost:5173 · почта: http://localhost:8025"
	@$(LOADENV) trap 'kill 0' INT TERM; \
	(cd backend && $(GOTOOL) air --tmp_dir tmp/server --build.cmd "$(GO) build -o tmp/server/server$(GOEXE) ./cmd/server" \
		--build.entrypoint "tmp/server/server$(GOEXE)" --build.exclude_dir "tmp,testdata,tools" --log.main_only true $(AIR_FLAGS)) & \
	(cd backend && $(GOTOOL) air --tmp_dir tmp/worker --build.cmd "$(GO) build -o tmp/worker/worker$(GOEXE) ./cmd/worker" \
		--build.entrypoint "tmp/worker/worker$(GOEXE)" --build.exclude_dir "tmp,testdata,tools" --log.main_only true $(AIR_FLAGS)) & \
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
		cd backend && $(GO) test -tags integration -count=1 -coverprofile=coverage.out ./internal/billing/... && \
		total=$$($(GO) tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
		echo "Покрытие internal/billing: $$total %"; \
		awk -v t="$$total" 'BEGIN { exit (t < 75) }' || (echo "Покрытие ниже 75 %"; exit 1); \
	else echo "Пакет internal/billing появится на этапе M4"; fi

bench:
	cd backend && $(GO) test -tags integration -run '^$$' -bench . -benchtime 1x ./internal/billing/...

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
# Образы сервера (docs/spec/18, раздел 2). VERSION — по умолчанию dev (make build VERSION=$$(git rev-parse --short HEAD)).
build:
	docker build -t landlord-backend:$(VERSION) backend
	docker build -t landlord-web:$(VERSION) -f frontend/Dockerfile .
	docker build -t landlord-postgres:$(VERSION) -f deploy/postgres.Dockerfile deploy
	docker build -t landlord-tbank-mock:$(VERSION) mocks/tbank
	docker build -t landlord-atol-mock:$(VERSION) mocks/atol

# Стек demo для сквозных и нагрузочных тестов: проект landlord-e2e, порты 80, 443, 8081, 8082, 8025
# (окружение разработки make dev на время испытаний остановить: make down).
E2E_COMPOSE = $(COMPOSE) -f docker-compose.prod.yml --env-file tests/e2e/e2e.env --profile demo

e2e-up: build
	$(E2E_COMPOSE) up -d
	@for i in $$(seq 1 60); do curl -fsk https://localhost/api/v1/readyz >/dev/null 2>&1 && exit 0; sleep 2; done; \
		echo "Сервер не ответил на /api/v1/readyz"; exit 1

e2e-down:
	$(E2E_COMPOSE) down -v

# Демо-данные загружаются заново перед каждым файлом сценариев (tests/e2e/support/stack.ts).
e2e: e2e-up
	cd tests/e2e && npm ci && npx playwright install chromium && npx playwright test

# Нагрузка (docs/spec/16, раздел 5): демо-данные с --load, затем k6 в сетевом пространстве контейнера web.
# Сценарий передаётся через stdin, итоги забираются docker cp: Docker Desktop на Windows не монтирует каталоги
# с кириллицей в пути (docs/open-questions.md, № 9). Итоги — tests/load/summary.json и tests/load/summary.txt.
load: e2e-up
	MSYS_NO_PATHCONV=1 $(E2E_COMPOSE) exec -T server /app/cli seed --demo --load
	-docker rm -f landlord-k6 >/dev/null 2>&1
	MSYS_NO_PATHCONV=1 docker run -i --name landlord-k6 --network container:landlord-e2e-web-1 grafana/k6:1.3.0 \
		run --quiet --summary-export /tmp/summary.json - < tests/load/mixed.js > tests/load/summary.txt; \
		status=$$?; cat tests/load/summary.txt; docker cp landlord-k6:/tmp/summary.json tests/load/summary.json; \
		docker rm landlord-k6 >/dev/null; exit $$status

# Описание БД из схемы и комментариев COMMENT ON (docs/spec/18, раздел 10).
docs-db: .env
	$(LOADENV) cd backend && $(GO) run ./cmd/cli docs db > ../docs/database.md
