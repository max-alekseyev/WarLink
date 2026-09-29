const fs = require('fs');

// 1. Load database
const dbPath = 'internal/progression/progression_db.json';
const db = JSON.parse(fs.readFileSync(dbPath, 'utf8'));

// Build lookup map from DB by item_id or unlock_id
const dbMap = {};
if (db.unlocks && Array.isArray(db.unlocks)) {
    db.unlocks.forEach(u => {
        dbMap[u.unlock_id] = u;
        if (u.item_id) dbMap[u.item_id] = u;
    });
}

// 2. Comprehensive fix mapping for 88 issues
const CORRECTIONS = {
    // 1.1 Structural / semantic mismatches
    "Id.Item.MAGZ_019": "30-зарядный удлиненный магазин для AMP-9",
    "Id.Item.MAGZ_021": "40-зарядный удлиненный магазин для СКС",
    "Id.Item.HNDG_005": "Полимерное цевье с перфорацией",
    "Id.Item.STOK_006": "Скелетонизированный полимерный приклад",
    "Id.Item.STOK_008": "Деревянный приклад РПК-74",
    "Id.Item.HNDG_007": "Цевье с рукояткой типа «акулий плавник» (Sharkfin)",
    "Id.Item.HNDG_006": "Деревянное цевье с рукояткой («Румынский донг») для АК",
    "Id.Item.PGRP_004": "Деревянная пистолетная рукоятка для АК",

    // 1.2 Typos, calques and linguistic fixes
    "Id.Item.BasicRedDot": "Компактный коллиматорный прицел T-2",
    "Id.Item.SGHT_029": "3-кратный тактический призматический прицел",
    "Id.Item.MUZL_013": "Трехщелевой пламегаситель",
    "Id.Item.MAGZ_059": "10-зарядный магазин для ПП-19 Витязь",
    "Id.Item.SGHT_011": "Оптический прицел 3x-6x LPVO Short Dot",
    "Id.Item.SGHT_036": "6-кратный марксманский прицел с коллиматором",
    "Id.Item.MUZL_057": "Газоразгруженный глушитель калибра .308",
    "Id.Item.MUZL_038": "Щелевой пламегаситель",
    "Vehicle.Variant.Land.Wheeled.DuneBuggy.Default": "Дюнный багги",
    "Id.Item.MunitionsSupplies": "Запас боеприпасов",

    // 1.3 Muzzles and brakes
    "Id.Item.MUZL_024": "Шестигранный дульный тормоз",
    "Id.Item.MUZL_014": "Дульный тормоз TopComp",
    "ID.Item.PWSCQB74Muzzle": "Дульный тормоз CQB 74",
    "Id.Item.MZL_10": "Тактический глушитель ТГП-А 5,45 мм",
    "Id.Item.MUZL_027": "3-камерный дульный тормоз",
    "Id.Item.MUZL_019": "Двухпортовый дульный тормоз",
    "Id.Item.MUZL_020": "Дульный тормоз Ballista",
    "Id.Item.MUZL_051": "Глушитель для AMP-9 9x19 мм",
    "Id.Item.MUZL_053": "Глушитель для ПП-19-01 Витязь 9x19 мм",
    "Id.Item.MUZL_028": "Дульный тормоз-компенсатор ДТК-1",
    "Id.Item.MUZL_040": "Дульный тормоз СВД 7,62x54R",
    "Id.Item.MUZL_041": "Дульный тормоз SRVV",
    "Id.Item.MUZL_022": "Дульный тормоз Constrictor",
    "Id.Item.MUZL_025": "Дульный тормоз Slicktap",
    "Id.Item.MUZL_030": "Трехпортовый дульный тормоз",
    "Id.Item.MUZL_049": "Тяжелый глушитель .50 Cal",
    "Id.Item.MUZL_031": "Дульный тормоз Sabre для M500",
    "Id.Item.MUZL_023": "Дульный тормоз Tread",
    "Id.Item.MZL_06": "Глушитель 12-го калибра",
    "Id.Item.MUZL_029": "Дульный тормоз Orpheus Max",

    // 1.4 Optics
    "Id.Item.OPT_08": "Боевой оптический прицел 2.5x",
    "Id.Item.SGHT_026": "Компактный призматический прицел Trijicon 1.5x",
    "Id.Item.SGHT_028": "Боевой призматический прицел CQ-2x",
    "Id.Item.3xScope": "Призматический прицел Spitfire 3x",
    "Id.Item.SGHT_023": "Коллиматорный прицел ОКП-7",
    "Id.Item.SniperScope10x": "Оптический прицел 6-10x MRAD",
    "Id.Item.10xScopeMoa": "Оптический прицел 6-10x MOA",
    "Id.Item.SGHT_032": "Высокоточный оптический прицел Frontier 2.5x-10x",
    "Id.Item.4xElcanSpectr": "Оптический прицел Elcan Specter 4x",
    "Id.Item.HybridScope4x": "4x гибридный прицел",
    "Id.Item.SGHT_033": "Оптический прицел MAAWS (CGM4)",
    "Id.Item.SGHT_034": "Прицел ПГО-7",

    // 1.5 Grips and bipods
    "Id.Item.FGRP_014": "Тактическая передняя рукоятка CQR",
    "Id.Item.FGRP_011": "Сошки для СВД",
    "Id.Item.FGRP_009": "Сошки для СВ-98",
    "Id.Item.FGRP_013": "Сошки Pro Tilt",
    "Id.Item.FGRP_012": "Сошки для M249",
    "Id.Item.FGRP_008": "Сошки для ПКМ",
    "Id.Item.FGP_04": "Гибридная рукоятка-сошка Grip Pod",

    // 1.6 Vehicles and weapons
    "Vehicle.Variant.Land.Tracked.TNK_01.Artillery": "САУ SPH-2",
    "Vehicle.Variant.Land.Wheeled.Ural.Battle": "Урал Дефендер",
    "Vehicle.Variant.Land.Wheeled.Ural.Attack": "Урал Дефендер [M249]",
    "Vehicle.Variant.Air.Rotary.Littlebird.MountedMachineGuns": "AH-6M Литлберд [Миниганы]",
    "Vehicle.Variant.Air.Rotary.ROT_04.Default": "Z20 Лакота",
    "Vehicle.Variant.Air.Rotary.Littlebird.RocketPods": "AH-6R Литлберд [НАР]",
    "Vehicle.Variant.Air.Rotary.ROT_04.MountedMachineGuns": "Z20 Лакота [Миниганы]",
    "Vehicle.Variant.Air.Rotary.Havoc.Default": "Вертолет Havoc",
    "Vehicle.Variant.Air.Rotary.Littlebird.Default": "Вертолет MH-6 Литлберд",
    "Id.Vehicle.WeaponExtension.ROT_03.RocketPods": "Блок НАР Б-13 (122 мм)",
    "Vehicle.Variant.Stationary.STN_05": "Стационарная пусковая установка «Стингрей»",
    "ID.Item.VehicleSupplyCrate.Large": "Большой ящик снабжения",
    "ID.Item.VehicleSupplyCrate.Small.Armoured": "Малый бронированный ящик снабжения",
    "ID.Item.VehicleSupplyCrate.Large.Armoured": "Большой бронированный ящик снабжения",

    // 1.7 Fortifications & Tools
    "ID.Item.BuildTool.Hammer.Medium": "Средний строительный молоток",
    "ID.Item.BuildTool.Hammer.Large": "Большой строительный молоток",
    "Id.Buildable.AirRaidShelter.Ceiling": "Перекрытие блиндажа",
    "Id.Buildable.AirRaidShelter.Floor": "Пол блиндажа",
    "Id.Buildable.AirRaidShelter": "Полевой блиндаж от артобстрелов",
    "Id.Buildable.Mortar": "Позиция 81-мм миномета L81",
    "Id.Buildable.Phalanx": "ЗАК Vanguard (Вулкан Фаланкс)",

    // 1.8 Calibres and Tracers
    "Id.Item.9mm.HollowPoint.Tracer": "9 мм экспансивные трассирующие (HP-T)",
    "Id.Item.556mm.HollowPoint.Tracer": "5,56 мм экспансивные трассирующие (HP-T)",
    "Id.Item.762mm.HollowPoint.Tracer": "7,62 мм экспансивные трассирующие (HP-T)",
    "Id.Item.545mm.HollowPoint.Tracer": "5,45 мм экспансивные трассирующие (HP-T)",
    "Id.Item.45Colt.HollowPoint.Tracer": ".45 Colt экспансивные трассирующие (HP-T)",
    "Id.Item.762x54mm.HollowPoint.Tracer": "7,62x54R мм экспансивные трассирующие (HP-T)",
    "Id.Item.12g.RifledSlug": "Пуля 12-го калибра (нарезная)",
    "Id.Item.45ACP.HollowPoint.Tracer": ".45 ACP экспансивные трассирующие (HP-T)",
    "Id.Item.762mm.ArmorPiercing.Tracer": "7,62 мм бронебойные трассирующие (AP-T)",
    "Id.Item.308Win.HollowPoint.Tracer": ".308 Win экспансивные трассирующие (HP-T)",
    "Id.Item.9mm.ArmorPiercing.Tracer": "9 мм бронебойные трассирующие (AP-T)",
    "Id.Item.762x54mm.ArmorPiercing.Tracer": "7,62x54R мм бронебойные трассирующие (AP-T)",
    "Id.Item.556mm.ArmorPiercing.Tracer": "5,56 мм бронебойные трассирующие (AP-T)",
    "Id.Item.545mm.ArmorPiercing.Tracer": "5,45 мм бронебойные трассирующие (AP-T)",
    "Id.Item.50AE.HollowPoint.Tracer": ".50 AE экспансивные трассирующие (HP-T)",
    "Id.Item.45Colt.ArmorPiercing.Tracer": ".45 Colt бронебойные трассирующие (AP-T)",
    "Id.Item.45ACP.ArmorPiercing.Tracer": ".45 ACP бронебойные трассирующие (AP-T)",
    "Id.Item.50AE.ArmorPiercing.Tracer": ".50 AE бронебойные трассирующие (AP-T)",
    "Id.Item.308Win.ArmorPiercing.Tracer": ".308 Win бронебойные трассирующие (AP-T)",
    "Id.Item.SV98": "СВ-98",
    "Id.Item.93mm": "Выстрел 93 мм (ПГ-7В)",
    "Id.Item.40mm": "Граната 40 мм",
    "Id.Item.Launcher_04": "ПЗРК 9К333 Верба",
    "ID.Item.ATMine": "Противотанковая мина",

    // 1.9 Magazines & Boxes
    "Id.Item.STANAGMagazine": "30-зарядный магазин STANAG",
    "Id.Item.STANAGExtendedMagazine": "60-зарядный магазин STANAG",
    "Id.Item.MAGZ_062": "100-зарядный мягкий короб для M249",
    "Id.Item.M249Magazine": "200-зарядный короб для M249",
    "Id.Item.LMG_02Magazine": "100-зарядный короб для ПКМ",
    "Id.Item.MAGZ_031": "150-зарядный сдвоенный барабанный магазин SAW-MAG"
};

// 3. Category corrections for Section 1
const CATEGORY_FIXES = {
    "Id.Item.Resuscitator.Standard": "Медицина",
    "Id.Item.SVDMMagazine": "Магазин",
    "Id.Item.SV98Magazine": "Магазин",
    "Id.Item.SR_04Magazine": "Магазин",
    "Id.Item.MK22Mag": "Магазин",
    "Id.Item.MusicTape.01": "Музыкальная запись",
    "Id.Item.MusicTape.02": "Музыкальная запись",
    "Id.Item.MusicTape.03": "Музыкальная запись",
    "Id.Item.MusicTape.04": "Музыкальная запись",
    "Id.Item.MusicTape.06": "Музыкальная запись",
    "Id.Item.MusicTape.07": "Музыкальная запись",
    "Id.Item.MusicTape.08": "Музыкальная запись",
    "Id.Item.MusicTape.09": "Музыкальная запись"
};

// 4. Role translation for display in Unlock column
const ROLE_NAMES = {
    career: "Карьера",
    assault: "Штурмовик",
    medic: "Медик",
    recon: "Разведчик",
    support: "Поддержка",
    driver: "Водитель",
    pilot: "Пилот"
};

// 5. Read ITEMS_TRANSLATION.md
const mdContent = fs.readFileSync('ITEMS_TRANSLATION.md', 'utf8');
const lines = mdContent.split('\n');

let inSection1 = false;
let inSection2 = false;
const processedLines = [];

for (let line of lines) {
    if (line.includes('## 1. Награды прокачки')) {
        inSection1 = true;
        inSection2 = false;
        processedLines.push(line);
        continue;
    }
    if (line.includes('## 2. Предметы арсенала')) {
        inSection1 = false;
        inSection2 = true;
        processedLines.push(line);
        continue;
    }

    if (inSection1) {
        // Check for table header
        if (line.includes('| № | Картинка | ID предмета |')) {
            processedLines.push('| № | Картинка | ID предмета | Текущее название (EN) | Название в игре (RU) | Категория | Разблокировка | Цена |');
            continue;
        }
        if (line.includes('| :-- | :---: | --- |')) {
            processedLines.push('| :-- | :---: | --- | --- | --- | --- | --- | --- |');
            continue;
        }

        // Process Section 1 row
        if (line.trim().startsWith('|') && !line.includes('---')) {
            const rawParts = line.split('|').map(s => s.trim());
            // Filter out empty ends
            const parts = rawParts.filter((_, i) => i > 0 && i < rawParts.length - 1);
            if (parts.length >= 6) {
                const num = parts[0];
                const icon = parts[1];
                const rawId = parts[2];
                const cleanId = rawId.replace(/`/g, '').trim();
                const en = parts[3];
                let ru = parts[4];
                let cat = parts[5];

                // Check DB for this unlock to restore exact unlock level, role and price
                const dbItem = dbMap[cleanId];
                let unlockText = '—';
                let priceText = '—';

                if (dbItem) {
                    const roleName = ROLE_NAMES[dbItem.role] || dbItem.role;
                    unlockText = `${roleName} Ур. ${dbItem.level}`;
                    priceText = dbItem.price > 0 ? '$' + dbItem.price.toLocaleString('en-US') : '$0';
                    if (dbItem.category) {
                        cat = dbItem.category;
                    }
                }

                // Apply corrections
                if (CORRECTIONS[cleanId]) {
                    ru = CORRECTIONS[cleanId];
                }
                if (CATEGORY_FIXES[cleanId]) {
                    cat = CATEGORY_FIXES[cleanId];
                }

                processedLines.push(`| ${num} | ${icon} | \`${cleanId}\` | ${en} | ${ru} | ${cat} | ${unlockText} | ${priceText} |`);
                continue;
            }
        }
    }

    if (inSection2) {
        // Strip leading whitespace before pipe in section 2
        let trimmedLine = line.trim();
        if (trimmedLine.startsWith('|') && !trimmedLine.includes('---') && !trimmedLine.includes('ID предмета')) {
            const rawParts = trimmedLine.split('|').map(s => s.trim());
            const parts = rawParts.filter((_, i) => i > 0 && i < rawParts.length - 1);
            if (parts.length >= 6) {
                const num = parts[0];
                const icon = parts[1];
                const cleanId = parts[2].replace(/`/g, '').trim();
                const en = parts[3];
                let ru = parts[4];
                let cat = parts[5];
                const price = parts[6] || '—';

                if (CORRECTIONS[cleanId]) {
                    ru = CORRECTIONS[cleanId];
                }
                if (CATEGORY_FIXES[cleanId]) {
                    cat = CATEGORY_FIXES[cleanId];
                }

                processedLines.push(`| ${num} | ${icon} | \`${cleanId}\` | ${en} | ${ru} | ${cat} | ${price} |`);
                continue;
            }
        }
    }

    processedLines.push(line);
}

fs.writeFileSync('ITEMS_TRANSLATION.md', processedLines.join('\n'), 'utf8');
console.log('Successfully updated and restructured ITEMS_TRANSLATION.md');

// 6. Synchronize name_ru in internal/progression/progression_db.json
let dbSyncCount = 0;
// Re-read updated markdown to get the authoritative name_ru for all unlocks
const finalMd = fs.readFileSync('ITEMS_TRANSLATION.md', 'utf8');
const finalMdLines = finalMd.split('\n');

const authoritativeMap = {};
for (const l of finalMdLines) {
    if (l.trim().startsWith('|') && !l.includes('---') && !l.includes('ID предмета')) {
        const parts = l.split('|').map(s => s.trim()).filter(Boolean);
        if (parts.length >= 5) {
            const cleanId = parts[2].replace(/`/g, '').trim();
            const ru = parts[4];
            if (ru) {
                authoritativeMap[cleanId] = ru;
            }
        }
    }
}

if (db.unlocks && Array.isArray(db.unlocks)) {
    db.unlocks.forEach(u => {
        const ru = authoritativeMap[u.item_id] || authoritativeMap[u.unlock_id];
        if (ru) {
            u.name_ru = ru;
            dbSyncCount++;
        }
    });
}

fs.writeFileSync(dbPath, JSON.stringify(db, null, 2), 'utf8');
console.log(`Synchronized ${dbSyncCount} name_ru entries in ${dbPath}`);
