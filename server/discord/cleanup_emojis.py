import asyncio
import os
import re
import sys
import discord
from dotenv import load_dotenv

sys.stdout.reconfigure(encoding='utf-8')
load_dotenv()

TOKEN = os.getenv("DISCORD_CORE_BOT_TOKEN")
GUILD_ID = int(os.getenv("DISCORD_GUILD_ID", "1555211397900673086"))

intents = discord.Intents.default()
client = discord.Client(intents=intents)

# Regex to strip emojis
EMOJI_PATTERN = re.compile(
    r"[\U00010000-\U0010ffff"
    r"\U00002600-\U000027BF"
    r"\U00002300-\U000023FF"
    r"\U00002B50-\U00002B55"
    r"\U0000FE00-\U0000FE0F"
    r"\U0001F600-\U0001F64F"
    r"\U0001F300-\U0001F5FF"
    r"\U0001F680-\U0001F6FF"
    r"\U0001F7E0-\U0001F7EB"
    r"\U0001F900-\U0001F9FF"
    r"\U0001FA00-\U0001FA6F"
    r"\U0001FA70-\U0001FAFF"
    r"]+",
    flags=re.UNICODE
)

def clean_text(s: str) -> str:
    cleaned = EMOJI_PATTERN.sub("", s)
    return " ".join(cleaned.split()).strip()


@client.event
async def on_ready():
    print(f"Авторизован как {client.user}. Начинаем очистку от emoji...")
    guild = client.get_guild(GUILD_ID)
    if not guild:
        print(f"Сервер {GUILD_ID} не найден!")
        await client.close()
        return

    # 1. Очистка названий ролей
    print("\n--- ОЧИСТКА РОЛЕЙ ---")
    for r in guild.roles:
        if r.is_default() or r.managed:
            continue
        cleaned = clean_text(r.name)
        if cleaned and cleaned != r.name:
            print(f"Переименование роли: '{r.name}' -> '{cleaned}'")
            await r.edit(name=cleaned)

    # 2. Очистка категорий
    print("\n--- ОЧИСТКА КАТЕГОРИЙ ---")
    for c in guild.categories:
        cleaned = clean_text(c.name)
        if cleaned and cleaned != c.name:
            print(f"Переименование категории: '{c.name}' -> '{cleaned}'")
            await c.edit(name=cleaned)

    # 3. Очистка каналов
    print("\n--- ОЧИСТКА КАНАЛОВ ---")
    for ch in guild.channels:
        if isinstance(ch, discord.CategoryChannel):
            continue
        cleaned = clean_text(ch.name)
        if cleaned and cleaned != ch.name:
            print(f"Переименование канала: '{ch.name}' -> '{cleaned}'")
            await ch.edit(name=cleaned)

    # 4. Обновление панели в #привязка-warlink
    link_chan_id = int(os.getenv("DISCORD_LINK_CHANNEL_ID", "0"))
    if link_chan_id:
        link_chan = guild.get_channel(link_chan_id)
        if link_chan:
            print("\n--- ОБНОВЛЕНИЕ ПАНЕЛИ В #привязка-warlink (БЕЗ EMOJI) ---")
            async for msg in link_chan.history(limit=10):
                if msg.author == client.user:
                    await msg.delete()

            class CleanLinkView(discord.ui.View):
                def __init__(self):
                    super().__init__(timeout=None)

                @discord.ui.button(label="Привязать аккаунт WarLink", style=discord.ButtonStyle.primary, custom_id="btn_open_link_modal")
                async def open_modal(self, interaction: discord.Interaction, button: discord.ui.Button):
                    pass

            embed = discord.Embed(
                title="СВЯЗКА ПРОФИЛЯ // WARLINK DISCORD",
                description=(
                    "Привязка игрового профиля WarLink для авторизации, получения статуса верифицированного игрока "
                    "и автоматического присвоения спонсорских ролей.\n\n"
                    "ПОРЯДОК ДЕЙСТВИЙ:\n"
                    "1. Запустите приложение WarLink на ПК.\n"
                    "2. Откройте раздел «Настройки аккаунта» -> «Интеграция с Discord».\n"
                    "3. Нажмите кнопку «Код для Discord» (6-значный одноразовый ключ).\n"
                    "4. Нажмите кнопку «Привязать аккаунт WarLink» ниже и укажите код."
                ),
                color=0xFF5E1F
            )
            embed.set_footer(text="WarLink // Hysteria 2 Gateway Infrastructure")
            await link_chan.send(embed=embed, view=CleanLinkView())
            print("Новая строгая панель без emoji успешно опубликована!")

    print("\nВсе названия ролей, категорий, каналов и сообщения очищены от emoji.")
    await client.close()


if __name__ == "__main__":
    client.run(TOKEN)
