# Наймодатель

Информационная система управления арендой жилой недвижимости (ВКР). Спецификация — [`docs/spec/`](docs/spec/),
принятые по ходу решения — [`docs/open-questions.md`](docs/open-questions.md).

## Состав репозитория
| Каталог | Содержимое |
|---|---|
| `api/openapi.yaml` | Контракт REST API (OpenAPI 3.1) — источник генерации кода сервера и типов фронтенда |
| `backend/` | Go: `server` (HTTP API), `worker` (фоновые задачи River), `cli` (миграции и служебные команды) |
| `frontend/` | Vue 3 + TypeScript + PrimeVue 4.5: рабочее место собственника и кабинет нанимателя |
| `mocks/tbank`, `mocks/atol` | Эмуляторы Т-Банк эквайринга и АТОЛ Онлайн |
| `deploy/` | Caddyfile, образ PostgreSQL с ролями, `restore.sh`, `backup-files.sh` |
| `tests/e2e`, `tests/load` | Сквозные сценарии Playwright (E2E-1…E2E-8) и нагрузочный тест k6 |
| `docs/` | Руководства собственника и нанимателя, `deploy.md`, `database.md`, тест-кейсы и отчёт о тестировании |

## Быстрый старт (разработка)
Нужны Go ≥ 1.26, Node.js ≥ 22, Docker Desktop (Compose v2), GNU make и POSIX-оболочка (на Windows — Git Bash).

```bash
cd frontend && npm ci && cd ..
make dev
```

`make dev` создаёт `.env` из `.env.example` (если его нет), поднимает контейнеры (PostgreSQL 18, Gotenberg, Mailpit,
эмуляторы), применяет миграции и запускает `server`, `worker` и Vite с перезапуском при изменениях.

| Адрес | Что |
|---|---|
| http://localhost:5173 | Интерфейс |
| http://localhost:8080/api/v1/healthz, `/readyz` | Состояние API |
| http://localhost:8025 | Mailpit (перехваченные письма) |
| http://localhost:8081, http://localhost:8082 | Эмуляторы Т-Банка и АТОЛ |

`make seed` очищает бизнес-данные и загружает демо (docs/spec/17 Демо-данные.md); повторный запуск загружает заново.
Учётные записи демо: собственник `owner@landlord.local` / `Demo-2026-owner`; наниматели `kuznetsov@landlord.local`,
`romashka@landlord.local`, `smirnova@landlord.local` / `Demo-2026-tenant`; `lebedev@landlord.local` — приглашён
(письмо в Mailpit). Сдвиг даты системы — только при `APP_ENV=demo` и `DEMO_CLOCK_ENABLED=true` (Настройки → Служебное).

## Команды
| Команда | Действие |
|---|---|
| `make gen` | Генерация кода из `api/openapi.yaml` (oapi-codegen, openapi-typescript) и SQL (sqlc) |
| `make test` | Тесты backend (интеграционные — с Docker), эмуляторов и frontend |
| `make lint` | golangci-lint, eslint, vue-tsc, prettier |
| `make check` | `gen` + `lint` + `test` + проверка уязвимостей; после `gen` в git нет изменений |
| `make migrate`, `make migrate-down`, `make migrate-status` | Миграции схемы и очереди задач |
| `make cover` | Покрытие `internal/billing` (не ниже 70 %) |

Инструменты генерации и проверки (oapi-codegen, sqlc, golangci-lint, air, govulncheck) зафиксированы в
`backend/tools/go.mod` и запускаются через `go tool`; отдельно их ставить не нужно.

## Сервер и испытания
| Команда | Действие |
|---|---|
| `make build` | Образы `landlord-backend`, `landlord-web`, `landlord-postgres`, эмуляторов (`VERSION`, по умолчанию `dev`) |
| `make e2e` | Стек demo (`docker-compose.prod.yml --profile demo`, переменные `tests/e2e/e2e.env`, https://localhost) и Playwright |
| `make load` | Тот же стек, `seed --demo --load` и k6 (`tests/load/mixed.js`); итоги — `tests/load/summary.*` |
| `make e2e-down` | Остановить стек demo и удалить его тома |
| `make docs-db` | Описание БД `docs/database.md` из схемы (`cli docs db`) |

Стек demo занимает порты 80, 443, 8081, 8082, 8025 — окружение разработки на это время остановить (`make down`).
Установка на сервер, резервное копирование и восстановление — [`docs/deploy.md`](docs/deploy.md).
