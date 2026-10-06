# PostgreSQL 18 для docker-compose.yml (разработка): скрипт ролей копируется в образ, а не монтируется —
# Docker Desktop на Windows не монтирует каталоги с кириллицей в пути (docs/open-questions.md, № 9).
FROM postgres:18
COPY db-init/ /docker-entrypoint-initdb.d/
RUN chmod +x /docker-entrypoint-initdb.d/*.sh
