-- ==============================================================================
-- DIRECTUS INSIGHTS (ANALYTICS DASHBOARDS & PANELS)
-- ==============================================================================

BEGIN;

-- 1. Создание дашборда поддержки
INSERT INTO directus_dashboards (id, name, icon, note, color, user_created)
VALUES (
    'c1111111-1111-1111-1111-111111111111',
    'Оперативный пульт поддержки',
    'support_agent',
    'Контроль входящих обращений игроков, вылетов и очередей поддержки',
    '#3b82f6',
    '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
) ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    note = EXCLUDED.note,
    color = EXCLUDED.color;

-- 2. Создание дашборда финансов и сообщества
INSERT INTO directus_dashboards (id, name, icon, note, color, user_created)
VALUES (
    'c2222222-2222-2222-2222-222222222222',
    'Финансы, Спонсоры и Сообщество',
    'payments',
    'Мониторинг спонсоров, пожертвований, реестра банов и каталога голосования',
    '#FF5E1F',
    '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
) ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    note = EXCLUDED.note,
    color = EXCLUDED.color;

-- Очистка старых панелей
DELETE FROM directus_panels WHERE dashboard IN (
    'c1111111-1111-1111-1111-111111111111',
    'c2222222-2222-2222-2222-222222222222'
);

-- 3. Панели для дашборда поддержки
INSERT INTO directus_panels (
    id, dashboard, name, icon, color, show_header, note, type, 
    position_x, position_y, width, height, options, user_created
) VALUES
    (
        'd1111111-0001-0000-0000-000000000001',
        'c1111111-1111-1111-1111-111111111111',
        'Новые тикеты (требуют ответа)',
        'mark_email_unread',
        '#ef4444',
        true,
        'Обращения в статусе new',
        'metric',
        1, 1, 6, 6,
        '{"collection":"support_tickets","filter":{"status":{"_eq":"new"}},"function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd1111111-0002-0000-0000-000000000002',
        'c1111111-1111-1111-1111-111111111111',
        'В работе / Диагностика',
        'pending_actions',
        '#f59e0b',
        true,
        'Обращения в процессе решения',
        'metric',
        7, 1, 6, 6,
        '{"collection":"support_tickets","filter":{"status":{"_eq":"in_progress"}},"function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd1111111-0003-0000-0000-000000000003',
        'c1111111-1111-1111-1111-111111111111',
        'Успешно решено',
        'task_alt',
        '#10b981',
        true,
        'Тикеты в статусе resolved',
        'metric',
        13, 1, 6, 6,
        '{"collection":"support_tickets","filter":{"status":{"_eq":"resolved"}},"function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd1111111-0004-0000-0000-000000000004',
        'c1111111-1111-1111-1111-111111111111',
        'Всего тикетов в архиве',
        'archive',
        '#3b82f6',
        true,
        'Общее количество за все время',
        'metric',
        19, 1, 6, 6,
        '{"collection":"support_tickets","function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd1111111-0005-0000-0000-000000000005',
        'c1111111-1111-1111-1111-111111111111',
        'Последние обращения игроков',
        'format_list_bulleted',
        '#3b82f6',
        true,
        'Оперативная очередь тикетов',
        'list',
        1, 7, 24, 12,
        '{"collection":"support_tickets","fields":["id","status","account_number","incident_type","comment"],"sort":["-created_at"],"limit":8}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    );

-- 4. Панели для дашборда финансов и сообщества
INSERT INTO directus_panels (
    id, dashboard, name, icon, color, show_header, note, type, 
    position_x, position_y, width, height, options, user_created
) VALUES
    (
        'd2222222-0001-0000-0000-000000000001',
        'c2222222-2222-2222-2222-222222222222',
        'Активные спонсоры',
        'military_tech',
        '#FF5E1F',
        true,
        'Пользователи со статусом sponsor',
        'metric',
        1, 1, 6, 6,
        '{"collection":"accounts","filter":{"tier":{"_eq":"sponsor"}},"function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd2222222-0002-0000-0000-000000000002',
        'c2222222-2222-2222-2222-222222222222',
        'Всего игроков в базе',
        'people',
        '#10b981',
        true,
        'Уникальные игровые аккаунты',
        'metric',
        7, 1, 6, 6,
        '{"collection":"accounts","function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd2222222-0003-0000-0000-000000000003',
        'c2222222-2222-2222-2222-222222222222',
        'Игр на голосовании',
        'sports_esports',
        '#8b5cf6',
        true,
        'Каталог предложенных игр',
        'metric',
        13, 1, 6, 6,
        '{"collection":"game_suggestions","function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd2222222-0004-0000-0000-000000000004',
        'c2222222-2222-2222-2222-222222222222',
        'Заблокировано нарушителей',
        'gavel',
        '#ef4444',
        true,
        'Записи в реестре банов',
        'metric',
        19, 1, 6, 6,
        '{"collection":"security_bans","function":"count"}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    ),
    (
        'd2222222-0005-0000-0000-000000000005',
        'c2222222-2222-2222-2222-222222222222',
        'Реестр пожертвований и спонсорства',
        'receipt_long',
        '#10b981',
        true,
        'История платежей',
        'list',
        1, 7, 24, 12,
        '{"collection":"pending_donations","fields":["id","account_number","amount_rub","status","created_at"],"sort":["-created_at"],"limit":8}'::json,
        '052b044b-61c6-41bd-8883-1ddaa9c02e3d'
    );

COMMIT;
