-- Тексты писем по умолчанию (docs/spec/10 Уведомления.md, раздел 4).
-- В спецификации это миграция 0003; перенесена раньше, потому что письма invite и password_reset
-- нужны на этапе M1 (docs/open-questions.md, № 10). Шаблоны документов — миграция 0003.

-- +goose Up
INSERT INTO email_templates (code, name, recipient, subject, body_html) VALUES
('invite', 'Приглашение в личный кабинет', 'user',
 'Приглашение в личный кабинет — {{app_name}}',
 $$<p>Здравствуйте, {{display_name}}!</p>
<p>{{org.short_name}} приглашает вас в личный кабинет. В нём можно смотреть начисления и договор, оплачивать онлайн, передавать показания счётчиков и подавать заявки.</p>
<p><a href="{{link}}">Создать пароль и войти</a></p>
<p>Ссылка действует 7 дней.</p>$$),

('password_reset', 'Восстановление пароля', 'user',
 'Восстановление пароля — {{app_name}}',
 $$<p>Вы запросили восстановление пароля. <a href="{{link}}">Задать новый пароль</a>. Ссылка действует 1 час.</p>
<p>Если вы не запрашивали восстановление, просто удалите это письмо.</p>$$),

('payment_reminder', 'Напоминание об оплате', 'tenant',
 'Напоминание об оплате до {{due_date}}',
 $$<p>{{tenant.greeting}}, здравствуйте!</p>
<p>Напоминаем: до {{due_date}} нужно внести оплату по договору № {{contract.number}} ({{property.address}}).</p>
{{block:lines}}
<p><b>Итого: {{total}}</b></p>
<p><a href="{{cabinet_url}}">Оплатить в личном кабинете</a></p>$$),

('payment_overdue', 'Просрочка оплаты', 'tenant',
 'Просрочена оплата по договору № {{contract.number}}',
 $$<p>{{tenant.greeting}}, здравствуйте!</p>
<p>По договору № {{contract.number}} есть просроченная задолженность: <b>{{overdue_total}}</b>.</p>
{{block:lines}}
<p>Начисленные на сегодня пени: {{penalties_accruing}}. Пени начисляются за каждый день просрочки по условиям договора.</p>
<p><a href="{{cabinet_url}}">Оплатить в личном кабинете</a></p>$$),

('payment_receipt', 'Квитанция об оплате', 'tenant',
 'Квитанция {{receipt_number}} об оплате',
 $$<p>{{tenant.greeting}}, спасибо! Мы получили оплату {{amount}} от {{received_on}} по договору № {{contract.number}}.</p>
<p>Квитанция во вложении и в <a href="{{cabinet_url}}">личном кабинете</a>.</p>$$),

('fiscal_receipt', 'Кассовый чек', 'tenant',
 'Кассовый чек на {{amount}}',
 $$<p>Кассовый чек по оплате {{amount}} по договору № {{contract.number}}: <a href="{{receipt_url}}">открыть чек</a>.</p>$$),

('contract_ending', 'Окончание договора', 'tenant',
 'Договор № {{contract.number}} заканчивается {{end_date}}',
 $$<p>{{tenant.greeting}}, здравствуйте!</p>
<p>Срок договора № {{contract.number}} ({{property.address}}) заканчивается {{end_date}} — через {{days_left}} дн. Сообщите, пожалуйста, планируете ли продлить договор: {{org.phone}}, {{org.email}}.</p>$$),

('rent_changed', 'Изменение платы', 'tenant',
 'Изменение платы по договору № {{contract.number}}',
 $$<p>{{tenant.greeting}}, здравствуйте!</p>
<p>По подписанному дополнительному соглашению с {{effective_from}} плата составляет {{new_rent}} в месяц. Документ доступен в <a href="{{cabinet_url}}">личном кабинете</a>.</p>$$),

('readings_reminder', 'Напоминание о показаниях', 'tenant',
 'Передайте показания счётчиков за {{period}}',
 $$<p>{{tenant.greeting}}, здравствуйте! Передайте, пожалуйста, показания счётчиков за {{period}} до {{window_to}} в <a href="{{cabinet_url}}">личном кабинете</a>.</p>$$),

('readings_rejected', 'Показание не принято', 'tenant',
 'Показание не принято',
 $$<p>Показание «{{meter}}»: {{value}} не принято. Причина: {{reason}}. Передайте исправленное значение в <a href="{{cabinet_url}}">личном кабинете</a>.</p>$$),

('request_status_changed', 'Изменение статуса заявки', 'tenant',
 'Заявка {{request.number}}: {{status_title}}',
 $$<p>Статус заявки {{request.number}} «{{request.title}}»: <b>{{status_title}}</b>.</p>
<p>{{comment}}</p>
<p><a href="{{cabinet_url}}">Открыть заявку</a></p>$$),

('deposit_withheld', 'Удержание из залога', 'tenant',
 'Удержание из обеспечительного платежа',
 $$<p>Из обеспечительного платежа по договору № {{contract.number}} удержано {{amount}}. Основание: {{reason}}. Остаток: {{balance}}.</p>$$),

('deposit_refunded', 'Возврат залога', 'tenant',
 'Возврат обеспечительного платежа',
 $$<p>По договору № {{contract.number}} {{op_date}} возвращён обеспечительный платёж {{amount}}.</p>$$),

('owner_payment_received', 'Поступил платёж', 'owner',
 'Поступил платёж {{amount}} — {{tenant.short_name}}',
 $$<p>{{received_on}} поступил платёж {{amount}} ({{method_title}}) от {{tenant.short_name}} по договору № {{contract.number}}. <a href="{{link}}">Открыть</a></p>$$),

('owner_discrepancy', 'Расхождение при сверке', 'owner',
 'Расхождение при сверке: {{kind_title}}',
 $$<p>{{kind_title}} на сумму {{amount}}. {{details}}</p><p><a href="{{link}}">Разобрать</a></p>$$),

('owner_request_created', 'Новая заявка', 'owner',
 'Новая заявка {{request.number}}: {{request.title}}',
 $$<p>{{tenant.short_name}} ({{property.code}}) создал заявку «{{request.title}}», категория «{{request.category_title}}». <a href="{{link}}">Открыть</a></p>$$),

('owner_readings_submitted', 'Переданы показания', 'owner',
 'Показания за {{period}}: {{property.code}}',
 $$<p>Наниматель передал показания за {{period}}. <a href="{{link}}">Проверить и принять</a></p>$$),

('owner_indexation_due', 'Предстоит индексация', 'owner',
 'Индексация платы с {{effective_from}}: договор № {{contract.number}}',
 $$<p>Подготовлен черновик допсоглашения: с {{effective_from}} плата {{current_rent}} → {{new_rent}}. Подпишите его с нанимателем или отмените. <a href="{{link}}">Открыть</a></p>$$),

('owner_fiscal_error', 'Ошибка кассового чека', 'owner',
 'Ошибка кассового чека на {{amount}}',
 $$<p>Чек по платежу {{amount}} не сформирован: {{error_text}}. <a href="{{link}}">Повторить</a></p>$$),

('owner_daily_digest', 'Утренняя сводка', 'owner',
 'Сводка на {{today}}',
 $$<p>Добрый день! Что важно сегодня:</p>
{{block:lines}}
<p><a href="{{owner_url}}">Открыть рабочее место</a></p>$$);

-- +goose Down
DELETE FROM email_log;
DELETE FROM email_templates;
