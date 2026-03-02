"""Telegram-specific configuration — bot token and authorized user."""

import os
import logging
from dotenv import load_dotenv

# Load environment variables
load_dotenv()

# Configure logging
logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s", level=logging.INFO
)
logging.getLogger("httpx").setLevel(logging.WARNING)


def get_bot_token():
    """Get Telegram bot token from environment."""
    return os.getenv("BOT_TOKEN")


def get_root_id():
    """Get authorized user's Telegram ID from environment."""
    _root_id = os.getenv("ROOT_ID")
    return int(_root_id) if _root_id else None


BOT_TOKEN = get_bot_token()
ROOT_ID = get_root_id()

if ROOT_ID is None:
    raise ValueError(
        "ROOT_ID environment variable is not set. "
        "Set it to your Telegram user ID to authorize bot access."
    )

# Timezone and day-start settings — must match core service configuration.
TIMEZONE_OFFSET_HOURS: int = int(os.getenv("TIMEZONE_OFFSET_HOURS", "3"))
DAY_START_HOUR: int = int(os.getenv("DAY_START_HOUR", "7"))
