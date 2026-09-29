const fs = require('fs');
const dict = require('./translations_dictionary.js');

// 1. Update ITEMS_TRANSLATION.md
const mdPath = 'ITEMS_TRANSLATION.md';
let mdContent = fs.readFileSync(mdPath, 'utf8');
const lines = mdContent.split('\n');

let updatedMdCount = 0;
const newLines = lines.map(line => {
    if (line.trim().startsWith('|') && !line.includes(':---') && !line.includes('ID предмета') && !line.includes('Текущее название')) {
        const parts = line.split('|');
        if (parts.length >= 7) {
            // parts[0] is '', parts[1] is num, parts[2] is icon, parts[3] is id, parts[4] is en, parts[5] is ru
            const rawId = parts[3].trim();
            const cleanId = rawId.replace(/`/g, '').trim();
            const currentRu = parts[5].trim();

            if (!currentRu && dict[cleanId]) {
                parts[5] = ' ' + dict[cleanId] + ' ';
                updatedMdCount++;
                return parts.join('|');
            }
        }
    }
    return line;
});

fs.writeFileSync(mdPath, newLines.join('\n'), 'utf8');
console.log(`Updated ${updatedMdCount} empty translations in ${mdPath}`);

// 2. Update internal/progression/progression_db.json
const dbPath = 'internal/progression/progression_db.json';
const db = JSON.parse(fs.readFileSync(dbPath, 'utf8'));

// Also read all translations from ITEMS_TRANSLATION.md to ensure we have user translations + new translations
const allTranslations = { ...dict };
newLines.forEach(line => {
    if (line.trim().startsWith('|') && !line.includes(':---') && !line.includes('ID предмета') && !line.includes('Текущее название')) {
        const parts = line.split('|');
        if (parts.length >= 7) {
            const rawId = parts[3].trim().replace(/`/g, '').trim();
            const ru = parts[5].trim();
            if (ru) {
                allTranslations[rawId] = ru;
            }
        }
    }
});

let updatedDbCount = 0;
if (db.unlocks && Array.isArray(db.unlocks)) {
    db.unlocks.forEach(item => {
        if (allTranslations[item.item_id]) {
            item.name_ru = allTranslations[item.item_id];
            updatedDbCount++;
        } else if (allTranslations[item.unlock_id]) {
            item.name_ru = allTranslations[item.unlock_id];
            updatedDbCount++;
        }
    });
}

fs.writeFileSync(dbPath, JSON.stringify(db, null, 2), 'utf8');
console.log(`Updated ${updatedDbCount} items with name_ru in ${dbPath} (out of ${db.unlocks.length})`);
