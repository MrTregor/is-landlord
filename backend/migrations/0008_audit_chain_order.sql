-- Порядок цепочки журнала действий при параллельных транзакциях (docs/open-questions.md, № 109).
-- id записи выдаётся до триггера, то есть до advisory-блокировки: транзакция A получает id N и ждёт блокировку,
-- транзакция B получает id N+1, первой проходит блокировку и фиксируется; A затем берёт prev_hash от записи N+1.
-- Цепочка в порядке id разрывается, и проверка (cli audit verify) сообщает о нарушении без подделки.
-- Исправление: под блокировкой запись получает новый id, если он не больше последнего (только при гонке —
-- в обычном случае id не меняется). Записи, уже разорванные гонкой, остаются как есть (журнал неизменяем).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_log_chain() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE last_id bigint; last_hash bytea;
BEGIN
  PERFORM pg_advisory_xact_lock(7428001);
  SELECT id, hash INTO last_id, last_hash FROM audit_log ORDER BY id DESC LIMIT 1;
  IF last_id IS NOT NULL AND NEW.id <= last_id THEN
    NEW.id := nextval(pg_get_serial_sequence('audit_log', 'id'));
  END IF;
  NEW.prev_hash := COALESCE(last_hash, '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea);
  NEW.hash := digest(NEW.prev_hash || convert_to(concat_ws('|',
      NEW.id::text,
      to_char(NEW.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      COALESCE(NEW.actor_user_id::text, ''), NEW.actor_role, NEW.action, NEW.entity_type, NEW.entity_id,
      NEW.summary, COALESCE(NEW.before::text, ''), COALESCE(NEW.after::text, '')), 'UTF8'), 'sha256');
  RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_log_chain() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE last_hash bytea;
BEGIN
  PERFORM pg_advisory_xact_lock(7428001);
  SELECT hash INTO last_hash FROM audit_log ORDER BY id DESC LIMIT 1;
  NEW.prev_hash := COALESCE(last_hash, '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea);
  NEW.hash := digest(NEW.prev_hash || convert_to(concat_ws('|',
      NEW.id::text,
      to_char(NEW.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      COALESCE(NEW.actor_user_id::text, ''), NEW.actor_role, NEW.action, NEW.entity_type, NEW.entity_id,
      NEW.summary, COALESCE(NEW.before::text, ''), COALESCE(NEW.after::text, '')), 'UTF8'), 'sha256');
  RETURN NEW;
END $$;
-- +goose StatementEnd
