-- Migration: Align game_suggestions Primary Key with Directus 'id' contract
-- Zero Emoji Compliance: AGENTS.md Rule 3

BEGIN;

-- 1. Добавляем колонку id в game_suggestions
ALTER TABLE game_suggestions ADD COLUMN IF NOT EXISTS id INTEGER;
UPDATE game_suggestions SET id = steam_app_id WHERE id IS NULL;
ALTER TABLE game_suggestions ALTER COLUMN id SET NOT NULL;

-- 2. Перенастраиваем первичный ключ и внешние ключи
ALTER TABLE device_votes DROP CONSTRAINT IF EXISTS device_votes_steam_app_id_fkey;
ALTER TABLE game_suggestions DROP CONSTRAINT IF EXISTS game_suggestions_pkey;
ALTER TABLE game_suggestions DROP CONSTRAINT IF EXISTS game_suggestions_steam_app_id_unique;

ALTER TABLE game_suggestions ADD CONSTRAINT game_suggestions_steam_app_id_unique UNIQUE (steam_app_id);
ALTER TABLE device_votes ADD CONSTRAINT device_votes_steam_app_id_fkey FOREIGN KEY (steam_app_id) REFERENCES game_suggestions(steam_app_id) ON DELETE CASCADE;
ALTER TABLE game_suggestions ADD CONSTRAINT game_suggestions_pkey PRIMARY KEY (id);

-- 3. Обновляем триггер BEFORE INSERT: автоматическая синхронизация id <-> steam_app_id
CREATE OR REPLACE FUNCTION trg_fn_game_suggestions_before() 
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS NULL AND NEW.steam_app_id IS NOT NULL THEN
        NEW.id := NEW.steam_app_id;
    END IF;
    IF NEW.steam_app_id IS NULL AND NEW.id IS NOT NULL THEN
        NEW.steam_app_id := NEW.id;
    END IF;
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

-- 4. Добавляем поле id в directus_fields
DELETE FROM directus_fields WHERE collection = 'game_suggestions' AND field = 'id';
INSERT INTO directus_fields (collection, field, interface, readonly, hidden, sort, width, translations)
VALUES ('game_suggestions', 'id', 'input', true, true, 0, 'half', '[{"language":"ru-RU","translation":"ID"}]');

-- 5. Обновляем пресет канбана
UPDATE directus_presets
SET 
    layout = 'kanban',
    layout_query = '{"kanban":{"sort":["-votes_count"]}}',
    layout_options = '{"kanban":{"groupField":"status","titleField":"title","textField":"subtitle","dateField":"created_at","showUngrouped":false}}'
WHERE collection = 'game_suggestions';

COMMIT;
