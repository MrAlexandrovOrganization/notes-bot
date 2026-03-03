"""
Pytest configuration for Telegram frontend tests.

Injects mocks into sys.modules BEFORE any telegram frontend modules are
collected, preventing import-time errors:
  - frontends/telegram/config.py raises ValueError if ROOT_ID is not set
  - grpc_client.py / notifications_client.py import grpc at module level
    and create channel objects

Strategy: mock only the telegram-specific gRPC client modules
(frontends.telegram.grpc_client and frontends.telegram.notifications_client).
This keeps the real proto/* and grpc imports intact so that existing
core/server.py tests are not affected.
"""

import os
import sys
from unittest.mock import AsyncMock, MagicMock

# ---------------------------------------------------------------------------
# Set required env vars before frontends.telegram.config is imported.
# config.py raises ValueError if ROOT_ID is not set.
# ---------------------------------------------------------------------------
os.environ.setdefault("ROOT_ID", "42")
os.environ.setdefault("BOT_TOKEN", "test-token")

# ---------------------------------------------------------------------------
# Mock the two gRPC client modules used by telegram handlers.
# By placing them in sys.modules here (at conftest import time, before any
# test file is imported), handlers/* will pick up the mocks instead of the
# real modules. This prevents grpcio version checks and real channel creation,
# without touching proto.* or grpc itself so core tests stay green.
# ---------------------------------------------------------------------------
sys.modules["frontends.telegram.grpc_client"] = MagicMock()
sys.modules["frontends.telegram.notifications_client"] = MagicMock()


# ---------------------------------------------------------------------------
# Helper factories shared across handler tests
# ---------------------------------------------------------------------------

ROOT_USER_ID = 42


def make_callback_query(data: str, user_id: int = ROOT_USER_ID) -> MagicMock:
    """Build a mock CallbackQuery with the given callback_data."""
    query = MagicMock()
    query.data = data
    query.from_user.id = user_id
    query.edit_message_text = AsyncMock()
    query.answer = AsyncMock()
    return query


def make_text_update(text: str, user_id: int = ROOT_USER_ID) -> MagicMock:
    """Build a mock Update carrying a plain-text message."""
    update = MagicMock()
    update.effective_user.id = user_id
    update.message.text = text
    update.message.reply_text = AsyncMock()
    update.callback_query = None
    return update


def make_callback_update(data: str, user_id: int = ROOT_USER_ID) -> MagicMock:
    """Build a mock Update carrying an inline-keyboard callback."""
    update = MagicMock()
    update.effective_user.id = user_id
    update.callback_query = make_callback_query(data, user_id)
    return update
