-- Fix Directus Server Config fields: remove auto-parser remnants and raise limits to 100,000,000 RUB

-- 1. Ensure server_config and server_settings have manual source
UPDATE server_config 
SET boosty_goal_source = 'manual' 
WHERE id = 1;

UPDATE server_settings 
SET setting_value = 'manual' 
WHERE setting_key = 'boosty_goal_source';

-- 2. Update Directus field metadata for boosty_goal_target (allow up to 100,000,000 without warning)
UPDATE directus_fields 
SET options = '{"min": 1, "max": 100000000, "step": 1, "iconLeft": "flag"}'::json,
    note = 'Сумма целевого сбора в рублях (например, 100000)'
WHERE collection = 'server_config' AND field = 'boosty_goal_target';

-- 3. Update Directus field metadata for boosty_goal_current (allow up to 100,000,000)
UPDATE directus_fields 
SET options = '{"min": 0, "max": 100000000, "step": 1, "iconLeft": "payments"}'::json,
    note = 'Текущая собранная сумма в рублях'
WHERE collection = 'server_config' AND field = 'boosty_goal_current';

-- 4. Hide boosty_goal_source completely since it is always manual
UPDATE directus_fields 
SET hidden = true,
    options = '{"choices": [{"text": "Ручной ввод администратора", "value": "manual"}]}'::json
WHERE collection = 'server_config' AND field = 'boosty_goal_source';

-- 5. Hide boosty_goal_updated_at from the form
UPDATE directus_fields 
SET hidden = true
WHERE collection = 'server_config' AND field = 'boosty_goal_updated_at';

-- 6. Hide technical updated_at from the form
UPDATE directus_fields 
SET hidden = true
WHERE collection = 'server_config' AND field = 'updated_at';
