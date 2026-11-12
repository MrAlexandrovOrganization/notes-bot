# Independent search worker

Core, the primary vault, notifications and the user-facing Telegram/web/native
frontends stay on the application host. The existing search service, its
pgvector database, lexical index, embeddings and query-time RAG run in a separate
WSL checkout with `NOTES_ROLE=search-worker`. The existing model server is reused;
no models or inference workers are started on the application host.

## Failure boundaries

Frontends have no Compose startup dependency on search. Their search client
checks gRPC readiness with a one-second deadline and permits at most four
concurrent search operations per frontend. Busy or unavailable search returns
an error without consuming all Telegram update slots. A healthy LLM operation
retains the existing five-minute upper bound. Search readiness includes a
bounded database ping; loss of its database does not hold ordinary queries for
the LLM deadline. Web/mobile errors use HTTP 503. Other notes and task operations
use Core independently.

`compose/compose.remote-search.yml` retains the original search Docker aliases
on a small HAProxy router. TCP connection timeout is two seconds, retries are
disabled, and active checks close already-open sessions when WSL fails. The
router does not queue requests or become a local search worker. Its metrics
listener forwards the existing `notes-bot-search:9103` target, preserving the
application-host collector and dashboards. Its own `/health` is router liveness;
upstream health is separate. A prolonged WSL outage disables search while the
primary vault and Core remain on the application host. Existing external
dependencies, such as Kafka, retain their separate availability characteristics.

## Read-only vault replica

WSL does not run a second Yandex Disk client. A dedicated SSH key permits only
read-only `rrsync` of the primary vault. Host keys are pinned; the private key
stays on WSL. The publisher transfers Markdown into a fresh generation, checks
the result, and atomically publishes `current`. A failed, incomplete or empty
transfer retains the previous generation. Unchanged files use hard links;
`--inplace` is deliberately absent. External symlinks are not followed.

Search mounts the snapshot parent read-only and pins the resolved generation
for each filesystem reconciliation. Its shared `.snapshot.lock` prevents the
publisher from deleting an in-use generation. Slow embedding/profile backfills
run after releasing this filesystem lease. Only the current and previous
complete generations are retained. Snapshot refresh and index reconciliation
normally each run every five minutes, so updates are asynchronous. The
`state.json` file records the last successful transfer and generation fingerprint.
Search can continue serving its last index during a mirror outage.

`ops/search/notes-search-sync@.service` and `.timer` run the checked
`scripts/search_snapshot.py` with a protected per-host configuration. Deployment
of these native units and the read-only SSH key is an explicit administrator
step. The sample worker `.env` contains search settings only, not a bot token,
web session secret or notifications credentials.

## Storage and delivery

1. Back up current configuration and take a preliminary search database dump.
   Prepare independent empty destination volumes and the read-only mirror.
2. Stop only the source search writer. Take a final logical dump, then stop its
   PostgreSQL cleanly and copy the complete cold cluster with numeric ownership.
   Use the exact source image digest and architecture. Do not initialize the
   destination cluster over the restored data.
3. Compare all cold files, public table contents, IDs, sequences, schema and
   database roles before enabling the destination writer. Preserve the old
   stopped source containers and volumes with restart disabled.
4. Bootstrap the dedicated persistent tailnet namespace with
   `make search-tailnet-up`. Only the application host may reach search RPCs and
   metrics over the tailnet; IPv6 access to these ports is blocked. No Windows
   host ports or global host routes are added. The sidecar joins local database,
   Prometheus and Jaeger networks; search shares its namespace.
5. CI builds the usual complete release once. With
   `SEARCH_WORKER_DEPLOY_ENABLED=true`, the restricted SSH receiver deploys only
   its search image to WSL before application-host delivery. Registry credentials
   travel in stdin, not command arguments. The receiver accepts only the latest
   main SHA and this repository. Other application images cannot be activated
   by the search-worker role.
6. Select `compose.remote-search.yml` on the application host and set
   `NOTES_SEARCH_REMOTE=1`. Keep the original client entry points. Test search,
   database and mirror failures separately from Core operations and frontend
   startup. Never start the old local-search profile over stale source data.

Native namespace recovery must include the new sidecar and search container:
Docker daemon startup does not apply Compose dependency ordering, and shared
namespaces can fail or become stale when a sidecar restarts. Recovery acts only
on failed startup or positively identified stale namespaces and preserves
intentional stops.

## Backups and rollback

Core backups remain independent of WSL. `NOTES_SEARCH_REMOTE=1` selects backup
format 2: full vault, Yandex configuration, application settings and notifications
database, with an explicit external search marker. Existing format 1 backups
containing both databases remain supported. Install the updated backup reader
and source exporter before switching this marker.

`scripts/search_backup.py` separately exports, pulls and verifies search backups.
The off-host pull uses a dedicated key forced to the read-only export command;
new dumps are validated before replacing the latest pointer or applying
retention. A failed WSL backup retains earlier copies and does not block Core
backups. The verify operation restores into an isolated temporary database with
no network and deletes only that fixture. Schedule this independently of Core
backups, and verify an initial restore before leaving the migration.

After WSL accepts index writes, its database is newer than the source cold
cluster. Rollback requires stopping the destination writer and moving its latest
consistent database back, or explicitly accepting an index rebuild. Do not
silently reactivate the old source. The primary vault is never replaced by the
WSL read-only replica.
