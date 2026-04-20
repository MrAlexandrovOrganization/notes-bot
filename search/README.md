# Search Service POC

Semantic search for notes using ChromaDB and sentence-transformers.

## Architecture

```
┌─────────────────┐     gRPC      ┌─────────────────┐
│  Telegram Bot   │ ─────────────►│  Search Service │ (Python)
│     (Go)        │               │   :50054        │
└─────────────────┘               └────────┬────────┘
                                           │
                                    ┌──────▼──────┐
                                    │  ChromaDB   │
                                    │  (vectors)  │
                                    └─────────────┘
```

## Quick Start

### 1. Build and run

```bash
docker compose build search
docker compose up -d search
```

### 2. Index notes

```bash
docker compose exec search python -c "
import grpc
from search_pb2_grpc import SearchServiceStub
from search_pb2 import IndexNotesRequest

channel = grpc.insecure_channel('localhost:50054')
stub = SearchServiceStub(channel)
resp = stub.IndexNotes(IndexNotesRequest(notes_path='/notes'))
print(f'Indexed {resp.indexed_count} notes')
"
```

### 3. Search

```bash
docker compose exec search python -c "
import grpc
from search_pb2_grpc import SearchServiceStub
from search_pb2 import SearchNotesRequest

channel = grpc.insecure_channel('localhost:50054')
stub = SearchServiceStub(channel)
resp = stub.SearchNotes(SearchNotesRequest(query='what did I work on', limit=5))
for r in resp.results:
    print(f'{r.date}: {r.score:.2f}')
    print(r.content[:200])
    print('---')
"
```

## Integration with Telegram Bot

The `SearchClient` in `frontends/telegram/clients/search.go` provides:

- `IndexNotes(ctx, notesPath)` - Index all markdown files
- `SearchNotes(ctx, query, limit)` - Semantic search
- `GetIndexStatus(ctx)` - Get index stats

Example usage in a handler:

```go
searchClient, err := clients.NewSearchClient("search:50054")
if err != nil {
    // handle error
}
defer searchClient.Close()

results, err := searchClient.SearchNotes(ctx, "project X", 5)
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_PORT` | `50054` | gRPC server port |
| `NOTES_DIR` | `/notes` | Notes directory (from docker-compose) |

## Tech Stack

- **ChromaDB** - Vector database for embeddings
- **sentence-transformers** - `all-MiniLM-L6-v2` model for embeddings
- **gRPC** - Service communication

## MemPalace Comparison

This POC uses a simplified approach compared to MemPalace:

| Feature | MemPalace | This POC |
|---------|-----------|----------|
| Storage | ChromaDB | ChromaDB |
| Embeddings | sentence-transformers | sentence-transformers |
| Organization | Palace (wings/rooms) | Flat index |
| Dialect | AAAK (compression) | None |
| MCP Server | Yes | No (gRPC only) |

For full MemPalace integration, you could:
1. Use MemPalace's CLI directly via subprocess
2. Run MemPalace's MCP server and call via HTTP
3. Port key modules (knowledge_graph, searcher) to Go

## Files

- `proto/search/search.proto` - gRPC service definition
- `search/server.py` - Python gRPC server
- `search/Dockerfile` - Container definition
- `search/requirements.txt` - Python dependencies
- `frontends/telegram/clients/search.go` - Go client
