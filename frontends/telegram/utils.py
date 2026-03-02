"""Telegram-specific utilities."""

import re


def escape_markdown_v2(text: str) -> str:
    """Escape special characters for MarkdownV2"""
    # Characters that need to be escaped in MarkdownV2
    escape_chars = r"_*[]()~`>#+-=|{}.!"
    return re.sub(f"([{re.escape(escape_chars)}])", r"\\\1", text)
