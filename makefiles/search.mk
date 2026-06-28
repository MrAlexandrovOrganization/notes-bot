# Search service introspection targets.
#
# Local-only. Run against your local docker compose stack:
#
#   make search-stats
#   make search-chunks
#   make search-doctor
#
# Assumes default container/network names (notes-bot-postgres-search,
# notes-bot-search, notes-bot_default).

SEARCH_DB_CONTAINER  ?= notes-bot-postgres-search
SEARCH_CONTAINER     ?= notes-bot-search
SEARCH_DB_USER       ?= search
SEARCH_DB_NAME       ?= search
SEARCH_DOCKER_NET    ?= notes-bot_default

SEARCH_PSQL = docker exec $(SEARCH_DB_CONTAINER) psql -U $(SEARCH_DB_USER) -d $(SEARCH_DB_NAME) -P pager=off -c

# ── Overview ──────────────────────────────────────────────────────────────────

search-stats:  ## Single-row summary: notes, chunks, sizes, last sync.
	@$(SEARCH_PSQL) "SELECT (SELECT count(*) FROM notes) AS notes_total, (SELECT count(*) FROM note_chunks) AS chunks_total, (SELECT count(*) FROM notes n WHERE NOT EXISTS (SELECT 1 FROM note_chunks c WHERE c.note_id=n.id)) AS notes_no_chunks, pg_size_pretty(pg_total_relation_size('notes')) AS notes_size, pg_size_pretty(pg_total_relation_size('note_chunks')) AS chunks_size, (SELECT max(indexed_at) FROM notes)::text AS last_indexed_at;"

search-chunks:  ## Chunks by kind: counts, distinct notes, length stats.
	@$(SEARCH_PSQL) "SELECT kind, count(*) AS chunks, count(DISTINCT note_id) AS distinct_notes, round(avg(length(text)))::int AS avg_chars, min(length(text)) AS min_chars, max(length(text)) AS max_chars, pg_size_pretty(sum(length(text))::bigint) AS total_text FROM note_chunks GROUP BY kind ORDER BY kind;"

search-top-notes:  ## Top-10 largest notes by stored size.
	@$(SEARCH_PSQL) "SELECT name, pg_size_pretty(size::bigint) AS bytes, length(content) AS chars FROM notes ORDER BY size DESC LIMIT 10;"

# ── Logs ──────────────────────────────────────────────────────────────────────

search-logs:  ## Recent sync results + errors from the search container.
	@docker logs --tail 300 $(SEARCH_CONTAINER) 2>&1 | grep -E 'sync done|backfill progress|backfill pass done|"level":"error"' | tail -20

search-logs-errors:  ## Just errors from the search container.
	@docker logs --tail 500 $(SEARCH_CONTAINER) 2>&1 | grep -i 'error' | tail -20

# ── Metrics ───────────────────────────────────────────────────────────────────

search-metrics:  ## Dump all search_* Prometheus metrics live.
	@curl -sf http://127.0.0.1:9103/metrics | grep -E '^search_' | grep -v '^#' | sort

# ── Actions ───────────────────────────────────────────────────────────────────

search-reindex:  ## Force a full Reindex over gRPC.
	@docker run --rm --network $(SEARCH_DOCKER_NET) fullstorydev/grpcurl:v1.9.1 -plaintext -emit-defaults -d '{"force":true}' $(SEARCH_CONTAINER):50054 search.SearchService/Reindex

# ── Combined ──────────────────────────────────────────────────────────────────

search-doctor: search-stats search-chunks search-logs  ## One-stop health overview.

search-help:  ## Print available search-* targets.
	@awk -F':.*##' '/^search-[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}' makefiles/search.mk

.PHONY: search-stats search-chunks search-top-notes search-logs search-logs-errors search-metrics search-reindex search-doctor search-help
