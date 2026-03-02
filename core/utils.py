"""Utility functions — date helpers."""

from datetime import datetime, timedelta, timezone
from core.config import TIMEZONE_OFFSET_HOURS, DAY_START_HOUR


def get_today_filename() -> str:
    """Generate filename in format dd-Mmm-yyyy (e.g., 11-Oct-2025)"""
    # Get current UTC time
    now_utc = datetime.now(timezone.utc)

    # Convert to Moscow time (UTC+3)
    moscow_time = now_utc + timedelta(hours=TIMEZONE_OFFSET_HOURS)

    # If time is before 7 AM in Moscow, consider it previous day
    if moscow_time.hour < DAY_START_HOUR:
        # Subtract one day
        adjusted_time = moscow_time - timedelta(days=1)
    else:
        adjusted_time = moscow_time

    return adjusted_time.strftime("%d-%b-%Y") + ".md"
