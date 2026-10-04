-- ==============================================================================
-- WARLINK CLUSTER: MIGRATION V2.1 (BF6 SPECIAL PROJECT IN DIRECTUS & BOOSTY MANUAL)
-- ==============================================================================

BEGIN;

-- 1. Добавление колонок Battlefield 6 в server_config
ALTER TABLE server_config 
    ADD COLUMN IF NOT EXISTS bf6_goal_target INT NOT NULL DEFAULT 1600,
    ADD COLUMN IF NOT EXISTS bf6_goal_current INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bf6_goal_completed BOOLEAN NOT NULL DEFAULT false;

-- Перевод источника Boosty по умолчанию в 'manual'
ALTER TABLE server_config
    ALTER COLUMN boosty_goal_source SET DEFAULT 'manual';

-- Заполнение значений BF6 из server_settings если они были
UPDATE server_config SET
    bf6_goal_target = COALESCE((SELECT value::int FROM server_settings WHERE key = 'bf6_goal_target'), 1600),
    bf6_goal_current = COALESCE((SELECT value::int FROM server_settings WHERE key = 'bf6_goal_current'), 0),
    bf6_goal_completed = COALESCE((SELECT value = 'true' FROM server_settings WHERE key = 'bf6_goal_completed'), false),
    boosty_goal_source = 'manual'
WHERE id = 1;

-- 2. Обновление триггера синхронизации server_config -> server_settings
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
        ('boosty_goal_source', NEW.boosty_goal_source),
        ('bf6_goal_target', NEW.bf6_goal_target::text),
        ('bf6_goal_current', NEW.bf6_goal_current::text),
        ('bf6_goal_completed', CASE WHEN NEW.bf6_goal_completed THEN 'true' ELSE 'false' END)
    ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 3. Регистрация полей в directus_fields для server_config
DELETE FROM directus_fields WHERE collection = 'server_config' AND field IN ('bf6_divider', 'bf6_goal_target', 'bf6_goal_current', 'bf6_goal_completed');

INSERT INTO directus_fields (collection, field, special, interface, options, display, readonly, hidden, sort, width, translations)
VALUES
    ('server_config', 'bf6_divider', 'alias,no-data', 'presentation-divider', '{"title": "Спецпроект: Battlefield 6 в WarLink", "icon": "sports_esports"}', NULL, false, false, 14, 'full', '[{"language": "ru-RU", "translation": "Спецпроект Battlefield 6"}]'),
    ('server_config', 'bf6_goal_target', NULL, 'input', '{"min": 0, "iconLeft": "flag"}', 'formatted-value', false, false, 15, 'half', '[{"language": "ru-RU", "translation": "Цель сбора на BF6 (руб)"}]'),
    ('server_config', 'bf6_goal_current', NULL, 'input', '{"min": 0, "iconLeft": "payments"}', 'formatted-value', false, false, 16, 'half', '[{"language": "ru-RU", "translation": "Собрано на BF6 (руб)"}]'),
    ('server_config', 'bf6_goal_completed', 'cast-boolean', 'boolean', '{"color": "#FF5E1F", "label": "Сбор средств на Battlefield 6 завершен"}', 'boolean', false, false, 17, 'full', '[{"language": "ru-RU", "translation": "Сбор завершен"}]');

COMMIT;
