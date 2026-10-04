-- Migration: setup_supported_games.sql
-- Создание таблицы официально добавленных игр и автоматических триггеров возврата голосов

-- 1. Таблица официально поддерживаемых/добавленных в WarLink игр
CREATE TABLE IF NOT EXISTS supported_games (
    steam_app_id INT PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL DEFAULT '',
    icon_url TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 2. Функция автоматического возврата голосов и очистки из голосования при добавлении в supported_games
CREATE OR REPLACE FUNCTION on_supported_game_added()
RETURNS TRIGGER AS $$
BEGIN
    -- Удаляем все отданные голоса пользователей за эту игру из device_votes.
    -- Это мгновенно освобождает слоты голосов у всех голосовавших игроков (user_votes_used уменьшается).
    DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;

    -- Удаляем игру из списка предложений сообщества game_suggestions
    DELETE FROM game_suggestions WHERE steam_app_id = NEW.steam_app_id;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_supported_game_cleanup ON supported_games;
CREATE TRIGGER trg_supported_game_cleanup
AFTER INSERT OR UPDATE ON supported_games
FOR EACH ROW
EXECUTE FUNCTION on_supported_game_added();

-- 3. Функция автоматического возврата голосов при добавлении в forbidden_steam_games
CREATE OR REPLACE FUNCTION on_forbidden_game_added()
RETURNS TRIGGER AS $$
BEGIN
    -- Если игра запрещена, также возвращаем все отданные голоса и удаляем ее из голосования
    DELETE FROM device_votes WHERE steam_app_id = NEW.steam_app_id;
    DELETE FROM game_suggestions WHERE steam_app_id = NEW.steam_app_id;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_forbidden_game_cleanup ON forbidden_steam_games;
CREATE TRIGGER trg_forbidden_game_cleanup
AFTER INSERT OR UPDATE ON forbidden_steam_games
FOR EACH ROW
EXECUTE FUNCTION on_forbidden_game_added();

-- 4. Первичное наполнение официально добавленных игр (WARDOGS, ARC Raiders, Dark and Darker)
INSERT INTO supported_games (steam_app_id, title, slug, icon_url)
VALUES 
    (1867240, 'WARDOGS', 'wardogs', 'https://shared.fastly.steamstatic.com/community_assets/images/apps/1867240/capsule_231x87.jpg'),
    (1808500, 'ARC Raiders', 'arcraiders', 'https://shared.fastly.steamstatic.com/community_assets/images/apps/1808500/capsule_231x87.jpg'),
    (2016590, 'Dark and Darker', 'darkanddarker', 'https://shared.fastly.steamstatic.com/community_assets/images/apps/2016590/capsule_231x87.jpg')
ON CONFLICT (steam_app_id) DO UPDATE 
SET title = EXCLUDED.title, slug = EXCLUDED.slug, icon_url = EXCLUDED.icon_url, updated_at = NOW();

-- Ручной вызов очистки для уже имеющихся голосов по добавленным играм на случай, если строки уже были
DELETE FROM device_votes WHERE steam_app_id IN (1867240, 1808500, 2016590);
DELETE FROM game_suggestions WHERE steam_app_id IN (1867240, 1808500, 2016590);

-- 5. Регистрация supported_games в Directus UI
INSERT INTO directus_collections (collection, icon, note, display_template, hidden, singleton, archive_field)
VALUES ('supported_games', 'sports_esports', 'Официально добавленные в WarLink игры (исключаются из голосования с возвратом голосов)', '{{title}} (AppID: {{steam_app_id}})', false, false, NULL)
ON CONFLICT (collection) DO UPDATE 
SET icon = EXCLUDED.icon, note = EXCLUDED.note, display_template = EXCLUDED.display_template, hidden = false;

-- Регистрация полей в directus_fields
DELETE FROM directus_fields WHERE collection = 'supported_games';
INSERT INTO directus_fields (collection, field, special, interface, options, display, display_options, readonly, hidden, sort, width, note)
VALUES 
    ('supported_games', 'steam_app_id', NULL, 'input', NULL, 'raw', NULL, false, false, 1, 'half', 'Steam AppID игры (число из магазина Steam)'),
    ('supported_games', 'title', NULL, 'input', NULL, 'raw', NULL, false, false, 2, 'half', 'Официальное название игры'),
    ('supported_games', 'slug', NULL, 'input', NULL, 'raw', NULL, false, false, 3, 'half', 'Системный идентификатор (например: arcraiders, darkanddarker)'),
    ('supported_games', 'icon_url', NULL, 'input', NULL, 'raw', NULL, false, false, 4, 'full', 'URL иконки или постера из Steam CDN'),
    ('supported_games', 'created_at', 'date-created', 'datetime', NULL, 'datetime', NULL, true, false, 5, 'half', 'Дата добавления в WarLink');
