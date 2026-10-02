import asyncio
import io
import json
import os
import sys
import aiohttp
import discord
from dotenv import load_dotenv

sys.stdout.reconfigure(encoding='utf-8')
load_dotenv(os.path.join(os.path.dirname(__file__), ".env"))

TOKEN = os.getenv("DISCORD_CORE_BOT_TOKEN")
GUILD_ID = int(os.getenv("DISCORD_GUILD_ID"))
API_URL = os.getenv("WARLINK_API_URL", "http://127.0.0.1:8081")
DASH_KEY = os.getenv("WARLINK_DASHBOARD_KEY")
FORUM_ID = int(os.getenv("DISCORD_FORUM_CHANNEL_ID", "0"))

intents = discord.Intents.default()
client = discord.Client(intents=intents)

CATEGORY_NAMES = {
    "windivert": "Сетевой фильтр (WinDivert)",
    "wintun": "Сетевой адаптер (Wintun)",
    "hysteria": "Шлюз Hysteria 2 / Сессия",
    "steam": "Steam / WARDOGS",
    "antivirus": "Антивирус / Защитник",
    "latency": "Высокий пинг / Потери",
    "high_ping": "Высокий пинг",
    "game_crash": "Сбой / Краш игры",
    "crash": "Сбой клиента",
    "other": "Общий вопрос",
}


@client.event
async def on_ready():
    print(f"Авторизован как {client.user}.")
    guild = client.get_guild(GUILD_ID)
    if not guild:
        print("Сервер не найден!")
        await client.close()
        return

    # 1. ПЕРЕИМЕНОВАНИЕ РОЛЕЙ БЕЗ EMOJI
    print("\n--- 1. РОЛИ БЕЗ EMOJI ---")
    clean_roles = {
        "СИСТЕМНЫЙ СТАТУС": "СИСТЕМНЫЙ СТАТУС",
        "Администратор": "Администратор",
        "Спонсор WarLink": "Спонсор WarLink",
        "Боец WarLink": "Боец WarLink",
    }
    for r in guild.roles:
        for key, target in clean_roles.items():
            if key in r.name:
                if r.name != target:
                    print(f"Роль: '{r.name}' -> '{target}'")
                    await r.edit(name=target)

    # 2. ПОИСК ПАТИ -> ФОРУМ
    print("\n--- 2. ПОИСК ПАТИ: ПРЕОБРАЗОВАНИЕ В ФОРУМ ---")
    comm_cat = None
    for c in guild.categories:
        if "СООБЩЕСТВО" in c.name:
            comm_cat = c
            break

    # Ищем существующий канал поиск-пати
    existing_lfg = None
    for ch in guild.channels:
        if "поиск-пати" in ch.name:
            existing_lfg = ch
            break

    if existing_lfg and not isinstance(existing_lfg, discord.ForumChannel):
        print(f"Удаляем текстовый канал '{existing_lfg.name}' для замены на форум...")
        await existing_lfg.delete()
        existing_lfg = None

    if not existing_lfg:
        print("Создаем форумный канал '🎯・поиск-пати'...")
        tags = [
            discord.ForumTag(name="Сквад (4)", emoji="🛡️"),
            discord.ForumTag(name="Дуо (2)", emoji="👥"),
            discord.ForumTag(name="Турнир / Праки", emoji="🏆"),
            discord.ForumTag(name="Новичок", emoji="🔰"),
        ]
        new_forum = await guild.create_forum(
            name="🎯・поиск-пати",
            category=comm_cat,
            topic="Поиск команды и напарников для WARDOGS. Создайте тему с описанием вашего отряда.",
            available_tags=tags
        )
        print(f"Форумный канал создан: #{new_forum.name} (ID: {new_forum.id})")
    else:
        print("Форум 'поиск-пати' уже существует.")

    # 3. СИНХРОНИЗАЦИЯ ВСЕХ СУЩЕСТВУЮЩИХ ТИКЕТОВ ИЗ БД
    print("\n--- 3. СИНХРОНИЗАЦИЯ ТИКЕТОВ ИЗ БАЗЫ ---")
    forum_channel = guild.get_channel(FORUM_ID)
    if not forum_channel or not isinstance(forum_channel, discord.ForumChannel):
        print(f"Канал форума {FORUM_ID} не найден!")
        await client.close()
        return

    # Загружаем существующие ветки в форуме
    existing_thread_ids = set()
    for th in forum_channel.threads:
        import re
        m = re.search(r"#TK-(\d+)", th.name)
        if m:
            existing_thread_ids.add(int(m.group(1)))

    async for th in forum_channel.archived_threads(limit=100):
        import re
        m = re.search(r"#TK-(\d+)", th.name)
        if m:
            existing_thread_ids.add(int(m.group(1)))

    print(f"Уже синхронизировано в Discord тредов: {existing_thread_ids}")

    # Запрашиваем все тикеты из API WarLink
    url = f"{API_URL}/api/v1/admin/tickets?status=all&limit=100"
    headers = {"X-Dashboard-Key": DASH_KEY}
    tickets = []
    async with aiohttp.ClientSession() as session:
        async with session.get(url, headers=headers) as resp:
            if resp.status == 200:
                data = await resp.json()
                tickets = data.get("tickets", [])

    print(f"Найдено тикетов в базе данных: {len(tickets)}")

    # Импортируем в порядке возрастания ID (от старых к новым)
    tickets.sort(key=lambda x: x["id"])

    for t in tickets:
        tid = t["id"]
        if tid in existing_thread_ids:
            print(f"Тикет #TK-{tid:04d} уже есть в Discord, пропускаем.")
            continue

        print(f"Синхронизируем тикет #TK-{tid:04d} ({t.get('category')})...")
        acc = t.get("account_number", "Не указан")
        dev = t.get("device_id", "none")
        ver = t.get("app_version", "v2.1.10")
        cat = t.get("category", "other")
        cat_title = CATEGORY_NAMES.get(cat, cat)
        comment = t.get("user_comment", "") or "Без комментария"
        status = t.get("status", "new")
        admin_reply = t.get("admin_reply", "")

        sys_info = t.get("system_info", {})
        if isinstance(sys_info, str):
            try:
                sys_info = json.loads(sys_info)
            except Exception:
                sys_info = {}
        os_str = sys_info.get("os", "Windows amd64")
        isp_str = sys_info.get("isp", "Direct")

        thread_name = f"[#TK-{tid:04d}] {cat_title} ({ver})"[:100]

        color = 0x2ECC71 if status == "resolved" else (0x95A5A6 if status == "closed" else 0xFF5E1F)
        embed = discord.Embed(
            title=f"Тикет поддержки #TK-{tid:04d}",
            description=f"**Категория**: `{cat_title}`\n**Версия клиента**: `{ver}`\n**Статус**: `{status.upper()}`",
            color=color
        )
        embed.add_field(name="Аккаунт игрока", value=f"`{acc}`", inline=True)
        embed.add_field(name="Устройство", value=f"`{dev[:16]}...`" if len(dev) > 16 else f"`{dev}`", inline=True)
        embed.add_field(name="Сеть / Провайдер", value=f"`{isp_str}`", inline=True)
        embed.add_field(name="Система", value=f"**ОС**: {os_str}", inline=False)
        if len(comment) > 900:
            comment = comment[:890] + "..."
        embed.add_field(name="Описание проблемы игроком", value=f"```\n{comment}\n```", inline=False)

        if admin_reply:
            if len(admin_reply) > 900:
                admin_reply = admin_reply[:890] + "..."
            embed.add_field(name="Ответ администратора", value=f"```\n{admin_reply}\n```", inline=False)

        embed.set_footer(text=f"Создан: {t.get('created_at', '')}")

        # Скачиваем архив логов если есть
        archive_url = f"{API_URL}/api/v1/admin/tickets/{tid}/archive"
        file_att = None
        async with aiohttp.ClientSession() as session:
            async with session.get(archive_url, headers=headers) as a_resp:
                if a_resp.status == 200:
                    b = await a_resp.read()
                    if b:
                        file_att = discord.File(io.BytesIO(b), filename=f"ticket_{tid:04d}_logs.tar.gz")

        files = [file_att] if file_att else []

        from bot_core import TicketControlView
        view = TicketControlView(tid)

        th = await forum_channel.create_thread(
            name=thread_name,
            embed=embed,
            files=files,
            view=view
        )

        if status in ("resolved", "closed"):
            await th.thread.edit(locked=True, archived=True)
            print(f"Тикет #TK-{tid:04d} опубликован и архивирован (статус: {status}).")
        else:
            print(f"Тикет #TK-{tid:04d} опубликован как активный!")

        await asyncio.sleep(1)  # rate limit safety

    print("\nВсе тикеты успешно синхронизированы в Discord!")
    await client.close()


if __name__ == "__main__":
    client.run(TOKEN)
