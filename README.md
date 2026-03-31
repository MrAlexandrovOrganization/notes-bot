# Telegram Notes Bot

Личный Telegram-бот для управления дневными заметками в формате Obsidian: текст, задачи, оценка дня, напоминания, голосовые сообщения и создание напоминаний через естественный язык.

## Возможности

- **Заметки** — сохранение текстовых сообщений в дневную заметку Obsidian (`Daily/DD-Mmm-YYYY.md`)
- **Задачи** — просмотр, отметка выполнения, добавление задач из заметки (`- [ ]` / `- [x]`)
- **Оценка дня** — запись числа от 0 до 10 в поле `Оценка:` в frontmatter заметки
- **Календарь** — навигация по месяцам, выбор любой даты, работа с заметкой за любой день
- **Напоминания** — создание напоминаний с расписанием (once / daily / weekly / monthly / yearly / каждые N дней), хранение в PostgreSQL
- **Создание напоминаний через ИИ** — описание напоминания произвольным текстом, разбор через локальный LLM (Ollama)
- **Голос** — транскрибация голосовых и видео-сообщений через faster-whisper

Интерфейс полностью на inline-кнопках. Авторизован единственный пользователь (`ROOT_ID`).

## Архитектура

7 основных сервисов + 8 сервисов инструментирования в Docker, взаимодействие через gRPC, Kafka и HTTP:

```
[Telegram Bot] ──gRPC──► [Core Service]          :50051
               ──gRPC──► [Notifications Service]  :50052
               ──gRPC──► [Whisper Service]         :50053
               ──HTTP──► [Ollama LLM]              :11434
               ──────────[Redis]                   :6379  (состояние пользователей)
                                  │
                          [PostgreSQL]              :5432

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
```

### Основные сервисы

| Сервис | Язык | Назначение |
|--------|------|-----------|
| `core` | Go | Работа с заметками: задачи, оценки, контент |
| `notifications` | Go | Напоминания: создание, расписание, хранение в БД, публикация в Kafka |
| `whisper` | Python | Голос → текст (faster-whisper) |
| `telegram` | Go | Telegram-бот, все обработчики, UI, consumer Kafka |
| `postgres` | — | База данных напоминаний |
| `kafka` | — | Очередь событий напоминаний (Kafka 4.0, KRaft) |
| `redis` | — | Состояние пользователей (TTL 7 дней) |
| `ollama` | — | Локальная LLM для разбора напоминаний на естественном языке |

### Сервисы инструментирования (localhost-only)

| Сервис | Порт | Назначение |
|--------|------|-----------|
| `jaeger` | 16686 | Распределённый трейсинг (UI) |
| `prometheus` | 9090 | Сбор метрик |
| `grafana` | 3000 | Дашборды метрик |
| `redisinsight` | 5540 | Redis GUI |
| `kafka-ui` | 8080 | Kafka GUI |
| `pgadmin` | 5050 | PostgreSQL GUI |
| `open-webui` | 3001 | Чат-интерфейс для Ollama |

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

Опциональные параметры для инструментирования:

```env
PGADMIN_EMAIL=admin@example.com
PGADMIN_PASSWORD=change_this_password
GRAFANA_PASSWORD=change_this_password
OLLAMA_MODEL=qwen2.5:1.5b   # Модель для разбора напоминаний на естественном языке
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
│   ├── utils.go                  # TodayDate() с TZ
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
│   ├── metrics.go                # Prometheus метрики
│   └── scheduler_test.go         # Unit tests
│
├── whisper/                      # Whisper gRPC сервис — Python
│   ├── main.py                   # Точка входа, порт 50053
│   └── server.py                 # TranscriptionServicer (1 RPC)
│
├── frontends/telegram/           # Telegram-бот — Go
│   ├── config/config.go          # Load() — конфигурация бота (включая LLM)
│   ├── clients/
│   │   ├── interfaces.go         # CoreService, NotificationsService, WhisperService, LLMService
│   │   ├── core.go               # CoreClient (10 методов)
│   │   ├── notifications.go      # NotificationsClient (4 метода)
│   │   ├── whisper.go            # WhisperClient (50MB max)
│   │   └── llm.go                # LLMClient → Ollama /api/chat (structured output)
│   ├── tgstates/
│   │   ├── context.go            # UserState + UserContext
│   │   ├── manager.go            # StateManager (Redis backend)
│   │   └── draft.go              # ReminderDraft struct + ToParamsJSON()
│   ├── tgkeyboards/              # Inline-клавиатуры
│   ├── tghandlers/               # Обработчики обновлений
│   │   ├── app.go                # App struct (все зависимости + LLM)
│   │   ├── commands.go           # /start
│   │   ├── messages.go           # Текст, роутинг по состоянию (stateTextHandlers map)
│   │   ├── callbacks.go          # Нажатия кнопок
│   │   ├── voice.go              # Голосовые/видео-сообщения
│   │   ├── reminders.go          # Создание напоминаний (multi-step + NL)
│   │   ├── kafka.go              # MakeReminderHandler()
│   │   └── middleware.go         # EscapeMarkdownV2(), sendText(), editText()
│   └── bot/
│       ├── kafka_consumer.go     # RunKafkaConsumer()
│       └── metrics.go            # Prometheus метрики telegram-сервиса
│
├── internal/
│   ├── applog/applog.go          # New() + With(ctx, logger) — zap + OTel
│   ├── telemetry/
│   │   ├── tracer.go             # InitTracer() — OTel + Jaeger
│   │   └── metrics.go            # InitMetrics() — Prometheus exporter
│   ├── kafkacarrier/carrier.go   # W3C trace context в Kafka headers
│   └── timeutil/timeutil.go      # FixedZone, LocalNow, TodayDate, FormatLocalTime
│
├── proto/                        # gRPC определения
│   ├── notes.proto               # 10 RPC
│   ├── notifications.proto       # 4 RPC
│   ├── whisper.proto             # 1 RPC
│   ├── notes/, notifications/, whisper/  # Сгенерированные Go stubs
│   └── whisper_pb2*.py           # Сгенерированные Python stubs (только для Whisper)
│
├── integration/                  # Integration tests (Go, 22 теста)
├── grafana/                      # Grafana provisioning и дашборды
├── prometheus.yml                # Prometheus конфиг (scrape targets)
├── jaeger.yaml                   # Jaeger конфиг
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
make test-go          # Go unit тесты (core + notifications + telegram handlers)
make test-go-cover    # Unit тесты + coverage
make cover            # Суммарное покрытие (unit + integration)
make cover-html       # Coverage HTML отчёт (открывает браузер)
make test-integration # Integration тесты
make proto            # Регенерация gRPC stubs
make format           # gofmt + ruff
make up               # docker-compose build + up
make down             # docker-compose down
make logs             # docker-compose logs -f
make restart          # Полный перезапуск с пересборкой
make build-core       # Пересборка core образа
make build-notifications # Пересборка notifications образа
make build-telegram   # Пересборка telegram образа
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
| `PGADMIN_EMAIL` | — | — | Email для pgAdmin UI |
| `PGADMIN_PASSWORD` | — | — | Пароль для pgAdmin UI |
| `GRAFANA_PASSWORD` | — | — | Пароль для Grafana |
| `OLLAMA_MODEL` | — | `qwen2.5:1.5b` | LLM модель для разбора напоминаний |
| `TEMPLATE_SUBDIR` | — | `Templates` | Путь к шаблонам (от NOTES_DIR) |
| `TIMEZONE_OFFSET_HOURS` | — | `3` | UTC offset (Москва) |
| `DAY_START_HOUR` | — | `7` | Час начала нового дня |
| `WHISPER_MODEL` | — | `base` | Размер модели Whisper |
| `REDIS_HOST` | — | `redis` | Redis хост |
| `KAFKA_BOOTSTRAP_SERVERS` | — | `kafka:9092` | Kafka брокер |
| `LLM_HOST` | — | `ollama` | Ollama хост |
| `LLM_PORT` | — | `11434` | Ollama порт |
| `LLM_MODEL` | — | `qwen2.5:1.5b` | Ollama модель |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | — | Jaeger OTLP endpoint (не задан = трейсинг выключен) |

## Безопасность

- Бот принимает сообщения **только** от пользователя с `ROOT_ID`
- PostgreSQL доступна **только внутри Docker-сети** (порты не проброшены)
- Все UI-инструменты (Grafana, pgAdmin и др.) привязаны к `127.0.0.1`
- Файл `.env` в `.gitignore`
- **ОБЯЗАТЕЛЬНО** смените все пароли в production

## Технологии

- **Go 1.25** — core, notifications, telegram
- **Python 3.11** — whisper (faster-whisper, нет Go-альтернативы)
- **gRPC** (grpcio / google.golang.org/grpc) — межсервисное взаимодействие
- **PostgreSQL 16** + pgx/v5 — напоминания
- **faster-whisper** — транскрибация речи
- **Kafka 4.0** (confluentinc/cp-kafka, KRaft) + segmentio/kafka-go — очередь напоминаний
- **Redis 7** + go-redis/v9 — состояние пользователей
- **Ollama** + qwen2.5 — локальная LLM для разбора напоминаний
- **OpenTelemetry** + Jaeger — распределённый трейсинг
- **Prometheus** + Grafana — метрики и дашборды
- **Docker & Docker Compose** — контейнеризация
