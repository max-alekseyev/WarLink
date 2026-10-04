UPDATE directus_fields 
SET interface = 'select-dropdown',
    display = 'labels',
    display_options = '{"choices":[{"text":"Спонсор","value":"sponsor","foreground":"#FFFFFF","background":"#FF5E1F"},{"text":"Игрок","value":"free","foreground":"#FFFFFF","background":"#374151"}]}'::json
WHERE collection = 'accounts' AND field = 'tier';
