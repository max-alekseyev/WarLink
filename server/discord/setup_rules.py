import asyncio
import os
import discord
from dotenv import load_dotenv

load_dotenv(os.path.join(os.path.dirname(__file__), ".env"))
token = os.getenv("DISCORD_CORE_BOT_TOKEN")
guild_id = int(os.getenv("DISCORD_GUILD_ID"))

client = discord.Client(intents=discord.Intents.default())


class ClassSelectionView(discord.ui.View):
    def __init__(self):
        super().__init__(timeout=None)

    async def toggle_class_role(self, interaction: discord.Interaction, class_name: str):
        guild = interaction.guild
        role = discord.utils.get(guild.roles, name=class_name)
        if not role:
            await interaction.response.send_message(f"Роль {class_name} не найдена на сервере.", ephemeral=True)
            return

        if role in interaction.user.roles:
            await interaction.user.remove_roles(role, reason="Class self-unassign")
            await interaction.response.send_message(f"Роль **{class_name}** снята.", ephemeral=True)
        else:
            await interaction.user.add_roles(role, reason="Class self-assign")
            await interaction.response.send_message(f"Вам выдана роль **{class_name}**!", ephemeral=True)

    @discord.ui.button(label="Штурмовик", style=discord.ButtonStyle.danger, custom_id="btn_class_assault")
    async def btn_assault(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Штурмовик")

    @discord.ui.button(label="Медик", style=discord.ButtonStyle.success, custom_id="btn_class_medic")
    async def btn_medic(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Медик")

    @discord.ui.button(label="Поддержка", style=discord.ButtonStyle.primary, custom_id="btn_class_support")
    async def btn_support(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Поддержка")

    @discord.ui.button(label="Разведчик", style=discord.ButtonStyle.secondary, custom_id="btn_class_recon")
    async def btn_recon(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Разведчик")

    @discord.ui.button(label="Пилот", style=discord.ButtonStyle.primary, custom_id="btn_class_pilot")
    async def btn_pilot(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Пилот")

    @discord.ui.button(label="Водитель", style=discord.ButtonStyle.secondary, custom_id="btn_class_driver")
    async def btn_driver(self, interaction: discord.Interaction, button: discord.ui.Button):
        await self.toggle_class_role(interaction, "Водитель")


@client.event
async def on_ready():
    guild = client.get_guild(guild_id)
    print(f"Logged in to {guild.name}")

    # 1. Post in #📜・правила
    rules_chan = discord.utils.get(guild.text_channels, name="📜・правила")
    if rules_chan:
        # Clear old messages
        async for m in rules_chan.history(limit=10):
            if m.author == client.user:
                await m.delete()

        embed_rules = discord.Embed(
            title="ПРАВИЛА И РЕГЛАМЕНТ СООБЩЕСТВА WARLINK",
            description=(
                "Добро пожаловать в сообщество игроков WarLink и бойцов WARDOGS.\n"
                "Сервер создан для комфортной совместной игры, поиска тиммейтов и технической взаимопомощи.\n\n"
                "**1. УВАЖЕНИЕ И СУБОРДИНАЦИЯ**\n"
                "• Запрещены прямые оскорбления, токсичное поведение, травля и разжигание конфликтов.\n"
                "• В голосовых комнатах уважайте эфир: настройте микрофон и не перебивайте тиммейтов во время боя.\n\n"
                "**2. ЧЕСТНАЯ ИГРА**\n"
                "• Категорически запрещено распространение и использование читов, эксплойтов и вредоносного ПО.\n"
                "• Обсуждение сетевых уязвимостей игры допускается только в контексте баг-репортов разработчикам.\n\n"
                "**3. СПАМ И РЕКЛАМА**\n"
                "• Запрещена несогласованная реклама сторонних дискорд-серверов, услуг и реферальных ссылок.\n"
                "• Медиа-контент и клипы публикуются исключительно в канале `🎬・медиа-и-клипы`.\n\n"
                "**4. ПОИСК ПАТИ И ПРЕДЛОЖЕНИЯ**\n"
                "• Создавайте карточки поиска сквада в `🎯・поиск-пати` с указанием нужного класса.\n"
                "• Идеи по улучшению приложения и шлюза оформляйте в форуме `💡・предложения`.\n\n"
                "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            ),
            color=0xFF5E1F
        )
        embed_rules.set_footer(text="WarLink Community Rules // Соблюдение обязательно")
        await rules_chan.send(embed=embed_rules)

        embed_roles = discord.Embed(
            title="ВЫБОР КЛАССА WARDOGS",
            description=(
                "Нажмите кнопку ниже, чтобы получить или снять роль вашей игровой специализации.\n"
                "Роли подсвечивают ваш профиль и помогают союзникам находить нужных бойцов в сквад!\n\n"
                "• **Штурмовик** — основная ударная сила, прорыв и штурм точек\n"
                "• **Медик** — восстановление отряда, аптечки и дефибрилляция\n"
                "• **Поддержка** — снабжение патронами, тяжелое подавление\n"
                "• **Разведчик** — дальняя разведка, споттинг и снайперский огонь\n"
                "• **Пилот** — управление боевой и транспортной авиацией\n"
                "• **Водитель** — управление бронетехникой и механизированными колоннами"
            ),
            color=0x3498DB
        )
        embed_roles.set_footer(text="Повторное нажатие на кнопку снимает роль")
        await rules_chan.send(embed=embed_roles, view=ClassSelectionView())
        print("Updated #📜・правила successfully.")

    # 2. Post in #💡・гайды-и-база-знаний
    guide_chan = discord.utils.get(guild.text_channels, name="💡・гайды-и-база-знаний")
    if guide_chan:
        async for m in guide_chan.history(limit=10):
            if m.author == client.user:
                await m.delete()

        embed_guide = discord.Embed(
            title="БАЗА ЗНАНИЙ // БЫСТРЫЙ СТАРТ WARLINK",
            description=(
                "**КАК РАБОТАЕТ WARLINK:**\n"
                "WarLink — это специализированный сетевой ускоритель для игроков WARDOGS. "
                "Он использует локальный сетевой фильтр WinDivert и выделенный европейский шлюз Hysteria 2 (QUIC/UDP) "
                "в дата-центре Стокгольма для обхода провайдерской фильтрации и стабилизации пинга до 35–65 мс.\n\n"
                "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
                "**ПОРЯДОК ЗАПУСКА:**\n"
                "1. Запустите `WarLink.exe` от имени администратора.\n"
                "2. Если требуется голосовая связь в Discord или браузер — включите режим «Свободный интернет».\n"
                "3. Нажмите кнопку «Подключить» и дождитесь статуса «ПОДКЛЮЧЕНО».\n"
                "4. Запускайте WARDOGS в Steam и наслаждайтесь стабильным соединением.\n\n"
                "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
                "**РЕШЕНИЕ ЧАСТЫХ ПРОБЛЕМ:**\n"
                "• **Пинг 400 мс в игре:** Проверьте, не запущена ли в фоне служба Cloudflare WARP. "
                "Остановите службу `warp-svc` в диспетчере служб Windows и перезагрузите ПК.\n"
                "• **Блокировка антивирусом:** Сетевой драйвер WinDivert является легитимным системным фильтром. "
                "Добавьте папку с WarLink в исключения Защитника Windows.\n"
                "• **Не открывается интерфейс:** Убедитесь, что в Windows установлен компонент Microsoft Edge WebView2 Runtime.\n\n"
                "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
                "**ТЕХНИЧЕСКАЯ ПОДДЕРЖКА:**\n"
                "Если соединение прерывается, откройте раздел «Поддержка» в приложении и отправьте диагностику. "
                "Ваш запрос автоматически поступит инженерам в канал `🎫・тикеты-входящие`."
            ),
            color=0xFF5E1F
        )
        embed_guide.set_footer(text="WarLink Knowledge Base // Актуальная версия v2.1.10")
        await guide_chan.send(embed=embed_guide)
        print("Updated #💡・гайды-и-база-знаний successfully.")

    await client.close()

if __name__ == "__main__":
    client.run(token)
