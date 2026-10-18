-- Согласие на обработку ПДн: «даю согласие оператору персональных данных ({{org.full_name}}, …)» — наименование
-- организации не согласовывалось по падежу (09 разд. 5.8, docs/open-questions.md, № 44).

-- +goose Up
UPDATE document_templates
SET body_html = replace(body_html, 'даю согласие {{org.full_name}} (ОГРНИП',
                        'даю согласие оператору персональных данных ({{org.full_name}}, ОГРНИП')
WHERE is_system AND kind = 'pdn_consent';

-- Изменённый собственником действующий шаблон: новая версия с заменой (прежние версии не меняются).
CREATE TEMP TABLE tpl_consent ON COMMIT DROP AS
SELECT id, kind, name,
       replace(body_html, 'даю согласие {{org.full_name}} (ОГРНИП',
               'даю согласие оператору персональных данных ({{org.full_name}}, ОГРНИП') AS body_html
FROM document_templates
WHERE is_active AND NOT is_system AND kind = 'pdn_consent'
  AND body_html LIKE '%даю согласие {{org.full_name}} (ОГРНИП%';

UPDATE document_templates SET is_active = false WHERE id IN (SELECT id FROM tpl_consent);

INSERT INTO document_templates (kind, name, body_html, version, is_active, is_system)
SELECT kind, name, body_html, (SELECT max(t.version) + 1 FROM document_templates t WHERE t.kind = d.kind), true, false
FROM tpl_consent d;

-- +goose Down
UPDATE document_templates
SET body_html = replace(body_html, 'даю согласие оператору персональных данных ({{org.full_name}}, ОГРНИП',
                        'даю согласие {{org.full_name}} (ОГРНИП')
WHERE is_system AND kind = 'pdn_consent';
