"""Notes management module."""

import logging
from pathlib import Path
from core.config import DAILY_NOTES_DIR, DAILY_TEMPLATE_PATH
from core.utils import get_today_filename

logger = logging.getLogger(__name__)


def create_daily_note_from_template(filepath: Path, date_str: str) -> None:
    """Create a new daily note from template"""
    if not DAILY_TEMPLATE_PATH.exists():
        logger.warning(
            f"Template not found at {DAILY_TEMPLATE_PATH}, creating basic note"
        )
        # Create a basic daily note if template doesn't exist
        with open(filepath, "w", encoding="utf-8") as f:
            f.write("---\n")
            f.write(f'date: "[[{date_str}]]"\n')
            f.write(f'title: "[[{date_str}]]"\n')
            f.write("tags:\n")
            f.write("  - daily\n")
            f.write("Оценка:\n")
            f.write("---\n")
            f.write("- [ ] Доброго утра!\n")
            f.write("- [ ] Заполнить дневник\n")
            f.write("---\n\n")
        return

    # Read template and replace date placeholders
    try:
        with open(DAILY_TEMPLATE_PATH, "r", encoding="utf-8") as f:
            template_content = f.read()

        # Replace Obsidian template variables with actual date
        # {{date:DD-MMM-YYYY}} -> actual date
        content = template_content.replace("{{date:DD-MMM-YYYY}}", date_str)

        # Write to new file
        with open(filepath, "w", encoding="utf-8") as f:
            f.write(content)

        logger.info(f"Created daily note from template: {filepath}")
    except Exception as e:
        logger.error(f"Error creating note from template: {e}")
        raise


def save_message(text: str) -> None:
    """Save message to today's daily note file"""
    filename = get_today_filename()
    filepath = DAILY_NOTES_DIR / filename

    # Extract date string without .md extension for template
    date_str = filename[:-3]  # Remove .md

    # Check if file exists
    file_exists = filepath.exists()

    if not file_exists:
        # Create from template
        create_daily_note_from_template(filepath, date_str)

    # Append message to the file
    with open(filepath, "a", encoding="utf-8") as f:
        # Add message with proper formatting
        f.write(f"{text}\n")

    logger.info(f"Message saved to {filename}")


def read_note(filename: str) -> str | None:
    """Read note file and return its content"""
    filepath = DAILY_NOTES_DIR / filename

    if not filepath.exists():
        return None

    try:
        with open(filepath, "r", encoding="utf-8") as f:
            return f.read()
    except Exception as e:
        logger.error(f"Error reading file {filename}: {e}")
        return None
