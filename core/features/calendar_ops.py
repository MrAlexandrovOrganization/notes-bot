"""Module for calendar operations."""

import logging
from pathlib import Path
from typing import Set

logger = logging.getLogger(__name__)


def get_existing_dates(notes_dir: Path) -> Set[str]:
    """
    Scan the Daily/ directory and return a set of existing note dates.

    Args:
        notes_dir: Path to the notes directory (should contain Daily/ subdirectory)

    Returns:
        Set of date strings in format DD-MMM-YYYY (e.g., "05-Feb-2025")
    """
    existing_dates: Set[str] = set()

    try:
        daily_dir = notes_dir / "Daily"

        if not daily_dir.exists():
            logger.warning(f"Daily directory not found: {daily_dir}")
            return existing_dates

        if not daily_dir.is_dir():
            logger.warning(f"Daily path is not a directory: {daily_dir}")
            return existing_dates

        # Scan for .md files
        for file_path in daily_dir.glob("*.md"):
            # Extract date from filename (e.g., "05-Feb-2025.md" -> "05-Feb-2025")
            date_str = file_path.stem
            existing_dates.add(date_str)

        logger.info(f"Found {len(existing_dates)} existing daily notes")

    except Exception as e:
        logger.error(f"Error scanning daily notes directory: {e}")

    return existing_dates


def format_date_for_display(date_str: str, is_active: bool, has_note: bool) -> str:
    """
    Format a date string for display in the calendar.

    Args:
        date_str: Date string to format (e.g., "05-Feb-2025")
        is_active: Whether this is the currently selected/active date
        has_note: Whether a note exists for this date

    Returns:
        Formatted date string with markdown formatting:
        - Active dates are wrapped in brackets: [05-Feb-2025]
        - Dates with notes are made bold: **05-Feb-2025**
        - Both: **[05-Feb-2025]**
    """
    formatted = date_str

    # Make bold if note exists
    if has_note:
        formatted = f"**{formatted}**"

    # Wrap in brackets if active
    if is_active:
        formatted = f"[{formatted}]"

    return formatted
