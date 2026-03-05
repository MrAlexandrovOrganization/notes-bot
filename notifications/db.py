"""Database functions for the notifications service."""

import json
import logging
from typing import Any, Dict, List, Optional

import psycopg2
import psycopg2.extras

from notifications.config import DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD

logger = logging.getLogger(__name__)

_CREATE_TABLE_SQL = """
CREATE TABLE IF NOT EXISTS reminders (
    id SERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    title TEXT NOT NULL,
    schedule_type TEXT NOT NULL,
    schedule_params JSONB NOT NULL DEFAULT '{}',
    next_fire_at TIMESTAMPTZ NOT NULL,
    last_fired_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    create_task BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_reminders_next_fire ON reminders (next_fire_at) WHERE is_active = TRUE;
"""

_MIGRATE_SQL = "ALTER TABLE reminders ADD COLUMN IF NOT EXISTS create_task BOOLEAN NOT NULL DEFAULT FALSE;"


def get_connection():
    return psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        dbname=DB_NAME,
        user=DB_USER,
        password=DB_PASSWORD,
    )


def ensure_schema() -> None:
    with get_connection() as conn:
        with conn.cursor() as cur:
            cur.execute(_CREATE_TABLE_SQL)
            cur.execute(_MIGRATE_SQL)
        conn.commit()
    logger.info("Database schema ensured")


def create_reminder(
    user_id: int,
    title: str,
    schedule_type: str,
    schedule_params: Dict[str, Any],
    next_fire_at: str,
    create_task: bool = False,
) -> Dict[str, Any]:
    sql = """
        INSERT INTO reminders (user_id, title, schedule_type, schedule_params, next_fire_at, create_task)
        VALUES (%s, %s, %s, %s, %s, %s)
        RETURNING id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
    """
    with get_connection() as conn:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(
                sql,
                (user_id, title, schedule_type, json.dumps(schedule_params), next_fire_at, create_task),
            )
            row = dict(cur.fetchone())
        conn.commit()
    row["schedule_params"] = dict(row["schedule_params"])
    return row


def list_reminders(user_id: int) -> List[Dict[str, Any]]:
    sql = """
        SELECT id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
        FROM reminders
        WHERE user_id = %s AND is_active = TRUE
        ORDER BY next_fire_at ASC
    """
    with get_connection() as conn:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql, (user_id,))
            rows = [dict(r) for r in cur.fetchall()]
    for row in rows:
        row["schedule_params"] = dict(row["schedule_params"])
    return rows


def delete_reminder(reminder_id: int, user_id: int) -> bool:
    sql = """
        UPDATE reminders SET is_active = FALSE
        WHERE id = %s AND user_id = %s AND is_active = TRUE
    """
    with get_connection() as conn:
        with conn.cursor() as cur:
            cur.execute(sql, (reminder_id, user_id))
            deleted = cur.rowcount > 0
        conn.commit()
    return deleted


def get_due_reminders() -> List[Dict[str, Any]]:
    sql = """
        SELECT id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
        FROM reminders
        WHERE is_active = TRUE AND next_fire_at <= NOW()
        FOR UPDATE SKIP LOCKED
    """
    with get_connection() as conn:
        with conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
            cur.execute(sql)
            rows = [dict(r) for r in cur.fetchall()]
        conn.commit()
    for row in rows:
        row["schedule_params"] = dict(row["schedule_params"])
    return rows


def update_next_fire(reminder_id: int, next_fire_at: Optional[str]) -> None:
    if next_fire_at is None:
        sql = "UPDATE reminders SET is_active = FALSE, last_fired_at = NOW() WHERE id = %s"
        params = (reminder_id,)
    else:
        sql = "UPDATE reminders SET next_fire_at = %s, last_fired_at = NOW() WHERE id = %s"
        params = (next_fire_at, reminder_id)
    with get_connection() as conn:
        with conn.cursor() as cur:
            cur.execute(sql, params)
        conn.commit()


def set_next_fire_at(reminder_id: int, user_id: int, next_fire_at: str) -> bool:
    sql = """
        UPDATE reminders SET next_fire_at = %s
        WHERE id = %s AND user_id = %s AND is_active = TRUE
    """
    with get_connection() as conn:
        with conn.cursor() as cur:
            cur.execute(sql, (next_fire_at, reminder_id, user_id))
            updated = cur.rowcount > 0
        conn.commit()
    return updated
