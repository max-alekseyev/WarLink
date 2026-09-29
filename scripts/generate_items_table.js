const fs = require('fs');
const path = require('path');

const rootDir = path.resolve(__dirname, '..');
const dbPath = path.join(rootDir, 'internal/progression/progression_db.json');
const outputPath = path.join(rootDir, 'ITEMS_TRANSLATION.md');

const db = JSON.parse(fs.readFileSync(dbPath, 'utf8'));

// Role display names
const ROLE_NAMES = {
    career: 'Карьера',
    assault: 'Штурмовик',
    medic: 'Медик',
    recon: 'Разведчик',
    support: 'Поддержка',
    driver: 'Водитель',
    pilot: 'Пилот'
};

// Category Russian labels
const CAT_NAMES = {
    weapons: 'Оружие',
    attachments: 'Модули и обвесы',
    ammunition: 'Боеприпасы',
    armor: 'Броня и шлемы',
    storage: 'Рюкзаки и разгрузки',
    equipment: 'Снаряжение',
    vehicles: 'Техника',
    'mounted-weapons': 'Стационарные орудия',
    buildables: 'Инженерные постройки',
    throwables: 'Гранаты и метательное',
    deployables: 'Развертываемое оборудование',
    other: 'Разное'
};

// Subcategory labels
const SUBCAT_RU = {
    'Pistol': 'Пистолет',
    'Magazines': 'Магазин',
    'Body Armour': 'Броня',
    'Helmets': 'Шлем',
    'Optics': 'Прицел',
    'Muzzles': 'ДТК / Глушитель',
    'Assault Rifle': 'Штурм. винтовка',
    'SMG': 'Пистолет-пулемет',
    'Shotgun': 'Дробовик',
    'Sniper Rifle': 'Снайп. винтовка',
    'DMR': 'Марксман. винтовка',
    'LMG': 'Пулемет',
    'Backpack': 'Рюкзак',
    'Grip': 'Рукоятка',
    'Ammunition': 'Боеприпасы',
    'Vehicle': 'Техника',
    'Patch': 'Патч'
};

// Map of all items keyed by ID
const allItemsMap = new Map();

// 1. Process Progression Unlocks first
for (const u of db.unlocks) {
    allItemsMap.set(u.unlock_id, {
        id: u.unlock_id,
        slug: u.slug,
        name: u.name,
        icon: u.icon,
        image: u.image,
        role: u.role,
        level: u.level,
        tab: u.tab || 'other',
        subcategory: u.subcategory || '',
        price: u.price || 0,
        amount: u.amount || 0,
        isUnlock: true
    });
}

// 2. Process Catalog items
for (const c of db.catalog) {
    if (!allItemsMap.has(c.id)) {
        allItemsMap.set(c.id, {
            id: c.id,
            slug: c.slug,
            name: c.name,
            icon: c.icon,
            image: c.image || '',
            role: c.role || '',
            level: '',
            tab: c.tab || c.category || 'other',
            subcategory: c.subcategory || '',
            price: c.price || 0,
            amount: c.amount || 0,
            isUnlock: false
        });
    }
}

// Resolve image path for an item
function resolveImage(it) {
    // 1. Direct path check
    if (it.icon && fs.existsSync(path.join(rootDir, it.icon))) {
        return it.icon.replace(/\\/g, '/');
    }
    // 2. Icon in guides/icons/
    if (it.icon) {
        const pWebp = path.join(rootDir, 'guides/icons', it.icon + '.webp');
        if (fs.existsSync(pWebp)) return ('guides/icons/' + it.icon + '.webp').replace(/\\/g, '/');

        const pRaw = path.join(rootDir, 'guides/icons', it.icon);
        if (fs.existsSync(pRaw)) return ('guides/icons/' + it.icon).replace(/\\/g, '/');

        const pPng = path.join(rootDir, 'guides/icons', it.icon + '.png');
        if (fs.existsSync(pPng)) return ('guides/icons/' + it.icon + '.png').replace(/\\/g, '/');
    }
    // 3. Direct image check
    if (it.image && fs.existsSync(path.join(rootDir, it.image))) {
        return it.image.replace(/\\/g, '/');
    }
    // 4. Image by slug in guides/images/
    if (it.slug) {
        const pSlugPng = path.join(rootDir, 'guides/images', it.slug + '.png');
        if (fs.existsSync(pSlugPng)) return ('guides/images/' + it.slug + '.png').replace(/\\/g, '/');

        const pSlugWebp = path.join(rootDir, 'guides/images', it.slug + '.webp');
        if (fs.existsSync(pSlugWebp)) return ('guides/images/' + it.slug + '.webp').replace(/\\/g, '/');
    }
    return '';
}

// Group items into Sections
const progressionUnlocks = [];
const catalogOther = [];

for (const it of allItemsMap.values()) {
    if (it.isUnlock) {
        progressionUnlocks.push(it);
    } else {
        catalogOther.push(it);
    }
}

// Sort unlocks by role order then level
const ROLE_ORDER = ['career', 'assault', 'medic', 'recon', 'support', 'driver', 'pilot'];
progressionUnlocks.sort((a, b) => {
    const rA = ROLE_ORDER.indexOf(a.role);
    const rB = ROLE_ORDER.indexOf(b.role);
    if (rA !== rB) return (rA === -1 ? 99 : rA) - (rB === -1 ? 99 : rB);
    return (a.level || 0) - (b.level || 0);
});

// Group catalog items by tab
const TAB_ORDER = [
    'weapons',
    'attachments',
    'ammunition',
    'armor',
    'storage',
    'equipment',
    'vehicles',
    'mounted-weapons',
    'buildables',
    'throwables',
    'deployables',
    'other'
];

catalogOther.sort((a, b) => {
    const tA = TAB_ORDER.indexOf(a.tab);
    const tB = TAB_ORDER.indexOf(b.tab);
    if (tA !== tB) return (tA === -1 ? 99 : tA) - (tB === -1 ? 99 : tB);
    return (a.name || '').localeCompare(b.name || '');
});

// Format Price & Level details
function formatMeta(it) {
    const parts = [];
    if (it.role && ROLE_NAMES[it.role]) {
        parts.push(ROLE_NAMES[it.role] + ' Ур. ' + it.level);
    }
    if (it.price > 0) {
        let p = '$' + it.price.toLocaleString('en-US');
        if (it.amount > 1) p += ' (' + it.amount + ' шт.)';
        parts.push(p);
    }
    return parts.join(' | ') || '—';
}

function escapeTableCell(str) {
    if (!str) return '';
    return String(str).replace(/\|/g, '\\|').replace(/\n/g, ' ');
}

let doc = `# Каталог предметов и таблица русификации WARDOGS

> **Инструкция для пользователя:**
> 1. **Название в игре (RU):** вписывайте точное русскоязычное название предмета так, как оно переведено в официальной русской локализации игры.
> 2. **Картинка:** если у предмета стоит \`[Вставить ссылку на картинку]\`, вы можете просто вставить прямую ссылку на изображение (например, \`https://.../item.png\`). Агент автоматически скачает картинку в локальную базу.
> 3. После заполнения просто напишите агенту: *«Я заполнил названия / вставил ссылки, обнови базу»*, и база \`progression_db.json\` и интерфейс WarLink автоматически обновятся.

---

## Статистика каталога
- **Всего уникальных предметов:** ${allItemsMap.size}
- **Наград прокачки ролей и карьеры:** ${progressionUnlocks.length}
- **Дополнительных предметов из магазина и арсенала:** ${catalogOther.length}

---

`;

let globalCounter = 1;

// Section 1: Progression Unlocks
doc += `## 1. Награды прокачки (Роли и Карьера)\n\n`;
doc += `| № | Картинка | ID предмета | Текущее название (EN) | Название в игре (RU) | Категория | Разблокировка / Цена |\n`;
doc += `|---|:---:|---|---|---|---|---|\n`;

for (const it of progressionUnlocks) {
    const imgPath = resolveImage(it);
    const imgCell = imgPath
        ? `<img src="${imgPath}" width="36" height="36" alt="">`
        : `[Вставить ссылку на картинку]`;
    const catName = SUBCAT_RU[it.subcategory] || it.subcategory || CAT_NAMES[it.tab] || it.tab;
    const meta = formatMeta(it);

    doc += `| ${globalCounter++} | ${imgCell} | \`${it.id}\` | ${escapeTableCell(it.name)} |  | ${escapeTableCell(catName)} | ${escapeTableCell(meta)} |\n`;
}

// Section 2: Catalog items grouped by Category
doc += `\n---\n\n## 2. Предметы арсенала, магазина и снаряжения\n\n`;

let currentTab = null;

for (const it of catalogOther) {
    if (it.tab !== currentTab) {
        currentTab = it.tab;
        const tabTitle = CAT_NAMES[currentTab] || currentTab.toUpperCase();
        doc += `\n### 2.${TAB_ORDER.indexOf(currentTab) + 1}. ${tabTitle}\n\n`;
        doc += `| № | Картинка | ID предмета | Текущее название (EN) | Название в игре (RU) | Категория | Цена |\n`;
        doc += `|---|:---:|---|---|---|---|---|\n`;
    }

    const imgPath = resolveImage(it);
    const imgCell = imgPath
        ? `<img src="${imgPath}" width="36" height="36" alt="">`
        : `[Вставить ссылку на картинку]`;
    const catName = SUBCAT_RU[it.subcategory] || it.subcategory || CAT_NAMES[it.tab] || it.tab;
    const meta = formatMeta(it);

    doc += `| ${globalCounter++} | ${imgCell} | \`${it.id}\` | ${escapeTableCell(it.name)} |  | ${escapeTableCell(catName)} | ${escapeTableCell(meta)} |\n`;
}

fs.writeFileSync(outputPath, doc, 'utf8');
console.log('Successfully generated ITEMS_TRANSLATION.md with', allItemsMap.size, 'items.');
