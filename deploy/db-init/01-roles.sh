#!/bin/bash
# Создание роли приложения landlord_app (docs/spec/18 Развёртывание и эксплуатация.md, раздел 4).
# Выполняется контейнером postgres при первой инициализации тома.
set -euo pipefail
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
  CREATE ROLE landlord_app LOGIN PASSWORD '${LANDLORD_APP_PASSWORD}';
  GRANT CONNECT ON DATABASE ${POSTGRES_DB} TO landlord_app;
  GRANT USAGE ON SCHEMA public TO landlord_app;
  ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO landlord_app;
  ALTER DEFAULT PRIVILEGES FOR ROLE landlord_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO landlord_app;
SQL
