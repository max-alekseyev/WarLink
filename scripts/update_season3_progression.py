#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Update WarLink progression database to Season 3 & Patch 0.31 specifications
from MetaForge data, including Multi-Track unlocks and Russian localization.
"""

import json
import os
import re

SCRATCH_DIR = r"C:\Users\Max\.gemini\antigravity\brain\c4eee947-7676-4446-8d50-efdf214f51c6\scratch"
DB_PATH = os.path.join(os.path.dirname(__file__), "..", "internal", "progression", "progression_db.json")
BACKUP_DB_PATH = os.path.join(os.path.dirname(__file__), "..", "progression", "progression_db.json")

# Role normalization
ROLE_MAP = {
    'wardog': 'career',
    'career': 'career',
    'infantry': 'assault',
    'assault': 'assault',
    'driver': 'driver',
    'medic': 'medic',
    'pilot': 'pilot',
    'recon': 'recon',
    'support': 'support'
}

# New translations dictionary for Season 2/3 items and catalog
TRANSLATIONS = {
    # Unlocks & Catalog Items
    "wolf-110 [m2 browning]": "Бронеавтомобиль Wolf-110 [M2 Browning]",
    "wolf-110-m2-browning": "Бронеавтомобиль Wolf-110 [M2 Browning]",
    "srd762ti-qd": "Глушитель SRD762TI-QD",
    "evo-3 ultra 50 rnd box": "50-зарядный короб для Evo-3 Ultra",
    "evo-3-ultra-50-rnd-box": "50-зарядный короб для Evo-3 Ultra",
    "evo-3 ultra bipod": "Сошки для Evo-3 Ultra",
    "evo-3-ultra-bipod": "Сошки для Evo-3 Ultra",
    "gold exchange": "Обмен золота",
    "gold-exchange": "Обмен золота",
    "125mm heat-mp shell": "125-мм кумулятивно-осколочный снаряд (HEAT-MP)",
    "125mm-heat-mp-shell": "125-мм кумулятивно-осколочный снаряд (HEAT-MP)",
    "2a46m-5 cannon": "125-мм танковая пушка 2А46М-5",
    "2a46m-5-cannon": "125-мм танковая пушка 2А46М-5",
    "hydra 70mm": "Неуправляемая авиаракета Hydra 70mm",
    "hydra-70mm": "Неуправляемая авиаракета Hydra 70mm",
    "large shelter": "Большое укрытие",
    "large-shelter": "Большое укрытие",
    "m2 browning machine gun": "Крупнокалиберный пулемет M2 Browning",
    "m2-browning-machine-gun": "Крупнокалиберный пулемет M2 Browning",
    "m2-browning-machine-gun-2": "Крупнокалиберный пулемет M2 Browning (спаренный)",
    "mg nest": "Пулеметное гнездо",
    "mg-nest": "Пулеметное гнездо",
    "pilot helmet": "Шлем пилота",
    "pilot-helmet": "Шлем пилота",
    "pktm coaxial gun": "Спаренный пулемет ПКТМ",
    "pktm-coaxial-gun": "Спаренный пулемет ПКТМ",
    "pp-19 bizon": "Пистолет-пулемет ПП-19 Бизон",
    "pp-19-bizon": "Пистолет-пулемет ПП-19 Бизон",
    "pp-19 bizon 53 rnd magazine": "53-зарядный шнековый магазин для ПП-19 Бизон",
    "pp-19-bizon-53-rnd-magazine": "53-зарядный шнековый магазин для ПП-19 Бизон",
    "smoke": "Дымовые гранаты / завеса",
    "tank helmet": "Танковый шлемофон",
    "tank-helmet": "Танковый шлемофон",
    "wooden step": "Деревянная ступенька",
    "wooden-step": "Деревянная ступенька",
    "c4 charge": "Заряд C4",
    "c4-charge": "Заряд C4",
    "flakpanzer gepard": "ЗСУ Flakpanzer Gepard",
    "flakpanzer-gepard": "ЗСУ Flakpanzer Gepard",
    "l2a6": "Основной боевой танк Leopard 2A6",
    "sph-2": "Самоходная гаубица SPH-2",
    "t-72b3": "Основной боевой танк Т-72Б3",
    "ah-6m [miniguns]": "Ударный вертолет AH-6M [Miniguns]",
    "z20 lakota [miniguns]": "Вертолет Z20 Lakota [Miniguns]"
}

# Category translations
SUBCATEGORY_RU = {
    "Magazines": "Магазин",
    "Pistol": "Пистолет",
    "Body Armour": "Броня",
    "Helmets": "Шлем",
    "Optics": "Оптика",
    "Logistics": "Логистика",
    "General": "Снаряжение",
    "Backpack": "Рюкзак",
    "Vest": "Жилет",
    "Foregrips": "Сошка / Рукоятка",
    "Sights": "Прицел",
    "Muzzles": "Дульный тормоз / Глушитель",
    "Building": "Строительство",
    "Traversal": "Передвижение",
    "Ground": "Наземная техника",
    "Air": "Воздушная техника",
    "Cannons": "Орудие",
    "Guns": "Пулемет",
    "Emplacements": "Огневая точка",
    "Countermeasures": "Контрмеры",
    "Charge": "Взрывчатка",
    "Pallet": "Поддон снабжения",
    "Crate": "Ящик",
    "Barrels": "Ствол",
    "Handguards": "Цевье",
    "Stocks": "Приклад",
    "Pistol Grips": "Пистолетная рукоятка",
    "Triggers": "Спусковой механизм",
    "Dust Covers": "Крышка ствольной коробки",
    "Vehicle & Heavy": "Тяжелое вооружение",
    "Rockets": "Ракеты",
    "Submachine Gun": "Пистолет-пулемет",
    "Assault Rifle": "Штурмовая винтовка",
    "Sniper Rifle": "Снайперская винтовка",
    "Marksman Rifle": "Марксманская винтовка",
    "Shotgun": "Дробовик",
    "Light Machine Gun": "Ручной пулемет",
    "Battle Rifle": "Боевая винтовка",
    "Launchers": "Гранатомет",
    ".308 Win": "Боеприпасы .308 Win",
    ".338": "Боеприпасы .338",
    ".45 ACP": "Боеприпасы .45 ACP",
    ".45 Colt": "Боеприпасы .45 Colt",
    ".50Cal": "Боеприпасы .50 Cal",
    "5.45x39mm": "Боеприпасы 5.45x39mm",
    "5.56x45mm": "Боеприпасы 5.56x45mm",
    "7.62x39mm": "Боеприпасы 7.62x39mm",
    "7.62x51mm": "Боеприпасы 7.62x51mm",
    "7.62x54mm": "Боеприпасы 7.62x54mm",
    "9x19mm": "Боеприпасы 9x19mm",
    "12g": "Боеприпасы 12g",
    "50AE": "Боеприпасы 50AE",
    "Tapes": "Аудиокассета",
    "Market": "Рынок"
}

def load_sources():
    # 1. New S3 Progression Raw
    with open(os.path.join(SCRATCH_DIR, 'metaforge_progression_s3_raw.json'), 'r', encoding='utf-8') as f:
        s3_raw = json.load(f)

    # 2. Patch 0.31 Catalog Data
    with open(os.path.join(SCRATCH_DIR, 'patch-0-31-data.json'), 'r', encoding='utf-8') as f:
        p31_data = json.load(f)

    # 3. Existing progression DB (for existing translations)
    with open(DB_PATH, 'r', encoding='utf-8') as f:
        existing_db = json.load(f)

    return s3_raw, p31_data, existing_db

def build_translation_dicts(existing_db):
    ru_by_name = {}
    ru_by_slug = {}
    
    # Load from existing unlocks
    for u in existing_db.get('unlocks', []):
        n = (u.get('name') or '').strip().lower()
        s = (u.get('slug') or '').strip().lower()
        nr = u.get('name_ru', '').strip()
        if n and nr:
            ru_by_name[n] = nr
        if s and nr:
            ru_by_slug[s] = nr
            
    # Load from existing catalog
    for it in existing_db.get('catalog', []):
        n = (it.get('name') or '').strip().lower()
        s = (it.get('slug') or '').strip().lower()
        nr = it.get('name_ru', '').strip()
        if n and nr:
            ru_by_name[n] = nr
        if s and nr:
            ru_by_slug[s] = nr
            
    # Overlay manual overrides from TRANSLATIONS
    for k, v in TRANSLATIONS.items():
        ru_by_name[k.lower()] = v
        ru_by_slug[k.lower()] = v
        
    return ru_by_name, ru_by_slug

def resolve_translation(name, slug, ru_by_name, ru_by_slug):
    n_clean = (name or '').strip().lower()
    s_clean = (slug or '').strip().lower()
    
    if s_clean in ru_by_slug:
        return ru_by_slug[s_clean]
    if n_clean in ru_by_name:
        return ru_by_name[n_clean]
    if s_clean in TRANSLATIONS:
        return TRANSLATIONS[s_clean]
    if n_clean in TRANSLATIONS:
        return TRANSLATIONS[n_clean]
        
    return name

def main():
    print("Building unified Season 3 progression database...")
    s3_raw, p31_data, existing_db = load_sources()
    ru_by_name, ru_by_slug = build_translation_dicts(existing_db)

    # Build catalog lookup
    catalog_items = p31_data.get('items', [])
    catalog_by_slug = {it.get('slug'): it for it in catalog_items if it.get('slug')}
    catalog_by_id = {it.get('id'): it for it in catalog_items if it.get('id')}

    # Check available local icons in guides/icons
    available_icons = set()
    if os.path.exists('guides/icons'):
        available_icons = set(os.listdir('guides/icons'))

    # Build Unlocks
    raw_unlocks = s3_raw.get('unlocks', [])
    enriched_unlocks = []

    for u in raw_unlocks:
        slug = u.get('slug') or ''
        name = u.get('name') or slug
        role_raw = u.get('role', 'career')
        role = ROLE_MAP.get(role_raw, role_raw)
        level = u.get('level', 1)
        total_xp = u.get('totalXp')

        cat_item = catalog_by_slug.get(slug) or catalog_by_id.get(u.get('id')) or {}

        # Subcategory and Category RU
        subcat = u.get('subcategory') or cat_item.get('subcategory') or 'General'
        category_ru = SUBCATEGORY_RU.get(subcat, 'Предмет')

        # Name RU
        name_ru = resolve_translation(name, slug, ru_by_name, ru_by_slug)

        # Tab
        tab = u.get('tab') or cat_item.get('tab') or 'equipment'

        # Multi-Track info
        raw_other = u.get('otherTracks', [])
        other_tracks = []
        for ot in raw_other:
            other_role = ROLE_MAP.get(ot.get('role'), ot.get('role'))
            other_tracks.append({
                "role": other_role,
                "level": ot.get('level', 1)
            })

        # Special handling for Music Tapes
        u_id = u.get('id') or ''
        if 'musictape' in u_id.lower() or 'musictape' in name.lower() or 'tape' in (u.get('subcategory') or '').lower():
            if not slug:
                slug = u_id.lower().replace('id.item.', '').replace('.', '-')
            subcat = 'Tapes'
            category_ru = 'Аудиокассета'
            tab = 'gear'

        # Icon path
        icon_name = u.get('icon') or cat_item.get('icon') or ''
        icon_path = ""
        if icon_name:
            icon_path = f"static/wardogs/items/{icon_name}.webp"
        elif 'musictape' in u_id.lower() or 'musictape' in name.lower():
            icon_path = "static/wardogs/items/t_ui_item_musictape.webp"
        elif slug:
            icon_path = f"static/wardogs/items/{slug}.webp"

        # Vendor Unlock Cost in in-game cash ($), matching MetaForge
        unlock_cost = u.get('cash') if u.get('cash') is not None else 0

        # In-game loadout purchase price
        price = cat_item.get('price', 0)

        # Limited mode banned flag
        limited_banned = cat_item.get('limitedGearBanned', False)

        unlock_id = u.get('id') or slug

        entry = {
            "unlock_id": unlock_id,
            "slug": slug,
            "name": name,
            "name_ru": name_ru,
            "role": role,
            "level": level,
            "total_xp": total_xp,
            "tab": tab,
            "subcategory": subcat,
            "category_ru": category_ru,
            "price": price,
            "unlock_cost": unlock_cost,
            "icon": icon_path,
            "image": "",
            "guide": "",
            "description": cat_item.get('description', ''),
            "specs": {
                "weight": cat_item.get('weight'),
                "gridSize": cat_item.get('gridSize'),
                "slot": cat_item.get('slot')
            },
            "other_tracks": other_tracks,
            "limited_gear_banned": limited_banned
        }
        enriched_unlocks.append(entry)

    # Sort unlocks strictly by role, then level ascending, then name
    enriched_unlocks.sort(key=lambda x: (x['role'], x['level'], x['name']))

    # Build enriched catalog
    enriched_catalog = []
    for it in catalog_items:
        slug = it.get('slug') or ''
        name = it.get('name') or slug
        subcat = it.get('subcategory') or 'General'
        category_ru = SUBCATEGORY_RU.get(subcat, 'Предмет')
        name_ru = resolve_translation(name, slug, ru_by_name, ru_by_slug)

        icon_name = it.get('icon') or ''
        icon_path = f"static/wardogs/items/{icon_name}.webp" if icon_name else f"static/wardogs/items/{slug}.webp"

        entry = {
            "id": it.get('id'),
            "slug": slug,
            "name": name,
            "name_ru": name_ru,
            "tab": it.get('tab', 'other'),
            "category": it.get('category', ''),
            "subcategory": subcat,
            "category_ru": category_ru,
            "role": it.get('role', 'None'),
            "price": it.get('price', 0),
            "icon": icon_path,
            "description": it.get('description', ''),
            "flavor": it.get('flavor', ''),
            "weight": it.get('weight', 0),
            "gridSize": it.get('gridSize', [1, 1]),
            "slot": it.get('slot', ''),
            "limitedGearBanned": it.get('limitedGearBanned', False),
            "inVendor": it.get('inVendor', True),
            "vendorCategories": it.get('vendorCategories', []),
            "effects": it.get('effects', []),
            "fits": it.get('fits', [])
        }
        enriched_catalog.append(entry)

    final_db = {
        "version": "3.0.0",
        "season": 3,
        "patch": "patch-0.31",
        "source": "MetaForge.app/wardogs",
        "total_unlocks": len(enriched_unlocks),
        "total_catalog_items": len(enriched_catalog),
        "roles": ["career", "assault", "medic", "recon", "support", "driver", "pilot"],
        "unlocks": enriched_unlocks,
        "catalog": enriched_catalog
    }

    # Save to internal/progression/progression_db.json
    with open(DB_PATH, 'w', encoding='utf-8') as f:
        json.dump(final_db, f, ensure_ascii=False, indent=2)

    # Save to progression/progression_db.json (backup & CLI tool reference)
    with open(BACKUP_DB_PATH, 'w', encoding='utf-8') as f:
        json.dump(final_db, f, ensure_ascii=False, indent=2)

    print(f"Successfully generated progression_db.json:")
    print(f"  Version: {final_db['version']} (Season {final_db['season']}, {final_db['patch']})")
    print(f"  Total Unlocks: {len(enriched_unlocks)}")
    print(f"  Total Catalog Items: {len(enriched_catalog)}")

    # Role breakdown
    from collections import Counter
    role_counts = Counter(u['role'] for u in enriched_unlocks)
    for r in ["career", "assault", "driver", "medic", "pilot", "recon", "support"]:
        print(f"    - {r.upper():<8}: {role_counts[r]} unlocks")

if __name__ == '__main__':
    main()
