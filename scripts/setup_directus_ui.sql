-- ==============================================================================
-- DIRECTUS UI CONFIGURATION & BEAUTIFICATION
-- ==============================================================================

BEGIN;

-- 1. Скрываем старую таблицу server_settings
UPDATE directus_collections 
SET hidden = true 
WHERE collection = 'server_settings';

-- 2. Регистрируем server_config как SINGLETON
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'server_config', 'tune', 'Единая форма конфигурации узлов, слотов и цели сбора',
    NULL, false, true,
    '[{"language":"ru-RU","translation":"Параметры и слоты"}]',
    'all', '#FF5E1F', 1, 'grp_control', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    singleton = true,
    hidden = false,
    translations = EXCLUDED.translations,
    "group" = 'grp_control',
    sort = 1;

-- 3. Регистрируем cluster_nodes
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'cluster_nodes', 'storage', 'Реестр узлов кластера, прокси Hysteria 2 и аренда Aeza',
    '{{name}} ({{ip_address}})', false, false,
    '[{"language":"ru-RU","translation":"Узлы кластера"}]',
    'all', '#3b82f6', 2, 'grp_control', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    translations = EXCLUDED.translations,
    "group" = 'grp_control',
    sort = 2;

-- 4. Регистрируем security_bans
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'security_bans', 'security', 'Бан-листы нарушителей: хэши MachineGUID, аккаунты, IP',
    '{{target_type}}: {{target_value}}', false, false,
    '[{"language":"ru-RU","translation":"Реестр банов"}]',
    'all', '#ef4444', 4, 'grp_control', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    translations = EXCLUDED.translations,
    "group" = 'grp_control',
    sort = 4;

-- 5. Регистрируем ticket_canned_responses
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'ticket_canned_responses', 'quickreply', 'Шаблоны типовых быстрых ответов поддержки',
    '[{{category}}] {{title}}', false, false,
    '[{"language":"ru-RU","translation":"Шаблоны ответов"}]',
    'all', '#3b82f6', 3, 'grp_support', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    translations = EXCLUDED.translations,
    "group" = 'grp_support',
    sort = 3;

-- 6. Регистрируем forbidden_steam_games
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'forbidden_steam_games', 'block', 'Реестр Steam AppID, запрещенных к голосованию',
    '{{title}} (AppID {{steam_app_id}})', false, false,
    '[{"language":"ru-RU","translation":"Запрещенные игры"}]',
    'all', '#ef4444', 4, 'grp_community', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    translations = EXCLUDED.translations,
    "group" = 'grp_community',
    sort = 4;

-- 7. Регистрируем nickname_rules
INSERT INTO directus_collections (
    collection, icon, note, display_template, hidden, singleton, 
    translations, accountability, color, sort, "group", collapse
) VALUES (
    'nickname_rules', 'spellcheck', 'Черный список стоп-слов, мата, impersonation и исключений',
    '{{pattern}} ({{rule_type}})', false, false,
    '[{"language":"ru-RU","translation":"Фильтр позывных"}]',
    'all', '#ef4444', 5, 'grp_community', 'open'
) ON CONFLICT (collection) DO UPDATE SET
    translations = EXCLUDED.translations,
    "group" = 'grp_community',
    sort = 5;

-- 8. Конфигурация полей server_config
DELETE FROM directus_fields WHERE collection = 'server_config';

INSERT INTO directus_fields (collection, field, interface, options, display, display_options, readonly, hidden, sort, width, translations, note)
VALUES 
    ('server_config', 'id', 'input', NULL, NULL, NULL, true, true, 1, 'full', NULL, NULL),
    ('server_config', 'drain_mode', 'boolean', '{"colorOn":"#FF5E1F","label":"Включен (блокировать новые сессии)"}', 'boolean', NULL, false, false, 2, 'half', '[{"language":"ru-RU","translation":"Режим техобслуживания (Drain Mode)"}]', 'Запрещает создание новых сессий (HTTP 503 Service Unavailable)'),
    ('server_config', 'max_sessions', 'input', '{"min":1,"max":500,"step":1}', 'formatted-value', NULL, false, false, 3, 'half', '[{"language":"ru-RU","translation":"Общий пул слотов (Max Sessions)"}]', 'Максимальное число одновременных подключений к кластеру'),
    ('server_config', 'dedicated_sponsor_slots', 'input', '{"min":0,"max":100,"step":1}', 'formatted-value', NULL, false, false, 4, 'half', '[{"language":"ru-RU","translation":"Резерв слотов спонсоров"}]', 'Количество зарезервированных слотов для поддержавших проект'),
    ('server_config', 'donate_amount_rub', 'input', '{"min":10,"max":10000,"step":1}', 'formatted-value', NULL, false, false, 5, 'half', '[{"language":"ru-RU","translation":"Сумма спонсорства (руб/мес)"}]', 'Стоимость активации приоритетного статуса на 30 дней'),
    ('server_config', 'enable_donate', 'boolean', '{"colorOn":"#10b981","label":"Активен"}', 'boolean', NULL, false, false, 6, 'half', '[{"language":"ru-RU","translation":"Прием пожертвований"}]', 'Отображение кнопки доната и спонсорства в клиенте'),
    ('server_config', 'enable_voting', 'boolean', '{"colorOn":"#10b981","label":"Активно"}', 'boolean', NULL, false, false, 7, 'half', '[{"language":"ru-RU","translation":"Каталог голосования за игры"}]', 'Включение вкладки голосования за добавление новых игр'),
    ('server_config', 'enable_community_goal', 'boolean', '{"colorOn":"#10b981","label":"Активна"}', 'boolean', NULL, false, false, 8, 'half', '[{"language":"ru-RU","translation":"Цель сообщества (Boosty)"}]', 'Отображение баннера прогресса сбора средств в клиенте'),
    ('server_config', 'boosty_goal_title', 'input', NULL, NULL, NULL, false, false, 9, 'full', '[{"language":"ru-RU","translation":"Заголовок цели сбора на Boosty"}]', 'Название целевого сбора в шапке клиента'),
    ('server_config', 'boosty_goal_target', 'input', '{"min":1,"max":1000}', 'formatted-value', NULL, false, false, 10, 'half', '[{"language":"ru-RU","translation":"Целевой порог сбора"}]', 'Количество подписок или сумма цели'),
    ('server_config', 'boosty_goal_current', 'input', '{"min":0,"max":1000}', 'formatted-value', NULL, false, false, 11, 'half', '[{"language":"ru-RU","translation":"Текущий прогресс цели"}]', 'Текущее количество собранных единиц цели'),
    ('server_config', 'boosty_goal_source', 'select-dropdown', '{"choices":[{"text":"Автоматический парсер Boosty API","value":"auto_parser"},{"text":"Ручной ввод администратора","value":"manual"}]}', 'labels', NULL, false, false, 12, 'half', '[{"language":"ru-RU","translation":"Источник обновления цели"}]', 'Способ обновления данных о сборе'),
    ('server_config', 'boosty_goal_updated_at', 'datetime', NULL, 'datetime', NULL, true, false, 13, 'half', '[{"language":"ru-RU","translation":"Последний опрос Boosty"}]', 'Время последней синхронизации парсера'),
    ('server_config', 'updated_at', 'datetime', NULL, 'datetime', NULL, true, false, 14, 'half', '[{"language":"ru-RU","translation":"Время сохранения"}]', 'Метка последнего изменения параметров');

-- 9. Конфигурация полей cluster_nodes
DELETE FROM directus_fields WHERE collection = 'cluster_nodes';
INSERT INTO directus_fields (collection, field, interface, options, display, display_options, readonly, hidden, sort, width, translations, note)
VALUES
    ('cluster_nodes', 'node_id', 'input', NULL, NULL, NULL, false, false, 1, 'half', '[{"language":"ru-RU","translation":"ID узла"}]', 'Уникальный идентификатор ноды (например: sto-master)'),
    ('cluster_nodes', 'name', 'input', NULL, NULL, NULL, false, false, 2, 'half', '[{"language":"ru-RU","translation":"Название сервера"}]', 'Читаемое наименование хоста'),
    ('cluster_nodes', 'role', 'select-dropdown', '{"choices":[{"text":"Edge Прокси-шлюз (Hysteria 2)","value":"edge_proxy"},{"text":"Master Сервер (API, DB, UI)","value":"master_hub"}]}', 'labels', '{"choices":[{"text":"Edge Прокси-шлюз (Hysteria 2)","value":"edge_proxy","foreground":"#FFFFFF","background":"#3b82f6"},{"text":"Master Сервер (API, DB, UI)","value":"master_hub","foreground":"#FFFFFF","background":"#FF5E1F"}]}', false, false, 3, 'half', '[{"language":"ru-RU","translation":"Роль в кластере"}]', 'Назначение сервера'),
    ('cluster_nodes', 'location', 'select-dropdown', '{"choices":[{"text":"Москва (MSK)","value":"Moscow"},{"text":"Франкфурт (FRA)","value":"Frankfurt"},{"text":"Стокгольм (STO)","value":"Stockholm"}]}', 'labels', NULL, false, false, 4, 'half', '[{"language":"ru-RU","translation":"Локация дата-центра"}]', 'Географическое размещение'),
    ('cluster_nodes', 'ip_address', 'input', NULL, NULL, NULL, false, false, 5, 'half', '[{"language":"ru-RU","translation":"IPv4 адрес"}]', 'Публичный сетевой адрес'),
    ('cluster_nodes', 'status', 'select-dropdown', '{"choices":[{"text":"Активен (Active)","value":"active"},{"text":"Дренаж (Drain)","value":"drain"},{"text":"Техобслуживание (Maintenance)","value":"maintenance"},{"text":"Отключен (Offline)","value":"offline"}]}', 'labels', '{"choices":[{"text":"Активен","value":"active","foreground":"#FFFFFF","background":"#10b981"},{"text":"Дренаж","value":"drain","foreground":"#FFFFFF","background":"#f59e0b"},{"text":"Техобслуживание","value":"maintenance","foreground":"#FFFFFF","background":"#8b5cf6"},{"text":"Отключен","value":"offline","foreground":"#FFFFFF","background":"#ef4444"}]}', false, false, 6, 'half', '[{"language":"ru-RU","translation":"Статус узла"}]', 'Текущее эксплуатационное состояние'),
    ('cluster_nodes', 'paid_until', 'datetime', NULL, 'datetime', NULL, false, false, 7, 'half', '[{"language":"ru-RU","translation":"Оплачен до (Aeza VPS)"}]', 'Дата истечения периода аренды хостинга'),
    ('cluster_nodes', 'safety_buffer_days', 'input', '{"min":0,"max":30}', 'formatted-value', NULL, false, false, 8, 'half', '[{"language":"ru-RU","translation":"Буфер безопасности (дней)"}]', 'Дней вычитания из отображаемого срока (норматив: 7 дней)'),
    ('cluster_nodes', 'created_at', 'datetime', NULL, 'datetime', NULL, true, false, 9, 'half', '[{"language":"ru-RU","translation":"Дата создания"}]', NULL);

-- 10. Конфигурация полей forbidden_steam_games
DELETE FROM directus_fields WHERE collection = 'forbidden_steam_games';
INSERT INTO directus_fields (collection, field, interface, options, display, display_options, readonly, hidden, sort, width, translations, note)
VALUES
    ('forbidden_steam_games', 'steam_app_id', 'input', NULL, NULL, NULL, false, false, 1, 'half', '[{"language":"ru-RU","translation":"Steam App ID"}]', 'Числовой ID игры в каталоге Steam'),
    ('forbidden_steam_games', 'title', 'input', NULL, NULL, NULL, false, false, 2, 'half', '[{"language":"ru-RU","translation":"Название игры"}]', 'Официальное название игры'),
    ('forbidden_steam_games', 'reason', 'select-dropdown', '{"choices":[{"text":"NSFW / Непристойный контент","value":"nsfw"},{"text":"Читерское ПО / Вредоносное","value":"cheat_software"},{"text":"Троллинг / Спам каталога","value":"troll_spam"},{"text":"Неподдерживаемый сетевой протокол","value":"unsupported"}]}', 'labels', '{"choices":[{"text":"NSFW","value":"nsfw","foreground":"#FFFFFF","background":"#ef4444"},{"text":"Читерское ПО","value":"cheat_software","foreground":"#FFFFFF","background":"#dc2626"},{"text":"Спам/Троллинг","value":"troll_spam","foreground":"#FFFFFF","background":"#f59e0b"},{"text":"Неподдерживается","value":"unsupported","foreground":"#FFFFFF","background":"#6b7280"}]}', false, false, 3, 'half', '[{"language":"ru-RU","translation":"Причина блокировки"}]', 'Категория нарушения'),
    ('forbidden_steam_games', 'blocked_by', 'input', NULL, NULL, NULL, false, false, 4, 'half', '[{"language":"ru-RU","translation":"Кем заблокировано"}]', 'Администратор или система'),
    ('forbidden_steam_games', 'created_at', 'datetime', NULL, 'datetime', NULL, true, false, 5, 'half', '[{"language":"ru-RU","translation":"Дата блокировки"}]', NULL);

-- 11. Конфигурация полей nickname_rules
DELETE FROM directus_fields WHERE collection = 'nickname_rules';
INSERT INTO directus_fields (collection, field, interface, options, display, display_options, readonly, hidden, sort, width, translations, note)
VALUES
    ('nickname_rules', 'id', 'input', NULL, NULL, NULL, true, true, 1, 'full', NULL, NULL),
    ('nickname_rules', 'pattern', 'input', NULL, NULL, NULL, false, false, 2, 'half', '[{"language":"ru-RU","translation":"Слово / Корень / Паттерн"}]', 'Запрещенная последовательность символов или исключение'),
    ('nickname_rules', 'rule_type', 'select-dropdown', '{"choices":[{"text":"Оскорбление / Мат / Токсичность","value":"profanity"},{"text":"Фейковый админ (Impersonation)","value":"impersonation"},{"text":"Безопасное исключение (Whitelist)","value":"whitelist"}]}', 'labels', '{"choices":[{"text":"Мат / Токсичность","value":"profanity","foreground":"#FFFFFF","background":"#ef4444"},{"text":"Фейковый админ","value":"impersonation","foreground":"#FFFFFF","background":"#dc2626"},{"text":"Исключение (Whitelist)","value":"whitelist","foreground":"#FFFFFF","background":"#10b981"}]}', false, false, 3, 'half', '[{"language":"ru-RU","translation":"Тип правила"}]', 'Классификация фильтра'),
    ('nickname_rules', 'description', 'input', NULL, NULL, NULL, false, false, 4, 'half', '[{"language":"ru-RU","translation":"Пояснение / Описание"}]', 'Краткий комментарий для модераторов'),
    ('nickname_rules', 'is_active', 'boolean', '{"colorOn":"#10b981","label":"Активно"}', 'boolean', NULL, false, false, 5, 'half', '[{"language":"ru-RU","translation":"Статус правила"}]', 'Включение / отключение без удаления'),
    ('nickname_rules', 'created_at', 'datetime', NULL, 'datetime', NULL, true, false, 6, 'half', '[{"language":"ru-RU","translation":"Дата добавления"}]', NULL);

-- 12. Настройка цветных бейджей для support_tickets
UPDATE directus_fields 
SET interface = 'select-dropdown',
    display = 'labels',
    display_options = '{"choices":[{"text":"Новый","value":"new","foreground":"#FFFFFF","background":"#ef4444"},{"text":"В работе","value":"in_progress","foreground":"#FFFFFF","background":"#f59e0b"},{"text":"Решен","value":"resolved","foreground":"#FFFFFF","background":"#10b981"},{"text":"Закрыт","value":"closed","foreground":"#FFFFFF","background":"#4b5563"}]}'::json
WHERE collection = 'support_tickets' AND field = 'status';

-- 13. Настройка цветных бейджей для accounts (Спонсор vs Игрок)
UPDATE directus_fields 
SET interface = 'select-dropdown',
    display = 'labels',
    display_options = '{"choices":[{"text":"Спонсор","value":"sponsor","foreground":"#FFFFFF","background":"#FF5E1F"},{"text":"Игрок","value":"free","foreground":"#FFFFFF","background":"#374151"}]}'::json
WHERE collection = 'accounts' AND field = 'account_tier';

COMMIT;
