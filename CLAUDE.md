# Notes Bot — Context for AI Assistants

A personal Telegram bot for managing daily Obsidian-style markdown notes, tasks, ratings, reminders, and voice-to-text transcription.

## Quick Reference

```bash
make test        # Run pytest (64+ tests)
make proto       # Regenerate gRPC stubs from proto/*.proto
make run         # Run telegram bot locally (poetry)
make up          # docker-compose down + build + up + logs
make format      # ruff format + check
```

## Architecture: 5 gRPC Microservices

```
[Telegram Bot] ──gRPC──► [Core Service]         :50051
               ──gRPC──► [Notifications Service] :50052
               ──gRPC──► [Whisper Service]        :50053
                                 │
                         [PostgreSQL :5432]
```

All services run in Docker (`docker-compose.yml`). Startup order: postgres → core → notifications + whisper → telegram.

## Service Map

| Service | Entry Point | Port | Purpose |
|---------|------------|------|---------|
| core | `core/main.py` | 50051 | Notes CRUD, tasks, ratings |
| notifications | `notifications/main.py` | 50052 | Reminders with DB persistence |
| whisper | `whisper/main.py` | 50053 | Voice→text via faster-whisper |
| telegram | `main.py` → `frontends/telegram/bot.py` | — | User-facing Telegram bot |
| postgres | docker image | 5432 | Reminders storage |

## Key Files

### Core Service (`core/`)
- `core/server.py` — NotesServicer, implements all 10 gRPC RPCs
- `core/notes.py` — File I/O: create note from template, append text
- `core/features/rating.py` — Parse/update `Оценка:` in YAML frontmatter
- `core/features/tasks.py` — Parse `- [ ]`/`- [x]`, toggle, add
- `core/features/calendar_ops.py` — Scan `Daily/` for existing dates
- `core/utils.py` — `get_today_filename()` with timezone-aware date
- `core/config.py` — `NOTES_DIR`, `TEMPLATE_DIR`, timezone settings

### Notifications Service (`notifications/`)
- `notifications/server.py` — NotificationsServicer (4 RPCs)
- `notifications/db.py` — PostgreSQL schema + CRUD
- `notifications/scheduler.py` — Background thread, fires due reminders

### Whisper Service (`whisper/`)
- `whisper/server.py` — TranscriptionServicer, 1 RPC (`Transcribe`)

### Telegram Frontend (`frontends/telegram/`)
- `bot.py` — Initializes Application, registers all handlers
- `grpc_client.py` — `CoreClient` singleton (`core_client`)
- `notifications_client.py` — `NotificationsClient` singleton
- `whisper_client.py` — `WhisperClient` singleton
- `middleware.py` — `reply_message()` abstraction (Update vs CallbackQuery)
- `utils.py` — `escape_markdown_v2()`
- `states/context.py` — `UserContext` dataclass + `UserState` enum
- `states/manager.py` — `StateManager` singleton (in-memory `Dict[user_id, UserContext]`)
- `handlers/commands.py` — `/start`
- `handlers/messages.py` — Text input routing by current state
- `handlers/callbacks.py` — Button press handler + calendar navigation
- `handlers/voice.py` — Voice/video note transcription
- `handlers/reminders.py` — Multi-step reminder creation workflow
- `keyboards/main_menu.py` — 5 buttons: Rating, Tasks, Note, Calendar, Notifications
- `keyboards/tasks.py` — Task list with pagination
- `keyboards/calendar.py` — Month calendar with date selection
- `keyboards/reminders.py` — Reminder management UI

### Proto / gRPC (`proto/`)
- `proto/notes.proto` — 10 RPCs for notes
- `proto/notifications.proto` — 4 RPCs for reminders
- `proto/whisper.proto` — 1 RPC for transcription
- `*_pb2.py`, `*_pb2_grpc.py` — generated stubs (run `make proto` to regenerate)
- **Import patch**: generated files use `from proto import notes_pb2` (not `import notes_pb2`)

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

```python
class UserState(Enum):
    IDLE                          # Default, text messages → append to note
    WAITING_RATING                # Expecting 0-10 integer input
    TASKS_VIEW                    # Showing paginated task list
    WAITING_NEW_TASK              # Expecting new task text
    CALENDAR_VIEW                 # Showing month calendar
    REMINDER_LIST                 # Showing reminder list
    REMINDER_CREATE_TITLE         # Multi-step reminder creation
    REMINDER_CREATE_TYPE          # (5+ states)
    REMINDER_CREATE_TIME
    REMINDER_CREATE_DATE
    REMINDER_CREATE_CONFIRM
    REMINDER_POSTPONE_DATE
    REMINDER_POSTPONE_CAL
```

`UserContext` stores: `user_id`, `state`, `active_date` (DD-MMM-YYYY), `calendar_month/year`, `task_page`, `last_message_id`, `reminder_draft` dict.

State is **in-memory only** — lost on bot restart.

## Callback Data Format

```
"menu:rating"            # Main menu → rating
"menu:tasks"             # Main menu → tasks
"task:toggle:0"          # Toggle task index 0
"task:add"               # Add task
"task:page:1"            # Tasks pagination
"cal:select:09-Nov-2025" # Calendar date pick
"cal:prev" / "cal:next"  # Month navigation
"nav:menu"               # Back to main menu
"notif:list"             # Reminder list
"notif:delete:42"        # Delete reminder id=42
"notif:postpone:42"      # Postpone reminder
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
DAY_START_HOUR=7             # Day "starts" at 7 AM (affects get_today_filename)
TEMPLATE_SUBDIR=Templates    # Relative to NOTES_DIR
GRPC_PORT=50051              # core
# GRPC_PORT=50052            # notifications
# GRPC_PORT=50053            # whisper
CORE_GRPC_HOST=core
CORE_GRPC_PORT=50051
NOTIFICATIONS_GRPC_HOST=notifications
NOTIFICATIONS_GRPC_PORT=50052
WHISPER_GRPC_HOST=whisper
WHISPER_GRPC_PORT=50053
WHISPER_MODEL=base            # small/base/medium/large/turbo
SCHEDULER_INTERVAL_SECONDS=60
```

## Conventions and Patterns

### Adding a new gRPC method
1. Add to `proto/notes.proto` (or relevant proto file)
2. Run `make proto`
3. Implement in `core/server.py` (NotesServicer)
4. Add method to `frontends/telegram/grpc_client.py` (CoreClient)
5. Call from a handler or keyboard

### Adding a new Telegram feature
1. Add new `UserState` values if multi-step in `states/context.py`
2. Add new fields to `UserContext` if needed
3. Create/update keyboard in `keyboards/`
4. Add handler in appropriate `handlers/` file
5. Register handler in `bot.py`
6. Route new state in `handlers/messages.py` or `handlers/callbacks.py`

### Adding a new service (microservice)
1. Create `newservice/` directory with `main.py`, `server.py`, `config.py`, `Dockerfile`
2. Define proto in `proto/newservice.proto`
3. Run `make proto`
4. Add service to `docker-compose.yml`
5. Create `frontends/telegram/newservice_client.py` singleton
6. Import and use in handlers

### gRPC client pattern
```python
# Singleton at module level
client = SomeClient(
    host=os.environ.get("SERVICE_GRPC_HOST", "localhost"),
    port=int(os.environ.get("SERVICE_GRPC_PORT", "50051"))
)

# Error handling decorator
class SomeUnavailableError(Exception): ...

def _handle_unavailable(func):
    @functools.wraps(func)
    def wrapper(*args, **kwargs):
        try:
            return func(*args, **kwargs)
        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.UNAVAILABLE:
                raise SomeUnavailableError() from e
            raise
    return wrapper
```

### Sending messages
Always use `reply_message()` from `middleware.py` — it handles both `Update` and `CallbackQuery` contexts uniformly. For editing: use `context.bot.edit_message_text()` with `last_message_id` from `UserContext`.

### Markdown
All Telegram messages use **MarkdownV2**. Always wrap user-provided text in `escape_markdown_v2()` from `utils.py`.

### Timezone
Day boundary is at `DAY_START_HOUR` (7 AM), not midnight. `get_today_filename()` in `core/utils.py` applies this logic. Consistency is important across all services.

## Testing

```bash
make test                # run all tests with coverage
poetry run pytest tests/test_notes.py   # run specific file
```

Tests are in `tests/`. Structure mirrors source:
- `tests/test_notes.py`, `tests/test_server.py` — core service (direct, no gRPC)
- `tests/features/` — rating, tasks, calendar_ops
- `tests/telegram/` — state manager, keyboards, handlers
- `tests/test_notifications_*.py` — notifications service

Fixtures in `conftest.py` create temp directories with sample markdown files.

Tests use **direct imports** of service modules — no gRPC calls in tests.

## Notes Volume Structure (expected)

```
$NOTES_DIR/
├── Daily/                    # Auto-created; one file per day
│   ├── 09-Nov-2025.md
│   └── ...
└── Templates/
    └── Daily.md              # Template with {{date:DD-MMM-YYYY}} placeholders
```
