-- ============================================================================
-- WARLINK DATABASE MIGRATION: UNIFIED GAMES KANBAN & AUTOMATIONS
-- Zero Emoji Compliance: AGENTS.md Rule 3
-- Author: Antigravity AI & WarLink Core Team
-- ============================================================================

BEGIN;

-- 1. Расширение таблицы game_suggestions новыми полями
ALTER TABLE game_suggestions ADD COLUMN IF NOT EXISTS slug VARCHAR(100) DEFAULT '';
ALTER TABLE game_suggestions ADD COLUMN IF NOT EXISTS reason VARCHAR(255) DEFAULT '';
ALTER TABLE game_suggestions ADD COLUMN IF NOT EXISTS blocked_by VARCHAR(100) DEFAULT 'admin';
ALTER TABLE game_suggestions ADD COLUMN IF NOT EXISTS subtitle TEXT DEFAULT '';

-- 2. Заполнение subtitle для существующих строк
UPDATE game_suggestions SET subtitle = CASE 
    WHEN status = 'supported' THEN 'Официально добавлена (поддерживается)'
    WHEN status = 'forbidden' THEN 'Запрещена: ' || COALESCE(NULLIF(reason, ''), 'нарушение правил')
    WHEN status = 'queue_integration' THEN 'В очереди интеграции (голосов: ' || votes_count || ')'
    ELSE 'Голосов сообщества: ' || votes_count
END WHERE subtitle IS NULL OR subtitle = '';

-- 3. Импорт официально поддерживаемых игр в game_suggestions
INSERT INTO game_suggestions (steam_app_id, title, slug, icon_url, votes_count, status, subtitle, created_at, updated_at)
SELECT 
    steam_app_id, 
    title, 
    slug, 
    COALESCE(NULLIF(icon_url, ''), 'https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/' || steam_app_id || '/header.jpg'),
    0, 
    'supported', 
    'Официально добавлена (поддерживается)', 
    created_at, 
    updated_at
FROM supported_games
ON CONFLICT (steam_app_id) DO UPDATE SET
    status = 'supported',
    slug = EXCLUDED.slug,
    title = EXCLUDED.title,
    icon_url = CASE WHEN EXCLUDED.icon_url <> '' THEN EXCLUDED.icon_url ELSE game_suggestions.icon_url END,
    subtitle = 'Официально добавлена (поддерживается)',
    updated_at = NOW();

-- 4. Импорт запрещенных игр в game_suggestions
INSERT INTO game_suggestions (steam_app_id, title, reason, blocked_by, icon_url, votes_count, status, subtitle, created_at, updated_at)
SELECT 
    steam_app_id, 
    title, 
    reason, 
    blocked_by, 
    'https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/' || steam_app_id || '/header.jpg',
    0, 
    'forbidden', 
    'Запрещена: ' || COALESCE(NULLIF(reason, ''), 'система'), 
    created_at, 
    created_at
FROM forbidden_steam_games
ON CONFLICT (steam_app_id) DO UPDATE SET
    status = 'forbidden',
    reason = EXCLUDED.reason,
    blocked_by = EXCLUDED.blocked_by,
    title = EXCLUDED.title,
    subtitle = 'Запрещена: ' || COALESCE(NULLIF(EXCLUDED.reason, ''), 'система'),
    updated_at = NOW();

-- 5. Триггерная функция для автоматического вычисления subtitle и timestamp перед сохранением
CREATE OR REPLACE FUNCTION trg_fn_game_suggestions_before() 
RETURNS TRIGGER AS $$
BEGIN
    NEW.subtitle := CASE 
        WHEN NEW.status = 'supported' THEN 'Официально добавлена (поддерживается)'
        WHEN NEW.status = 'forbidden' THEN 'Запрещена: ' || COALESCE(NULLIF(NEW.reason, ''), 'нарушение правил')
        WHEN NEW.status = 'queue_integration' THEN 'В очереди интеграции (голосов: ' || NEW.votes_count || ')'
        ELSE 'Голосов сообщества: ' || NEW.votes_count
    END;
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_game_suggestions_before ON game_suggestions;
CREATE TRIGGER trg_game_suggestions_before
BEFORE INSERT OR UPDATE ON game_suggestions
FOR EACH ROW EXECUTE FUNCTION trg_fn_game_suggestions_before();

-- 6. Триггерная функция для автоматизаций при перемещении карточек канбана
CREATE OR REPLACE FUNCTION trg_fn_game_suggestions_after_sync() 
RETURNS TRIGGER AS $$
BEGIN
    -- Защита от рекурсивных триггеров
    IF pg_trigger_depth() > 1 THEN
        RETURN NEW;
    END IF;

    IF NEW.status = 'supported' THEN
        -- 1. Возврат голосов игрокам
        DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;
        -- 2. Удаление из списка запрещенных
        DELETE FROM forbidden_steam_games WHERE steam_app_id = NEW.steam_app_id;
        -- 3. Добавление/обновление в supported_games
        INSERT INTO supported_games (steam_app_id, title, slug, icon_url, created_at, updated_at)
        VALUES (
            NEW.steam_app_id,
            NEW.title,
            COALESCE(NULLIF(NEW.slug, ''), LOWER(REGEXP_REPLACE(NEW.title, '[^a-zA-Z0-9]', '', 'g'))),
            COALESCE(NEW.icon_url, ''),
            NOW(),
            NOW()
        )
        ON CONFLICT (steam_app_id) DO UPDATE SET
            title = EXCLUDED.title,
            slug = CASE WHEN EXCLUDED.slug <> '' THEN EXCLUDED.slug ELSE supported_games.slug END,
            icon_url = CASE WHEN EXCLUDED.icon_url <> '' THEN EXCLUDED.icon_url ELSE supported_games.icon_url END,
            updated_at = NOW();

    ELSIF NEW.status = 'forbidden' THEN
        -- 1. Возврат голосов игрокам
        DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;
        -- 2. Удаление из поддерживаемых игр
        DELETE FROM supported_games WHERE steam_app_id = NEW.steam_app_id;
        -- 3. Добавление/обновление в forbidden_steam_games
        INSERT INTO forbidden_steam_games (steam_app_id, title, reason, blocked_by, created_at)
        VALUES (
            NEW.steam_app_id,
            NEW.title,
            COALESCE(NULLIF(NEW.reason, ''), 'admin_ban'),
            COALESCE(NULLIF(NEW.blocked_by, ''), 'admin'),
            NOW()
        )
        ON CONFLICT (steam_app_id) DO UPDATE SET
            title = EXCLUDED.title,
            reason = EXCLUDED.reason,
            blocked_by = EXCLUDED.blocked_by;

    ELSIF NEW.status IN ('voting', 'queue_integration') THEN
        -- Если карточку перетащили обратно в голосование - удаляем из supported и forbidden
        DELETE FROM supported_games WHERE steam_app_id = NEW.steam_app_id;
        DELETE FROM forbidden_steam_games WHERE steam_app_id = NEW.steam_app_id;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_game_suggestions_after_sync ON game_suggestions;
CREATE TRIGGER trg_game_suggestions_after_sync
AFTER INSERT OR UPDATE ON game_suggestions
FOR EACH ROW EXECUTE FUNCTION trg_fn_game_suggestions_after_sync();

-- 7. Триггер при удалении карточки из game_suggestions
CREATE OR REPLACE FUNCTION trg_fn_game_suggestions_delete() 
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;

    DELETE FROM supported_games WHERE steam_app_id = OLD.steam_app_id;
    DELETE FROM forbidden_steam_games WHERE steam_app_id = OLD.steam_app_id;
    DELETE FROM device_votes WHERE steam_app_id = OLD.steam_app_id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_game_suggestions_delete ON game_suggestions;
CREATE TRIGGER trg_game_suggestions_delete
AFTER DELETE ON game_suggestions
FOR EACH ROW EXECUTE FUNCTION trg_fn_game_suggestions_delete();

-- 8. Обновление обратных триггеров на supported_games и forbidden_steam_games
-- Чтобы ручные изменения в supported_games синхронизировались в канбан-доску game_suggestions
CREATE OR REPLACE FUNCTION on_supported_game_added() 
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN NEW;
    END IF;

    DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;
    INSERT INTO game_suggestions (steam_app_id, title, slug, icon_url, status, votes_count, subtitle, updated_at)
    VALUES (
        NEW.steam_app_id,
        NEW.title,
        NEW.slug,
        COALESCE(NEW.icon_url, ''),
        'supported',
        0,
        'Официально добавлена (поддерживается)',
        NOW()
    )
    ON CONFLICT (steam_app_id) DO UPDATE SET
        status = 'supported',
        title = EXCLUDED.title,
        slug = CASE WHEN EXCLUDED.slug <> '' THEN EXCLUDED.slug ELSE game_suggestions.slug END,
        icon_url = CASE WHEN EXCLUDED.icon_url <> '' THEN EXCLUDED.icon_url ELSE game_suggestions.icon_url END,
        subtitle = 'Официально добавлена (поддерживается)',
        updated_at = NOW();

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION on_forbidden_game_added() 
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN NEW;
    END IF;

    DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;
    INSERT INTO game_suggestions (steam_app_id, title, reason, blocked_by, status, votes_count, subtitle, updated_at)
    VALUES (
        NEW.steam_app_id,
        NEW.title,
        NEW.reason,
        NEW.blocked_by,
        'forbidden',
        0,
        'Запрещена: ' || COALESCE(NULLIF(NEW.reason, ''), 'система'),
        NOW()
    )
    ON CONFLICT (steam_app_id) DO UPDATE SET
        status = 'forbidden',
        title = EXCLUDED.title,
        reason = EXCLUDED.reason,
        blocked_by = EXCLUDED.blocked_by,
        subtitle = 'Запрещена: ' || COALESCE(NULLIF(EXCLUDED.reason, ''), 'система'),
        updated_at = NOW();

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Обратный триггер при удалении из supported_games: переводим в voting в канбане
CREATE OR REPLACE FUNCTION on_supported_game_deleted() 
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    UPDATE game_suggestions SET status = 'voting', updated_at = NOW() WHERE steam_app_id = OLD.steam_app_id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_supported_game_deleted ON supported_games;
CREATE TRIGGER trg_supported_game_deleted
AFTER DELETE ON supported_games
FOR EACH ROW EXECUTE FUNCTION on_supported_game_deleted();

-- Обратный триггер при удалении из forbidden_steam_games: переводим в voting в канбане
CREATE OR REPLACE FUNCTION on_forbidden_game_deleted() 
RETURNS TRIGGER AS $$
BEGIN
    IF pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    UPDATE game_suggestions SET status = 'voting', updated_at = NOW() WHERE steam_app_id = OLD.steam_app_id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_forbidden_game_deleted ON forbidden_steam_games;
CREATE TRIGGER trg_forbidden_game_deleted
AFTER DELETE ON forbidden_steam_games
FOR EACH ROW EXECUTE FUNCTION on_forbidden_game_deleted();

-- 9. Конфигурация Directus: коллекция game_suggestions
UPDATE directus_collections
SET 
    translations = '[{"language":"ru-RU","translation":"Канбан каталог игр"}]',
    icon = 'view_week',
    note = 'Единая канбан-доска: голосование, добавленные и запрещенные игры с авто-синхронизацией',
    color = '#FF5E1F',
    sort = 1
WHERE collection = 'game_suggestions';

-- 10. Конфигурация Directus: поля коллекции game_suggestions в directus_fields
-- Обновление поля status с выпадающим списком и цветными бейджами колонок
UPDATE directus_fields
SET 
    interface = 'select-dropdown',
    options = '{"choices":[{"text":"На голосовании","value":"voting"},{"text":"В очереди интеграции (50+ голосов)","value":"queue_integration"},{"text":"Добавленные игры (Поддерживается)","value":"supported"},{"text":"Запрещенные игры (Блокировка)","value":"forbidden"}]}',
    display = 'labels',
    display_options = '{"choices":[{"text":"На голосовании","value":"voting","foreground":"#FFFFFF","background":"#3b82f6"},{"text":"В очереди","value":"queue_integration","foreground":"#FFFFFF","background":"#FF5E1F"},{"text":"Поддерживается","value":"supported","foreground":"#FFFFFF","background":"#10b981"},{"text":"Запрещена","value":"forbidden","foreground":"#FFFFFF","background":"#ef4444"}]}',
    translations = '[{"language":"ru-RU","translation":"Статус / Колонка канбана"}]',
    width = 'half',
    sort = 3
WHERE collection = 'game_suggestions' AND field = 'status';

-- Обновление существующих полей
UPDATE directus_fields SET translations = '[{"language":"ru-RU","translation":"Steam App ID"}]', sort = 1, width = 'half' WHERE collection = 'game_suggestions' AND field = 'steam_app_id';
UPDATE directus_fields SET translations = '[{"language":"ru-RU","translation":"Название игры"}]', sort = 2, width = 'half' WHERE collection = 'game_suggestions' AND field = 'title';
UPDATE directus_fields SET translations = '[{"language":"ru-RU","translation":"Голосов сообщества"}]', sort = 4, width = 'half' WHERE collection = 'game_suggestions' AND field = 'votes_count';

-- Добавление новых полей в directus_fields (если отсутствуют)
DELETE FROM directus_fields WHERE collection = 'game_suggestions' AND field IN ('slug', 'reason', 'blocked_by', 'subtitle', 'icon_url', 'created_at', 'updated_at');

INSERT INTO directus_fields (collection, field, interface, options, display, display_options, readonly, hidden, sort, width, translations)
VALUES 
('game_suggestions', 'slug', 'input', NULL, NULL, NULL, false, false, 5, 'half', '[{"language":"ru-RU","translation":"Игровой Slug (имя в шлюзе)"}]'),
('game_suggestions', 'reason', 'input', NULL, NULL, NULL, false, false, 6, 'half', '[{"language":"ru-RU","translation":"Причина запрета (для заблокированных)"}]'),
('game_suggestions', 'blocked_by', 'input', NULL, NULL, NULL, false, false, 7, 'half', '[{"language":"ru-RU","translation":"Кем заблокировано"}]'),
('game_suggestions', 'subtitle', 'input', NULL, NULL, NULL, true, false, 8, 'full', '[{"language":"ru-RU","translation":"Сводка на карточке"}]'),
('game_suggestions', 'icon_url', 'input', NULL, NULL, NULL, false, false, 9, 'full', '[{"language":"ru-RU","translation":"URL обложки Steam"}]'),
('game_suggestions', 'created_at', 'datetime', NULL, 'datetime', '{"format":"short"}', true, false, 10, 'half', '[{"language":"ru-RU","translation":"Дата создания"}]'),
('game_suggestions', 'updated_at', 'datetime', NULL, 'datetime', '{"format":"short"}', true, false, 11, 'half', '[{"language":"ru-RU","translation":"Дата обновления"}]');

-- 11. Настройка пресета вида по умолчанию на KANBAN в directus_presets
UPDATE directus_presets
SET 
    layout = 'kanban',
    layout_query = '{"kanban":{"sort":["-votes_count"]}}',
    layout_options = '{"kanban":{"groupField":"status","titleField":"title","textField":"subtitle","dateField":"created_at","showUngrouped":false}}'
WHERE collection = 'game_suggestions';

-- Если глобального системного пресета не было, создаем его
INSERT INTO directus_presets (collection, layout, layout_query, layout_options)
SELECT 'game_suggestions', 'kanban', '{"kanban":{"sort":["-votes_count"]}}', '{"kanban":{"groupField":"status","titleField":"title","textField":"subtitle","dateField":"created_at","showUngrouped":false}}'
WHERE NOT EXISTS (SELECT 1 FROM directus_presets WHERE collection = 'game_suggestions' AND "user" IS NULL);

COMMIT;
