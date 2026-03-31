# Notes Bot — Context for AI Assistants

A personal Telegram bot for managing daily Obsidian-style markdown notes, tasks, ratings, reminders, and voice-to-text transcription. Supports natural-language reminder creation via a local LLM (Ollama).

## Quick Reference

```bash
make test-go          # Run Go unit tests (core + notifications + telegram handlers)
make test-go-cover    # Go unit tests + coverage report
make cover            # Combined unit + integration coverage
make test-integration # Integration tests (requires running services)
make test-notifications # Notifications package tests
make proto            # Regenerate gRPC stubs from proto/*.proto
make format           # gofmt (Go) + ruff (whisper Python)
make up               # docker-compose build + up + logs
make build-core       # Rebuild core service image
make build-notifications # Rebuild notifications image
make build-telegram   # Rebuild telegram image
```

## Architecture: 7 Application Services + 8 Tooling Services

```
[Telegram Bot] ──gRPC──► [Core Service]         :50051
               ──gRPC──► [Notifications Service] :50052
               ──gRPC──► [Whisper Service]        :50053
               ──HTTP──► [Ollama LLM]             :11434
               ──────────[Redis]                  :6379  (user state)
                                 │
                         [PostgreSQL :5432]

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
```

All services run in Docker (`docker-compose.yml`). Startup order: postgres + redis + jaeger → core → kafka → notifications + telegram.

Health checks use `grpc.health.v1`. Core, Notifications, and Telegram use `grpc_health_probe` binary. Whisper uses `whisper/healthcheck.py`. Ollama uses `ollama list`.

### Application Services

| Service | Language | Entry Point | Port | Purpose |
|---------|----------|------------|------|---------|
| core | Go | `cmd/core/main.go` | 50051 / metrics: 9100 | Notes CRUD, tasks, ratings |
| notifications | Go | `cmd/notifications/main.go` | 50052 / metrics: 9101 | Reminders with DB persistence, publishes to Kafka |
| whisper | Python | `whisper/main.py` | 50053 | Voice→text via faster-whisper |
| telegram | Go | `cmd/telegram/main.go` | metrics: 9102 | User-facing Telegram bot, Kafka consumer |
| postgres | docker image | — | 5432 | Reminders storage |
| kafka | confluentinc/cp-kafka:8.2.0 (=Kafka 4.0, KRaft) | — | 9092 | Reminder event queue |
| redis | redis:7-alpine | — | 6379 | User state (TTL 7 days) |
| ollama | ollama/ollama:0.18.2 | — | 11434 | Local LLM for NL reminder parsing |

### Tooling Services (localhost-only ports)

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| jaeger | jaegertracing/jaeger:2.4.0 | 16686 (UI), 4317 (OTLP) | Distributed tracing |
| prometheus | prom/prometheus:v3.4.0 | 9090 | Metrics scraping |
| grafana | grafana/grafana:12.0.0 | 3000 | Metrics dashboards |
| redisinsight | redis/redisinsight:3.2 | 5540 | Redis GUI |
| kafka-ui | provectuslabs/kafka-ui:v0.7.2 | 8080 | Kafka GUI |
| pgadmin | dpage/pgadmin4 | 5050 | PostgreSQL GUI |
| open-webui | ghcr.io/open-webui/open-webui | 3001 | Ollama chat UI |

## Key Files

### Core Service (`core/`)
- `core/server.go` — `NewNotesServer()`, implements all 10 gRPC RPCs
- `core/stores.go` — 4 DI interfaces: `CalendarStore`, `NoteStore`, `RatingStore`, `TaskStore`
- `core/notes.go` — File I/O: create note from template, append text
- `core/utils.go` — `TodayDate()`, timezone helpers
- `core/features/rating.go` — Parse/update `Оценка:` in YAML frontmatter
- `core/features/tasks.go` — Parse `- [ ]`/`- [x]`, toggle, add
- `core/features/calendar_ops.go` — Scan `Daily/` for existing dates
- `cmd/core/main.go` — gRPC server entry point

### Notifications Service (`notifications/`)
- `notifications/server.go` — `NotificationsServer`, 4 gRPC RPCs
- `notifications/db.go` — PostgreSQL CRUD via pgx/v5 (EnsureSchema, CreateReminder, ListReminders, DeleteReminder, GetDueReminders, UpdateNextFire, SetNextFireAt)
- `notifications/scheduler.go` — `ComputeNextFire()` for 6 schedule types; `Scheduler.Run()` goroutine publishing to Kafka topic `reminders_due`
- `notifications/config.go` — `LoadConfig()`, `Config` struct, `DSN()` helper
- `notifications/metrics.go` — Prometheus metrics for notifications service
- `notifications/scheduler_test.go` — unit tests for all schedule types
- `cmd/notifications/main.go` — entry point

### Whisper Service (`whisper/`) — Python only
- `whisper/server.py` — `TranscriptionServicer`, 1 RPC (`Transcribe`); model loaded eagerly at `__init__`
- `whisper/main.py` — gRPC server, sets SERVING only after model is fully loaded

### Telegram Frontend (`frontends/telegram/`)
- `cmd/telegram/main.go` — entry point, polling loop, goroutine for Kafka consumer
- `frontends/telegram/config/config.go` — `Load()` returns `*Config` (includes LLM config)
- `frontends/telegram/clients/interfaces.go` — `CoreService`, `NotificationsService`, `WhisperService`, `LLMService` interfaces
- `frontends/telegram/clients/core.go` — `CoreClient` (10 methods)
- `frontends/telegram/clients/notifications.go` — `NotificationsClient` (4 methods)
- `frontends/telegram/clients/whisper.go` — `WhisperClient` (50MB max message)
- `frontends/telegram/clients/llm.go` — `LLMClient` HTTP client for Ollama `/api/chat`; `LLMReminderResult` struct; `ErrLLMUnavailable`
- `frontends/telegram/clients/errors.go` — shared client errors
- `frontends/telegram/tgstates/context.go` — `UserState` constants + `UserContext` struct
- `frontends/telegram/tgstates/manager.go` — `StateManager` backed by Redis (JSON, TTL 7 days)
- `frontends/telegram/tgstates/draft.go` — typed `ReminderDraft` struct + `ToParamsJSON(tzOffset)`
- `frontends/telegram/tgkeyboards/` — `MainMenu`, `Tasks`, `Calendar`, `RemindersList`, `ScheduleType`, `ReminderCalendar` keyboards
- `frontends/telegram/tghandlers/app.go` — `App` struct with all clients (`Core`, `Notifications`, `Whisper`, `LLM`) + state manager
- `frontends/telegram/tghandlers/commands.go` — `/start` with ROOT_ID check
- `frontends/telegram/tghandlers/messages.go` — text routing by `UserState` via `stateTextHandlers` map
- `frontends/telegram/tghandlers/callbacks.go` — callback_data routing
- `frontends/telegram/tghandlers/voice.go` — Voice/VideoNote → Whisper → append to note
- `frontends/telegram/tghandlers/reminders.go` — multi-step reminder creation wizard + NL handler
- `frontends/telegram/tghandlers/kafka.go` — `MakeReminderHandler()` for Kafka events
- `frontends/telegram/tghandlers/middleware.go` — `EscapeMarkdownV2()`, `sendText()`, `editText()`, `replyToUpdate()`, `replyToCallback()`
- `frontends/telegram/bot/kafka_consumer.go` — `RunKafkaConsumer()` goroutine
- `frontends/telegram/bot/metrics.go` — Prometheus metrics for telegram service (`UpdatesTotal`, `KafkaMessagesConsumed`, `ReminderDeliveryErrors`, `HandlerDuration`)

### Internal Packages (`internal/`)
- `internal/applog/applog.go` — `New()` creates zap logger; `With(ctx, l)` enriches with OTel trace/span IDs
- `internal/telemetry/tracer.go` — `InitTracer(ctx, serviceName)` — no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` not set
- `internal/telemetry/metrics.go` — `InitMetrics()` creates Prometheus exporter + global MeterProvider, returns `/metrics` handler
- `internal/kafkacarrier/carrier.go` — `HeaderCarrier` for W3C trace context in Kafka message headers
- `internal/timeutil/timeutil.go` — `FixedZone()`, `LocalNow()`, `TodayDate()`, `FormatLocalTime()` — shared across services

### Proto / gRPC (`proto/`)
- `proto/notes.proto` — 10 RPCs for notes
- `proto/notifications.proto` — 4 RPCs for reminders
- `proto/whisper.proto` — 1 RPC for transcription
- `proto/notes/`, `proto/notifications/`, `proto/whisper/` — generated Go stubs
- `proto/whisper_pb2.py`, `proto/whisper_pb2_grpc.py` — generated Python stubs (Whisper service only)
- `proto/__init__.py` — makes proto a Python package

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
    StateReminderCreateNL           UserState = "reminder_create_nl"  // natural language input
)
```

`UserContext` stores: `user_id`, `state`, `active_date` (DD-MMM-YYYY), `calendar_month/year`, `task_page`, `last_message_id`, `reminder_draft` (`ReminderDraft` struct), `pending_postpone_reminder_id`, `reminder_cal_month/year`, `reminder_list_page`.

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

### Tooling (optional)
```env
PGADMIN_EMAIL=admin@example.com
PGADMIN_PASSWORD=<pgadmin_password>
GRAFANA_PASSWORD=<grafana_password>
OLLAMA_MODEL=qwen2.5:1.5b           # LLM model for NL reminder parsing
```

### Optional (defaults shown)
```env
TIMEZONE_OFFSET_HOURS=3     # UTC+3 Moscow
DAY_START_HOUR=7             # Day "starts" at 7 AM
TEMPLATE_SUBDIR=Templates    # Relative to NOTES_DIR
GRPC_PORT=50051              # core / notifications / whisper (per service)
METRICS_PORT=9100            # Prometheus /metrics port (9100/9101/9102 per service)
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
LLM_HOST=ollama
LLM_PORT=11434
LLM_MODEL=qwen2.5:1.5b
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317  # unset = tracing disabled
```

## Conventions and Patterns

### Adding a new gRPC method (Go)
1. Add to the relevant `proto/*.proto` file
2. Run `make proto`
3. Implement in the service's `server.go`
4. Add method to the corresponding interface in `frontends/telegram/clients/interfaces.go`
5. Implement in the corresponding client in `frontends/telegram/clients/`
6. Call from a handler

### Adding a new Telegram feature
1. Add new `UserState` constants if multi-step in `tgstates/context.go`
2. Add new fields to `UserContext` or `ReminderDraft` if needed
3. Create/update keyboard in `tgkeyboards/`
4. Add handler function in appropriate `tghandlers/` file
5. Register routing in `tghandlers/messages.go` (`stateTextHandlers` map) or `tghandlers/callbacks.go`
6. Wire up in `cmd/telegram/main.go` if needed

### Go package naming
Telegram bot sub-packages use prefixed names to avoid conflicts:
- `tgstates` — user state types and Redis manager
- `tgkeyboards` — inline keyboard builders
- `tghandlers` — update/callback handlers + `App` struct

### Markdown
All Telegram messages use **MarkdownV2**. Always wrap user-provided text in `EscapeMarkdownV2()` from `frontends/telegram/tghandlers/middleware.go`.

### Timezone
Day boundary is at `DAY_START_HOUR` (7 AM), not midnight. Shared logic is in `internal/timeutil/timeutil.go`. Consistency across all services is important.

### Logging
All Go services use `applog.New()` to create a production zap logger. Use `applog.With(ctx, logger)` inside handlers to get a child logger enriched with OTel trace/span IDs.

### Metrics
Each Go service calls `telemetry.InitMetrics()` at startup and exposes `/metrics` on its `METRICS_PORT`. Prometheus scrapes all three. Service-specific metric instruments are in `bot/metrics.go` and `notifications/metrics.go`.

### LLM (Ollama)
Natural language reminder parsing uses `clients.LLMClient` → Ollama `/api/chat` with structured JSON output (schema enforced). State `StateReminderCreateNL` routes text to `handleReminderNLInput`. If Ollama is unavailable (`ErrLLMUnavailable`), the handler falls back gracefully.

## Kafka Known Issues
- `confluentinc/cp-kafka:8.2.0` = Kafka 4.0 (KRaft mode). `kafka-go v0.4.x` GroupID support is **broken**: `FetchMessage` hangs forever on JoinGroup. Do NOT use `GroupID` in `ReaderConfig`.
- Consumer skips messages older than 5 minutes using `msg.Time` (Kafka broker timestamp) — see `staleMessageThreshold` in `kafka_consumer.go`.
- Kafka has a persistent volume (`kafka_data`) — topics survive container restarts.
- Scheduler fires ALL reminders where `next_fire_at <= NOW()` on startup — after downtime, a batch of old notifications may be sent.

## Testing

```bash
make test-go               # Go unit tests: core + notifications + telegram handlers/keyboards/states
make test-notifications    # Notifications package tests (verbose)
make test-integration      # Integration tests (needs running services)
make cover                 # Combined unit + integration coverage
make test-go-cover         # Unit tests with coverage report
make cover-html            # Coverage HTML report (opens in browser)
```

Unit test packages: `./core/...`, `./core/features/...`, `./notifications/...`, `./frontends/telegram/tghandlers/...`, `./frontends/telegram/tgkeyboards/...`, `./frontends/telegram/tgstates/...`

Integration tests: `integration/core_test.go` — 22 tests (require running core service).

## Notes Volume Structure (expected)

```
$NOTES_DIR/
├── Daily/                    # Auto-created; one file per day
│   ├── 09-Nov-2025.md
│   └── ...
└── Templates/
    └── Daily.md              # Template with {{date:DD-MMM-YYYY}} placeholders
```
