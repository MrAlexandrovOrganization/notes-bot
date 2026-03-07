# Telegram Notes Bot

Личный Telegram-бот для управления дневными заметками в формате Obsidian: текст, задачи, оценка дня, напоминания, голосовые сообщения.

## Возможности

- **Заметки** — сохранение текстовых сообщений в дневную заметку Obsidian (`Daily/DD-Mmm-YYYY.md`)
- **Задачи** — просмотр, отметка выполнения, добавление задач из заметки (`- [ ]` / `- [x]`)
- **Оценка дня** — запись числа от 0 до 10 в поле `Оценка:` в frontmatter заметки
- **Календарь** — навигация по месяцам, выбор любой даты, работа с заметкой за любой день
- **Напоминания** — создание напоминаний (once / daily / weekly / monthly) с хранением в PostgreSQL
- **Голос** — транскрибация голосовых и видео-сообщений через OpenAI Whisper

Интерфейс полностью на inline-кнопках. Авторизован единственный пользователь (`ROOT_ID`).

## Архитектура

6 сервисов в Docker, взаимодействие через gRPC и Kafka:

```
[Telegram Bot] ──gRPC──► [Core Service]          :50051
               ──gRPC──► [Notifications Service]  :50052
               ──gRPC──► [Whisper Service]         :50053
                                  │
                          [PostgreSQL]             :5432

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
```

| Сервис | Назначение |
|--------|-----------|
| `core` | Работа с заметками: задачи, оценки, контент |
| `notifications` | Напоминания: создание, расписание, хранение в БД, публикация в Kafka |
| `whisper` | Голос → текст (faster-whisper) |
| `telegram` | Telegram-бот, все обработчики, UI, consumer Kafka |
| `postgres` | База данных напоминаний |
| `kafka` | Очередь событий напоминаний (confluentinc/cp-kafka) |

Подробная документация для разработки — в `CLAUDE.md`.

## Быстрый старт

### 1. Создайте бота

1. [@BotFather](https://t.me/botfather) → `/newbot` → получите токен
2. Узнайте свой Telegram ID через [@userinfobot](https://t.me/userinfobot)

### 2. Настройте окружение

```bash
cp .env.example .env
```

Обязательные параметры в `.env`:

```env
BOT_TOKEN=your_bot_token_here
ROOT_ID=your_telegram_id_here

DB_NAME=notifications
DB_USER=notif
DB_PASSWORD=change_this_password

NOTES_DIR=/path/to/your/obsidian/vault
```

### 3. Запустите через Docker

```bash
docker-compose up -d
```

### 4. Альтернативно — локальный запуск

Требуется запущенный PostgreSQL и все три gRPC-сервиса. Для простого запуска используйте Docker.

```bash
poetry install
make run   # запускает только telegram-бота
```

## Структура проекта

```
notes_bot/
├── core/                         # Core gRPC сервис (заметки)
│   ├── main.py                   # Точка входа, порт 50051
│   ├── server.py                 # NotesServicer (10 RPC)
│   ├── notes.py                  # Чтение/запись markdown-файлов
│   ├── utils.py                  # get_today_filename() с TZ
│   ├── config.py                 # NOTES_DIR, TEMPLATE_DIR
│   └── features/
│       ├── rating.py             # Парсинг/обновление Оценка:
│       ├── tasks.py              # Парсинг/тоггл/добавление задач
│       └── calendar_ops.py       # Сканирование Daily/
│
├── notifications/                # Notifications gRPC сервис
│   ├── main.py                   # Точка входа, порт 50052
│   ├── server.py                 # NotificationsServicer (4 RPC)
│   ├── db.py                     # PostgreSQL CRUD
│   └── scheduler.py              # Фоновый поток, триггер напоминаний
│
├── whisper/                      # Whisper gRPC сервис
│   ├── main.py                   # Точка входа, порт 50053
│   └── server.py                 # TranscriptionServicer (1 RPC)
│
├── frontends/telegram/           # Telegram-бот
│   ├── bot.py                    # Инициализация, регистрация хендлеров
│   ├── kafka_consumer.py         # AIOKafkaConsumer, отправка напоминаний
│   ├── grpc_client.py            # CoreClient синглтон
│   ├── notifications_client.py   # NotificationsClient синглтон
│   ├── whisper_client.py         # WhisperClient синглтон
│   ├── middleware.py             # reply_message() абстракция
│   ├── utils.py                  # escape_markdown_v2()
│   ├── states/
│   │   ├── context.py            # UserContext dataclass + UserState enum
│   │   └── manager.py            # StateManager синглтон (in-memory)
│   ├── handlers/
│   │   ├── commands.py           # /start
│   │   ├── messages.py           # Текст, роутинг по состоянию
│   │   ├── callbacks.py          # Нажатия кнопок, навигация
│   │   ├── voice.py              # Голосовые/видео-сообщения
│   │   └── reminders.py          # Создание напоминаний (multi-step)
│   └── keyboards/
│       ├── main_menu.py          # 5 кнопок главного меню
│       ├── tasks.py              # Список задач с пагинацией
│       ├── calendar.py           # Месячный календарь
│       └── reminders.py          # Управление напоминаниями
│
├── proto/                        # gRPC определения
│   ├── notes.proto               # 10 RPC
│   ├── notifications.proto       # 4 RPC
│   ├── whisper.proto             # 1 RPC
│   └── *_pb2.py, *_pb2_grpc.py  # Сгенерированные стабы (make proto)
│
├── tests/                        # pytest, 64+ тестов
├── docker-compose.yml
├── Makefile
├── pyproject.toml                # Poetry зависимости
├── CLAUDE.md                     # Документация для AI-агентов
└── main.py                       # → frontends/telegram/bot.py
```

## Формат заметок

Заметки совместимы с Obsidian. Файлы: `$NOTES_DIR/Daily/DD-Mmm-YYYY.md`.

```markdown
---
date: "[[09-Nov-2025]]"
title: "[[09-Nov-2025]]"
tags:
  - daily
Оценка: 8
---
- [ ] Доброго утра!
- [x] Заполнить дневник [completion:: 2025-03-07]
- [ ] Новая задача
---

Текстовое сообщение 1
Текстовое сообщение 2
```

Три секции, разделённые `---`:
1. **YAML frontmatter** — метаданные Obsidian + поле `Оценка:`
2. **Задачи** — `- [ ]` невыполненные, `- [x]` выполненные
3. **Контент** — тексты сообщений, добавляются в конец

Шаблон: `$NOTES_DIR/Templates/Daily.md`

## Команды разработки

```bash
make test        # pytest + coverage
make proto       # регенерация gRPC стабов
make format      # ruff format + check
make up          # docker-compose build + up
make down        # docker-compose down
make logs        # docker-compose logs -f
make restart     # полный перезапуск с пересборкой
```

## Переменные окружения

| Переменная | Обязательно | По умолчанию | Описание |
|-----------|-------------|-------------|---------|
| `BOT_TOKEN` | ✅ | — | Токен Telegram-бота |
| `ROOT_ID` | ✅ | — | ID авторизованного пользователя |
| `NOTES_DIR` | ✅ | — | Путь к папке с заметками |
| `DB_NAME` | ✅ | — | Имя базы данных |
| `DB_USER` | ✅ | — | Пользователь БД |
| `DB_PASSWORD` | ✅ | — | Пароль БД |
| `TEMPLATE_SUBDIR` | — | `Templates` | Путь к шаблонам (от NOTES_DIR) |
| `TIMEZONE_OFFSET_HOURS` | — | `3` | UTC offset (Москва) |
| `DAY_START_HOUR` | — | `7` | Час начала нового дня |
| `WHISPER_MODEL` | — | `base` | Размер модели Whisper |

## Безопасность

- Бот принимает сообщения **только** от пользователя с `ROOT_ID`
- PostgreSQL доступна **только внутри Docker-сети** (порты не проброшены)
- Файл `.env` в `.gitignore`
- **ОБЯЗАТЕЛЬНО** смените `DB_PASSWORD` в production

## Технологии

- **Python 3.11**, Poetry
- **python-telegram-bot 21.0** (polling)
- **gRPC** (grpcio 1.62) — межсервисное взаимодействие
- **PostgreSQL 16** + psycopg2 — напоминания
- **faster-whisper 1.0** — транскрибация речи
- **Kafka** (confluentinc/cp-kafka) + kafka-python + aiokafka — очередь напоминаний
- **Docker & Docker Compose** — контейнеризация
- **pytest** — тестирование
- **ruff** — форматирование
