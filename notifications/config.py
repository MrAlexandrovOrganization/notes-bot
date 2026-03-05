"""Configuration for the notifications service."""

import os

DB_HOST = os.getenv("DB_HOST", "localhost")
DB_PORT = int(os.getenv("DB_PORT", "5432"))
DB_NAME = os.getenv("DB_NAME", "notifications")
DB_USER = os.getenv("DB_USER", "notif")
DB_PASSWORD = os.getenv("DB_PASSWORD", "notif")

BOT_TOKEN = os.getenv("BOT_TOKEN", "")
ROOT_ID = int(os.getenv("ROOT_ID", "0")) or None

TIMEZONE_OFFSET_HOURS = int(os.getenv("TIMEZONE_OFFSET_HOURS", "0"))

GRPC_PORT = os.getenv("GRPC_PORT", "50052")
SCHEDULER_INTERVAL_SECONDS = int(os.getenv("SCHEDULER_INTERVAL_SECONDS", "60"))

CORE_GRPC_HOST = os.getenv("CORE_GRPC_HOST", "localhost")
CORE_GRPC_PORT = os.getenv("CORE_GRPC_PORT", "50051")
