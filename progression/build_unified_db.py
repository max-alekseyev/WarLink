import json
import os

# Load complete items DB (531 items)
with open('progression/metaforge_complete_items_db.json', 'r', encoding='utf-8') as f:
    items_raw = json.load(f)

# Load progression unlocks (233 unlocks)
with open('progression/metaforge_full_progression.json', 'r', encoding='utf-8') as f:
    prog_raw = json.load(f)

all_items = items_raw.get('items', [])
items_by_id = {it.get('id'): it for it in all_items}
items_by_slug = {it.get('slug'): it for it in all_items if it.get('slug')}

unlocks = prog_raw.get('unlocks', [])
print(f'Total items: {len(all_items)}, Total unlocks: {len(unlocks)}')

# Check available icons in guides/icons/
available_icons = set()
if os.path.exists('guides/icons'):
    available_icons = set(os.listdir('guides/icons'))

# Check available images in guides/images/
available_images = set()
if os.path.exists('guides/images'):
    available_images = set(os.listdir('guides/images'))

enriched_unlocks = []
for u in unlocks:
    u_id = u.get('id')
    u_slug = u.get('slug')
    item_info = items_by_id.get(u_id) or items_by_slug.get(u_slug) or {}

    role = u.get('role')
    # Normalize role names
    if role == 'wardog':
        role = 'career'
    elif role == 'infantry':
        role = 'assault'

    icon_name = u.get('icon') or item_info.get('icon')
    icon_path = ""
    if icon_name:
        webp_name = f"{icon_name}.webp"
        if webp_name in available_icons:
            icon_path = f"guides/icons/{webp_name}"

    image_path = ""
    if u_slug:
        png_name = f"{u_slug}.png"
        if png_name in available_images:
            image_path = f"guides/images/{png_name}"

    guide_path = ""
    if u_slug and os.path.exists(f"guides/{u_slug}.md"):
        guide_path = f"guides/{u_slug}.md"

    # Unlock cost estimation based on level and category
    lvl = u.get('level', 1)
    tab = u.get('tab') or item_info.get('tab', 'other')
    unlock_cost = 1
    if tab == 'weapons':
        unlock_cost = 0 if lvl <= 1 else (1 if lvl <= 10 else (2 if lvl <= 25 else 3))
    elif tab in ['attachments', 'vehicles']:
        unlock_cost = 1 if lvl <= 15 else 2

    entry = {
        "unlock_id": u_id,
        "slug": u_slug,
        "name": u.get('name') or item_info.get('name'),
        "role": role,
        "level": lvl,
        "total_xp": u.get('totalXp', 0),
        "tab": tab,
        "subcategory": u.get('subcategory') or item_info.get('subcategory'),
        "price": u.get('cash') or item_info.get('price', 0),
        "unlock_cost": unlock_cost,
        "icon": icon_path,
        "image": image_path,
        "guide": guide_path,
        "description": item_info.get('description', ''),
        "specs": {
            "calibre": item_info.get('calibre'),
            "calibreLabel": item_info.get('calibreLabel'),
            "roundType": item_info.get('roundType'),
            "gridSize": item_info.get('gridSize'),
            "maxStack": item_info.get('maxStack'),
            "amount": item_info.get('amount')
        }
    }
    enriched_unlocks.append(entry)

# Sort by role and level
enriched_unlocks.sort(key=lambda x: (x['role'], x['level']))

final_db = {
    "version": "1.0.0",
    "source": "MetaForge.app/wardogs",
    "total_unlocks": len(enriched_unlocks),
    "total_catalog_items": len(all_items),
    "roles": ["career", "assault", "medic", "recon", "support", "driver", "pilot"],
    "unlocks": enriched_unlocks,
    "catalog": all_items
}

with open('progression/progression_db.json', 'w', encoding='utf-8') as out:
    json.dump(final_db, out, ensure_ascii=False, indent=2)

print(f'Successfully built progression/progression_db.json with {len(enriched_unlocks)} unlocks and {len(all_items)} catalog items!')
