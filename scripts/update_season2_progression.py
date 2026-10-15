#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Update WarLink progression database to full Season 2 specifications according to Steam patch notes.
"""

import json
import os
import re

DB_PATH = os.path.join(os.path.dirname(__file__), "..", "internal", "progression", "progression_db.json")

def load_db():
    with open(DB_PATH, "r", encoding="utf-8") as f:
        return json.load(f)

def save_db(db):
    with open(DB_PATH, "w", encoding="utf-8") as f:
        json.dump(db, f, ensure_ascii=False, indent=2)

def main():
    db = load_db()
    unlocks = db.get("unlocks", [])
    print(f"Loaded {len(unlocks)} unlocks from {DB_PATH}")

    # Build lookup by name (lowercase normalized) and unlock_id
    by_name = {}
    by_id = {}
    for item in unlocks:
        n = item["name"].strip().lower()
        by_name[n] = item
        by_id[item["unlock_id"]] = item

    def find_item(*names):
        for n in names:
            nl = n.strip().lower()
            if nl in by_name:
                return by_name[nl]
            for k, v in by_name.items():
                if nl == k or nl in k:
                    return v
        return None

    # Track updates
    updated_count = 0
    added_count = 0

    # 1. WARDOG Track (Career)
    career_updates = [
        # items moved into career
        ("Tricon 1.5x Compact Prism Scope", {"role": "career", "level": 26, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел Tricon 1.5x Prism"}),
        ("Spitfire 3X", {"role": "career", "level": 31, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел Spitfire 3X"}),
        # additions
        ("Compact P-1 Red Dot", {"role": "career", "level": 21, "price": 480, "unlock_cost": 12500, "tab": "attachments", "subcategory": "Sights", "name_ru": "Коллиматор Compact P-1"}),
        ("Tape Blue", {"role": "career", "level": 29, "price": 100, "unlock_cost": 25000, "tab": "gear", "subcategory": "Tapes", "name": "Tape Blue (Da Ba Dee)", "name_ru": "Аудиокассета Blue (Da Ba Dee)"}),
        ("Gold Market", {"role": "career", "level": 35, "price": 0, "unlock_cost": 150000, "tab": "gear", "subcategory": "Market", "name": "Gold Market Access", "name_ru": "Золотой рынок"}),
        # changes
        ("Mechanical Supply Pallet", {"role": "career", "level": 5, "price": 10000, "unlock_cost": 10000, "name_ru": "Поддон запчастей"}),
        ("Ammo Supply Pallet", {"role": "career", "level": 9, "price": 25000, "unlock_cost": 25000, "name_ru": "Поддон боеприпасов"}),
        ("Fuel Supply Pallet", {"role": "career", "level": 16, "price": 50000, "unlock_cost": 50000, "name_ru": "Поддон топлива"}),
        ("Judge", {"role": "career", "level": 1, "price": 5000, "unlock_cost": 5000, "name_ru": "Револьвер Judge"}),
        ("GGX 17", {"role": "career", "level": 18, "price": 15000, "unlock_cost": 15000, "name_ru": "Пистолет GGX 17"}),
        ("Deagle", {"role": "career", "level": 65, "price": 750, "unlock_cost": 75000, "name_ru": "Пистолет Deagle"}),
        ("GGX 18", {"role": "career", "level": 85, "price": 1000, "unlock_cost": 125000, "name_ru": "Пистолет GGX 18"}),
        ("Kobra Reflex", {"role": "career", "level": 4, "name_ru": "Прицел Кобра"}),
        ("Compact T-2 Red Dot", {"role": "career", "level": 17, "name_ru": "Коллиматор Compact T-2"}),
        ("Sport Parachute", {"role": "career", "level": 34, "price": 700, "name_ru": "Спортивный парашют"}),
        ("12g Rifled Slug", {"role": "career", "level": 36, "name_ru": "12g Нарезная пуля (Slug)"}),
        ("7.62x39mm Armor Piercing", {"role": "career", "level": 60, "name_ru": "7.62x39mm Бронебойные (AP)"}),
        (".308 Win Flesh Damage", {"role": "career", "level": 63, "name_ru": ".308 Win Экспансивные (HP)"}),
        ("Level 3 Helmet", {"role": "career", "level": 70, "price": 2000, "name_ru": "Шлем 3 уровня"}),
        ("Level 3 Armor", {"role": "career", "level": 70, "price": 2500, "name_ru": "Броня 3 уровня"}),
        ("5.45x39mm Flesh Damage", {"role": "career", "level": 6, "name_ru": "5.45x39mm Экспансивные (HP)"}),
        ("9x19mm Flesh Damage", {"role": "career", "level": 10, "name_ru": "9x19mm Экспансивные (HP)"}),
        ("5.56mm Flesh Damage", {"role": "career", "level": 13, "name_ru": "5.56x45mm Экспансивные (HP)"}),
        ("5.56mm Armor Piercing", {"role": "career", "level": 84, "name_ru": "5.56x45mm Бронебойные (AP)"}),
        ("5.45x39mm Armor Piercing", {"role": "career", "level": 90, "name_ru": "5.45x39mm Бронебойные (AP)"}),
    ]

    # 2. Assault Progression Track
    assault_updates = [
        ("Molotov Cocktail", {"role": "assault", "level": 13, "price": 340, "unlock_cost": 25000, "tab": "gear", "subcategory": "Throwables", "name_ru": "Коктейль Молотова"}),
        ("TCX-SPR 28 RND Magazine", {"role": "assault", "level": 20, "price": 50, "unlock_cost": 0, "name_ru": "Магазин TCX-SPR на 28 патронов"}),
        ("TCX-SPR", {"role": "assault", "level": 20, "price": 3200, "unlock_cost": 100000, "name_ru": "Винтовка TCX-SPR"}),
        ("Cross Rifle Muzzle Brake", {"role": "assault", "level": 32, "price": 780, "unlock_cost": 35000, "name_ru": "Дульный тормоз Cross Rifle (TCX-SPR)"}),
        ("Clutch Lok Muzzle Brake", {"role": "assault", "level": 37, "price": 1100, "unlock_cost": 37500, "name_ru": "Дульный тормоз Clutch Lok (TCX-SPR)"}),
        ("SRD762TI-QD Suppressor", {"role": "assault", "level": 40, "price": 1350, "unlock_cost": 40000, "name_ru": "Глушитель SRD762TI-QD (TCX-SPR)"}),
        # Moved into Assault
        ("Remote Detonator", {"role": "assault", "level": 4, "price": 650, "unlock_cost": 15000, "name_ru": "Детонатор C4"}),
        ("2.5x Combat Optic", {"role": "assault", "level": 17, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел 2.5x Combat Optic"}),
        ("Four Reticle Reflex", {"role": "assault", "level": 22, "tab": "attachments", "subcategory": "Sights", "name_ru": "Коллиматор Four Reticle"}),
        ("3x Tactical Prism Scope", {"role": "assault", "level": 33, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел 3x Tactical Prism"}),
        ("RVG Vertical Foregrip", {"role": "assault", "level": 38, "unlock_cost": 25000, "name_ru": "Рукоятка RVG Vertical"}),
        ("Spectr 4x", {"role": "assault", "level": 43, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел Spectr 4x"}),
        # Assault track changes
        ("T-21", {"role": "assault", "level": 3, "price": 1200, "unlock_cost": 10000, "tab": "weapons", "subcategory": "Assault Rifles", "name": "T-21", "name_ru": "Штурмовая винтовка T-21"}),
        ("STANAG 30 RND Magazine", {"role": "assault", "level": 3, "price": 35, "unlock_cost": 0, "name_ru": "Магазин STANAG на 30 патронов"}),
        ("M4", {"role": "assault", "level": 28, "price": 2800, "unlock_cost": 150000, "name_ru": "Штурмовая винтовка M4A1"}),
        ("FAL", {"role": "assault", "unlock_cost": 250000, "name_ru": "Штурмовая винтовка FAL"}),
        ("AK74 75 RND Drum Magazine", {"role": "assault", "level": 36, "name_ru": "Барабанный магазин АК-74 на 75 патронов"}),
        ("Birdcage Flash Hider", {"role": "assault", "level": 2, "unlock_cost": 5000, "name_ru": "Пламегаситель Birdcage"}),
        ("Hexagon Brake", {"role": "assault", "level": 5, "unlock_cost": 12500, "name_ru": "Дульный тормоз Hexagon"}),
        ("3 Prong Flash Hider", {"role": "assault", "level": 8, "unlock_cost": 7500, "name_ru": "Пламегаситель 3 Prong"}),
        ("CQ-2x Prism Combat Scope", {"role": "assault", "level": 12, "name_ru": "Прицел CQ-2x Prism"}),
        ("CQR Tactical Front Rail Grip", {"role": "assault", "level": 14, "unlock_cost": 10000, "name_ru": "Рукоятка CQR Tactical"}),
        ("CQB 74 Brake", {"role": "assault", "level": 16, "unlock_cost": 15000, "name_ru": "Дульный тормоз CQB 74"}),
        ("PBS-4 Suppressor", {"role": "assault", "level": 19, "name_ru": "Глушитель ПБС-4"}),
        ("TopComp Brake", {"role": "assault", "level": 21, "unlock_cost": 20000, "name_ru": "Дульный тормоз TopComp"}),
        ("RC-556 Suppressor", {"role": "assault", "level": 24, "name_ru": "Глушитель RC-556"}),
        ("OKP 7 Reflex", {"role": "assault", "level": 26, "name_ru": "Прицел ОКП-7"}),
        ("TGP-A Suppressor 5.45", {"role": "assault", "level": 27, "name_ru": "Глушитель ТГП-А 5.45"}),
        ("AR Multi-Caliber Suppressor", {"role": "assault", "level": 29, "unlock_cost": 22500, "name_ru": "Глушитель AR Multi-Caliber"}),
    ]

    # 3. Medic Progression Track
    medic_updates = [
        ("PP-19 Bizon 53 RND Magazine", {"role": "medic", "level": 1, "price": 200, "unlock_cost": 0, "name_ru": "Шнековый магазин Бизона на 53 патрона"}),
        ("PP-19 Bizon", {"role": "medic", "level": 1, "price": 1000, "unlock_cost": 0, "name_ru": "Пистолет-пулемет PP-19 Бизон"}),
        ("AMP-9 15 RND Magazine", {"role": "medic", "level": 20, "price": 30, "unlock_cost": 0, "name_ru": "Магазин AMP-9 на 15 патронов"}),
        ("AMP-9 20 RND Magazine", {"role": "medic", "level": 25, "price": 45, "unlock_cost": 12500, "name_ru": "Магазин AMP-9 на 20 патронов"}),
        ("AMP-9", {"role": "medic", "level": 20, "price": 1375, "unlock_cost": 75000, "name_ru": "Пистолет-пулемет AMP-9"}),
        # Moved into Medic
        ("Holographic Sight", {"role": "medic", "level": 6, "tab": "attachments", "subcategory": "Sights", "name_ru": "Голографический прицел"}),
        ("AFG Angled Foregrip", {"role": "medic", "level": 11, "name_ru": "Угловая рукоятка AFG"}),
        # Changes
        ("MP5", {"role": "medic", "level": 4, "price": 1800, "unlock_cost": 25000, "name_ru": "Пистолет-пулемет MP5"}),
        ("Medical Bag", {"role": "medic", "level": 14, "unlock_cost": 50000, "name_ru": "Медицинская сумка"}),
        ("PP-19 Vityaz", {"role": "medic", "level": 15, "unlock_cost": 50000, "name_ru": "Пистолет-пулемет PP-19 Витязь"}),
        ("Defibrillator", {"role": "medic", "level": 22, "unlock_cost": 110000, "name_ru": "Дефибриллятор"}),
        ("Super-45", {"role": "medic", "price": 5600, "unlock_cost": 200000, "name_ru": "Пистолет-пулемет Super-45"}),
        ("Individual First Aid Kit", {"role": "medic", "level": 10, "name_ru": "Индивидуальная аптечка (IFAK)"}),
        ("Adrenaline Pen", {"role": "medic", "level": 16, "name_ru": "Шприц с адреналином"}),
        ("M18 Smoke Grenade: Black", {"role": "medic", "level": 23, "name_ru": "Дымовая граната M18 (Черный)"}),
        ("Eclipse Flash Hider", {"role": "medic", "level": 1, "unlock_cost": 7500, "name_ru": "Пламегаситель Eclipse"}),
        ("MP5 30 RND Magazine", {"role": "medic", "level": 8, "unlock_cost": 10000, "name_ru": "Магазин MP5 на 30 патронов"}),
        ("Ballista Brake", {"role": "medic", "level": 12, "name_ru": "Дульный тормоз Ballista"}),
        ("QD-5 Suppressor", {"role": "medic", "level": 13, "unlock_cost": 12500, "name_ru": "Глушитель QD-5"}),
        ("PP-19 Vityaz 10 RND Magazine", {"role": "medic", "level": 17, "unlock_cost": 10000, "name_ru": "Магазин Витязь на 10 патронов"}),
        ("PP-19 Vityaz Flash Hider", {"role": "medic", "level": 18, "unlock_cost": 17500, "name_ru": "Пламегаситель Витязь"}),
        ("Vektor Frenix-X Micro Reflex Sight", {"role": "medic", "level": 19, "name_ru": "Прицел Vektor Frenix-X"}),
        ("PP-19 50 RND Vityaz Drum Magazine", {"role": "medic", "level": 21, "unlock_cost": 15000, "name_ru": "Барабанный магазин Витязь на 50 патронов"}),
        ("PP-19-01 Vityaz 9x19 Suppressor", {"role": "medic", "level": 24, "unlock_cost": 20000, "name_ru": "Глушитель ПП-19-01 Витязь"}),
        ("AMP-9 Tactical Flash Hider", {"role": "medic", "level": 26, "unlock_cost": 45000, "name_ru": "Тактический пламегаситель AMP-9"}),
        ("AMP-9 9x19 Suppressor", {"role": "medic", "level": 27, "name_ru": "Глушитель AMP-9"}),
        ("Dual Port Brake", {"role": "medic", "level": 28, "unlock_cost": 22500, "name_ru": "Дульный тормоз Dual Port"}),
        ("MP5 50 RND Drum Magazine", {"role": "medic", "level": 29, "unlock_cost": 50000, "name_ru": "Барабанный магазин MP5 на 50 патронов"}),
        ("AMP-9 30 RND Magazine", {"role": "medic", "level": 30, "unlock_cost": 50000, "name_ru": "Магазин AMP-9 на 30 патронов"}),
        ("Super-45 Flash Hider", {"role": "medic", "level": 36, "name_ru": "Пламегаситель Super-45"}),
        ("Ghost LITE Muzzle Brake", {"role": "medic", "level": 40, "name_ru": "Дульный тормоз Ghost LITE"}),
        ("Super-45 30 RND Magazine", {"role": "medic", "level": 43, "name_ru": "Магазин Super-45 на 30 патронов"}),
        ("AMP-9 50 RND Drum Magazine", {"role": "medic", "level": 45, "unlock_cost": 75000, "name_ru": "Барабанный магазин AMP-9 на 50 патронов"}),
    ]

    # 4. Recon Progression Track
    recon_updates = [
        ("PU 91/30 Scope", {"role": "recon", "level": 10, "price": 780, "unlock_cost": 12500, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел ПУ 91/30 (Мосин)"}),
        ("M14 10 RND Magazine", {"role": "recon", "level": 25, "price": 60, "unlock_cost": 0, "name_ru": "Магазин M14 на 10 патронов"}),
        ("M14", {"role": "recon", "level": 25, "price": 7400, "unlock_cost": 100000, "name_ru": "Винтовка M14"}),
        ("Leatherwood ART II", {"role": "recon", "level": 27, "price": 950, "unlock_cost": 35000, "name_ru": "Прицел Leatherwood ART II (M14)"}),
        ("M14 Bipod", {"role": "recon", "level": 29, "price": 1480, "unlock_cost": 25000, "name_ru": "Сошки для M14"}),
        ("M14 20 RND Magazine", {"role": "recon", "level": 31, "price": 110, "unlock_cost": 27500, "name_ru": "Магазин M14 на 20 патронов"}),
        ("M14 30 RND Magazine", {"role": "recon", "level": 36, "price": 180, "unlock_cost": 50000, "name_ru": "Магазин M14 на 30 патронов"}),
        (".30 Cal SS Phosphate Muzzle Brake", {"role": "recon", "level": 37, "price": 950, "unlock_cost": 22500, "name_ru": "Дульный тормоз .30 Cal SS Phosphate (M14)"}),
        (".30 Cal Steel Black Flash Hider", {"role": "recon", "level": 42, "price": 730, "unlock_cost": 27500, "name_ru": "Пламегаситель .30 Cal Steel Black (M14)"}),
        # Changes
        ("BMR-308", {"role": "recon", "level": 20, "price": 6250, "name_ru": "Снайперская винтовка BMR-308"}),
        ("SV98", {"role": "recon", "level": 18, "name_ru": "Снайперская винтовка СВ-98"}),
        ("MK22", {"role": "recon", "level": 30, "name_ru": "Снайперская винтовка MK22"}),
        ("Ghillie Headwear & Body Suit", {"role": "recon", "level": 26, "name_ru": "Маскировочный халат Гилли"}),
        ("Ghillie Headwear & Body Suit (Dead)", {"role": "recon", "level": 41, "name_ru": "Маскировочный халат Гилли (Сухой)"}),
        ("Compound Bow", {"role": "recon", "level": 16, "price": 1350, "unlock_cost": 150000, "name_ru": "Блочный лук"}),
        ("Claymore", {"role": "recon", "level": 23, "unlock_cost": 90000, "name_ru": "Мина Клеймор"}),
        ("Range Finder", {"role": "recon", "price": 1450, "unlock_cost": 25000, "name_ru": "Лазерный дальномер"}),
        ("SVD 10 RND Magazine", {"role": "recon", "level": 17, "name_ru": "Магазин СВД на 10 патронов"}),
        ("Deadeye Flash Hider", {"role": "recon", "level": 6, "name_ru": "Пламегаситель Deadeye"}),
        ("DTK-1 Brake", {"role": "recon", "level": 9, "name_ru": "Дульный тормоз ДТК-1"}),
        ("6-10x Scope MOA", {"role": "recon", "level": 19, "name_ru": "Прицел 6-10x Scope MOA"}),
        ("SV98 Bipod", {"role": "recon", "unlock_cost": 17500, "name_ru": "Сошки СВ-98"}),
        ("SRVV Brake", {"role": "recon", "level": 22, "name_ru": "Дульный тормоз SRVV"}),
        ("BMR-308 Flash Hider", {"role": "recon", "level": 24, "unlock_cost": 50000, "name_ru": "Пламегаситель BMR-308"}),
        ("6-10x Scope MRAD", {"role": "recon", "level": 28, "name_ru": "Прицел 6-10x Scope MRAD"}),
        ("BMR-308 Suppressor", {"role": "recon", "unlock_cost": 25000, "name_ru": "Глушитель BMR-308"}),
        ("Constrictor Brake", {"role": "recon", "level": 34, "name_ru": "Дульный тормоз Constrictor"}),
        ("AMR 50 Cal Muzzle Brake", {"role": "recon", "level": 38, "name_ru": "Дульный тормоз крупнокалиберный .50"}),
        ("6x Marksman Scope + Reflex", {"role": "recon", "level": 43, "unlock_cost": 75000, "name_ru": "Прицел 6x Marksman + Коллиматор"}),
        ("Slicktap Brake", {"role": "recon", "level": 44, "name_ru": "Дульный тормоз Slicktap"}),
        ("Frontier 2.5x-10x Precision Scope", {"role": "recon", "level": 47, "name_ru": "Прицел Frontier 2.5x-10x"}),
        (".50 Cal Heavy Suppressor", {"role": "recon", "level": 48, "name_ru": "Тяжелый глушитель .50 Cal"}),
        ("Three Port Brake", {"role": "recon", "level": 49, "name_ru": "Дульный тормоз Three Port"}),
    ]

    # 5. Support Progression Track
    support_updates = [
        ("Evo-3 Ultra", {"role": "support", "level": 30, "price": 7250, "unlock_cost": 200000, "name_ru": "Ручной пулемет Evo-3 Ultra"}),
        ("Evolys 50 RND Box", {"role": "support", "level": 30, "price": 110, "unlock_cost": 0, "name_ru": "Короб на 50 патронов Evolys"}),
        ("Evolys Bipod", {"role": "support", "level": 33, "price": 1450, "unlock_cost": 75000, "name_ru": "Сошки Evolys (Evo-3 Ultra)"}),
        ("SL7i-BL Suppressor", {"role": "support", "level": 40, "price": 1400, "unlock_cost": 35000, "name_ru": "Глушитель SL7i-BL (Evo-3 Ultra)"}),
        # Moved into Support
        ("4x Combat Prism Scope with Reflex", {"role": "support", "level": 21, "tab": "attachments", "subcategory": "Sights", "name_ru": "Прицел 4x Combat Prism + Reflex"}),
        # Changes
        ("PKM", {"role": "support", "level": 15, "price": 3500, "unlock_cost": 100000, "name_ru": "Пулемет ПКМ"}),
        ("M249", {"role": "support", "level": 20, "unlock_cost": 150000, "name_ru": "Пулемет M249"}),
        ("MAAWS", {"role": "support", "level": 18, "price": 2400, "unlock_cost": 100000, "name_ru": "Гранатомет MAAWS"}),
        ("MGL-40", {"role": "support", "price": 7500, "unlock_cost": 250000, "name_ru": "Гранатомет MGL-40"}),
        ("Heavy Drill", {"role": "support", "unlock_cost": 50000, "name_ru": "Тяжелый бур"}),
        ("9K333 Verba", {"role": "support", "level": 14, "name_ru": "ПЗРК 9К333 Верба"}),
        ("Improved Cylinder Choke", {"role": "support", "level": 13, "name_ru": "Получок (Дробовик)"}),
        ("Full Choke", {"role": "support", "level": 16, "name_ru": "Полный чок (Дробовик)"}),
        ("PKM Bipod", {"role": "support", "level": 17, "unlock_cost": 25000, "name_ru": "Сошки ПКМ"}),
        ("Slotted Flash Hider", {"role": "support", "level": 19, "name_ru": "Щелевой пламегаситель"}),
        ("M249 Bipod", {"role": "support", "level": 24, "unlock_cost": 50000, "name_ru": "Сошки M249"}),
        ("M249 200 RND Box", {"role": "support", "level": 26, "name_ru": "Короб на 200 патронов M249"}),
        ("Tread Brake", {"role": "support", "level": 27, "name_ru": "Дульный тормоз Tread"}),
        ("12 Gauge Suppressor", {"role": "support", "level": 32, "name_ru": "Глушитель 12 калибра"}),
    ]

    # 6. Pilot Progression Track
    pilot_updates = [
        ("Little Bird AH-6M [Miniguns]", {"role": "pilot", "level": 8, "price": 12000, "unlock_cost": 125000, "name_ru": "Вертолет Little Bird AH-6M (Пулеметы)"}),
        ("Little Bird AH-6M", {"role": "pilot", "level": 8, "price": 12000, "unlock_cost": 125000, "name_ru": "Вертолет Little Bird AH-6M"}),
        ("Z20 Lakota [Miniguns]", {"role": "pilot", "price": 15000, "unlock_cost": 85000, "name_ru": "Вертолет Z20 Lakota (Пулеметы)"}),
        ("Havoc", {"role": "pilot", "price": 28000, "unlock_cost": 650000, "name_ru": "Ударный вертолет Havoc"}),
    ]

    # 7. Driver Progression Track
    driver_updates = [
        ("Wolf-110", {"role": "driver", "level": 8, "price": 2200, "unlock_cost": 50000, "name_ru": "Бронеавтомобиль Wolf-110"}),
        ("Wolf-110 [M2 Browning]", {"role": "driver", "level": 16, "price": 7800, "unlock_cost": 125000, "name_ru": "Бронеавтомобиль Wolf-110 (M2 Браунинг)"}),
        ("T-72B3", {"role": "driver", "level": 20, "price": 16000, "unlock_cost": 250000, "name_ru": "Основной танк T-72Б3"}),
        ("Flakpanzer Gepard", {"role": "driver", "level": 25, "price": 14000, "unlock_cost": 300000, "name_ru": "ЗСУ Flakpanzer Gepard"}),
        ("Tape Dyshat", {"role": "driver", "level": 33, "price": 100, "unlock_cost": 25000, "name": "Tape Dyshat' - Baryber", "name_ru": "Аудиокассета «Дышать» - Baryber"}),
        ("SPH-2", {"role": "driver", "level": 31, "price": 8000, "unlock_cost": 350000, "name_ru": "Самоходная артиллерия SPH-2"}),
        ("Dune Buggy", {"role": "driver", "level": 9, "price": 300, "name_ru": "Багги Dune Buggy"}),
        ("Humvee", {"role": "driver", "level": 14, "price": 600, "unlock_cost": 75000, "name_ru": "Бронеавтомобиль Humvee"}),
        ("Ural Defender", {"role": "driver", "level": 15, "price": 7200, "name_ru": "Грузовик Урал Defender"}),
        ("Large Crate", {"role": "driver", "level": 17, "name_ru": "Большой ящик снабжения"}),
        ("Ural Defender [M249]", {"role": "driver", "level": 22, "price": 9500, "unlock_cost": 100000, "name_ru": "Грузовик Урал Defender (M249)"}),
        ("Humvee [M249]", {"role": "driver", "level": 24, "price": 8500, "name_ru": "Бронеавтомобиль Humvee (M249)"}),
    ]

    all_groups = [
        ("Career", career_updates),
        ("Assault", assault_updates),
        ("Medic", medic_updates),
        ("Recon", recon_updates),
        ("Support", support_updates),
        ("Pilot", pilot_updates),
        ("Driver", driver_updates),
    ]

    for group_name, updates in all_groups:
        for search_name, fields in updates:
            item = find_item(search_name)
            if item:
                for k, v in fields.items():
                    item[k] = v
                updated_count += 1
            else:
                # Need to create new item
                slug = re.sub(r'[^a-z0-9]+', '-', search_name.lower()).strip('-')
                new_item = {
                    "unlock_id": f"Id.Item.{slug.replace('-', '_')}",
                    "slug": slug,
                    "name": fields.get("name", search_name),
                    "name_ru": fields.get("name_ru", search_name),
                    "role": fields.get("role", "career"),
                    "level": fields.get("level", 1),
                    "total_xp": None,
                    "tab": fields.get("tab", "weapons"),
                    "subcategory": fields.get("subcategory", "General"),
                    "category_ru": fields.get("category_ru", "Предмет"),
                    "price": fields.get("price", 1000),
                    "unlock_cost": fields.get("unlock_cost", 10000),
                    "icon": f"static/wardogs/items/{slug}.webp",
                    "image": "",
                    "guide": "",
                    "description": "",
                    "specs": {}
                }
                for k, v in fields.items():
                    new_item[k] = v
                unlocks.append(new_item)
                by_name[new_item["name"].strip().lower()] = new_item
                by_id[new_item["unlock_id"]] = new_item
                added_count += 1
                print(f"[{group_name}] Added new item: {new_item['name']} (LVL {new_item['level']}, {new_item['role']})")

    # Clean up AK74 from progression unlocks if it was starter, or keep as lvl 1 default
    ak74 = find_item("AK74")
    if ak74:
        ak74["level"] = 1
        ak74["unlock_cost"] = 0
        ak74["price"] = 800
        ak74["name_ru"] = "Штурмовая винтовка АК-74"

    # Price rebalances from patch notes:
    vendor_prices = {
        "t-21": 1200,
        "amp-9": 1375,
        "mp5": 1800,
        "super-45": 5600,
        "scout-rifle-td": 1200,
        "compound-bow": 1350,
        "ggx-18": 1000,
        "bmr-308": 6250,
        "mgl-40": 7500,
        "amr-50": 14800,
        "level-3-armor": 2500,
        "level-3-helmet": 2000,
        "level-4-armor": 5000,
        "level-4-helmet": 4500,
        "binoculars": 180,
        "monocular": 350,
        "range-finder": 1450,
        "infrared-range-finder": 3200,
        "basic-parachute": 500,
        "sport-parachute": 700,
        "remote-detonator": 650,
        "wrench": 180,
        "sks-bipod": 810,
        "kodiak-m249": 6750,
        "humvee-m249": 8500,
        "humvee-minigun": 10000,
        "ural-defender": 7200,
        "ural-defender-m249": 9500,
        "l2a6": 22000,
        "little-bird-ah-6m-miniguns": 12000,
        "little-bird-ah-6r-rockets": 18800,
        "z20-lakota-miniguns": 15000,
        "havoc": 28000,
        "ak74": 800,
        "deagle": 750,
        "galil": 2000,
        "rpg-7": 1800,
        "maaws": 2400,
        "pkm": 3500,
        "m249": 4500,
        "little-bird-ah-6m": 12000,
        "ural": 1000,
        "humvee": 600,
        "dune-buggy": 300,
        "wolf-110": 2200,
        "wolf-110-m2-browning": 7800,
        "t-72b3": 16000,
        "flakpanzer-gepard": 14000,
        "sph-2": 8000,
        "tcx-spr": 3200,
        "pp-19-bizon": 1000,
        "evo-3-ultra": 7250,
        "m14": 7400,
        "molotov-cocktail": 340,
    }

    for item in unlocks:
        slug = item.get("slug", "")
        for pkey, pval in vendor_prices.items():
            if pkey == slug or pkey == item.get("name", "").lower():
                item["price"] = pval

    # Sort all unlocks by role then level ascending
    role_order = {"career": 0, "assault": 1, "medic": 2, "recon": 3, "support": 4, "driver": 5, "pilot": 6}
    unlocks.sort(key=lambda x: (role_order.get(x.get("role", "career"), 99), x.get("level", 0), x.get("name", "")))

    db["unlocks"] = unlocks
    db["version"] = "2.0.0"
    db["total_unlocks"] = len(unlocks)

    save_db(db)
    print(f"Successfully updated progression database: {updated_count} updated, {added_count} added. Total unlocks now: {len(unlocks)}")

if __name__ == "__main__":
    main()
