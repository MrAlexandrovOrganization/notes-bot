"""Module for working with tasks in notes."""

import logging
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List

logger = logging.getLogger(__name__)


def parse_tasks(content: str) -> List[Dict[str, Any]]:
    """
    Parse tasks from markdown content.

    Searches for tasks between the SECOND and THIRD '---' delimiters (tasks section).
    Tasks are lines starting with '- [ ]' (incomplete) or '- [x]' (completed).

    Args:
        content: Markdown content to parse

    Returns:
        List of task dictionaries with keys:
        - text: Task text (without [completion:: ...] metadata)
        - completed: Boolean indicating if task is completed
        - index: Task index (0-based)
        - line_number: Line number in the original content (1-based)
    """
    tasks: List[Dict[str, Any]] = []

    try:
        # Split by frontmatter delimiters
        parts = content.split("---")
        if len(parts) < 4:
            logger.warning(
                "Invalid format: need at least 3 '---' delimiters for tasks section"
            )
            return tasks

        # Get tasks section (between second and third ---)
        tasks_section = parts[2]
        lines = tasks_section.split("\n")

        # Calculate line offset to second ---
        first_delimiter_pos = content.find("---")
        second_delimiter_pos = content.find("---", first_delimiter_pos + 3)
        line_offset = content[: second_delimiter_pos + 3].count("\n") + 1

        task_index = 0
        for i, line in enumerate(lines):
            stripped = line.strip()

            # Check for incomplete task
            if stripped.startswith("- [ ]"):
                # Extract task text, removing [completion:: ...] if present
                task_text = stripped[5:].strip()
                # Remove completion metadata if present
                if "[completion::" in task_text:
                    task_text = task_text[: task_text.find("[completion::")].strip()

                tasks.append(
                    {
                        "text": task_text,
                        "completed": False,
                        "index": task_index,
                        "line_number": line_offset + i,
                    }
                )
                task_index += 1

            # Check for completed task
            elif stripped.startswith("- [x]") or stripped.startswith("- [X]"):
                # Extract task text, removing [completion:: ...] if present
                task_text = stripped[5:].strip()
                # Remove completion metadata if present
                if "[completion::" in task_text:
                    task_text = task_text[: task_text.find("[completion::")].strip()

                tasks.append(
                    {
                        "text": task_text,
                        "completed": True,
                        "index": task_index,
                        "line_number": line_offset + i,
                    }
                )
                task_index += 1

        logger.info(f"Parsed {len(tasks)} tasks from content")

    except Exception as e:
        logger.error(f"Error parsing tasks: {e}")

    return tasks


def toggle_task(filepath: Path, task_index: int) -> bool:
    """
    Toggle a task's completion status by index.

    Reads the file, finds the task at the given index, toggles its status
    between [ ] and [x], and saves the file.
    When marking as completed, adds [completion:: YYYY-MM-DD] with current date.
    When marking as incomplete, removes [completion:: ...] metadata.

    Args:
        filepath: Path to the note file
        task_index: Index of the task to toggle (0-based)

    Returns:
        True if successful, False otherwise
    """
    try:
        if not filepath.exists():
            logger.error(f"File not found: {filepath}")
            return False

        # Read file content
        content = filepath.read_text(encoding="utf-8")

        # Parse tasks to find the target
        tasks = parse_tasks(content)

        if task_index < 0 or task_index >= len(tasks):
            logger.error(
                f"Invalid task index: {task_index} (total tasks: {len(tasks)})"
            )
            return False

        target_task = tasks[task_index]
        line_number = target_task["line_number"]

        # Split content into lines
        lines = content.split("\n")

        # Toggle the task (line_number is 1-based, list is 0-based)
        line_idx = line_number - 1
        if line_idx < 0 or line_idx >= len(lines):
            logger.error(f"Invalid line number: {line_number}")
            return False

        line = lines[line_idx]

        # Toggle task status
        if "- [ ]" in line:
            # Mark as completed: change [ ] to [x] and add completion date
            current_date = datetime.now().strftime("%Y-%m-%d")

            # Remove existing completion metadata if any
            if "[completion::" in line:
                # Remove old completion metadata
                completion_start = line.find("[completion::")
                completion_end = line.find("]", completion_start) + 1
                line = line[:completion_start].rstrip() + line[completion_end:]

            # Replace [ ] with [x] and add completion metadata
            line = line.replace("- [ ]", "- [x]", 1)
            # Add two spaces before [completion::] as per Obsidian format
            line = line.rstrip() + f"  [completion:: {current_date}]"
            lines[line_idx] = line

        elif "- [x]" in line or "- [X]" in line:
            # Mark as incomplete: change [x] to [ ] and remove completion date
            line = line.replace("- [x]", "- [ ]", 1).replace("- [X]", "- [ ]", 1)

            # Remove completion metadata if present
            if "[completion::" in line:
                completion_start = line.find("[completion::")
                completion_end = line.find("]", completion_start) + 1
                line = line[:completion_start].rstrip() + line[completion_end:]

            lines[line_idx] = line
        else:
            logger.error(f"Line {line_number} does not contain a valid task")
            return False

        # Write back to file
        new_content = "\n".join(lines)
        filepath.write_text(new_content, encoding="utf-8")

        logger.info(f"Successfully toggled task {task_index} in {filepath}")
        return True

    except Exception as e:
        logger.error(f"Error toggling task in {filepath}: {e}")
        return False


def add_task(filepath: Path, task_text: str) -> bool:
    """
    Add a new task to the note.

    Adds a new task '- [ ] {task_text}' in the tasks section
    (between the second and third '---' delimiters), after the last existing task.

    Args:
        filepath: Path to the note file
        task_text: Text of the new task

    Returns:
        True if successful, False otherwise
    """
    try:
        if not filepath.exists():
            logger.error(f"File not found: {filepath}")
            return False

        # Read file content
        content = filepath.read_text(encoding="utf-8")

        # Split by frontmatter delimiters
        parts = content.split("---")
        if len(parts) < 4:
            logger.error(
                f"Invalid format in {filepath}: need at least 3 '---' delimiters"
            )
            return False

        # Get tasks section (between second and third ---)
        tasks_section = parts[2]
        lines = tasks_section.split("\n")

        # Find the last task line
        last_task_idx = -1
        for i, line in enumerate(lines):
            stripped = line.strip()
            if (
                stripped.startswith("- [ ]")
                or stripped.startswith("- [x]")
                or stripped.startswith("- [X]")
            ):
                last_task_idx = i

        # Prepare new task line
        new_task = f"- [ ] {task_text}"

        # Insert after last task or at the beginning if no tasks found
        if last_task_idx >= 0:
            lines.insert(last_task_idx + 1, new_task)
        else:
            # Insert at the beginning of tasks section (after first empty line if exists)
            if lines and lines[0] == "":
                lines.insert(1, new_task)
            else:
                lines.insert(0, new_task)

        # Reconstruct content
        parts[2] = "\n".join(lines)
        new_content = "---".join(parts)

        # Write back to file
        filepath.write_text(new_content, encoding="utf-8")

        logger.info(f"Successfully added task '{task_text}' to {filepath}")
        return True

    except Exception as e:
        logger.error(f"Error adding task to {filepath}: {e}")
        return False
