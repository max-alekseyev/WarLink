import os
from dotenv import load_dotenv

load_dotenv()

def get_int(key, default=0):
    val = os.getenv(key, "")
    if not val or not str(val).strip():
        return default
    try:
        return int(str(val).strip())
    except ValueError:
        return default

# Discord Tokens
DISCORD_CORE_BOT_TOKEN = os.getenv("DISCORD_CORE_BOT_TOKEN", "").strip()
DISCORD_STATUS_BOT_TOKEN = os.getenv("DISCORD_STATUS_BOT_TOKEN", "").strip()

# Discord Guild & Channels
DISCORD_GUILD_ID = get_int("DISCORD_GUILD_ID", 1555211397900673086)
DISCORD_FORUM_CHANNEL_ID = get_int("DISCORD_FORUM_CHANNEL_ID", 0)
DISCORD_MONITOR_CHANNEL_ID = get_int("DISCORD_MONITOR_CHANNEL_ID", 0)
DISCORD_LINK_CHANNEL_ID = get_int("DISCORD_LINK_CHANNEL_ID", 0)

# Discord Role IDs
DISCORD_ADMIN_ROLE_ID = get_int("DISCORD_ADMIN_ROLE_ID", 0)
DISCORD_SPONSOR_ROLE_ID = get_int("DISCORD_SPONSOR_ROLE_ID", 0)
DISCORD_VERIFIED_ROLE_ID = get_int("DISCORD_VERIFIED_ROLE_ID", 0)

# WarLink Backend Server
WARLINK_API_URL = os.getenv("WARLINK_API_URL", "http://127.0.0.1:8081").rstrip("/")
WARLINK_DASHBOARD_KEY = os.getenv("WARLINK_DASHBOARD_KEY", "").strip()

# Redis Connection
REDIS_URL = os.getenv("REDIS_URL", "redis://127.0.0.1:6379/0").strip()
