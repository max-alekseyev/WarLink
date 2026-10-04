import asyncio
import io
import json
import logging
import re
from datetime import datetime
import aiohttp
import discord
from discord import app_commands
from discord.ext import commands, tasks
import redis.asyncio as aioredis

import config

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("WarLinkCoreBot")

intents = discord.Intents.default()
intents.message_content = True

bot = commands.Bot(command_prefix="!", intents=intents)

# State for live monitor message
monitor_message_id = None

CATEGORY_NAMES = {
    "windivert": "Сетевой фильтр (WinDivert)",
    "wintun": "Сетевой адаптер (Wintun)",
    "hysteria": "Шлюз Hysteria 2 / Сессия",
    "steam": "Steam / WARDOGS",
    "antivirus": "Антивирус / Защитник",
    "latency": "Высокий пинг / Потери",
    "crash": "Сбой / Краш клиента",
    "other": "Общий вопрос",
}


# ==============================================================================
# UI COMPONENTS: TICKET CONTROL & MODALS
# ==============================================================================

class TicketReplyModal(discord.ui.Modal, title="Ответ поддержки WarLink"):
    reply_input = discord.ui.TextInput(
        label="Сообщение игроку в приложение",
        style=discord.TextStyle.paragraph,
        placeholder="Введите ответ, который поступит в Центр уведомлений WarLink...",
        min_length=1,
        max_length=2000,
        required=True
    )

    def __init__(self, ticket_id: int):
        super().__init__()
        self.ticket_id = ticket_id

    async def on_submit(self, interaction: discord.Interaction):
        await interaction.response.defer(ephemeral=False)
        content = self.reply_input.value.strip()

        url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{self.ticket_id}/reply"
        headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY, "Content-Type": "application/json"}
        payload = {
            "title": f"Ответ поддержки по тикету #TK-{self.ticket_id:04d}",
            "message": content,
            "severity": "update",
            "action_label": "Открыть диалог",
            "action_url": "#view-support",
            "status": "in_progress"
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(url, headers=headers, json=payload) as resp:
                    if resp.status == 200:
                        await interaction.followup.send(
                            f"**Ответ администратора {interaction.user.mention}:**\n{content}\n\n*(Сообщение доставлено игроку в WarLink)*"
                        )
                        await update_discord_ticket_thread(self.ticket_id)
                    else:
                        await interaction.followup.send(f"[ОШИБКА] Статус HTTP {resp.status}", ephemeral=True)
        except Exception as e:
            logger.error(f"Error sending reply: {e}")
            await interaction.followup.send(f"[СЕТЕВАЯ ОШИБКА] {e}", ephemeral=True)


class TicketControlView(discord.ui.View):
    def __init__(self, ticket_id: int = 0):
        super().__init__(timeout=None)
        self.ticket_id = ticket_id

    def get_ticket_id(self, interaction: discord.Interaction) -> int:
        if self.ticket_id > 0:
            return self.ticket_id
        if interaction.channel and isinstance(interaction.channel, discord.Thread):
            match = re.search(r"#TK-(\d+)", interaction.channel.name)
            if match:
                return int(match.group(1))
        return 0

    @discord.ui.button(label="Ответить", style=discord.ButtonStyle.primary, custom_id="btn_reply_ticket")
    async def reply_button(self, interaction: discord.Interaction, button: discord.ui.Button):
        tid = self.get_ticket_id(interaction)
        if tid == 0:
            await interaction.response.send_message("[ОШИБКА] Не удалось определить ID тикета из ветки.", ephemeral=True)
            return
        await interaction.response.send_modal(TicketReplyModal(tid))

    @discord.ui.button(label="Решено / Закрыть", style=discord.ButtonStyle.success, custom_id="btn_resolve_ticket")
    async def resolve_button(self, interaction: discord.Interaction, button: discord.ui.Button):
        tid = self.get_ticket_id(interaction)
        if tid == 0:
            await interaction.response.send_message("[ОШИБКА] Не удалось определить ID тикета из ветки.", ephemeral=True)
            return

        await interaction.response.defer(ephemeral=False)

        url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{tid}/status"
        headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY, "Content-Type": "application/json"}
        payload = {"status": "resolved"}

        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(url, headers=headers, json=payload) as resp:
                    if resp.status == 200:
                        button.disabled = True
                        button.label = "[РЕШЕНО] Тикет закрыт"
                        await interaction.message.edit(view=self)
                        await interaction.followup.send(
                            f"Тикет #TK-{tid:04d} переведен в статус **Решено** администратором {interaction.user.mention}."
                        )
                        await update_discord_ticket_thread(tid)
                    else:
                        await interaction.followup.send(f"Ошибка смены статуса (HTTP {resp.status})", ephemeral=True)
        except Exception as e:
            logger.error(f"Error resolving ticket: {e}")
            await interaction.followup.send(f"Сетевая ошибка: {e}", ephemeral=True)


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


def get_level_color(level: int) -> discord.Color:
    if level >= 150:
        return discord.Color(0xFF5E1F) # WarLink Brand Orange - MAX CAP
    elif level >= 140:
        return discord.Color(0xE74C3C) # Crimson Ruby
    elif level >= 120:
        return discord.Color(0xC0392B) # Deep Crimson
    elif level >= 100:
        return discord.Color(0x9B59B6) # Amethyst / Master
    elif level >= 80:
        return discord.Color(0x8E44AD) # Violet / Specialist
    elif level >= 60:
        return discord.Color(0x1ABC9C) # Turquoise / Diamond
    elif level >= 40:
        return discord.Color(0x2ECC71) # Emerald / Veteran
    elif level >= 25:
        return discord.Color(0xF1C40F) # Gold / Senior
    elif level >= 15:
        return discord.Color(0xBDC3C7) # Silver / Trooper
    else:
        return discord.Color(0x7F8C8D) # Slate / Recruit


CLASS_ROLE_NAMES = {
    "assault": "Штурмовик",
    "medic": "Медик",
    "support": "Поддержка",
    "recon": "Разведчик",
    "pilot": "Пилот",
    "driver": "Водитель",
}


async def sync_member_progression(member: discord.Member, progression):
    if not member or not progression:
        return

    guild = member.guild
    if isinstance(progression, str):
        try:
            progression = json.loads(progression)
        except Exception:
            return

    if not isinstance(progression, dict):
        return

    career_lvl = progression.get("career_level", 0)
    roles_dict = progression.get("roles", {})

    roles_to_add = []
    roles_to_remove = []

    # 1. Career Level Role
    if career_lvl > 0:
        target_role_name = f"Уровень {career_lvl}"
        target_role = discord.utils.get(guild.roles, name=target_role_name)
        if not target_role:
            try:
                target_role = await guild.create_role(
                    name=target_role_name,
                    color=get_level_color(career_lvl),
                    mentionable=False,
                    reason="Dynamic WARDOGS Career Level"
                )
            except Exception as e:
                logger.error(f"Error creating level role {target_role_name}: {e}")

        if target_role and target_role not in member.roles:
            roles_to_add.append(target_role)

        # Remove previous level roles
        for r in member.roles:
            if re.match(r"^Уровень \d+$", r.name) and r.name != target_role_name:
                roles_to_remove.append(r)

    # 2. Class Roles
    if isinstance(roles_dict, dict):
        for class_key, class_name in CLASS_ROLE_NAMES.items():
            lvl = roles_dict.get(class_key, 0)
            c_role = discord.utils.get(guild.roles, name=class_name)
            if c_role:
                if lvl > 0 and c_role not in member.roles:
                    roles_to_add.append(c_role)

    try:
        if roles_to_remove:
            await member.remove_roles(*roles_to_remove, reason="WarLink Progression Sync")
        if roles_to_add:
            await member.add_roles(*roles_to_add, reason="WarLink Progression Sync")
        logger.info(f"Progression synced for {member.display_name}: Level {career_lvl}, added {len(roles_to_add)}, removed {len(roles_to_remove)}")
    except Exception as e:
        logger.error(f"Error updating progression roles for {member.display_name}: {e}")


class LinkCodeModal(discord.ui.Modal, title="Привязка профиля WarLink"):
    code_input = discord.ui.TextInput(
        label="Код привязки из WarLink",
        placeholder="6-значный код (например: 582910)",
        min_length=6,
        max_length=6,
        required=True
    )

    async def on_submit(self, interaction: discord.Interaction):
        await interaction.response.defer(ephemeral=True)
        code = self.code_input.value.strip()

        url = f"{config.WARLINK_API_URL}/api/v1/internal/discord/verify-link"
        headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY, "Content-Type": "application/json"}
        payload = {
            "code": code,
            "discord_id": str(interaction.user.id),
            "discord_tag": str(interaction.user),
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(url, headers=headers, json=payload) as resp:
                    data = await resp.json()
                    if resp.status == 200 and data.get("success"):
                        acc = data.get("account_number", "")
                        is_sponsor = data.get("is_sponsor", False)
                        progression = data.get("progression")

                        # Assign Verified Role
                        roles_to_add = []
                        if config.DISCORD_VERIFIED_ROLE_ID:
                            v_role = interaction.guild.get_role(config.DISCORD_VERIFIED_ROLE_ID)
                            if v_role and v_role not in interaction.user.roles:
                                roles_to_add.append(v_role)

                        # Assign Sponsor Role if eligible
                        if is_sponsor and config.DISCORD_SPONSOR_ROLE_ID:
                            s_role = interaction.guild.get_role(config.DISCORD_SPONSOR_ROLE_ID)
                            if s_role and s_role not in interaction.user.roles:
                                roles_to_add.append(s_role)

                        if roles_to_add:
                            await interaction.user.add_roles(*roles_to_add, reason="WarLink Account Linked")

                        # Sync progression & level
                        if progression:
                            await sync_member_progression(interaction.user, progression)

                        status_msg = f"Аккаунт **{acc}** успешно привязан к вашему Discord!"
                        if is_sponsor:
                            status_msg += "\nПрисвоена роль: **Спонсор WarLink**."
                        else:
                            status_msg += "\nПрисвоена роль: **Боец WarLink**."

                        if isinstance(progression, dict) and progression.get("career_level", 0) > 0:
                            status_msg += f"\nСинхронизирован ранг WARDOGS: **Уровень {progression.get('career_level')}**"

                        embed = discord.Embed(
                            title="СВЯЗКА ПРОФИЛЯ ЗАВЕРШЕНА",
                            description=status_msg,
                            color=0xFF5E1F
                        )
                        await interaction.followup.send(embed=embed, ephemeral=True)
                    else:
                        err = data.get("error", "invalid_code")
                        if err == "invalid_or_expired_code":
                            msg = "Неверный или истекший код. Нажмите кнопку в приложении WarLink повторно."
                        else:
                            msg = f"Ошибка проверки кода: {err}"
                        await interaction.followup.send(f"{msg}", ephemeral=True)
        except Exception as e:
            logger.error(f"Error verifying link code: {e}")
            await interaction.followup.send(f"Ошибка связи с сервером WarLink: {e}", ephemeral=True)


class LinkButtonView(discord.ui.View):
    def __init__(self):
        super().__init__(timeout=None)

    @discord.ui.button(label="Привязать аккаунт WarLink", style=discord.ButtonStyle.primary, custom_id="btn_open_link_modal")
    async def open_modal(self, interaction: discord.Interaction, button: discord.ui.Button):
        await interaction.response.send_modal(LinkCodeModal())


# ==============================================================================
# REDIS TICKET SUBSCRIBER & FORUM THREAD DISPATCHER
# ==============================================================================

async def fetch_ticket_details(ticket_id: int):
    url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{ticket_id}"
    headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY}
    async with aiohttp.ClientSession() as session:
        async with session.get(url, headers=headers) as resp:
            if resp.status == 200:
                return await resp.json()
    return None


async def fetch_ticket_archive(ticket_id: int):
    url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{ticket_id}/archive"
    headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY}
    async with aiohttp.ClientSession() as session:
        async with session.get(url, headers=headers) as resp:
            if resp.status == 200:
                return await resp.read()
    return None


async def redis_ticket_listener():
    await bot.wait_until_ready()
    logger.info("Starting Redis ticket listener on %s...", config.REDIS_URL)

    while not bot.is_closed():
        try:
            r = aioredis.from_url(config.REDIS_URL)
            pubsub = r.pubsub()
            await pubsub.subscribe("tickets:new", "tickets:updated", "tickets:message", "discord:progression_update")
            logger.info("Subscribed to Redis channels: 'tickets:new', 'tickets:updated', 'tickets:message', 'discord:progression_update'")

            async for message in pubsub.listen():
                if message and message["type"] == "message":
                    ch = message.get("channel")
                    if isinstance(ch, bytes):
                        ch = ch.decode("utf-8")
                    if ch == "tickets:new":
                        try:
                            raw_id = message["data"].decode("utf-8")
                            ticket_id = int(raw_id)
                            logger.info(f"Received new ticket event: #{ticket_id}")
                            await handle_new_ticket_event(ticket_id)
                        except Exception as parse_err:
                            logger.error(f"Error handling ticket message: {parse_err}")
                    elif ch == "tickets:updated":
                        try:
                            raw_id = message["data"].decode("utf-8")
                            ticket_id = int(raw_id)
                            logger.info(f"Received ticket update event: #{ticket_id}")
                            await update_discord_ticket_thread(ticket_id)
                        except Exception as update_err:
                            logger.error(f"Error handling ticket update event: {update_err}")
                    elif ch == "tickets:message":
                        try:
                            raw_data = message["data"].decode("utf-8")
                            m_info = json.loads(raw_data)
                            logger.info(f"Received ticket message event for ticket #{m_info.get('ticket_id')}")
                            await handle_ticket_message_event(m_info)
                        except Exception as m_err:
                            logger.error(f"Error handling ticket message event: {m_err}")
                    elif ch == "discord:progression_update":
                        try:
                            raw_data = message["data"].decode("utf-8")
                            p_info = json.loads(raw_data)
                            discord_id = p_info.get("discord_id")
                            prog = p_info.get("progression")
                            if isinstance(prog, str):
                                prog = json.loads(prog)
                            if discord_id and config.DISCORD_GUILD_ID:
                                guild = bot.get_guild(config.DISCORD_GUILD_ID)
                                if guild:
                                    member = guild.get_member(int(discord_id))
                                    if not member:
                                        try:
                                            member = await guild.fetch_member(int(discord_id))
                                        except Exception:
                                            member = None
                                    if member:
                                        await sync_member_progression(member, prog)
                        except Exception as p_err:
                            logger.error(f"Error handling progression update event: {p_err}")
        except Exception as e:
            logger.warning(f"Redis connection error: {e}. Retrying in 5 seconds...")
            await asyncio.sleep(5)


async def get_ticket_thread(ticket_id: int):
    forum_channel = bot.get_channel(config.DISCORD_FORUM_CHANNEL_ID)
    if not forum_channel or not isinstance(forum_channel, discord.ForumChannel):
        return None
    for th in forum_channel.threads:
        if f"#TK-{ticket_id:04d}" in th.name:
            return th
    try:
        async for th in forum_channel.archived_threads(limit=100):
            if f"#TK-{ticket_id:04d}" in th.name:
                return th
    except Exception:
        pass
    return None


async def handle_ticket_message_event(m_info: dict):
    ticket_id = m_info.get("ticket_id")
    if not ticket_id:
        return
    sender_type = m_info.get("sender_type", "user")
    if sender_type == "admin":
        return

    thread = await get_ticket_thread(int(ticket_id))
    if not thread:
        await handle_new_ticket_event(int(ticket_id))
        thread = await get_ticket_thread(int(ticket_id))
        if not thread:
            return

    att_type = m_info.get("attachment_type", "")
    if att_type == "logs_archive":
        size_kb = (m_info.get("attachment_size", 0) + 1023) // 1024
        embed = discord.Embed(
            title="[СВЕЖИЕ ЛОГИ] Обновленный диагностический архив",
            description=f"Пользователь прикрепил актуальные логи ({size_kb} КБ).\n[Скачать архив логов]({config.WARLINK_API_URL}/api/v1/admin/tickets/{ticket_id}/archive)",
            color=0x3498DB
        )
        await thread.send(embed=embed)
    elif att_type == "telemetry_ping":
        embed = discord.Embed(
            title="[ДИАГНОСТИКА СВЯЗИ] Тест сетевого шлюза",
            description=m_info.get("message", ""),
            color=0x2ECC71
        )
        await thread.send(embed=embed)
    elif sender_type == "system":
        embed = discord.Embed(
            description=f"**Система:** {m_info.get('message', '')}",
            color=0x95A5A6
        )
        await thread.send(embed=embed)
    else:
        # Regular user message
        msg_text = m_info.get("message", "")
        sender_name = m_info.get("sender_name") or f"Пользователь #{m_info.get('account_number', '')}"
        embed = discord.Embed(
            description=msg_text,
            color=0xFF5E1F
        )
        embed.set_author(name=f"{sender_name} (Клиент WarLink)")
        await thread.send(embed=embed)


async def update_discord_ticket_thread(ticket_id: int):
    forum_channel = bot.get_channel(config.DISCORD_FORUM_CHANNEL_ID)
    if not forum_channel or not isinstance(forum_channel, discord.ForumChannel):
        return

    data = await fetch_ticket_details(ticket_id)
    if not data or not data.get("success"):
        return

    t = data.get("ticket", {})
    status = t.get("status", "open")
    admin_reply = t.get("admin_reply", "")
    cat = t.get("category", "other")
    cat_title = CATEGORY_NAMES.get(cat, cat)
    ver = t.get("app_version", "v2.2.0")

    target_thread = await get_ticket_thread(ticket_id)
    if not target_thread:
        logger.warning(f"Thread for ticket #{ticket_id} not found in forum for update.")
        return

    # Update thread name based on status
    if status in ("resolved", "closed"):
        new_name = f"[РЕШЕНО] #TK-{ticket_id:04d} {cat_title}"[:100]
    elif admin_reply:
        new_name = f"[В РАБОТЕ] #TK-{ticket_id:04d} {cat_title}"[:100]
    else:
        new_name = f"[#TK-{ticket_id:04d}] {cat_title} ({ver})"[:100]

    try:
        if target_thread.name != new_name:
            await target_thread.edit(name=new_name)
    except Exception as name_err:
        logger.debug(f"Could not rename thread: {name_err}")

    # Fetch starter message and update embed
    try:
        starter_msg = await target_thread.fetch_message(target_thread.id)
        if starter_msg and starter_msg.embeds:
            emb = starter_msg.embeds[0]
            st_text = "РЕШЕНО" if status in ("resolved", "closed") else ("В ОБРАБОТКЕ" if admin_reply else "НОВЫЙ")
            emb.description = f"**Категория**: `{cat_title}`\n**Версия клиента**: `{ver}`\n**Статус**: `{st_text}`"
            emb.color = 0x2ECC71 if status in ("resolved", "closed") else (0x3498DB if admin_reply else 0xFF5E1F)

            if admin_reply:
                reply_val = admin_reply[:900] + "..." if len(admin_reply) > 900 else admin_reply
                field_idx = None
                for idx, f in enumerate(emb.fields):
                    if f.name == "Ответ поддержки":
                        field_idx = idx
                        break
                if field_idx is not None:
                    emb.set_field_at(field_idx, name="Ответ поддержки", value=f"```\n{reply_val}\n```", inline=False)
                else:
                    emb.add_field(name="Ответ поддержки", value=f"```\n{reply_val}\n```", inline=False)

            await starter_msg.edit(embed=emb)
    except Exception as msg_err:
        logger.error(f"Error updating starter message for ticket #{ticket_id}: {msg_err}")

    # Post reply message in thread if not already present
    if admin_reply:
        has_reply_msg = False
        try:
            async for prev_m in target_thread.history(limit=20):
                if prev_m.author == bot.user and prev_m.embeds:
                    for em in prev_m.embeds:
                        if em.title and "Ответ" in em.title and admin_reply[:40] in (em.description or ""):
                            has_reply_msg = True
                            break
                if has_reply_msg:
                    break

            if not has_reply_msg:
                reply_emb = discord.Embed(
                    title="Ответ службы поддержки WarLink",
                    description=admin_reply,
                    color=0x2ECC71 if status in ("resolved", "closed") else 0x3498DB,
                    timestamp=datetime.utcnow()
                )
                reply_emb.set_footer(text="Ответ автоматически направлен в приложение игрока")
                await target_thread.send(embed=reply_emb)
        except Exception as post_err:
            logger.error(f"Error posting reply embed: {post_err}")

    # Archive / lock if resolved
    if status in ("resolved", "closed") and not target_thread.archived:
        try:
            await target_thread.edit(locked=True, archived=True)
        except Exception as arch_err:
            logger.debug(f"Could not archive thread: {arch_err}")


async def handle_new_ticket_event(ticket_id: int):
    forum_channel = bot.get_channel(config.DISCORD_FORUM_CHANNEL_ID)
    if not forum_channel or not isinstance(forum_channel, discord.ForumChannel):
        logger.error(f"Forum channel #{config.DISCORD_FORUM_CHANNEL_ID} not found or not a ForumChannel")
        return

    data = await fetch_ticket_details(ticket_id)
    if not data or not data.get("success"):
        logger.error(f"Failed to fetch ticket #{ticket_id} details from API")
        return

    t = data.get("ticket", {})
    acc = t.get("account_number", "Не указан")
    dev = t.get("device_id", "none")
    ver = t.get("app_version", "v2.2.0")
    cat = t.get("category", "other")
    cat_title = CATEGORY_NAMES.get(cat, cat)
    comment = t.get("user_comment", "Без комментария")
    sys_info = t.get("system_info", {})
    if isinstance(sys_info, str):
        try:
            sys_info = json.loads(sys_info)
        except Exception:
            sys_info = {}

    os_str = sys_info.get("os", "Windows")
    cpu_str = sys_info.get("cpu", "Unknown")
    ram_str = sys_info.get("ram", "Unknown")
    ip_isp = sys_info.get("isp", "Direct")

    # Download logs archive
    archive_bytes = await fetch_ticket_archive(ticket_id)
    file_attachment = None
    if archive_bytes:
        file_attachment = discord.File(io.BytesIO(archive_bytes), filename=f"ticket_{ticket_id:04d}_logs.tar.gz")

    thread_name = f"[#TK-{ticket_id:04d}] {cat_title} ({ver})"[:100]

    embed = discord.Embed(
        title=f"Тикет поддержки #TK-{ticket_id:04d}",
        description=f"**Категория**: `{cat_title}`\n**Версия клиента**: `{ver}`\n**Статус**: `НОВЫЙ`",
        color=0xFF5E1F,
        timestamp=datetime.utcnow()
    )
    embed.add_field(name="Аккаунт игрока", value=f"`{acc}`", inline=True)
    embed.add_field(name="Устройство", value=f"`{dev[:16]}...`" if len(dev) > 16 else f"`{dev}`", inline=True)
    embed.add_field(name="Провайдер / Сеть", value=f"`{ip_isp}`", inline=True)
    embed.add_field(name="Система", value=f"**ОС**: {os_str}\n**CPU**: {cpu_str}\n**RAM**: {ram_str}", inline=False)
    if len(comment) > 900:
        comment = comment[:890] + "..."
    embed.add_field(name="Описание проблемы игроком", value=f"```\n{comment}\n```", inline=False)
    embed.set_footer(text="Ответьте кнопкой [Ответить] ниже, командой /reply или сообщением в ветку")

    view = TicketControlView(ticket_id)

    files = [file_attachment] if file_attachment else []
    created_thread = await forum_channel.create_thread(
        name=thread_name,
        embed=embed,
        files=files,
        view=view
    )
    logger.info(f"Successfully created forum thread for ticket #{ticket_id} (ID: {created_thread.thread.id})")

    # Dispatch alert to #⚠️・алерты-сервера
    alerts_chan = discord.utils.get(forum_channel.guild.text_channels, name="⚠️・алерты-сервера")
    if alerts_chan:
        admin_role = discord.utils.get(forum_channel.guild.roles, name="Администратор")
        mention = admin_role.mention if admin_role else "@Администратор"
        alert_embed = discord.Embed(
            title=f"НОВЫЙ ТИКЕТ ПОДДЕРЖКИ #TK-{ticket_id:04d}",
            description=(
                f"**Игрок**: `{acc}`\n"
                f"**Категория**: `{cat_title}`\n"
                f"**Версия**: `{ver}`\n"
                f"**Описание**: {comment[:300]}\n\n"
                f"[Перейти к обращению]({created_thread.thread.jump_url})"
            ),
            color=0xFF5E1F,
            timestamp=datetime.utcnow()
        )
        try:
            await alerts_chan.send(content=mention, embed=alert_embed)
        except Exception as alert_err:
            logger.error(f"Error sending ticket alert: {alert_err}")


# ==============================================================================
# ADMIN REPLY CAPTURE IN FORUM THREADS
# ==============================================================================

@bot.event
async def on_message(message: discord.Message):
    if message.author.bot:
        return

    # Check if message is in a Forum Thread under tickets forum
    if isinstance(message.channel, discord.Thread) and message.channel.parent_id == config.DISCORD_FORUM_CHANNEL_ID:
        is_admin = False
        if message.author.guild_permissions.administrator or (message.guild and message.author == message.guild.owner):
            is_admin = True
        elif message.author.guild_permissions.manage_threads or message.author.guild_permissions.manage_messages:
            is_admin = True
        elif config.DISCORD_ADMIN_ROLE_ID and any(r.id == config.DISCORD_ADMIN_ROLE_ID for r in getattr(message.author, "roles", [])):
            is_admin = True

        if not is_admin:
            logger.info(f"Ignoring non-admin message from {message.author} in thread {message.channel.name}")
            return

        # Extract ticket ID from thread name [#TK-0482]
        match = re.search(r"#TK-(\d+)", message.channel.name)
        if not match:
            logger.warning(f"Could not extract ticket ID from thread name '{message.channel.name}'")
            return

        ticket_id = int(match.group(1))
        content = message.content.strip()
        if not content:
            logger.warning(f"Empty content from {message.author} in ticket #{ticket_id}. Message Content Intent might be disabled in portal.")
            hint_emb = discord.Embed(
                title="[УВЕДОМЛЕНИЕ] Текст сообщения не передан Discord",
                description=(
                    "Discord не передал текст обычного сообщения из-за отключенного разрешения **Message Content Intent** в Discord Developer Portal.\n\n"
                    "**Как ответить игроку прямо сейчас:**\n"
                    "1. Нажмите кнопку **[Ответить]** в первом сообщении тикета вверху ветки\n"
                    "2. Либо используйте команду **/reply <текст ответа>** прямо в этой ветке\n\n"
                    "*(Чтобы отвечать обычными сообщениями в чат, включите переключатель 'Message Content Intent' в https://discord.com/developers/applications -> Bot)*"
                ),
                color=0xE74C3C
            )
            await message.channel.send(embed=hint_emb, delete_after=20)
            return

        logger.info(f"Sending admin reply from Discord by {message.author} to ticket #{ticket_id}: '{content[:50]}'")

        # Send reply to WarLink backend API
        url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{ticket_id}/reply"
        headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY, "Content-Type": "application/json"}
        payload = {
            "title": f"Ответ поддержки по тикету #TK-{ticket_id:04d}",
            "message": content,
            "severity": "update",
            "action_label": "Открыть диалог",
            "action_url": "#view-support",
            "status": "in_progress"
        }

        try:
            async with aiohttp.ClientSession() as session:
                async with session.post(url, headers=headers, json=payload) as resp:
                    if resp.status == 200:
                        logger.info(f"Successfully delivered reply to ticket #{ticket_id} in backend")
                        try:
                            await message.add_reaction("✅")
                        except Exception:
                            pass
                        conf_msg = await message.channel.send(
                            f"[ОТПРАВЛЕНО] Ответ администратора {message.author.mention} успешно доставлен игроку в WarLink.",
                            delete_after=10
                        )
                        await update_discord_ticket_thread(ticket_id)
                    else:
                        resp_text = await resp.text()
                        logger.error(f"Failed to deliver reply to ticket #{ticket_id}: HTTP {resp.status} - {resp_text}")
                        await message.channel.send(f"[ОШИБКА] Не удалось отправить ответ в WarLink (HTTP {resp.status}).", delete_after=10)
        except Exception as e:
            logger.error(f"Error sending reply to WarLink API: {e}")
            await message.channel.send(f"[СЕТЕВАЯ ОШИБКА] {e}", delete_after=10)

    await bot.process_commands(message)


@bot.tree.command(name="reply", description="Ответить игроку в приложение WarLink по текущему тикету")
@app_commands.describe(message="Текст ответа, который поступит в приложение игрока")
async def slash_reply(interaction: discord.Interaction, message: str):
    ticket_id = 0
    if isinstance(interaction.channel, discord.Thread):
        match = re.search(r"#TK-(\d+)", interaction.channel.name)
        if match:
            ticket_id = int(match.group(1))

    if ticket_id == 0:
        await interaction.response.send_message("[ОШИБКА] Команда /reply должна вызываться внутри ветки тикета #TK-XXXX.", ephemeral=True)
        return

    await interaction.response.defer(ephemeral=False)
    url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets/{ticket_id}/reply"
    headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY, "Content-Type": "application/json"}
    payload = {
        "title": f"Ответ поддержки по тикету #TK-{ticket_id:04d}",
        "message": message,
        "severity": "update",
        "action_label": "Открыть диалог",
        "action_url": "#view-support",
        "status": "in_progress"
    }

    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(url, headers=headers, json=payload) as resp:
                if resp.status == 200:
                    await interaction.followup.send(
                        f"**Ответ администратора {interaction.user.mention}:**\n{message}\n\n*(Сообщение доставлено игроку в WarLink)*"
                    )
                    await update_discord_ticket_thread(ticket_id)
                else:
                    await interaction.followup.send(f"[ОШИБКА] Статус ответа API: {resp.status}", ephemeral=True)
    except Exception as e:
        logger.error(f"Error in /reply: {e}")
        await interaction.followup.send(f"[СЕТЕВАЯ ОШИБКА] {e}", ephemeral=True)


# ==============================================================================
# LIVE MONITOR EMBED TASK
# ==============================================================================

@tasks.loop(seconds=30)
async def live_monitor_task():
    if not config.DISCORD_MONITOR_CHANNEL_ID:
        return

    channel = bot.get_channel(config.DISCORD_MONITOR_CHANNEL_ID)
    if not channel:
        return

    global monitor_message_id

    url = f"{config.WARLINK_API_URL}/api/v1/status"
    try:
        async with aiohttp.ClientSession() as session:
            async with session.get(url, timeout=5) as resp:
                if resp.status == 200:
                    data = await resp.json()
                else:
                    data = None
    except Exception as e:
        logger.warning(f"Error fetching status from WarLink API: {e}")
        data = None

    embed = discord.Embed(
        title="СТАТУС ИГРОВЫХ ШЛЮЗОВ",
        color=0xFF5E1F if data else 0x555555,
        timestamp=datetime.utcnow()
    )

    if data:
        active_slots = data.get("active_sessions", 0)
        max_slots = data.get("max_sessions", 61)
        latency = data.get("ping_hint_ms", 25)
        version = data.get("version", "v2.2.0")

        embed.add_field(name="Швеция: Стокгольм", value=f"{latency:.0f} ms", inline=True)
        embed.add_field(name="Загрузка слотов", value=f"{active_slots} / {max_slots}", inline=True)
        embed.add_field(name="Версия клиента", value=f"`{version}`", inline=True)
        embed.set_footer(text="Автообновление каждые 30 секунд")
    else:
        embed.description = "Сервер временно не отвечает на опросы телеметрии."
        embed.set_footer(text="Проверка связи...")

    try:
        if monitor_message_id:
            try:
                msg = await channel.fetch_message(monitor_message_id)
                await msg.edit(embed=embed)
                return
            except discord.NotFound:
                monitor_message_id = None

        # Look for existing bot message or send new
        async for prev_msg in channel.history(limit=5):
            if prev_msg.author == bot.user:
                await prev_msg.edit(embed=embed)
                monitor_message_id = prev_msg.id
                return

        new_msg = await channel.send(embed=embed)
        monitor_message_id = new_msg.id
    except Exception as e:
        logger.error(f"Error updating monitor embed: {e}")


# ==============================================================================
# SLASH COMMANDS
# ==============================================================================

@bot.tree.command(name="setup_panels", description="Развернуть системные панели WarLink (привязка и мониторинг)")
@app_commands.default_permissions(administrator=True)
async def setup_panels(interaction: discord.Interaction):
    await interaction.response.defer(ephemeral=True)

    # 1. Setup Link Panel
    if config.DISCORD_LINK_CHANNEL_ID:
        link_chan = bot.get_channel(config.DISCORD_LINK_CHANNEL_ID)
        if link_chan:
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
            embed.set_footer(text="WarLink Network")
            await link_chan.send(embed=embed, view=LinkButtonView())

    await interaction.followup.send("Панель привязки развернута.", ephemeral=True)


@bot.tree.command(name="sync_profile", description="Синхронизировать ранг и роли WARDOGS из приложения WarLink")
async def sync_profile_cmd(interaction: discord.Interaction):
    await interaction.response.defer(ephemeral=True)
    url = f"{config.WARLINK_API_URL}/api/v1/internal/discord/profile?discord_id={interaction.user.id}"
    headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY}

    try:
        async with aiohttp.ClientSession() as session:
            async with session.get(url, headers=headers, timeout=5) as resp:
                if resp.status == 200:
                    data = await resp.json()
                    is_sponsor = data.get("is_sponsor", False)
                    prog = data.get("progression")

                    roles_to_add = []
                    if config.DISCORD_VERIFIED_ROLE_ID:
                        v_role = interaction.guild.get_role(config.DISCORD_VERIFIED_ROLE_ID)
                        if v_role and v_role not in interaction.user.roles:
                            roles_to_add.append(v_role)

                    if is_sponsor and config.DISCORD_SPONSOR_ROLE_ID:
                        s_role = interaction.guild.get_role(config.DISCORD_SPONSOR_ROLE_ID)
                        if s_role and s_role not in interaction.user.roles:
                            roles_to_add.append(s_role)

                    if roles_to_add:
                        await interaction.user.add_roles(*roles_to_add, reason="WarLink Profile Sync")

                    if prog:
                        await sync_member_progression(interaction.user, prog)

                    lvl = 0
                    if isinstance(prog, dict):
                        lvl = prog.get("career_level", 0)

                    await interaction.followup.send(
                        f"Профиль успешно синхронизирован!\n"
                        f"Аккаунт: **{data.get('account_number')}**\n"
                        f"Ранг WARDOGS: **Уровень {lvl}**",
                        ephemeral=True
                    )
                else:
                    await interaction.followup.send(
                        "Аккаунт WarLink не найден для вашего Discord. Сначала привяжите его в канале привязки!",
                        ephemeral=True
                    )
    except Exception as e:
        logger.error(f"Error executing /sync_profile: {e}")
        await interaction.followup.send(f"Ошибка обращения к сервису WarLink: {e}", ephemeral=True)

async def sync_all_tickets():
    await bot.wait_until_ready()
    logger.info("Checking tickets synchronization...")
    forum = bot.get_channel(config.DISCORD_FORUM_CHANNEL_ID)
    if not forum or not isinstance(forum, discord.ForumChannel):
        return

    existing_ids = set()
    for th in forum.threads:
        m = re.search(r"#TK-(\d+)", th.name)
        if m:
            existing_ids.add(int(m.group(1)))

    try:
        async for th in forum.archived_threads(limit=100):
            m = re.search(r"#TK-(\d+)", th.name)
            if m:
                existing_ids.add(int(m.group(1)))
    except Exception:
        pass

    url = f"{config.WARLINK_API_URL}/api/v1/admin/tickets?status=all&limit=100"
    headers = {"X-Dashboard-Key": config.WARLINK_DASHBOARD_KEY}
    tickets = []
    try:
        async with aiohttp.ClientSession() as session:
            async with session.get(url, headers=headers) as resp:
                if resp.status == 200:
                    data = await resp.json()
                    tickets = data.get("tickets", [])
    except Exception as e:
        logger.error(f"Error fetching tickets for sync: {e}")
        return

    tickets.sort(key=lambda x: x["id"])
    for t in tickets:
        tid = t["id"]
        if tid in existing_ids:
            continue
        try:
            logger.info(f"Syncing ticket #{tid} into Discord...")
            await handle_new_ticket_event(tid)
            if t.get("status") in ("resolved", "closed"):
                for th in forum.threads:
                    if f"#TK-{tid:04d}" in th.name:
                        await th.edit(locked=True, archived=True)
                        break
            await asyncio.sleep(1)
        except Exception as err:
            logger.error(f"Error syncing ticket #{tid}: {err}")
    logger.info("Tickets synchronization complete.")


# ==============================================================================
# DYNAMIC VOICE CHANNELS (JOIN-TO-CREATE)
# ==============================================================================

temp_voice_channel_ids = set()


@bot.event
async def on_voice_state_update(member: discord.Member, before: discord.VoiceState, after: discord.VoiceState):
    # Case 1: Member joined the "создать-комнату" trigger channel
    if after.channel and ("создать-комнату" in after.channel.name.lower() or "создать комнату" in after.channel.name.lower()):
        category = after.channel.category
        guild = member.guild
        owner_name = member.display_name
        new_chan_name = f"Комната {owner_name}"

        # Grant owner full control over the room
        overwrites = {
            guild.default_role: discord.PermissionOverwrite(
                connect=True,
                speak=True,
                view_channel=True
            ),
            member: discord.PermissionOverwrite(
                manage_channels=True,
                move_members=True,
                mute_members=True,
                deafen_members=True,
                manage_permissions=True,
                connect=True,
                speak=True
            )
        }

        try:
            new_voice = await category.create_voice_channel(
                name=new_chan_name,
                overwrites=overwrites,
                reason=f"Dynamic room created for {owner_name}"
            )
            temp_voice_channel_ids.add(new_voice.id)
            await member.move_to(new_voice)
            logger.info(f"Dynamic room created: {new_chan_name} (id={new_voice.id}) for {member.display_name}")
        except Exception as e:
            logger.error(f"Error creating dynamic voice channel: {e}")

    # Case 2: Member left a voice channel
    if before.channel and before.channel != after.channel:
        ch = before.channel
        is_temp = ch.id in temp_voice_channel_ids or (
            ch.category and "ГОЛОСОВЫЕ КАНАЛЫ" in ch.category.name and "создать" not in ch.name.lower()
        )
        if is_temp and len(ch.members) == 0:
            try:
                temp_voice_channel_ids.discard(ch.id)
                await ch.delete(reason="Temporary dynamic voice channel is empty")
                logger.info(f"Deleted empty dynamic voice room: {ch.name} (id={ch.id})")
            except Exception as e:
                logger.error(f"Error deleting dynamic voice channel: {e}")


@bot.event
async def on_member_join(member: discord.Member):
    guild = member.guild
    chat_chan = discord.utils.get(guild.text_channels, name="💬・основной-чат")
    if not chat_chan:
        return

    rules_ch = discord.utils.get(guild.text_channels, name="📜・правила")
    link_ch = discord.utils.get(guild.text_channels, name="🔗・привязка-warlink")
    lfg_ch = discord.utils.get(guild.channels, name="🎯・поиск-пати")
    voice_ch = discord.utils.get(guild.voice_channels, name="➕・создать-комнату")

    rules_mention = rules_ch.mention if rules_ch else "#правила"
    link_mention = link_ch.mention if link_ch else "#привязка-warlink"
    lfg_mention = lfg_ch.mention if lfg_ch else "#поиск-пати"
    voice_mention = voice_ch.mention if voice_ch else "#создать-комнату"

    embed = discord.Embed(
        title="ДОБРО ПОЖАЛОВАТЬ В WARLINK",
        description=(
            f"Приветствуем бойца {member.mention} в официальном сообществе WarLink & WARDOGS!\n\n"
            f"**БЫСТРАЯ НАВИГАЦИЯ:**\n"
            f"• Ознакомьтесь с правилами: {rules_mention}\n"
            f"• Привяжите игровой ранг и аккаунт: {link_mention}\n"
            f"• Найдите сквад или дуо в пати: {lfg_mention}\n"
            f"• Создайте голосовую комнату для связи: {voice_mention}\n\n"
            f"Приятной игры без сетевых задержек и фильтрации!"
        ),
        color=0xFF5E1F,
        timestamp=datetime.utcnow()
    )
    if guild.icon:
        embed.set_thumbnail(url=guild.icon.url)
    embed.set_footer(text="WarLink // Swedish Gateway Infrastructure")

    try:
        await chat_chan.send(content=member.mention, embed=embed)
    except Exception as e:
        logger.error(f"Error sending welcome message: {e}")


@bot.event
async def on_ready():
    logger.info(f"Logged in as {bot.user} (ID: {bot.user.id})")
    bot.add_view(LinkButtonView())
    bot.add_view(ClassSelectionView())
    bot.add_view(TicketControlView(0))
    try:
        synced = await bot.tree.sync()
        logger.info(f"Synced {len(synced)} application commands.")
    except Exception as e:
        logger.error(f"Failed to sync slash commands: {e}")

    # Cleanup any orphaned dynamic voice channels from previous runs
    if config.DISCORD_GUILD_ID:
        guild = bot.get_guild(config.DISCORD_GUILD_ID)
        if guild:
            voice_cat = discord.utils.get(guild.categories, name="🔊 • ГОЛОСОВЫЕ КАНАЛЫ")
            if voice_cat:
                for vch in voice_cat.voice_channels:
                    if "создать" not in vch.name.lower() and len(vch.members) == 0:
                        try:
                            await vch.delete(reason="Cleanup orphaned dynamic voice room on startup")
                            logger.info(f"Cleaned up orphaned voice room: {vch.name}")
                        except Exception as e:
                            logger.error(f"Error cleaning up voice room {vch.name}: {e}")

    live_monitor_task.start()
    asyncio.create_task(redis_ticket_listener())
    asyncio.create_task(sync_all_tickets())


if __name__ == "__main__":
    if not config.DISCORD_CORE_BOT_TOKEN:
        logger.error("DISCORD_CORE_BOT_TOKEN is not set in environment or .env file.")
    else:
        bot.run(config.DISCORD_CORE_BOT_TOKEN)
