import asyncio
import logging
import aiohttp
import discord
from discord.ext import tasks

import config

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("WarLinkStatusBot")

intents = discord.Intents.default()
client = discord.Client(intents=intents)


@tasks.loop(seconds=30)
async def update_status_task():
    url = f"{config.WARLINK_API_URL}/api/v1/status"
    data = None
    try:
        async with aiohttp.ClientSession() as session:
            async with session.get(url, timeout=5) as resp:
                if resp.status == 200:
                    data = await resp.json()
    except Exception as e:
        logger.warning(f"Error querying WarLink status: {e}")

    guild = None
    if config.DISCORD_GUILD_ID:
        guild = client.get_guild(config.DISCORD_GUILD_ID)

    nick_text = "Швеция: Стокгольм"
    if data:
        active_slots = data.get("active_sessions", 0)
        max_slots = data.get("max_sessions", 61)
        latency = data.get("ping_hint_ms", 25)
        status_text = f"{latency:.0f}ms • {active_slots}/{max_slots}"

        activity = discord.CustomActivity(name=status_text, state=status_text)
        await client.change_presence(status=discord.Status.online, activity=activity)

        if guild:
            try:
                member = guild.get_member(client.user.id)
                if not member:
                    member = await guild.fetch_member(client.user.id)
                if member and member.nick != nick_text:
                    await member.edit(nick=nick_text)
            except Exception as nick_err:
                logger.debug(f"Could not change nickname: {nick_err}")
    else:
        status_text = "Offline • 0/61"
        activity = discord.CustomActivity(name=status_text, state=status_text)
        await client.change_presence(status=discord.Status.dnd, activity=activity)

    if guild:
        try:
            member = guild.get_member(client.user.id)
            if not member:
                member = await guild.fetch_member(client.user.id)
            if member and member.nick != nick_text:
                await member.edit(nick=nick_text)
        except Exception as nick_err:
            logger.debug(f"Could not change nickname: {nick_err}")

    logger.info(f"Status update completed: {nick_text} | {status_text}")


@client.event
async def on_ready():
    logger.info(f"Status Bot logged in as {client.user} (ID: {client.user.id})")
    await update_status_task()
    if not update_status_task.is_running():
        update_status_task.start()


if __name__ == "__main__":
    if not config.DISCORD_STATUS_BOT_TOKEN:
        logger.error("DISCORD_STATUS_BOT_TOKEN is not set in environment or .env file.")
    else:
        client.run(config.DISCORD_STATUS_BOT_TOKEN)
