-- Вид договора после «к» и «по» — переменная contract.title_dative; contract.title остаётся в именительном падеже
-- для квитанции («Основание: договор найма…»). В акт сверки — строка об остатке обеспечительного платежа.
-- docs/open-questions.md, № 34 и № 37.

-- +goose Up
-- Системные тексты (по ним работает «Сбросить к стандартному»; версия 1 действует, пока шаблон не меняли).
UPDATE document_templates
SET body_html = replace(replace(body_html, 'к {{contract.title}}', 'к {{contract.title_dative}}'),
                        'по {{contract.title}}', 'по {{contract.title_dative}}')
WHERE is_system AND kind IN ('act_handover', 'act_return', 'amendment', 'reconciliation_act');

UPDATE document_templates
SET body_html = replace(body_html, '<p>{{recon.closing_text}}</p>',
                        E'<p>{{recon.closing_text}}</p>\n<p>{{recon.deposit_text}}</p>')
WHERE is_system AND kind = 'reconciliation_act';

-- Изменённые собственником действующие шаблоны: новая версия с заменой (прежние версии не меняются).
CREATE TEMP TABLE tpl_dative ON COMMIT DROP AS
SELECT id, kind, name,
       replace(replace(body_html, 'к {{contract.title}}', 'к {{contract.title_dative}}'),
               'по {{contract.title}}', 'по {{contract.title_dative}}') AS body_html
FROM document_templates
WHERE is_active AND NOT is_system AND kind IN ('act_handover', 'act_return', 'amendment', 'reconciliation_act')
  AND (body_html LIKE '%к {{contract.title}}%' OR body_html LIKE '%по {{contract.title}}%');

UPDATE document_templates SET is_active = false WHERE id IN (SELECT id FROM tpl_dative);

INSERT INTO document_templates (kind, name, body_html, version, is_active, is_system)
SELECT kind, name, body_html, (SELECT max(t.version) + 1 FROM document_templates t WHERE t.kind = d.kind), true, false
FROM tpl_dative d;

-- +goose Down
UPDATE document_templates
SET body_html = replace(replace(replace(body_html, E'\n<p>{{recon.deposit_text}}</p>', ''),
                                'к {{contract.title_dative}}', 'к {{contract.title}}'),
                        'по {{contract.title_dative}}', 'по {{contract.title}}')
WHERE is_system;
