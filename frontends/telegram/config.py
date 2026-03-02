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
