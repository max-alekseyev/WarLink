import asyncio
import logging
import config
from bot_core import bot as core_bot
from bot_status import client as status_client

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("WarLinkDiscordRunner")


async def main():
    tasks = []

    if config.DISCORD_CORE_BOT_TOKEN:
        logger.info("Scheduling WarLink Core Bot...")
        tasks.append(core_bot.start(config.DISCORD_CORE_BOT_TOKEN))
    else:
        logger.warning("DISCORD_CORE_BOT_TOKEN is not set.")

    if config.DISCORD_STATUS_BOT_TOKEN:
        logger.info("Scheduling WarLink Status Bot (Stockholm)...")
        tasks.append(status_client.start(config.DISCORD_STATUS_BOT_TOKEN))
    else:
        logger.warning("DISCORD_STATUS_BOT_TOKEN is not set.")

    if not tasks:
        logger.error("No bot tokens provided. Please configure .env file.")
        return

    logger.info(f"Starting {len(tasks)} Discord bot services...")
    await asyncio.gather(*tasks)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logger.info("WarLink Discord services stopped by user.")
