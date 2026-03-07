"""Kafka consumer for reminder events."""

import asyncio
import json
import logging
import os
from typing import Optional

from aiokafka import AIOKafkaConsumer
from aiokafka.errors import KafkaError
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import Application

logger = logging.getLogger(__name__)

KAFKA_BOOTSTRAP_SERVERS = os.environ.get("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092")
TOPIC = "reminders_due"

_consumer_task: Optional[asyncio.Task] = None


def _build_keyboard(
    reminder_id: int, create_task: bool, today_date: str
) -> InlineKeyboardMarkup:
    done_cb = (
        f"reminder:done:{reminder_id}:1:{today_date}"
        if create_task and today_date
        else f"reminder:done:{reminder_id}:0"
    )
    return InlineKeyboardMarkup(
        [
            [InlineKeyboardButton("✅ Принято", callback_data=done_cb)],
            [
                InlineKeyboardButton(
                    "+1 ч", callback_data=f"reminder:postpone_hours:1:{reminder_id}"
                ),
                InlineKeyboardButton(
                    "+3 ч", callback_data=f"reminder:postpone_hours:3:{reminder_id}"
                ),
            ],
            [
                InlineKeyboardButton(
                    "+1 д", callback_data=f"reminder:postpone:1:{reminder_id}"
                ),
                InlineKeyboardButton(
                    "+3 д", callback_data=f"reminder:postpone:3:{reminder_id}"
                ),
            ],
            [
                InlineKeyboardButton(
                    "📅 Выбрать дату",
                    callback_data=f"reminder:custom_date:{reminder_id}",
                )
            ],
        ]
    )


async def _handle_reminder_event(app: Application, event: dict) -> None:
    user_id = event["user_id"]
    title = event["title"]
    reminder_id = event["reminder_id"]
    create_task = event.get("create_task", False)
    today_date = event.get("today_date", "")

    keyboard = _build_keyboard(reminder_id, create_task, today_date)
    try:
        await app.bot.send_message(
            chat_id=user_id,
            text=f"🔔 Напоминание: {title}",
            reply_markup=keyboard,
        )
        logger.info(
            f"Sent reminder notification to user {user_id}, reminder {reminder_id}"
        )
    except Exception as e:
        logger.error(f"Failed to send reminder to user {user_id}: {e}")


async def _consume_loop(app: Application) -> None:
    while True:
        consumer = AIOKafkaConsumer(
            TOPIC,
            bootstrap_servers=KAFKA_BOOTSTRAP_SERVERS,
            auto_offset_reset="latest",
            enable_auto_commit=True,
            group_id="telegram-bot",
        )
        try:
            await consumer.start()
            logger.info("Kafka consumer started")
            async for msg in consumer:
                try:
                    event = json.loads(msg.value.decode("utf-8"))
                    await _handle_reminder_event(app, event)
                except Exception as e:
                    logger.error(f"Error processing Kafka message: {e}")
        except KafkaError as e:
            logger.warning(f"Kafka consumer error, retrying in 5s: {e}")
            await asyncio.sleep(5)
        except asyncio.CancelledError:
            await consumer.stop()
            raise
        finally:
            try:
                await consumer.stop()
            except Exception:
                pass


async def start_kafka_consumer(app: Application) -> None:
    global _consumer_task
    _consumer_task = asyncio.create_task(_consume_loop(app))
    logger.info("Kafka consumer task created")


async def stop_kafka_consumer(app: Application) -> None:
    global _consumer_task
    if _consumer_task is not None:
        _consumer_task.cancel()
        try:
            await _consumer_task
        except asyncio.CancelledError:
            pass
        _consumer_task = None
        logger.info("Kafka consumer task stopped")
