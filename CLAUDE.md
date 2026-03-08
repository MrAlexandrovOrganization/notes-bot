# Notes Bot — Context for AI Assistants

A personal Telegram bot for managing daily Obsidian-style markdown notes, tasks, ratings, reminders, and voice-to-text transcription.

## Quick Reference

```bash
make test-go          # Run Go unit tests (core + notifications)
make test-go-cover    # Go unit tests + coverage report
make cover-all        # Combined unit + integration coverage
make test-integration # Integration tests (requires running services)
make test-notifications # Notifications package tests
make proto            # Regenerate gRPC stubs from proto/*.proto
make format           # gofmt (Go) + ruff (whisper Python)
make up               # docker-compose down + build + up + logs
```

## Architecture: 7 Services

```
[Telegram Bot] ──gRPC──► [Core Service]         :50051
               ──gRPC──► [Notifications Service] :50052
               ──gRPC──► [Whisper Service]        :50053
               ──────────[Redis]                  :6379  (user state)
                                 │
                         [PostgreSQL :5432]

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
```

All services run in Docker (`docker-compose.yml`). Startup order: postgres + redis → core → kafka → notifications + telegram.

Health checks use `grpc.health.v1`. Core, Notifications, and Telegram use `grpc_health_probe` binary. Whisper uses `whisper/healthcheck.py`.

## Service Map

| Service | Language | Entry Point | Port | Purpose |
|---------|----------|------------|------|---------|
| core | Go | `cmd/core/main.go` | 50051 | Notes CRUD, tasks, ratings |
| notifications | Go | `cmd/notifications/main.go` | 50052 | Reminders with DB persistence, publishes to Kafka |
| whisper | Python | `whisper/main.py` | 50053 | Voice→text via faster-whisper |
| telegram | Go | `cmd/telegram/main.go` | — | User-facing Telegram bot, Kafka consumer |
| postgres | docker image | — | 5432 | Reminders storage |
| kafka | confluentinc/cp-kafka | — | 9092 | Reminder event queue |
| redis | redis:7-alpine | — | 6379 | User state persistence (TTL 7 days) |

## Key Files

### Core Service (`core/`)
- `core/server.go` — `NewNotesServer()`, implements all 10 gRPC RPCs
- `core/stores.go` — 4 DI interfaces: `CalendarStore`, `NoteStore`, `RatingStore`, `TaskStore`
- `core/notes.go` — File I/O: create note from template, append text
- `core/features/rating.go` — Parse/update `Оценка:` in YAML frontmatter
- `core/features/tasks.go` — Parse `- [ ]`/`- [x]`, toggle, add
- `core/features/calendar_ops.go` — Scan `Daily/` for existing dates
- `cmd/core/main.go` — gRPC server entry point

### Notifications Service (`notifications/`)
- `notifications/server.go` — `NotificationsServer`, 4 gRPC RPCs
- `notifications/db.go` — PostgreSQL CRUD via pgx/v5 (EnsureSchema, CreateReminder, ListReminders, DeleteReminder, GetDueReminders, UpdateNextFire, SetNextFireAt)
- `notifications/scheduler.go` — `ComputeNextFire()` for 6 schedule types; `Scheduler.Run()` goroutine publishing to Kafka topic `reminders_due`
- `notifications/config.go` — `LoadConfig()`, `Config` struct, `DSN()` helper
- `notifications/scheduler_test.go` — unit tests for all schedule types
- `cmd/notifications/main.go` — entry point

### Whisper Service (`whisper/`) — Python only
- `whisper/server.py` — `TranscriptionServicer`, 1 RPC (`Transcribe`); model loaded eagerly at `__init__`
- `whisper/main.py` — gRPC server, sets SERVING only after model is fully loaded

### Telegram Frontend (`frontends/telegram/`)
- `cmd/telegram/main.go` — entry point, polling loop, goroutine for Kafka consumer
- `frontends/telegram/config/config.go` — `Load()` returns `*Config`
- `frontends/telegram/clients/core.go` — `CoreClient` (10 methods)
- `frontends/telegram/clients/notifications.go` — `NotificationsClient` (4 methods)
- `frontends/telegram/clients/whisper.go` — `WhisperClient` (50MB max message)
- `frontends/telegram/tgstates/context.go` — `UserState` constants + `UserContext` struct
- `frontends/telegram/tgstates/manager.go` — `StateManager` backed by Redis (JSON, TTL 7 days)
- `frontends/telegram/tgkeyboards/` — `MainMenu`, `Tasks`, `Calendar`, `RemindersList`, `ScheduleType`, `ReminderCalendar` keyboards
- `frontends/telegram/tghandlers/app.go` — `App` struct with all clients + state manager
- `frontends/telegram/tghandlers/commands.go` — `/start` with ROOT_ID check
- `frontends/telegram/tghandlers/messages.go` — text routing by `UserState`
- `frontends/telegram/tghandlers/callbacks.go` — callback_data routing
- `frontends/telegram/tghandlers/voice.go` — Voice/VideoNote → Whisper → append to note
- `frontends/telegram/tghandlers/reminders.go` — multi-step reminder creation wizard
- `frontends/telegram/bot/kafka_consumer.go` — `RunKafkaConsumer()` goroutine
- `frontends/telegram/bot/utils.go` — `EscapeMarkdownV2()`

### Proto / gRPC (`proto/`)
- `proto/notes.proto` — 10 RPCs for notes
- `proto/notifications.proto` — 4 RPCs for reminders
- `proto/whisper.proto` — 1 RPC for transcription
- `proto/notes/`, `proto/notifications/`, `proto/whisper/` — generated Go stubs
- `proto/whisper_pb2.py`, `proto/whisper_pb2_grpc.py` — generated Python stubs (Whisper service only)
- `proto/__init__.py` — makes proto a Python package (needed for `from proto import whisper_pb2_grpc`)

## Note File Format

Notes live in `$NOTES_DIR/Daily/DD-Mmm-YYYY.md` (e.g. `09-Nov-2025.md`).

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
- [ ] New task
---

Text message 1
Text message 2
```

Three sections separated by `---`:
1. **YAML frontmatter** — date, title, tags, `Оценка:` (rating 0-10)
2. **Tasks** — `- [ ]` incomplete, `- [x]` completed (with optional `[completion:: date]`)
3. **Content** — plain text messages appended line by line

Template: `$NOTES_DIR/Templates/Daily.md` (uses `{{date:DD-MMM-YYYY}}` placeholders)

## User State Machine

```go
const (
    StateIdle                       UserState = "idle"
    StateWaitingRating              UserState = "waiting_rating"
    StateTasksView                  UserState = "tasks_view"
    StateWaitingNewTask             UserState = "waiting_new_task"
    StateCalendarView               UserState = "calendar_view"
    StateReminderList               UserState = "reminder_list"
    StateReminderCreateTitle        UserState = "reminder_create_title"
    StateReminderCreateScheduleType UserState = "reminder_create_schedule_type"
    StateReminderCreateTime         UserState = "reminder_create_time"
    StateReminderCreateDay          UserState = "reminder_create_day"
    StateReminderCreateInterval     UserState = "reminder_create_interval"
    StateReminderCreateDate         UserState = "reminder_create_date"
    StateReminderPostponeDate       UserState = "reminder_postpone_date"
    StateReminderCreateTaskConfirm  UserState = "reminder_create_task_confirm"
)
```

`UserContext` stores: `user_id`, `state`, `active_date` (DD-MMM-YYYY), `calendar_month/year`, `task_page`, `last_message_id`, `reminder_draft` map, `pending_postpone_reminder_id`, `reminder_cal_month/year`, `reminder_list_page`.

State is persisted in **Redis** — survives bot restarts (TTL 7 days). Key: `user_state:{user_id}`.

## Callback Data Format

```
"menu:rating"              # Main menu → rating
"menu:tasks"               # Main menu → tasks
"task:toggle:0"            # Toggle task index 0
"task:add"                 # Add task
"task:page:1"              # Tasks pagination
"cal:select:09-Nov-2025"   # Calendar date pick
"cal:prev" / "cal:next"    # Month navigation
"nav:menu"                 # Back to main menu
"reminder:list"            # Reminder list
"reminder:delete:42"       # Delete reminder id=42
"reminder:postpone:42"     # Postpone reminder (days menu)
"reminder:postpone_hours:1:42"  # Postpone by N hours
"reminder:done:42:0"       # Mark done (no task)
"reminder:done:42:1:DD-MMM-YYYY" # Mark done (create task for date)
"reminder:custom_date:42"  # Pick custom postpone date
"reminder:cal:select:YYYY-MM-DD:create" # Calendar date for creation
```

## Environment Variables

### Required for all setups
```env
BOT_TOKEN=<telegram_bot_token>
ROOT_ID=<telegram_user_id>          # Only this user can use the bot
NOTES_DIR=/path/to/obsidian/vault
DB_NAME=notifications
DB_USER=notif
DB_PASSWORD=<strong_password>
```

### Optional (defaults shown)
```env
TIMEZONE_OFFSET_HOURS=3     # UTC+3 Moscow
DAY_START_HOUR=7             # Day "starts" at 7 AM
TEMPLATE_SUBDIR=Templates    # Relative to NOTES_DIR
GRPC_PORT=50051              # core / notifications / whisper (per service)
CORE_GRPC_HOST=core
CORE_GRPC_PORT=50051
NOTIFICATIONS_GRPC_HOST=notifications
NOTIFICATIONS_GRPC_PORT=50052
WHISPER_GRPC_HOST=whisper
WHISPER_GRPC_PORT=50053
WHISPER_MODEL=base            # small/base/medium/large/turbo
SCHEDULER_INTERVAL_SECONDS=60
KAFKA_BOOTSTRAP_SERVERS=kafka:9092
REDIS_HOST=redis
REDIS_PORT=6379
```

## Conventions and Patterns

### Adding a new gRPC method (Go)
1. Add to the relevant `proto/*.proto` file
2. Run `make proto`
3. Implement in the service's `server.go`
4. Add method to the corresponding client in `frontends/telegram/clients/`
5. Call from a handler

### Adding a new Telegram feature
1. Add new `UserState` constants if multi-step in `tgstates/context.go`
2. Add new fields to `UserContext` if needed
3. Create/update keyboard in `tgkeyboards/`
4. Add handler function in appropriate `tghandlers/` file
5. Register routing in `tghandlers/messages.go` or `tghandlers/callbacks.go`
6. Wire up in `cmd/telegram/main.go` if needed

### Go package naming
Telegram bot sub-packages use prefixed names to avoid conflicts:
- `tgstates` — user state types and Redis manager
- `tgkeyboards` — inline keyboard builders
- `tghandlers` — update/callback handlers + `App` struct

### Markdown
All Telegram messages use **MarkdownV2**. Always wrap user-provided text in `EscapeMarkdownV2()` from `frontends/telegram/bot/utils.go`.

### Timezone
Day boundary is at `DAY_START_HOUR` (7 AM), not midnight. Applied in `core/utils.go` and `tgstates/manager.go`. Consistency is important across all services.

## Testing

```bash
make test-go               # Go unit tests: core + notifications
make test-notifications    # Notifications package tests (verbose)
make test-integration      # Integration tests (needs running services)
make cover-all             # Combined unit + integration coverage
make test-go-cover         # Unit tests with coverage report
```

Go tests:
- `core/` + `core/features/` — unit tests, no gRPC
- `notifications/scheduler_test.go` — ComputeNextFire for all 6 schedule types
- `integration/core_test.go` — 22 integration tests

Python tests: none (whisper has no automated tests).

## Notes Volume Structure (expected)

```
$NOTES_DIR/
├── Daily/                    # Auto-created; one file per day
│   ├── 09-Nov-2025.md
│   └── ...
└── Templates/
    └── Daily.md              # Template with {{date:DD-MMM-YYYY}} placeholders
```
