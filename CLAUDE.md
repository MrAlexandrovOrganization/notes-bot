# Notes Bot — Context for AI Assistants

A personal Telegram bot (plus a server-rendered web frontend) for managing daily Obsidian-style markdown notes, tasks, ratings, reminders, voice-to-text transcription, semantic search, and natural-language intent classification. Uses a local LLM (Ollama) for NL reminder parsing and smart routing.

## Quick Reference

```bash
make test-go          # Run Go unit tests (core + notifications + search + telegram)
make test-race        # Go unit tests with -race detector
make test             # Go unit tests with -race + coverage report
make lint             # gofmt check (fail on diff) + go vet
make cover            # Combined unit + integration coverage
make test-integration # Integration tests (requires running core service)
make proto            # Sync whisper.proto from backends/transcriber + regenerate gRPC stubs
make format           # gofmt -w (auto-fix formatting)
make up               # docker-compose build + up + logs
make build-core       # Rebuild core service image
make build-notifications # Rebuild notifications image
make build-search     # Rebuild search image
make build-telegram   # Rebuild telegram image
make build-web        # Rebuild web frontend image
make templ            # Regenerate frontends/web/views/*_templ.go from *.templ sources
```

## Architecture

```
[Telegram Bot] ──gRPC──► [Core Service]         :50051
[Web Frontend] ──gRPC──┘
               ──gRPC──► [Notifications Service] :50052
               ──gRPC──► [Search Service]        :50054
               ──gRPC──► [Whisper Service]        :50053  (external: backends/transcriber, telegram only)
               ──HTTP──► [Ollama LLM]             :11434  (external: backends/ollama)
               ──HTTP──► [Location Service]       :8080   (external: backends/location, telegram only)
               ──────────[Redis]                  :6379   (external: infra/redis, telegram user state)
                                 │
                          [PostgreSQL :5432]  (reminders)
                          [PostgreSQL :5432]  (search, pgvector)

[Notifications Service] ──Kafka──► topic: reminders_due ──► [Telegram Bot]
[Search Service]        ──indexing──► PostgreSQL pgvector + FTS
```

The web frontend is a second, independent client of the same `core`/`notifications`/`search`/Ollama backends — full feature parity with the Telegram bot except voice transcription and location sharing (both device-dependent, not applicable to a browser form). It does not touch Kafka, Redis, Whisper, or Location.

### Service topology

This repo (`notes-bot`) runs **5 Go services** in Docker. All infrastructure (Kafka, Redis, Jaeger, Prometheus, Grafana, Whisper, Ollama, Location) is **external** — consumed via Docker networks declared in `docker-compose.yml` as `external: true`. Sources of truth:

| External dependency | Source repo | Docker network |
|---------------------|-------------|-----------------|
| Whisper transcription | `backends/transcriber` | `whisper-net` |
| Ollama LLM | `backends/ollama` | `ollama-net` |
| Location storage | `backends/location` | `location-api-net` |
| Kafka | `infra/kafka` | `kafka-net` |
| Redis | `infra/redis` | `redis-net` |
| Jaeger + Prometheus + Grafana | `infra/observability` | `monitoring-net` |

### Application Services (this repo)

| Service | Entry Point | gRPC Port | Metrics Port | Purpose |
|---------|------------|-----------|--------------|---------|
| core | `cmd/core/main.go` | 50051 | 9100 | Notes CRUD, tasks, ratings, directory browsing |
| notifications | `cmd/notifications/main.go` | 50052 | 9101 | Reminders with DB persistence, publishes to Kafka |
| search | `cmd/search/main.go` | 50054 | 9103 | Full-text + semantic search (pgvector, Ollama embeddings) |
| telegram | `cmd/telegram/main.go` | — | 9102 | User-facing Telegram bot, Kafka consumer, LLM smart router |
| web | `cmd/web/main.go` | — | 9105 | Server-rendered web frontend (templ + htmx + Tailwind), password-gated |
| postgres | docker image | 5432 | — | Reminders storage (notifications) |
| postgres-search | `pgvector/pgvector:pg16` | 5432 | — | Search storage with pgvector extension |

Health checks: core, notifications, search use `grpc.health.v1` + `grpc_health_probe` binary. Telegram and web use HTTP wget checks (`/metrics`, `/healthz` respectively). All containers run as non-root user `app` (UID 10001).

## Key Files

### Core Service (`core/`)
- `core/server.go` — `NewNotesServer()`, implements gRPC RPCs
- `core/stores.go` — 4 DI interfaces: `CalendarStore`, `NoteStore`, `RatingStore`, `TaskStore`; file I/O
- `core/config.go` — singleton config via `goenv` + `sync.Once`
- `core/utils.go` — `TodayDate()`, timezone helpers
- `core/features/rating.go` — Parse/update `Оценка:` in YAML frontmatter
- `core/features/tasks.go` — Parse `- [ ]`/`- [x]`, toggle, add
- `cmd/core/main.go` — gRPC server entry point

### Notifications Service (`notifications/`)
- `notifications/server.go` — `NotificationsServer`, 4 gRPC RPCs; `reminderToProto` with nil-guard
- `notifications/db.go` — PostgreSQL CRUD via pgx/v5 (EnsureSchema, CreateReminder, ListReminders, DeleteReminder, GetDueReminders, UpdateNextFire, SetNextFireAt); `scanReminder` returns error on `ErrNoRows` (not nil,nil)
- `notifications/scheduler.go` — `ComputeNextFire()` for 6 schedule types; `Scheduler.Run()` goroutine publishing to Kafka topic `reminders_due`
- `notifications/config.go` — `LoadConfig()`, `Config` struct, `DSN()` helper
- `notifications/metrics.go` — Prometheus metrics
- `notifications/scheduler_test.go` — unit tests for all schedule types
- `notifications/server_test.go` — tests for `reminderToProto`, `scheduleParamsToMap`, `scanReminder`
- `cmd/notifications/main.go` — entry point

### Search Service (`search/`)
- `search/server.go` — `SearchServer`, gRPC RPCs for name/content/semantic search + GetNote
- `search/db.go` — PostgreSQL + pgvector: FTS (trigram), vector search, chunk storage
- `search/chunker.go` — Splits markdown notes into chunks for embedding
- `search/embedder.go` — Ollama embeddings client (`bge-m3` model)
- `search/indexer.go` — Background indexer loop
- `search/scheduler.go` — Index scheduling
- `search/config.go` — Config with embedding model, dimensions, index interval
- `search/metrics.go` — Prometheus metrics
- `cmd/search/main.go` — entry point

### Telegram Frontend (`frontends/telegram/`)
- `cmd/telegram/main.go` — entry point, polling/webhook loop, Kafka consumer goroutine, `semaphore.Weighted` for update concurrency limit (16)
- `frontends/telegram/config/config.go` — `Load()` returns `*Config` (includes LLM, search, location config)
- `frontends/telegram/clients/interfaces.go` — `CoreService`, `NotificationsService`, `WhisperService`, `SearchService`, `LLMService`, `LocationService` interfaces
- `frontends/telegram/clients/core.go` — `CoreClient` (12 methods: notes CRUD + directory browsing)
- `frontends/telegram/clients/notifications.go` — `NotificationsClient` (4 methods)
- `frontends/telegram/clients/whisper.go` — `WhisperClient` (50MB max message, chunked streaming)
- `frontends/telegram/clients/search.go` — `SearchClient` (4 methods: byName, byContent, semantic, getNote)
- `frontends/telegram/clients/llm.go` — `LLMClient` HTTP client for Ollama `/api/chat`; `LLMReminderResult`, `LLMIntentResult` structs; `ErrLLMUnavailable`; structured JSON output via schema
- `frontends/telegram/clients/location.go` — `LocationClient` HTTP client
- `frontends/telegram/clients/errors.go` — `ServiceUnavailableError` typed error
- `frontends/telegram/clients/clients_test.go` — tests for `sendChunks`, `isUnavailable`, `extractJSON`, `protoToReminderInfo`
- `frontends/telegram/tgfmt/tgfmt.go` — **HTML** formatting helpers (`Escape`, `Bold`, `Italic`, `Code`, `Blockquote`, `Link`, `Join`)
- `frontends/telegram/tgstates/context.go` — `UserState` constants (24 states) + `UserContext` struct (18 fields)
- `frontends/telegram/tgstates/manager.go` — `StateManager` backed by Redis (JSON, TTL 7 days); **per-user mutex** via `sync.Map` to prevent lost updates
- `frontends/telegram/tgstates/manager_test.go` — `memoryStore` mock + tests
- `frontends/telegram/tgstates/draft.go` — typed `ReminderDraft` struct + `ToScheduleParams(tzOffset)` + `SmartDraft`
- `frontends/telegram/tgstates/transitions.go` — advisory state transition map
- `frontends/telegram/tgkeyboards/` — `MainMenu`, `Tasks`, `Calendar`, `RemindersList`, `ScheduleType`, `ReminderCalendar`, `BrowseFolder`, `FindResults`, `NoteView`, `SmartConfirm`, `SmartIntentPicker` keyboards
- `frontends/telegram/tghandlers/app.go` — `App` struct with all clients + state manager; `authorized()`, `updateState()` (logged wrapper), `setActiveDate()`, `cancelVoiceJob()`
- `frontends/telegram/tghandlers/commands.go` — `/start` with ROOT_ID check
- `frontends/telegram/tghandlers/messages.go` — text routing by `UserState` via `stateTextHandlers` map (14 state handlers + default append-to-note)
- `frontends/telegram/tghandlers/callbacks.go` — callback_data routing via `callbackActionHandlers` map (9 action namespaces)
- `frontends/telegram/tghandlers/voice.go` — Voice/VideoNote → Whisper → reorder buffer → append to note / NL / smart
- `frontends/telegram/tghandlers/reminder_create.go` — multi-step reminder creation wizard
- `frontends/telegram/tghandlers/reminder_postpone.go` — postpone by duration / by date+time
- `frontends/telegram/tghandlers/reminder_actions.go` — reminder list, delete, done, reject, back, cancel; `extractReminderTitle` (HTML unescape)
- `frontends/telegram/tghandlers/reminder_nl.go` — NL reminder parsing via LLM
- `frontends/telegram/tghandlers/smart.go` — Smart router: LLM classifies intent (note/task/reminder/unknown) → confirm → execute
- `frontends/telegram/tghandlers/find.go` — Search by name + content, open note, append to note
- `frontends/telegram/tghandlers/ask.go` — Semantic Q&A: vector search + LLM RAG
- `frontends/telegram/tghandlers/browse.go` — Vault directory browser (rune-safe truncation)
- `frontends/telegram/tghandlers/location.go` — Location message handler
- `frontends/telegram/tghandlers/kafka.go` — `MakeReminderHandler()` for Kafka events
- `frontends/telegram/tghandlers/middleware.go` — `sendText()`, `editText()`, `replyToUpdate()`, `replyToCallback()` (all HTML parse mode); `isRetriableNetworkError()`
- `frontends/telegram/tghandlers/reminder_actions_test.go` — tests for `extractReminderTitle`
- `frontends/telegram/bot/kafka_consumer.go` — `RunKafkaConsumer()` goroutine, consumer group, retry loop
- `frontends/telegram/bot/kafka_consumer_test.go` — tests for `ReminderEvent` JSON round-trip
- `frontends/telegram/bot/metrics.go` — Prometheus metrics: `UpdatesTotal`, `KafkaMessagesConsumed`, `ReminderDeliveryErrors`, `HandlerDuration`, `SmartIntentTotal`, `SmartIntentConfirmed`, `SmartIntentRejected`

### Web Frontend (`frontends/web/`)
Server-rendered HTML (Go `templ` components + `htmx` for partial updates, Tailwind for styling) — same feature set as the Telegram bot minus voice transcription and location sharing. Reuses `frontends/telegram/clients` directly (same Go module) for `CoreClient`/`NotificationsClient`/`SearchClient`/`LLMClient` — no separate client layer.
- `cmd/web/main.go` — entry point, wires clients + `webapp.App`, starts `http.Server`
- `frontends/web/config/config.go` — `Load()`; requires `WEB_PASSWORD` + `WEB_SESSION_SECRET`
- `frontends/web/webapp/app.go` — `App` struct (clients + config + logger); `singleUserID = 0` used for all Notifications RPCs (single-user tool, no per-Telegram-user concept)
- `frontends/web/webapp/router.go` — `NewRouter()` builds the route table (Go 1.26 method+pattern `http.ServeMux`); `//go:embed static` serves `htmx.min.js` + built `tailwind.css`
- `frontends/web/webapp/auth.go` — stateless HMAC-signed session cookie (no Redis/DB) checked against `WEB_PASSWORD`; `requireAuth` middleware, sliding 30-day TTL
- `frontends/web/webapp/render.go` — `(*App).render()` writes a `templ.Component` to the response
- `frontends/web/webapp/day.go` — day view (note/rating/tasks, loaded concurrently via `errgroup`, mirrors Telegram's note-view pattern); task add/toggle, rating update, append-to-note
- `frontends/web/webapp/calendar.go` — month calendar grid (`buildCalendarWeeks`, Monday-start, mirrors `tgkeyboards/calendar.go`'s convention)
- `frontends/web/webapp/reminders.go` — list/create/delete/postpone + NL create; `formToScheduleParams()` validates form fields into `*pb.ScheduleParams` (mirrors `tgstates.ReminderDraft.ToScheduleParams`); `parseDuration()` copied from `reminder_postpone.go` (unexported there)
- `frontends/web/webapp/search.go` — name search w/ content fallback (mirrors `find.go`), open note, append-to-note-by-path
- `frontends/web/webapp/ask.go` — RAG Q&A: `hybridSearch()` (semantic + FTS, RRF-merged) + `LLM.Ask()`, mirrors `ask.go`
- `frontends/web/webapp/browse.go` — directory browser; unlike Telegram's success-based type inference, uses `DirEntry.IsDir` directly
- `frontends/web/webapp/smart.go` — LLM intent classification → confirm form (hidden fields) → dispatch to note/task/reminder
- `frontends/web/webapp/fakes_test.go` — fake `CoreService`/`NotificationsService`/`SearchService`/`LLMService` implementations for handler tests
- `frontends/web/views/*.templ` — templ components; compiled to `*_templ.go` via `templ generate` (gitignored, like `*.pb.go` — regenerate with `make templ`)
- `frontends/web/webapp/static/` — `htmx.min.js` (vendored) + `input.css` (Tailwind source, checked in) + `tailwind.css` (built at Docker-image-build time via a Node-based `@tailwindcss/cli` stage, gitignored)
- `frontends/web/Dockerfile` — 4 stages: `gobuilder` (proto+templ codegen, same as telegram's `buf generate`) → `twbuilder` (`node:20-alpine`, builds `tailwind.css` from the generated views — the standalone Bun-based Tailwind CLI is unreliable under some container runtimes, hence Node) → `finalbuilder` (copies `tailwind.css` back, runs `go build`) → `alpine:3.20` runtime

### Internal Packages (`internal/`)
- `internal/applog/applog.go` — `New()` creates zap logger; `With(ctx, l)` enriches with OTel trace/span IDs
- `internal/telemetry/tracer.go` — `InitTracer(ctx, serviceName)` — no-op when `OTEL_EXPORTER_OTLP_ENDPOINT` not set; `StartSpan` auto-resolves caller name
- `internal/telemetry/metrics.go` — `InitMetrics()` creates Prometheus exporter + global MeterProvider, returns `/metrics` handler
- `internal/grpcutil/dial.go` — `Dial(host, port, opts...)` shared gRPC client dial helper
- `internal/grpcutil/server.go` — `NewServer()` shared gRPC server setup with OTel stats handler
- `internal/grpcutil/interceptors.go` — `TimeoutInterceptor` for unary RPCs
- `internal/kafkacarrier/carrier.go` — `HeaderCarrier` for W3C trace context in Kafka message headers
- `internal/timeutil/timeutil.go` — `FixedZone()`, `LocalNow()`, `TodayDate()`, `LogicalToday()`, `FormatLocalTime()` — shared across services

### Proto / gRPC (`proto/`)
- `proto/notes/notes.proto` — RPCs for notes (CRUD, tasks, ratings, directory)
- `proto/notifications/notifications.proto` — 4 RPCs for reminders
- `proto/search/search.proto` — RPCs for search
- `proto/whisper/whisper.proto` — synced from `backends/transcriber/proto/whisper.proto` via `make proto`
- `proto/*/*.pb.go`, `proto/*/*_grpc.pb.go` — generated Go stubs (gitignored, regenerated via `buf generate`)

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

24 states defined in `tgstates/context.go`. Key states:

```go
StateIdle, StateWaitingRating, StateTasksView, StateWaitingNewTask,
StateCalendarView, StateReminderList,
StateReminderCreateTitle, StateReminderCreateScheduleType,
StateReminderCreateTime, StateReminderCreateDay,
StateReminderCreateInterval, StateReminderCreateDate,
StateReminderPostponeDate, StateReminderPostponeInput, StateReminderPostponeTime,
StateReminderCreateTaskConfirm, StateReminderCreateNL,
StateSmartInput, StateSmartConfirm,
StateFindNoteInput, StateFindNoteResults, StateViewNote,
StateAppendToNoteInput, StateAskQuestion,
StateBrowseView, StateBrowseFile
```

`UserContext` stores: `user_id`, `state`, `active_date`, `calendar_month/year`, `task_page`, `note_page`, `reminder_draft`, `pending_postpone_reminder_id`, `pending_postpone_date`, `reminder_cal_month/year`, `reminder_list_page`, `smart_draft`, `find_query`, `find_results`, `find_results_page`, `active_relpath`, `browse_path`.

State is persisted in **Redis** — survives bot restarts (TTL 7 days). Key: `user_state:{user_id}`. Per-user mutex (`sync.Map` in `StateManager`) serializes concurrent `UpdateContext` calls to prevent lost updates.

## Callback Data Format

```
"menu:rating"              "menu:tasks"         "menu:calendar"     "menu:notifications"
"menu:smart"               "menu:find"          "menu:ask"          "menu:browse"
"menu:back"                "menu:note"          "menu:noop"
"task:toggle:0"            "task:add"           "task:page:1"       "task:back"       "task:cancel"
"cal:prev"                 "cal:next"           "cal:select:DD-Mmm-YYYY"  "cal:today"  "cal:back"
"note:page:1"               "note:back"          "note:append"       "note:noop"
"reminder:list"            "reminder:create"    "reminder:create_nl" "reminder:nl_confirm"
"reminder:page:1"          "reminder:type:daily" "reminder:task_confirm:yes"
"reminder:delete:42"       "reminder:reject:42"  "reminder:done:42:0"
"reminder:done:42:1:DD-Mmm-YYYY"  "reminder:postpone_input:42"  "reminder:postpone_date:42"
"reminder:cal:prev:once"   "reminder:cal:next:pp"  "reminder:cal:today:yr"
"reminder:cal:select:YYYY-MM-DD:create"  "reminder:back"  "reminder:cancel"
"voice:cancel:<jobID>"     "voice:page:<msgID>:<page>"  "voice:noop"
"smart:yes"                "smart:no"            "smart:pick:note"   "smart:pick:task"   "smart:pick:reminder"
"find:open:42"             "find:page:1"         "find:back"         "find:retry"       "find:noop"
"browse:root"              "browse:up"           "browse:open:0"     "browse:page:1"   "browse:back"   "browse:file_back"   "browse:noop"
```

## Environment Variables

### Required
```env
BOT_TOKEN=<telegram_bot_token>
ROOT_ID=<telegram_user_id>          # Only this user can use the bot
NOTES_DIR=/path/to/obsidian/vault
DB_NAME=notifications
DB_USER=notif
DB_PASSWORD=<strong_password>
SEARCH_DB_NAME=search
SEARCH_DB_USER=search
SEARCH_DB_PASSWORD=<search_db_password>
WEB_PASSWORD=<web_frontend_password>       # single shared password, same trust model as ROOT_ID
WEB_SESSION_SECRET=<random_signing_secret> # e.g. `openssl rand -hex 32`; never reuse WEB_PASSWORD here
```

### Optional (defaults shown)
```env
TIMEZONE_OFFSET_HOURS=3     # UTC+3 Moscow
DAY_START_HOUR=7             # Day "starts" at 7 AM
TEMPLATE_SUBDIR=Templates    # Relative to NOTES_DIR
GRPC_PORT=50051              # per-service gRPC port
METRICS_PORT=9100            # Prometheus /metrics port (9100/9101/9102/9103 per service)
CORE_GRPC_HOST=core          CORE_GRPC_PORT=50051
NOTIFICATIONS_GRPC_HOST=notifications  NOTIFICATIONS_GRPC_PORT=50052
WHISPER_GRPC_HOST=whisper-ingress     WHISPER_GRPC_PORT=50053
SEARCH_GRPC_HOST=search      SEARCH_GRPC_PORT=50054
LOCATION_HOST=location       LOCATION_PORT=8080
SCHEDULER_INTERVAL_SECONDS=60
KAFKA_BOOTSTRAP_SERVERS=kafka:9092
REDIS_HOST=redis             REDIS_PORT=6379
LLM_HOST=ollama              LLM_PORT=11434
LLM_MODEL=qwen2.5:7b
SEARCH_EMBED_MODEL=bge-m3:567m
SEARCH_EMBED_DIM=1024
SEARCH_ENABLE_EMBEDDINGS=false
SEARCH_INDEX_INTERVAL=5m
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317  # unset = tracing disabled
WEBHOOK_URL=                 # empty = polling mode; set URL for webhook mode
WEB_LISTEN_ADDR=:8090        # web frontend listen address
```

## Conventions and Patterns

### Adding a new gRPC method (Go)
1. Add to the relevant `proto/*.proto` file
2. Run `make proto` (or `buf generate` if proto stubs already synced)
3. Implement in the service's `server.go`
4. Add method to the corresponding interface in `frontends/telegram/clients/interfaces.go`
5. Implement in the corresponding client in `frontends/telegram/clients/`
6. Call from a handler

### Adding a new Telegram feature
1. Add new `UserState` constants if multi-step in `tgstates/context.go`
2. Add new fields to `UserContext` or `ReminderDraft`/`SmartDraft` if needed
3. Create/update keyboard in `tgkeyboards/`
4. Add handler function in appropriate `tghandlers/` file
5. Register routing in `tghandlers/messages.go` (`stateTextHandlers` map) or `tghandlers/callbacks.go` (`callbackActionHandlers` map)
6. Wire up in `cmd/telegram/main.go` if needed

### Go package naming
Telegram bot sub-packages use prefixed names to avoid conflicts:
- `tgstates` — user state types and Redis manager
- `tgkeyboards` — inline keyboard builders
- `tghandlers` — update/callback handlers + `App` struct
- `tgfmt` — HTML formatting helpers for Telegram

### HTML formatting
All Telegram messages use **HTML parse mode** (`msg.ParseMode = "HTML"`). Always wrap user-provided text in `tgfmt.Escape()` from `frontends/telegram/tgfmt/tgfmt.go` to escape `&`, `<`, `>`. Use `tgfmt.Join()`, `tgfmt.Bold()`, `tgfmt.Code()`, `tgfmt.Blockquote()` etc. to compose messages safely.

### State updates
Always use `a.updateState(ctx, userID, func(u *tgstates.UserContext) { ... })` instead of `a.State.UpdateContext(...)` directly — the wrapper logs Redis errors instead of silently dropping them. Similarly use `a.setActiveDate()` for date changes.

### Timezone
Day boundary is at `DAY_START_HOUR` (7 AM), not midnight. Shared logic is in `internal/timeutil/timeutil.go`. Consistency across all services is important.

### Logging
All Go services use `applog.New()` to create a production zap logger. Use `applog.With(ctx, logger)` inside handlers to get a child logger enriched with OTel trace/span IDs.

### Metrics
Each Go service calls `telemetry.InitMetrics()` at startup and exposes `/metrics` on its `METRICS_PORT`. Prometheus scrapes all services. Service-specific metric instruments are in `bot/metrics.go`, `notifications/metrics.go`, and `search/metrics.go`.

### LLM (Ollama)
- **NL reminder parsing**: `clients.LLMClient.ParseReminder()` → Ollama `/api/chat` with structured JSON output (schema enforced). State `StateReminderCreateNL` routes text to `handleReminderNLInput`.
- **Smart router**: `clients.LLMClient.ClassifyIntent()` classifies arbitrary text into `note`/`task`/`reminder`/`unknown` with confidence. State `StateSmartInput` routes to `handleSmartInput`. If Ollama is unavailable (`ErrLLMUnavailable`), the handler falls back gracefully.
- **RAG Q&A**: `clients.LLMClient.Ask()` for free-form answers over search results. State `StateAskQuestion` routes to `handleAskInput`.

### Concurrency
- Telegram updates are processed in goroutines limited by `semaphore.Weighted` (max 16 concurrent, `cmd/telegram/main.go`).
- `StateManager.UpdateContext` uses a per-user mutex (`sync.Map` of `*sync.Mutex`) to serialize concurrent state updates for the same user, preventing lost updates.
- Voice transcription uses a per-user reorder buffer (`voiceReorderBuffer`) to deliver results in Telegram message ID order.

## Kafka

- Topic: `reminders_due`
- Producer: `notifications/scheduler.go` — fires when `next_fire_at <= NOW()`
- Consumer: `frontends/telegram/bot/kafka_consumer.go` — consumer group `telegram-bot-reminders`, commits offsets after processing
- Kafka 4.0 (KRaft mode, `confluentinc/cp-kafka:8.2.0`) — external broker via `infra/kafka`
- Scheduler fires ALL due reminders on startup — after downtime, a batch of old notifications may be sent

## Testing

```bash
make test-go               # Go unit tests (no -race)
make test-race             # Go unit tests with -race detector (requires CGO_ENABLED=1)
make test                  # Go unit tests with -race + coverage report
make lint                  # gofmt check (fail on diff) + go vet
make test-integration      # Integration tests (needs running core service)
make cover                 # Combined unit + integration coverage
make cover-html            # Coverage HTML report (opens in browser)
```

Unit test packages: `./core/...`, `./core/features/...`, `./notifications/...`, `./search/...`, `./frontends/telegram/tghandlers/...`, `./frontends/telegram/tgkeyboards/...`, `./frontends/telegram/tgstates/...`, `./frontends/telegram/clients/...`, `./frontends/telegram/bot/...`, `./frontends/web/...`

Integration tests: `integration/core_test.go` — 22 tests (require running core service).

CI (`.github/workflows/ci-cd.yml`): runs `buf generate` + `make templ` (regenerates `frontends/web/views/*_templ.go`) before `make lint` + `make test` on every push/PR to main.

## Notes Volume Structure (expected)

```
$NOTES_DIR/
├── Daily/                    # Auto-created; one file per day
│   ├── 09-Nov-2025.md
│   └── ...
└── Templates/
    └── Daily.md              # Template with {{date:DD-MMM-YYYY}} placeholders
```

## Docker

- All Dockerfiles use multi-stage builds: `golang:1.26-alpine` builder → `alpine:3.20` runtime (`frontends/web/Dockerfile` adds a `node:20-alpine` stage to build Tailwind CSS — see Web Frontend section above)
- Binaries built with `CGO_ENABLED=0 -ldflags="-s -w"` (static, stripped)
- Containers run as non-root user `app` (UID 10001)
- `grpc_health_probe` downloaded at build time (v0.4.28)
- `.dockerignore` excludes `.git`, `docs/`, `notes/`, `third_party/`, test artifacts, env files
- `docker-compose.base.yml` defines shared logging + resource limits
