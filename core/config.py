"""Configuration module — notes paths and timezone settings."""

import os
from pathlib import Path
from dotenv import load_dotenv

# Load environment variables
load_dotenv()


def get_notes_dir():
    """Get notes directory path from environment.

    For local run: uses host path directly (e.g., /home/maxim/Yandex.Disk/notes)
    For Docker: automatically overridden to /notes in docker-compose.yml
    """
    NOTES_DIR_STR = os.getenv("NOTES_DIR")
    if not NOTES_DIR_STR:
        raise ValueError("NOTES_DIR environment variable must be set")

    NOTES_DIR = Path(NOTES_DIR_STR)
    if not NOTES_DIR.exists():
        raise ValueError(f"Notes directory does not exist: {NOTES_DIR}")

    return NOTES_DIR


# Notes directory - base path for all notes and templates
NOTES_DIR = get_notes_dir()

# Template subdirectory relative to NOTES_DIR (default: "Templates")
TEMPLATE_SUBDIR = os.getenv("TEMPLATE_SUBDIR", "Templates")

# Full path to templates directory
TEMPLATE_DIR = NOTES_DIR / TEMPLATE_SUBDIR
if not TEMPLATE_DIR.exists():
    raise ValueError(f"Template directory does not exist: {TEMPLATE_DIR}")

# Daily notes subdirectory
DAILY_NOTES_DIR = NOTES_DIR / "Daily"
DAILY_NOTES_DIR.mkdir(exist_ok=True)

# Daily template file
DAILY_TEMPLATE_PATH = TEMPLATE_DIR / "Daily.md"
if not DAILY_TEMPLATE_PATH.exists():
    raise ValueError(f"Daily template not found at: {DAILY_TEMPLATE_PATH}")

# Timezone settings
TIMEZONE_OFFSET_HOURS = 3  # Moscow time (UTC+3)
DAY_START_HOUR = 7  # Consider day starts at 7 AM
