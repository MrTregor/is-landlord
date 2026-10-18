#!/usr/bin/env bash
# Резервная копия загруженных файлов (том files) — docs/spec/18 Развёртывание и эксплуатация.md, раздел 8.
# Архив /backups/files/files-ГГГГММДД.tar.gz в томе backups, хранятся 30 последних дней.
# Запуск из каталога проекта ежедневно (cron на хосте, после копии базы в 02:00):
#   30 2 * * * cd /opt/landlord && deploy/backup-files.sh >> /var/log/landlord-backup-files.log 2>&1
set -euo pipefail
export MSYS_NO_PATHCONV=1 # Git Bash на Windows не должен переписывать пути контейнера

project="${COMPOSE_PROJECT_NAME:-landlord}"
stamp="$(TZ=Europe/Moscow date +%Y%m%d)"

docker run --rm \
  -v "${project}_files:/data/files:ro" \
  -v "${project}_backups:/backups" \
  alpine:3.22 sh -euc "
    mkdir -p /backups/files
    tar czf /backups/files/files-${stamp}.tar.gz -C /data/files .
    find /backups/files -name 'files-*.tar.gz' -mtime +29 -delete
    ls -l /backups/files/files-${stamp}.tar.gz"
echo "$(date '+%F %T') копия файлов: files/files-${stamp}.tar.gz"
