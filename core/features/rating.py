"""Module for working with note ratings."""

import logging
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)


def update_rating(filepath: Path, rating: int) -> bool:
    """
    Update the rating field in a note's frontmatter.

    Args:
        filepath: Path to the note file
        rating: New rating value (1-5)

    Returns:
        True if successful, False otherwise
    """
    try:
        if not filepath.exists():
            logger.error(f"File not found: {filepath}")
            return False

        # Read file content
        content = filepath.read_text(encoding="utf-8")

        new_content = update_rating_impl(content, rating)

        # Write back to file
        if new_content:
            filepath.write_text(new_content, encoding="utf-8")
            logger.info(f"Successfully updated rating to {rating} in {filepath}")
        else:
            logger.warning(f"Error while updating rating to {rating} in {filepath}")
        return True

    except Exception as e:
        logger.error(f"Error updating rating in {filepath}: {e}")
        return False


def update_rating_impl(content: str, rating: int) -> Optional[str]:
    # Split by frontmatter delimiters
    parts = content.split("---")
    if len(parts) < 3:
        logger.error("Invalid frontmatter format in note")
        return None

    # Parse frontmatter (between first and second ---)
    frontmatter = parts[1]
    lines = frontmatter.split("\n")

    # Update or add rating field
    rating_found = False
    updated_lines: list[str] = []
    for line in lines:
        if line.strip().startswith("Оценка:"):
            updated_lines.append(f"Оценка: {rating}")
            rating_found = True
        else:
            updated_lines.append(line)

    # If rating field not found, add it
    if not rating_found:
        # Insert before the last empty line if exists
        if updated_lines and updated_lines[-1] == "":
            updated_lines.insert(-1, f"Оценка: {rating}")
        else:
            updated_lines.append(f"Оценка: {rating}")

    # Reconstruct content
    parts[1] = "\n".join(updated_lines)
    new_content = "---".join(parts)

    # Write back to file
    return new_content


def get_rating(filepath: Path) -> Optional[int]:
    """
    Extract the rating value from a note's frontmatter.

    Args:
        filepath: Path to the note file

    Returns:
        Rating value as integer, or None if not found or on error
    """
    try:
        if not filepath.exists():
            logger.error(f"File not found: {filepath}")
            return None

        # Read file content
        content = filepath.read_text(encoding="utf-8")

        return get_rating_impl(content)

    except Exception as e:
        logger.error(f"Error reading rating from {filepath}: {e}")
        return None


def get_rating_impl(content: str) -> Optional[int]:
    # Split by frontmatter delimiters
    parts = content.split("---")
    if len(parts) < 3:
        logger.warning("Invalid frontmatter format in note")
        return None

    # Parse frontmatter (between first and second ---)
    frontmatter = parts[1]
    lines = frontmatter.split("\n")

    # Find rating field
    for line in lines:
        if line.strip().startswith("Оценка:"):
            # Extract value after colon
            rating_str = line.split(":", 1)[1].strip()
            try:
                rating = int(rating_str)
                return rating
            except ValueError:
                logger.warning(f"Invalid rating value in note: {rating_str}")
                return None

    # Rating field not found
    return None
