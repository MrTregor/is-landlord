#!/usr/bin/env bash
# Запуск окружения из конфигураций IDE (.run/*.run.xml) в WSL: make, Go и Node — Linux-версии.
#   deploy/run-wsl.sh dev   — make dev: контейнеры разработки, миграции, server, worker, Vite (http://localhost:5173)
#   deploy/run-wsl.sh prod  — make build и стек docker-compose.prod.yml --profile demo (https://localhost)
# Кнопка «Стоп» в IDE завершает wsl.exe — скрипт получает SIGHUP и останавливает всё, что запустил.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
if [ -s "$HOME/.nvm/nvm.sh" ]; then
	. "$HOME/.nvm/nvm.sh"
	nvm use "$(cat frontend/.nvmrc)" >/dev/null || exit 1
fi

# Стек prod занимает те же порты эмуляторов и почты (8081, 8082, 8025), что и контейнеры разработки.
PROD=(docker compose -f docker-compose.prod.yml --env-file tests/e2e/e2e.env --profile demo)

# Сборка запрашивает у Docker Hub метаданные только тех базовых образов, которых нет локально; соединение с ним
# бывает нестабильным («connection reset by peer»). Поэтому недостающие образы скачиваются заранее, с повторами.
pull_missing() {
	local img try
	for img in "$@"; do
		docker image inspect "$img" >/dev/null 2>&1 && continue
		for try in 1 2 3 4 5; do
			echo "Скачиваю $img (попытка $try)"
			docker pull -q "$img" >/dev/null && break
			[ "$try" = 5 ] && { echo "Не удалось скачать $img — проверьте доступ к Docker Hub" >&2; return 1; }
			sleep 5
		done
	done
}
# Базовые образы из FROM всех Dockerfile проекта (без стадий сборки «AS …»).
BASE_IMAGES=($(grep -hoP '^FROM\s+\K\S+' backend/Dockerfile frontend/Dockerfile deploy/postgres.Dockerfile mocks/*/Dockerfile | sort -u))

case "${1:-}" in
dev)
	"${PROD[@]}" stop >/dev/null 2>&1
	stop() {
		trap - HUP INT TERM
		kill -TERM -- "-$pid" 2>/dev/null
		# air запускает server и worker в своих группах процессов — их завершаем отдельно.
		pkill -f 'tmp/(server|worker)/(server|worker)$'
		wait
		exit 0
	}
	# make dev поднимает контейнеры с --build, а сборка каждый раз сверяет базовые образы с Docker Hub. Если образы
	# уже есть локально, контейнеры поднимаются без сборки, а цель up при make dev пропускается (-o up).
	goal=(dev)
	if docker image inspect landlord-postgres:dev landlord-tbank-mock:dev landlord-atol-mock:dev \
		gotenberg/gotenberg:8 axllent/mailpit:latest >/dev/null 2>&1; then
		docker compose up -d --wait || exit 1
		goal=(-o up dev)
	else
		pull_missing "${BASE_IMAGES[@]}" gotenberg/gotenberg:8 axllent/mailpit:latest || exit 1
	fi
	# Изменения файлов на диске Windows не приходят в WSL событиями — air и Vite опрашивают файлы сами.
	export AIR_FLAGS="--build.poll true"
	setsid make "${goal[@]}" &
	pid=$!
	trap stop HUP INT TERM
	wait "$pid"
	;;
prod)
	docker compose stop
	pull_missing "${BASE_IMAGES[@]}" gotenberg/gotenberg:8 axllent/mailpit:latest \
		prodrigestivill/postgres-backup-local:18 || exit 1
	make build || exit 1
	"${PROD[@]}" up -d || exit 1
	# «Стоп» закрывает сеанс WSL, и запущенное в этот момент (в том числе из ловушки сигнала) сразу завершается.
	# Поэтому сторож стартует заранее, в отдельном сеансе: когда скрипт завершится, он остановит контейнеры.
	setsid bash -c 'while kill -0 "$0" 2>/dev/null; do sleep 1; done; exec "$@" stop' "$$" "${PROD[@]}" \
		</dev/null >/dev/null 2>&1 &
	echo "Интерфейс: https://localhost · почта: http://localhost:8025 · банк: http://localhost:8081 · касса: http://localhost:8082"
	echo "Пустая база: ${PROD[*]} exec server /app/cli seed --demo (или user create-owner)"
	"${PROD[@]}" logs -f --tail 20 server worker &
	wait $!
	;;
*)
	echo "Использование: deploy/run-wsl.sh dev|prod" >&2
	exit 2
	;;
esac
