-- Описание БД для docs/database.md (cli docs db, docs/spec/18 Развёртывание и эксплуатация.md, раздел 10).
-- Таблицы схемы public, кроме служебных таблиц River и goose.

-- name: DocTables :many
SELECT c.relname::text AS name, coalesce(obj_description(c.oid, 'pg_class'), '')::text AS comment
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
  AND c.relname NOT LIKE 'river\_%' AND c.relname <> 'goose_db_version'
ORDER BY c.relname;

-- name: DocColumns :many
SELECT c.relname::text AS table_name, a.attname::text AS name,
       format_type(a.atttypid, a.atttypmod)::text AS type, a.attnotnull AS not_null,
       coalesce(pg_get_expr(d.adbin, d.adrelid), '')::text AS default_value,
       coalesce(col_description(c.oid, a.attnum), '')::text AS comment
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY c.relname, a.attnum;

-- name: DocConstraints :many
SELECT c.relname::text AS table_name, k.conname::text AS name, k.contype::text AS kind,
       pg_get_constraintdef(k.oid)::text AS definition
FROM pg_catalog.pg_constraint k
JOIN pg_catalog.pg_class c ON c.oid = k.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
ORDER BY c.relname, k.contype, k.conname;

-- name: DocIndexes :many
SELECT i.tablename::text AS table_name, i.indexname::text AS name, i.indexdef::text AS definition
FROM pg_catalog.pg_indexes i
WHERE i.schemaname = 'public'
  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_constraint k WHERE k.conname = i.indexname)
ORDER BY i.tablename, i.indexname;
