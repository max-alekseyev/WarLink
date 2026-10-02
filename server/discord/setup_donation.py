import asyncio
import os
import discord
from dotenv import load_dotenv

load_dotenv(os.path.join(os.path.dirname(__file__), ".env"))
token = os.getenv("DISCORD_CORE_BOT_TOKEN")
guild_id = int(os.getenv("DISCORD_GUILD_ID"))

client = discord.Client(intents=discord.Intents.default())


@client.event
async def on_ready():
    guild = client.get_guild(guild_id)
    cat_name = "💳 • ПОДДЕРЖКА ПРОЕКТА"
    chan_name = "💖・поддержать-warlink"

    # 1. Create or get category at position 0
    cat = discord.utils.get(guild.categories, name=cat_name)
    if not cat:
        cat = await guild.create_category(name=cat_name, position=0, reason="Top donation category")
        print(f"Created category {cat_name}")
    else:
        await cat.edit(position=0)
        print(f"Category {cat_name} already exists, moved to pos 0")

    # 2. Create channel inside category
    chan = discord.utils.get(cat.text_channels, name=chan_name)
    if not chan:
        overwrites = {
            guild.default_role: discord.PermissionOverwrite(send_messages=False, read_messages=True, add_reactions=True),
            guild.me: discord.PermissionOverwrite(send_messages=True, embed_links=True)
        }
        chan = await cat.create_text_channel(
            name=chan_name,
            topic="Поддержка игрового шлюза WarLink и личная поддержка разработчика на Boosty.",
            overwrites=overwrites,
            reason="Donations and support channel"
        )
        print(f"Created text channel {chan_name}")

    # 3. Post / update donation embed
    embed = discord.Embed(
        title="ПОДДЕРЖКА WARLINK И РАЗРАБОТЧИКА",
        description=(
            "WarLink разрабатывается и поддерживается независимо. "
            "Ниже приведены способы поддержать серверную инфраструктуру проекта и автора лично.\n\n"
            "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
            "**1. СЕРВЕРНЫЙ ШЛЮЗ WARLINK (СТОКГОЛЬМ)**\n"
            "Сервис работает через собственный выделенный игровой шлюз в дата-центре Стокгольма.\n\n"
            "**Привилегии спонсора:**\n"
            "• Гарантированный выделенный слот на шлюзе при любых пиковых нагрузках пула\n"
            "• Почетная роль **Спонсор WarLink** в Discord и статус в приложении\n"
            "• Приоритетная маршрутизация и ранний доступ к новым игровым профилям\n\n"
            "**Как внести вклад на оплату сервера:**\n"
            "Откройте приложение WarLink на ПК, перейдите в раздел **«Спонсорство и донаты»** и пополните пул (от 98 RUB). "
            "Спонсорский слот и роль выдаются автоматически на 30 дней.\n\n"
            "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
            "**2. ЛИЧНАЯ ПОДДЕРЖКА РАЗРАБОТЧИКА**\n"
            "Если WarLink помог вам комфортно играть без сбоев и сетевых блокировок, вы можете поддержать автора лично:\n\n"
            "• **Boosty (донат или подписка):** [boosty.to/pld1n/donate](https://boosty.to/pld1n/donate)\n"
            "• **Карта Т-Банк:** `5536 9139 3500 4040`\n\n"
            "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
            "*Спасибо каждому за вклад в развитие и поддержку WarLink!*"
        ),
        color=0xFF5E1F
    )
    embed.set_footer(text="WarLink Infrastructure Support")

    # Clear previous messages if any
    async for m in chan.history(limit=5):
        if m.author == client.user:
            await m.delete()

    view = discord.ui.View()
    view.add_item(discord.ui.Button(label="Поддержать на Boosty", style=discord.ButtonStyle.link, url="https://boosty.to/pld1n/donate"))

    await chan.send(embed=embed, view=view)
    print("Posted support embed successfully.")
    await client.close()

if __name__ == "__main__":
    client.run(token)
