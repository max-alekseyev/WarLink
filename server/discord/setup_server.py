import asyncio
import os
import sys
import discord
from dotenv import load_dotenv

# Ensure UTF-8 output
sys.stdout.reconfigure(encoding='utf-8')

load_dotenv()

TOKEN = os.getenv("DISCORD_CORE_BOT_TOKEN")
GUILD_ID = int(os.getenv("DISCORD_GUILD_ID", "1555211397900673086"))

intents = discord.Intents.default()

client = discord.Client(intents=intents)


async def get_or_create_role(guild, name, **kwargs):
    for r in guild.roles:
        if r.name == name:
            print(f"[ROLE] Найдена существующая роль: {name} (ID: {r.id})")
            return r
    role = await guild.create_role(name=name, **kwargs)
    print(f"[ROLE] Создана новая роль: {name} (ID: {role.id})")
    return role


async def get_or_create_category(guild, name, overwrites=None):
    for c in guild.categories:
        if c.name == name:
            print(f"[CAT] Найдена существующая категория: {name} (ID: {c.id})")
            return c
    cat = await guild.create_category(name=name, overwrites=overwrites or {})
    print(f"[CAT] Создана категория: {name} (ID: {cat.id})")
    return cat


async def get_or_create_text_channel(guild, category, name, topic="", overwrites=None):
    for c in category.text_channels:
        if c.name == name:
            print(f"[CHAN] Найден существующий текстовый канал: #{name} (ID: {c.id})")
            return c
    chan = await guild.create_text_channel(name=name, category=category, topic=topic, overwrites=overwrites or {})
    print(f"[CHAN] Создан текстовый канал: #{name} (ID: {chan.id})")
    return chan


async def get_or_create_voice_channel(guild, category, name, user_limit=0):
    for c in category.voice_channels:
        if c.name == name:
            print(f"[VOICE] Найден голосовой канал: {name} (ID: {c.id})")
            return c
    vc = await guild.create_voice_channel(name=name, category=category, user_limit=user_limit)
    print(f"[VOICE] Создан голосовой канал: {name} (ID: {vc.id})")
    return vc


async def get_or_create_forum_channel(guild, category, name, topic="", overwrites=None):
    for c in category.forums:
        if c.name == name:
            print(f"[FORUM] Найден форумный канал: #{name} (ID: {c.id})")
            return c
    forum = await guild.create_forum(name=name, category=category, topic=topic, overwrites=overwrites or {})
    print(f"[FORUM] Создан форумный канал: #{name} (ID: {forum.id})")
    return forum


@client.event
async def on_ready():
    print(f"Авторизован как {client.user} (ID: {client.user.id})")
    guild = client.get_guild(GUILD_ID)
    if not guild:
        print(f"Ошибка: Сервер с ID {GUILD_ID} не найден!")
        await client.close()
        return

    print(f"\n--- НАСТРОЙКА СЕРВЕРА: {guild.name} ---")

    everyone = guild.default_role

    # 1. СОЗДАНИЕ РОЛЕЙ
    status_role = await get_or_create_role(guild, "СИСТЕМНЫЙ СТАТУС", color=discord.Color.from_rgb(46, 204, 113), hoist=True)
    admin_role = await get_or_create_role(guild, "Администратор", color=discord.Color.from_rgb(231, 76, 60), hoist=True)
    sponsor_role = await get_or_create_role(guild, "Спонсор WarLink", color=discord.Color.from_rgb(255, 94, 31), hoist=True)
    verified_role = await get_or_create_role(guild, "Боец WarLink", color=discord.Color.from_rgb(52, 152, 219), hoist=True)

    # Выдать роль статуса Stockholm боту, если он на сервере
    stockholm_id = 1555223892832944188
    try:
        stockholm_member = await guild.fetch_member(stockholm_id)
        if stockholm_member and status_role not in stockholm_member.roles:
            await stockholm_member.add_roles(status_role, reason="WarLink Status Bot")
            print(f"[ROLE] Назначена роль статуса боту WarLink Stockholm!")
    except Exception:
        print(f"[INFO] Бот WarLink Stockholm пока не найден на сервере (или еще не приглашен).")

    # 2. КАТЕГОРИЯ: ИНФОРМАЦИЯ & СТАТУС
    ro_overwrites = {
        everyone: discord.PermissionOverwrite(read_messages=True, send_messages=False, add_reactions=True),
        admin_role: discord.PermissionOverwrite(read_messages=True, send_messages=True),
        client.user: discord.PermissionOverwrite(read_messages=True, send_messages=True, embed_links=True)
    }
    cat_info = await get_or_create_category(guild, "ИНФОРМАЦИЯ & СТАТУС")
    c_rules = await get_or_create_text_channel(guild, cat_info, "правила", "Правила сервера WarLink", ro_overwrites)
    c_news = await get_or_create_text_channel(guild, cat_info, "объявления", "Официальные релизы и новости", ro_overwrites)
    c_monitor = await get_or_create_text_channel(guild, cat_info, "мониторинг", "Статус шлюза и пинг в реальном времени", ro_overwrites)
    c_link = await get_or_create_text_channel(guild, cat_info, "привязка-warlink", "Связка клиента WarLink и Discord", ro_overwrites)
    c_guides = await get_or_create_text_channel(guild, cat_info, "гайды-и-база-знаний", "Инструкции по настройке сети и WARDOGS", ro_overwrites)

    # 3. КАТЕГОРИЯ: СООБЩЕСТВО
    cat_comm = await get_or_create_category(guild, "СООБЩЕСТВО WARDOGS")
    c_chat = await get_or_create_text_channel(guild, cat_comm, "основной-чат", "Общение бойцов WarLink")
    c_lfg = await get_or_create_text_channel(guild, cat_comm, "поиск-пати", "Поиск напарников в отряд WARDOGS")
    c_media = await get_or_create_text_channel(guild, cat_comm, "медиа-и-клипы", "Скриншоты и хайлайты")
    c_feedback = await get_or_create_text_channel(guild, cat_comm, "предложения", "Идеи и обратная связь по WarLink")

    # 4. КАТЕГОРИЯ: ГОЛОСОВЫЕ КАНАЛЫ
    cat_voice = await get_or_create_category(guild, "ГОЛОСОВЫЕ КАНАЛЫ")
    await get_or_create_voice_channel(guild, cat_voice, "Связь: Squad 1", user_limit=4)
    await get_or_create_voice_channel(guild, cat_voice, "Связь: Squad 2", user_limit=4)
    await get_or_create_voice_channel(guild, cat_voice, "Связь: Duo 1", user_limit=2)
    await get_or_create_voice_channel(guild, cat_voice, "Комната отдыха", user_limit=0)

    # 5. КАТЕГОРИЯ: STAFF DESK (ТОЛЬКО ДЛЯ АДМИНА И БОТА)
    staff_overwrites = {
        everyone: discord.PermissionOverwrite(read_messages=False),
        admin_role: discord.PermissionOverwrite(read_messages=True, send_messages=True, manage_messages=True, manage_threads=True),
        client.user: discord.PermissionOverwrite(read_messages=True, send_messages=True, manage_threads=True, attach_files=True, embed_links=True)
    }
    cat_staff = await get_or_create_category(guild, "STAFF DESK", staff_overwrites)
    c_forum = await get_or_create_forum_channel(guild, cat_staff, "тикеты-входящие", "Очередь тикетов игроков WarLink", staff_overwrites)
    c_alerts = await get_or_create_text_channel(guild, cat_staff, "алерты-сервера", "Системные алерты", staff_overwrites)

    # 6. АВТОМАТИЧЕСКОЕ ОБНОВЛЕНИЕ .ENV
    print("\n--- ЗАПИСЬ ID В .ENV ---")
    env_path = os.path.join(os.path.dirname(__file__), ".env")
    with open(env_path, "r", encoding="utf-8") as f:
        env_content = f.read()

    def replace_var(key, val):
        nonlocal env_content
        import re
        pattern = rf'^{key}=.*$'
        repl = f'{key}="{val}"'
        if re.search(pattern, env_content, flags=re.MULTILINE):
            env_content = re.sub(pattern, repl, env_content, flags=re.MULTILINE)
        else:
            env_content += f'\n{repl}'

    replace_var("DISCORD_FORUM_CHANNEL_ID", c_forum.id)
    replace_var("DISCORD_MONITOR_CHANNEL_ID", c_monitor.id)
    replace_var("DISCORD_LINK_CHANNEL_ID", c_link.id)
    replace_var("DISCORD_ADMIN_ROLE_ID", admin_role.id)
    replace_var("DISCORD_SPONSOR_ROLE_ID", sponsor_role.id)
    replace_var("DISCORD_VERIFIED_ROLE_ID", verified_role.id)

    with open(env_path, "w", encoding="utf-8") as f:
        f.write(env_content)

    print(f"Файл {env_path} успешно синхронизирован с созданными каналами и ролями!")

    # 7. РАЗВЕРТЫВАНИЕ СТАРТОВЫХ ИНТЕРАКТИВНЫХ ПАНЕЛЕЙ
    print("\n--- РАЗВЕРТЫВАНИЕ ПАНЕЛИ В #привязка-warlink ---")
    from bot_core import LinkButtonView
    async for prev in c_link.history(limit=5):
        if prev.author == client.user:
            await prev.delete()

    embed = discord.Embed(
        title="СВЯЗКА ПРОФИЛЯ WARLINK // DISCORD",
        description=(
            "Привяжите ваш игровой профиль WarLink, чтобы получить статус верифицированного игрока, "
            "автоматические роли за спонсорство и доступ к закрытым функциям.\n\n"
            "**Инструкция:**\n"
            "1. Запустите приложение **WarLink** на ПК.\n"
            "2. Откройте вкладку **«Настройки аккаунта»** и нажмите **«Код для Discord»**.\n"
            "3. Нажмите кнопку **«Привязать аккаунт WarLink»** ниже и введите 6 цифр."
        ),
        color=0xFF5E1F
    )
    embed.set_footer(text="WarLink • Выделенный игровой шлюз Hysteria 2")
    await c_link.send(embed=embed, view=LinkButtonView())
    print("Панель привязки аккаунта успешно опубликована!")

    print("\nВСЯ ИНФРАСТРУКТУРА DISCORD-СЕРВЕРА ПОЛНОСТЬЮ СОЗДАНА И НАСТРОЕНА!")
    await client.close()


if __name__ == "__main__":
    client.run(TOKEN)
