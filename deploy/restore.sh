#!/usr/bin/env bash
# Восстановление из резервной копии (docs/spec/18 Развёртывание и эксплуатация.md, раздел 8; NFR-Rel-01).
#   deploy/restore.sh <копия базы .sql.gz> [<архив файлов .tar.gz>]
# Путь — файл на хосте или путь внутри тома backups, например daily/landlord-20261018.sql.gz
# и files/files-20261018.tar.gz (список: docker compose -f docker-compose.prod.yml exec backup ls -R /backups).
# Запуск из каталога проекта. Другой файл compose или переменных — через COMPOSE и COMPOSE_PROJECT_NAME.
set -euo pipefail
export MSYS_NO_PATHCONV=1 # Git Bash на Windows не должен переписывать пути контейнера

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  sed -n '3,6p' "$0"
  exit 2
fi
dump="$1"
files="${2:-}"
COMPOSE="${COMPOSE:-docker compose -f docker-compose.prod.yml}"
project="${COMPOSE_PROJECT_NAME:-landlord}"
started=$(date +%s)
echo "Начало восстановления: $(date '+%F %T')"

# Содержимое файла копии: с хоста или из тома backups (через контейнер backup).
read_backup() {
  if [ -f "$1" ]; then
    cat "$1"
  else
    $COMPOSE exec -T backup cat "/backups/$1"
  fi
}
read_backup "$dump" | gzip -t || { echo "Копия базы не найдена или повреждена: $dump"; exit 1; }
if [ -n "$files" ]; then
  read_backup "$files" | gzip -t || { echo "Архив файлов не найден или повреждён: $files"; exit 1; }
fi

echo "1. Остановка server и worker"
$COMPOSE stop server worker

echo "2. Пересоздание базы landlord"
psql_owner() { $COMPOSE exec -T postgres psql -v ON_ERROR_STOP=1 -q -U landlord_owner "$@"; }
psql_owner -d postgres -c "DROP DATABASE IF EXISTS landlord WITH (FORCE)" -c "CREATE DATABASE landlord OWNER landlord_owner"
psql_owner -d landlord <<'SQL'
GRANT CONNECT ON DATABASE landlord TO landlord_app;
GRANT USAGE ON SCHEMA public TO landlord_app;
ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO landlord_app;
ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO landlord_app;
SQL

echo "3. Загрузка копии $dump"
read_backup "$dump" | gunzip -c | psql_owner -d landlord >/dev/null

if [ -n "$files" ]; then
  echo "4. Распаковка файлов $files в том ${project}_files"
  read_backup "$files" | docker run --rm -i -v "${project}_files:/data/files" alpine:3.22 \
    sh -euc 'find /data/files -mindepth 1 -delete; tar xzf - -C /data/files'
else
  echo "4. Архив файлов не указан — том files не меняется"
fi

echo "5. Запуск server и worker"
$COMPOSE start server worker
for _ in $(seq 1 60); do
  if $COMPOSE exec -T web wget -qO- http://server:8080/api/v1/readyz >/dev/null 2>&1; then break; fi
  sleep 2
done
$COMPOSE exec -T web wget -qO- http://server:8080/api/v1/readyz
echo
$COMPOSE exec -T server /app/cli audit verify

finished=$(date +%s)
echo "Конец восстановления: $(date '+%F %T'), длительность $(( (finished - started) / 60 )) мин $(( (finished - started) % 60 )) с (норматив — не более 1 часа)"
