"""
Pytest configuration and shared fixtures.

Mock psycopg2 so notifications modules import without a real DB driver.
Individual tests patch the specific DB functions they need.
"""

import sys
from unittest.mock import MagicMock

sys.modules["psycopg2"] = MagicMock()
sys.modules["psycopg2.extras"] = MagicMock()
