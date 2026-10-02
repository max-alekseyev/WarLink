import asyncio
import os
import sys
import discord
from dotenv import load_dotenv

sys.stdout.reconfigure(encoding='utf-8')
load_dotenv()

TOKEN = os.getenv("DISCORD_CORE_BOT_TOKEN")
GUILD_ID = int(os.getenv("DISCORD_GUILD_ID", "1555211397900673086"))

intents = discord.Intents.default()
client = discord.Client(intents=intents)

ROLE_NAMES = {
    "СИСТЕМНЫЙ СТАТУС": "🟢 • СИСТЕМНЫЙ СТАТУС",
    "Администратор": "🛡️ • Администратор",
    "Спонсор WarLink": "⭐ • Спонсор WarLink",
    "Боец WarLink": "⚔️ • Боец WarLink",
}

CATEGORY_NAMES = {
    "ИНФОРМАЦИЯ & СТАТУС": "📌 • ИНФОРМАЦИЯ & СТАТУС",
    "СООБЩЕСТВО WARDOGS": "💬 • СООБЩЕСТВО WARDOGS",
    "ГОЛОСОВЫЕ КАНАЛЫ": "🔊 • ГОЛОСОВЫЕ КАНАЛЫ",
    "STAFF DESK": "🛡️ • STAFF DESK",
}

CHANNEL_NAMES = {
    # Текстовые каналы (используем bullet •)
    "правила": "📜・правила",
    "объявления": "📢・объявления",
    "мониторинг": "📊・мониторинг",
    "привязка-warlink": "🔗・привязка-warlink",
    "гайды-и-база-знаний": "💡・гайды-и-база-знаний",
    "основной-чат": "💬・основной-чат",
    "поиск-пати": "🎯・поиск-пати",
    "медиа-и-клипы": "🎬・медиа-и-клипы",
    "предложения": "💡・предложения",
    "тикеты-входящие": "🎫・тикеты-входящие",
    "алерты-сервера": "⚠️・алерты-сервера",
    # Голосовые каналы
    "Связь: Squad 1": "🔊 • Связь Squad 1",
    "Связь: Squad 2": "🔊 • Связь Squad 2",
    "Связь: Duo 1": "🔊 • Связь Duo 1",
    "Комната отдыха": "☕ • Комната отдыха",
}


@client.event
async def on_ready():
    print(f"Авторизован как {client.user}. Применяем формат 'emoji • название'...")
    guild = client.get_guild(GUILD_ID)
    if not guild:
        print(f"Сервер {GUILD_ID} не найден!")
        await client.close()
        return

    # 1. Форматирование ролей
    print("\n--- ФОРМАТИРОВАНИЕ РОЛЕЙ ---")
    for r in guild.roles:
        for base, new_name in ROLE_NAMES.items():
            if base in r.name:
                if r.name != new_name:
                    print(f"Роль: '{r.name}' -> '{new_name}'")
                    await r.edit(name=new_name)

    # 2. Форматирование категорий
    print("\n--- ФОРМАТИРОВАНИЕ КАТЕГОРИЙ ---")
    for cat in guild.categories:
        for base, new_name in CATEGORY_NAMES.items():
            if base in cat.name:
                if cat.name != new_name:
                    print(f"Категория: '{cat.name}' -> '{new_name}'")
                    await cat.edit(name=new_name)

    # 3. Форматирование каналов
    print("\n--- ФОРМАТИРОВАНИЕ КАНАЛОВ ---")
    for ch in guild.channels:
        if isinstance(ch, discord.CategoryChannel):
            continue
        for base, new_name in CHANNEL_NAMES.items():
            if base == ch.name or base in ch.name:
                if ch.name != new_name:
                    print(f"Канал: '{ch.name}' -> '{new_name}'")
                    try:
                        await ch.edit(name=new_name)
                    except Exception as e:
                        print(f"Ошибка переименования {ch.name}: {e}")

    # 4. Обновление карточки в #привязка-warlink
    link_chan_id = int(os.getenv("DISCORD_LINK_CHANNEL_ID", "0"))
    if link_chan_id:
        link_chan = guild.get_channel(link_chan_id)
        if link_chan:
            print("\n--- ОБНОВЛЕНИЕ КАРТОЧКИ В КАНАЛЕ ПРИВЯЗКИ ---")
            async for msg in link_chan.history(limit=5):
                if msg.author == client.user:
                    await msg.delete()

            class AestheticLinkView(discord.ui.View):
                def __init__(self):
                    super().__init__(timeout=None)

                @discord.ui.button(label="Привязать аккаунт WarLink", style=discord.ButtonStyle.primary, emoji="🔗", custom_id="btn_open_link_modal")
                async def open_modal(self, interaction: discord.Interaction, button: discord.ui.Button):
                    pass

            embed = discord.Embed(
                title="🔗 • СВЯЗКА ПРОФИЛЯ WARLINK",
                description=(
                    "Привязка игрового профиля WarLink для авторизации, получения статуса верифицированного игрока "
                    "и автоматического присвоения спонсорских ролей.\n\n"
                    "**Порядок действий:**\n"
                    "1. Запустите приложение **WarLink** на ПК.\n"
                    "2. Откройте **«Настройки аккаунта»** -> **«Интеграция с Discord»**.\n"
                    "3. Нажмите кнопку **«Код для Discord»** *(одноразовый 6-значный ключ)*.\n"
                    "4. Нажмите кнопку **«Привязать аккаунт WarLink»** ниже и введите код."
                ),
                color=0xFF5E1F
            )
            embed.set_footer(text="WarLink • Hysteria 2 Stockholm Gateway")
            await link_chan.send(embed=embed, view=AestheticLinkView())
            print("Карточка успешно обновлена в формате с маркерами!")

    print("\nФорматирование 'emoji • название' успешно применено ко всем ролям, категориям и каналам!")
    await client.close()


if __name__ == "__main__":
    client.run(TOKEN)
