-- ==============================================================================
-- WARLINK CLUSTER: MIGRATION V2.2.3 (DONATE METHODS MANAGEMENT IN DIRECTUS)
-- ==============================================================================

BEGIN;

-- 1. Добавление колонок управления способами пожертвований в server_config
ALTER TABLE server_config 
    ADD COLUMN IF NOT EXISTS donate_boosty_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS donate_sbp_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS donate_crypto_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS donate_paused_notice TEXT NOT NULL DEFAULT 'Сбор временно консолидирован на платформе Boosty для прозрачности целей';

-- Заполнение значений по умолчанию для существующей записи id = 1
UPDATE server_config SET
    donate_boosty_enabled = true,
    donate_sbp_enabled = false,
    donate_crypto_enabled = false,
    donate_paused_notice = 'Сбор временно консолидирован на платформе Boosty для прозрачности целей'
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
        ('bf6_goal_completed', CASE WHEN NEW.bf6_goal_completed THEN 'true' ELSE 'false' END),
        ('donate_boosty_enabled', CASE WHEN NEW.donate_boosty_enabled THEN 'true' ELSE 'false' END),
        ('donate_sbp_enabled', CASE WHEN NEW.donate_sbp_enabled THEN 'true' ELSE 'false' END),
        ('donate_crypto_enabled', CASE WHEN NEW.donate_crypto_enabled THEN 'true' ELSE 'false' END),
        ('donate_paused_notice', NEW.donate_paused_notice)
    ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Синхронизируем текущее состояние в server_settings
INSERT INTO server_settings (key, value) VALUES
    ('donate_boosty_enabled', 'true'),
    ('donate_sbp_enabled', 'false'),
    ('donate_crypto_enabled', 'false'),
    ('donate_paused_notice', 'Сбор временно консолидирован на платформе Boosty для прозрачности целей')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

-- 3. Регистрация полей в directus_fields для server_config
DELETE FROM directus_fields 
WHERE collection = 'server_config' 
  AND field IN ('donate_methods_divider', 'donate_boosty_enabled', 'donate_sbp_enabled', 'donate_crypto_enabled', 'donate_paused_notice');

INSERT INTO directus_fields (
    collection, field, special, interface, options, display, readonly, hidden, sort, width, translations, note
) VALUES
    (
        'server_config', 
        'donate_methods_divider', 
        'alias,no-data', 
        'presentation-divider', 
        '{"title": "Способы пожертвований (Boosty, СБП, Crypto)", "icon": "payments"}', 
        NULL, 
        false, 
        false, 
        18, 
        'full', 
        '[{"language": "ru-RU", "translation": "Раздельное управление методами доната"}]',
        'Включение и отключение конкретных способов пожертвований с показом карточки-заглушки'
    ),
    (
        'server_config', 
        'donate_boosty_enabled', 
        'cast-boolean', 
        'boolean', 
        '{"color": "#FF5E1F", "label": "Включен (Приоритетный сбор)"}', 
        'boolean', 
        false, 
        false, 
        19, 
        'half', 
        '[{"language": "ru-RU", "translation": "Способ: Boosty"}]',
        'Прием пожертвований через страницу Boosty с динамической целью сбора'
    ),
    (
        'server_config', 
        'donate_sbp_enabled', 
        'cast-boolean', 
        'boolean', 
        '{"color": "#FF5E1F", "label": "Включен (СБП / Карты РФ)"}', 
        'boolean', 
        false, 
        false, 
        20, 
        'half', 
        '[{"language": "ru-RU", "translation": "Способ: СБП / Карты РФ"}]',
        'Прямое пополнение баланса сервера через СБП и банковские карты'
    ),
    (
        'server_config', 
        'donate_crypto_enabled', 
        'cast-boolean', 
        'boolean', 
        '{"color": "#FF5E1F", "label": "Включен (USDT TRC20)"}', 
        'boolean', 
        false, 
        false, 
        21, 
        'half', 
        '[{"language": "ru-RU", "translation": "Способ: USDT TRC20"}]',
        'Криптовалютные переводы через сеть Tron (USDT TRC20)'
    ),
    (
        'server_config', 
        'donate_paused_notice', 
        NULL, 
        'input-multiline', 
        '{"placeholder": "Сбор временно консолидирован на платформе Boosty для прозрачности целей"}', 
        'formatted-value', 
        false, 
        false, 
        22, 
        'full', 
        '[{"language": "ru-RU", "translation": "Текст пояснения на заглушке"}]',
        'Сообщение на карточке-заглушке в клиенте при переходе на временно отключенный способ оплаты'
    );

COMMIT;
