# Telegram Notes Bot

Личный Telegram-бот для управления дневными заметками в формате Obsidian: текст, задачи, оценка дня, напоминания, голосовые сообщения.

## Возможности

- **Заметки** — сохранение текстовых сообщений в дневную заметку Obsidian (`Daily/DD-Mmm-YYYY.md`)
- **Задачи** — просмотр, отметка выполнения, добавление задач из заметки (`- [ ]` / `- [x]`)
- **Оценка дня** — запись числа от 0 до 10 в поле `Оценка:` в frontmatter заметки
- **Календарь** — навигация по месяцам, выбор любой даты, работа с заметкой за любой день
- **Напоминания** — создание напоминаний с расписанием (once / daily / weekly / monthly / yearly / каждые N дней), хранение в PostgreSQL
- **Голос** — транскрибация голосовых и видео-сообщений через faster-whisper

Интерфейс полностью на inline-кнопках. Авторизован единственный пользователь (`ROOT_ID`).

## Архитектура

7 сервисов в Docker, взаимодействие через gRPC и Kafka:

```
[Telegram Bot] ──gRPC──► [Core Service]          :50051
               ──gRPC──► [Notifications Service]  :50052
               ──gRPC──► [Whisper Service]         :50053
               ──────────[Redis]                   :6379  (состояние пользователей)
                                  │
                          [PostgreSQL]              :5432

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
```

| Сервис | Язык | Назначение |
|--------|------|-----------|
| `core` | Go | Работа с заметками: задачи, оценки, контент |
| `notifications` | Go | Напоминания: создание, расписание, хранение в БД, публикация в Kafka |
| `whisper` | Python | Голос → текст (faster-whisper) |
| `telegram` | Go | Telegram-бот, все обработчики, UI, consumer Kafka |
| `postgres` | — | База данных напоминаний |
| `kafka` | — | Очередь событий напоминаний |
| `redis` | — | Состояние пользователей (TTL 7 дней) |

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

## Структура проекта

```
notes_bot/
├── cmd/
│   ├── core/main.go              # Точка входа core-сервиса (gRPC :50051)
│   ├── notifications/main.go     # Точка входа notifications-сервиса (gRPC :50052)
│   └── telegram/main.go          # Точка входа Telegram-бота
│
├── core/                         # Core gRPC сервис (заметки) — Go
│   ├── server.go                 # NotesServer (10 RPC)
│   ├── stores.go                 # DI-интерфейсы: CalendarStore, NoteStore, RatingStore, TaskStore
│   ├── notes.go                  # Чтение/запись markdown-файлов
│   ├── utils.go                  # get_today_filename() с TZ
│   └── features/
│       ├── rating.go             # Парсинг/обновление Оценка:
│       ├── tasks.go              # Парсинг/тоггл/добавление задач
│       └── calendar_ops.go       # Сканирование Daily/
│
├── notifications/                # Notifications gRPC сервис — Go
│   ├── server.go                 # NotificationsServer (4 RPC)
│   ├── db.go                     # PostgreSQL CRUD (pgx/v5)
│   ├── scheduler.go              # ComputeNextFire() + Scheduler.Run()
│   ├── config.go                 # LoadConfig()
│   └── scheduler_test.go         # Unit tests
│
├── whisper/                      # Whisper gRPC сервис — Python
│   ├── main.py                   # Точка входа, порт 50053
│   └── server.py                 # TranscriptionServicer (1 RPC)
│
├── frontends/telegram/           # Telegram-бот — Go
│   ├── config/config.go          # Load() — конфигурация бота
│   ├── clients/
│   │   ├── core.go               # CoreClient (10 методов)
│   │   ├── notifications.go      # NotificationsClient (4 метода)
│   │   └── whisper.go            # WhisperClient (50MB max)
│   ├── tgstates/
│   │   ├── context.go            # UserState + UserContext
│   │   └── manager.go            # StateManager (Redis backend)
│   ├── tgkeyboards/              # Inline-клавиатуры
│   ├── tghandlers/               # Обработчики обновлений
│   │   ├── app.go                # App struct (все зависимости)
│   │   ├── commands.go           # /start
│   │   ├── messages.go           # Текст, роутинг по состоянию
│   │   ├── callbacks.go          # Нажатия кнопок
│   │   ├── voice.go              # Голосовые/видео-сообщения
│   │   └── reminders.go          # Создание напоминаний (multi-step)
│   └── bot/
│       ├── kafka_consumer.go     # RunKafkaConsumer()
│       └── utils.go              # EscapeMarkdownV2()
│
├── proto/                        # gRPC определения
│   ├── notes.proto               # 10 RPC
│   ├── notifications.proto       # 4 RPC
│   ├── whisper.proto             # 1 RPC
│   ├── notes/, notifications/, whisper/  # Сгенерированные Go stubs
│   └── whisper_pb2*.py           # Сгенерированные Python stubs (только для Whisper)
│
├── integration/                  # Integration tests (Go)
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
├── pyproject.toml                # Poetry (только для Whisper)
└── CLAUDE.md                     # Документация для AI-агентов
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
make test-go          # Go unit тесты (core + notifications)
make test-go-cover    # Unit тесты + coverage
make cover-all        # Суммарное покрытие (unit + integration)
make test-integration # Integration тесты
make proto            # Регенерация gRPC stubs
make format           # gofmt + ruff
make up               # docker-compose build + up
make down             # docker-compose down
make logs             # docker-compose logs -f
make restart          # Полный перезапуск с пересборкой
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
| `REDIS_HOST` | — | `redis` | Redis хост |
| `KAFKA_BOOTSTRAP_SERVERS` | — | `kafka:9092` | Kafka брокер |

## Безопасность

- Бот принимает сообщения **только** от пользователя с `ROOT_ID`
- PostgreSQL доступна **только внутри Docker-сети** (порты не проброшены)
- Файл `.env` в `.gitignore`
- **ОБЯЗАТЕЛЬНО** смените `DB_PASSWORD` в production

## Технологии

- **Go 1.24** — core, notifications, telegram
- **Python 3.11** — whisper (faster-whisper, нет Go-альтернативы)
- **gRPC** (grpcio / google.golang.org/grpc) — межсервисное взаимодействие
- **PostgreSQL 16** + pgx/v5 — напоминания
- **faster-whisper 1.0** — транскрибация речи
- **Kafka** (confluentinc/cp-kafka) + segmentio/kafka-go — очередь напоминаний
- **Redis 7** + go-redis/v9 — состояние пользователей
- **Docker & Docker Compose** — контейнеризация
