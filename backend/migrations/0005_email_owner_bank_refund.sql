-- Письмо собственнику о возврате проведённого онлайн-платежа через банк (REFUNDED, PARTIAL_REFUNDED):
-- система такой возврат не учитывает сама (docs/open-questions.md, № 57; 10 Уведомления, раздел 3).

-- +goose Up
INSERT INTO email_templates (code, name, recipient, subject, body_html) VALUES
('owner_bank_refund', 'Возврат платежа через банк', 'owner',
 'Банк сообщил о возврате платежа {{amount}} — {{tenant.short_name}}',
 $$<p>Банк сообщил: {{status_title}} по онлайн-платежу {{amount}} от {{received_on}} (квитанция {{receipt_number}}) от {{tenant.short_name}} по договору № {{contract.number}}.</p>
<p>Возвраты через банк система не учитывает автоматически: проверьте расчёты с нанимателем и при необходимости внесите корректировку вручную.</p>
<p><a href="{{link}}">Открыть платёж</a></p>$$)
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM email_templates WHERE code = 'owner_bank_refund';
