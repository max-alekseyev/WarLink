-- ==============================================================================
-- WARLINK CLUSTER: MIGRATION V2 (DIRECTUS SINGLETON & CODEBASE HARDFIX)
-- ==============================================================================

BEGIN;

-- 1. Создание таблицы server_config (Singleton конфигурация)
CREATE TABLE IF NOT EXISTS server_config (
    id INT PRIMARY KEY DEFAULT 1,
    drain_mode BOOLEAN NOT NULL DEFAULT true,
    max_sessions INT NOT NULL DEFAULT 61,
    dedicated_sponsor_slots INT NOT NULL DEFAULT 10,
    donate_amount_rub INT NOT NULL DEFAULT 98,
    enable_donate BOOLEAN NOT NULL DEFAULT true,
    enable_voting BOOLEAN NOT NULL DEFAULT true,
    enable_community_goal BOOLEAN NOT NULL DEFAULT true,
    boosty_goal_title VARCHAR(255) NOT NULL DEFAULT 'WarLink | Поддержка дальнейшей разработки | Долги',
    boosty_goal_target INT NOT NULL DEFAULT 25,
    boosty_goal_current INT NOT NULL DEFAULT 0,
    boosty_goal_source VARCHAR(64) NOT NULL DEFAULT 'auto_parser',
    boosty_goal_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT single_row CHECK (id = 1)
);

-- Заполнение актуальными значениями из server_settings
INSERT INTO server_config (
    id, drain_mode, max_sessions, dedicated_sponsor_slots, donate_amount_rub,
    enable_donate, enable_voting, enable_community_goal,
    boosty_goal_title, boosty_goal_target, boosty_goal_current, boosty_goal_source
) VALUES (
    1,
    COALESCE((SELECT value = 'true' FROM server_settings WHERE key = 'drain_mode'), true),
    COALESCE((SELECT value::int FROM server_settings WHERE key = 'max_sessions'), 61),
    COALESCE((SELECT value::int FROM server_settings WHERE key = 'dedicated_sponsor_slots'), 10),
    COALESCE((SELECT value::int FROM server_settings WHERE key = 'donate_amount_rub'), 98),
    COALESCE((SELECT value = 'true' FROM server_settings WHERE key = 'enable_donate'), true),
    COALESCE((SELECT value = 'true' FROM server_settings WHERE key = 'enable_voting'), true),
    COALESCE((SELECT value = 'true' FROM server_settings WHERE key = 'enable_community_goal'), true),
    COALESCE((SELECT value FROM server_settings WHERE key = 'boosty_goal_title'), 'WarLink | Поддержка дальнейшей разработки | Долги'),
    COALESCE((SELECT value::int FROM server_settings WHERE key = 'boosty_goal_target'), 25),
    COALESCE((SELECT value::int FROM server_settings WHERE key = 'boosty_goal_current'), 0),
    COALESCE((SELECT value FROM server_settings WHERE key = 'boosty_goal_source'), 'auto_parser')
) ON CONFLICT (id) DO UPDATE SET
    drain_mode = EXCLUDED.drain_mode,
    max_sessions = EXCLUDED.max_sessions,
    dedicated_sponsor_slots = EXCLUDED.dedicated_sponsor_slots,
    donate_amount_rub = EXCLUDED.donate_amount_rub,
    enable_donate = EXCLUDED.enable_donate,
    enable_voting = EXCLUDED.enable_voting,
    enable_community_goal = EXCLUDED.enable_community_goal,
    boosty_goal_title = EXCLUDED.boosty_goal_title,
    boosty_goal_target = EXCLUDED.boosty_goal_target,
    boosty_goal_current = EXCLUDED.boosty_goal_current,
    boosty_goal_source = EXCLUDED.boosty_goal_source,
    updated_at = NOW();

-- 2. Двусторонняя синхронизация server_config <-> server_settings через триггеры
CREATE OR REPLACE FUNCTION sync_server_config_to_settings()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO server_settings (key, value) VALUES
        ('drain_mode', CASE WHEN NEW.drain_mode THEN 'true' ELSE 'false' END),
        ('max_sessions', NEW.max_sessions::text),
        ('dedicated_sponsor_slots', NEW.dedicated_sponsor_slots::text),
        ('donate_amount_rub', NEW.donate_amount_rub::text),
        ('enable_donate', CASE WHEN NEW.enable_donate THEN 'true' ELSE 'false' END),
        ('enable_voting', CASE WHEN NEW.enable_voting THEN 'true' ELSE 'false' END),
        ('enable_community_goal', CASE WHEN NEW.enable_community_goal THEN 'true' ELSE 'false' END),
        ('boosty_goal_title', NEW.boosty_goal_title),
        ('boosty_goal_target', NEW.boosty_goal_target::text),
        ('boosty_goal_current', NEW.boosty_goal_current::text),
        ('boosty_goal_source', NEW.boosty_goal_source)
    ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sync_server_config_to_settings ON server_config;
CREATE TRIGGER trg_sync_server_config_to_settings
AFTER INSERT OR UPDATE ON server_config
FOR EACH ROW EXECUTE FUNCTION sync_server_config_to_settings();

-- 3. Реестр запрещенных игр для голосования
CREATE TABLE IF NOT EXISTS forbidden_steam_games (
    steam_app_id INT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    reason VARCHAR(64) NOT NULL DEFAULT 'troll_spam',
    blocked_by VARCHAR(128) NOT NULL DEFAULT 'admin',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO forbidden_steam_games (steam_app_id, title, reason, blocked_by)
VALUES (3602290, 'FEMBOY FUTA HOUSE', 'nsfw', 'system')
ON CONFLICT (steam_app_id) DO NOTHING;

-- 4. Реестр правил никнеймов
CREATE TABLE IF NOT EXISTS nickname_rules (
    id SERIAL PRIMARY KEY,
    pattern VARCHAR(255) NOT NULL UNIQUE,
    rule_type VARCHAR(64) NOT NULL, -- 'profanity', 'impersonation', 'whitelist'
    description VARCHAR(255),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Заполняем ключевые служебные ключевые слова (impersonation)
INSERT INTO nickname_rules (pattern, rule_type, description) VALUES
    ('admin', 'impersonation', 'Администратор'),
    ('administrator', 'impersonation', 'Администратор'),
    ('админ', 'impersonation', 'Администратор'),
    ('warlink', 'impersonation', 'Имя сервиса'),
    ('aeza', 'impersonation', 'Хостинг-провайдер'),
    ('support', 'impersonation', 'Техподдержка'),
    ('саппорт', 'impersonation', 'Техподдержка'),
    ('moderator', 'impersonation', 'Модератор'),
    ('root', 'impersonation', 'Системный аккаунт'),
    ('developer', 'impersonation', 'Разработчик'),
    ('official', 'impersonation', 'Официальный представитель')
ON CONFLICT (pattern) DO NOTHING;

-- Заполняем базовый whitelist
INSERT INTO nickname_rules (pattern, rule_type, description) VALUES
    ('хлеб', 'whitelist', 'Безопасное слово'),
    ('мудрость', 'whitelist', 'Безопасное слово'),
    ('колебан', 'whitelist', 'Безопасное слово'),
    ('скипидар', 'whitelist', 'Безопасное слово'),
    ('рубль', 'whitelist', 'Безопасное слово'),
    ('classic', 'whitelist', 'Безопасное слово'),
    ('document', 'whitelist', 'Безопасное слово')
ON CONFLICT (pattern) DO NOTHING;

-- 5. Реестр узлов кластера
CREATE TABLE IF NOT EXISTS cluster_nodes (
    node_id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    role VARCHAR(64) NOT NULL, -- 'edge_proxy', 'master_hub'
    location VARCHAR(64) NOT NULL, -- 'Moscow', 'Frankfurt', 'Stockholm'
    ip_address VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    paid_until DATE NOT NULL,
    safety_buffer_days INT NOT NULL DEFAULT 7,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO cluster_nodes (node_id, name, role, location, ip_address, status, paid_until, safety_buffer_days)
VALUES 
    ('sto-master', 'Стокгольм Core Master', 'master_hub', 'Stockholm', '138.124.103.99', 'active', '2026-11-03', 7),
    ('fra-edge', 'Франкфурт Edge Gateway', 'edge_proxy', 'Frankfurt', '85.192.24.254', 'active', '2026-11-03', 7),
    ('msk-ingress', 'Москва Ingress Gateway', 'edge_proxy', 'Moscow', '45.12.63.85', 'active', '2026-11-01', 7)
ON CONFLICT (node_id) DO UPDATE SET
    paid_until = EXCLUDED.paid_until,
    safety_buffer_days = EXCLUDED.safety_buffer_days,
    status = EXCLUDED.status;

-- 6. Реестр банов
CREATE TABLE IF NOT EXISTS security_bans (
    id SERIAL PRIMARY KEY,
    target_type VARCHAR(32) NOT NULL, -- 'account_number', 'machine_guid_hash', 'ip_address'
    target_value VARCHAR(255) NOT NULL,
    reason TEXT NOT NULL,
    banned_by VARCHAR(128) NOT NULL DEFAULT 'admin',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 7. Шаблоны быстрых ответов поддержки
CREATE TABLE IF NOT EXISTS ticket_canned_responses (
    id SERIAL PRIMARY KEY,
    shortcut VARCHAR(64) NOT NULL UNIQUE,
    title VARCHAR(128) NOT NULL,
    category VARCHAR(64) NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO ticket_canned_responses (shortcut, title, category, body)
VALUES 
    ('crash_windivert', 'Конфликт драйвера WinDivert', 'game_crash', 'Здравствуйте! В ваших логах зафиксирован конфликт со сторонней службой WinDivert. Пожалуйста, закройте сторонние утилиты обхода (Zapret / GoodbyeDPI), перезагрузите ПК и запустите WarLink от имени администратора.'),
    ('high_ping_region', 'Выбор удаленного региона сервера игры', 'high_ping', 'Здравствуйте! Изучил сетевые логи: ваш игровой клиент подключился к серверу в регионе США/Азия вместо Европы. Проверьте настройки подбора матча в меню игры WARDOGS и выберите европейский регион (EU Frankfurt).'),
    ('conn_repair', 'Инструкция по восстановлению туннеля', 'gateway_conn', 'Здравствуйте! Рекомендуем выполнить сброс сетевого стека: нажмите кнопку настроек в заголовке WarLink и выберите "Оптимизация сети Windows". После этого перезапустите подключение.')
ON CONFLICT (shortcut) DO NOTHING;

-- Выдача прав пользователю warlink
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO warlink;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO warlink;

COMMIT;
