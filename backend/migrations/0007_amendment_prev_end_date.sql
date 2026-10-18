-- Дата окончания договора до продления (docs/open-questions.md, № 107): по ней печатается первоначальный срок
-- в повторно сформированном договоре (№ 35). Заполняется при подписании допсоглашения о продлении.

-- +goose Up
ALTER TABLE contract_amendments ADD COLUMN prev_end_date date NULL;
COMMENT ON COLUMN contract_amendments.prev_end_date IS 'end_date договора до продления (для kind = extension)';

-- Уже подписанные продления: для второго и следующих — новая дата окончания предыдущего продления; для первого —
-- последний снимок договора в журнале (endDate в after) до записи о подписании; без записи — день перед датой
-- вступления продления.
WITH ext AS (
  SELECT a.id, a.contract_id, a.effective_from,
         lag(a.new_end_date) OVER (PARTITION BY a.contract_id ORDER BY a.applied_at, a.number) AS prev_new
  FROM contract_amendments a
  WHERE a.kind = 'extension' AND a.status = 'signed'
)
UPDATE contract_amendments t
SET prev_end_date = COALESCE(
  e.prev_new,
  (SELECT (l.after ->> 'endDate')::date FROM audit_log l
   WHERE l.entity_type = 'contract' AND l.entity_id = e.contract_id::text AND l.after ? 'endDate'
     AND l.id < COALESCE((SELECT min(s.id) FROM audit_log s WHERE s.action = 'amendment.sign' AND s.entity_id = e.id::text),
                         9223372036854775807)
   ORDER BY l.id DESC LIMIT 1),
  e.effective_from - 1)
FROM ext e
WHERE t.id = e.id;

-- +goose Down
ALTER TABLE contract_amendments DROP COLUMN prev_end_date;
