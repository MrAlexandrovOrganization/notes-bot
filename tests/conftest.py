"""
Pytest configuration and shared fixtures.

core/config.py runs environment-variable checks at import time (raises ValueError
if NOTES_DIR is not set or directories are missing).  Because core/utils.py and
core/notes.py import from core.config at *module* level, we must inject a mock
into sys.modules BEFORE any test file is collected/imported by pytest.

This module-level injection happens when conftest.py itself is loaded, which is
always before the test files, so the mock is in place by the time any import
in test_utils.py or test_notes.py runs.
"""

import sys
from pathlib import Path
from unittest.mock import MagicMock

# ---------------------------------------------------------------------------
# Inject a lightweight mock for core.config
# ---------------------------------------------------------------------------
_mock_config = MagicMock()
_mock_config.NOTES_DIR = Path("/tmp/mock_notes")
_mock_config.DAILY_NOTES_DIR = Path("/tmp/mock_notes/Daily")
_mock_config.DAILY_TEMPLATE_PATH = Path("/tmp/mock_notes/Templates/Daily.md")
_mock_config.TEMPLATE_DIR = Path("/tmp/mock_notes/Templates")
_mock_config.TIMEZONE_OFFSET_HOURS = 3
_mock_config.DAY_START_HOUR = 7

sys.modules["core.config"] = _mock_config

# ---------------------------------------------------------------------------
# Mock psycopg2 so notifications modules import without a real DB driver.
# Individual tests patch the specific DB functions they need.
# ---------------------------------------------------------------------------
sys.modules["psycopg2"] = MagicMock()
sys.modules["psycopg2.extras"] = MagicMock()
