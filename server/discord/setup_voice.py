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
    cat = discord.utils.get(guild.categories, name="🔊 • ГОЛОСОВЫЕ КАНАЛЫ")
    if not cat:
        print("Voice category not found!")
        await client.close()
        return

    # Delete static voice channels
    for ch in cat.voice_channels:
        try:
            await ch.delete(reason="Replacing with dynamic voice channels")
            print(f"Deleted voice channel: {ch.name}")
        except Exception as e:
            print(f"Error deleting {ch.name}: {e}")

    # Create the single Join-to-Create trigger channel
    trigger_chan = await cat.create_voice_channel(
        name="➕・создать-комнату",
        reason="Trigger channel for dynamic temporary voice channels"
    )
    print(f"Created trigger voice channel: {trigger_chan.name} (id={trigger_chan.id})")

    await client.close()

if __name__ == "__main__":
    client.run(token)
