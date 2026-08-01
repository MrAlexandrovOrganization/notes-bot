package search

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/internal/applog"
	pb "notes-bot/proto/search"
)

type SearchServer struct {
	pb.UnimplementedSearchServiceServer
	pool     *pgxpool.Pool
	cfg      *Config
	indexer  *Indexer
	metrics  *searchMetrics
	embedder *Embedder
}

func NewSearchServer(pool *pgxpool.Pool, cfg *Config, indexer *Indexer, metrics *searchMetrics, embedder *Embedder) *SearchServer {
	return &SearchServer{pool: pool, cfg: cfg, indexer: indexer, metrics: metrics, embedder: embedder}
}

func hitsToProto(hits []SearchHit, kind string) []*pb.Hit {
	out := make([]*pb.Hit, len(hits))
	for i, h := range hits {
		out[i] = &pb.Hit{
			NoteId:      h.NoteID,
			Relpath:     h.Relpath,
			Name:        h.Name,
			Snippet:     h.Snippet,
			Score:       h.Score,
			ChunkKind:   firstNonEmpty(h.ChunkKind, kind),
			ChunkId:     h.ChunkID,
			HeadingPath: h.Heading,
			Ord:         int32(h.Ord),
			Neighbor:    h.Neighbor,
			NoteDate:    h.NoteDate,
			Title:       h.Title,
			Tags:        h.Tags,
			Links:       h.Links,
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *SearchServer) SearchByName(ctx context.Context, req *pb.SearchRequest) (resp *pb.SearchResponse, err error) {
	defer s.metrics.recordRPC(ctx, "SearchByName", &err)
	defer s.metrics.recordSearch(ctx, "name", &err)
	log := applog.With(ctx, logger)

	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	hits, err := SearchByName(ctx, s.pool, req.Query, int(req.Limit))
	if err != nil {
		log.Error("search by name", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SearchResponse{Hits: hitsToProto(hits, "")}, nil
}

func (s *SearchServer) SearchByContent(ctx context.Context, req *pb.SearchRequest) (resp *pb.SearchResponse, err error) {
	defer s.metrics.recordRPC(ctx, "SearchByContent", &err)
	defer s.metrics.recordSearch(ctx, "content", &err)
	log := applog.With(ctx, logger)

	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	filters, err := searchFiltersFromRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	hits, err := SearchChunksByContent(ctx, s.pool, req.Query, normalizedLimit(int(req.Limit), 10, 100), filters)
	if err != nil {
		log.Error("search by content", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SearchResponse{Hits: hitsToProto(hits, "")}, nil
}

func (s *SearchServer) SearchSemantic(ctx context.Context, req *pb.SearchRequest) (resp *pb.SearchResponse, err error) {
	defer s.metrics.recordRPC(ctx, "SearchSemantic", &err)
	defer s.metrics.recordSearch(ctx, "semantic", &err)
	log := applog.With(ctx, logger)

	if !s.cfg.EnableEmbeddings || s.embedder == nil {
		return nil, status.Error(codes.Unimplemented, "semantic search disabled (set SEARCH_ENABLE_EMBEDDINGS=true)")
	}
	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	filters, err := searchFiltersFromRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	vec, err := s.embedder.EmbedOne(ctx, req.Query, s.metrics)
	if err != nil {
		log.Error("embed query", zap.Error(err))
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	hits, err := SearchByVector(ctx, s.pool, vec, normalizedLimit(int(req.Limit), 8, 100), filters)
	if err != nil {
		log.Error("vector search", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SearchResponse{Hits: hitsToProto(hits, "")}, nil
}

func (s *SearchServer) SearchHybrid(ctx context.Context, req *pb.SearchRequest) (resp *pb.SearchResponse, err error) {
	defer s.metrics.recordRPC(ctx, "SearchHybrid", &err)
	defer s.metrics.recordSearch(ctx, "hybrid", &err)
	log := applog.With(ctx, logger)

	if !s.cfg.EnableEmbeddings || s.embedder == nil {
		return nil, status.Error(codes.Unimplemented, "semantic search disabled (set SEARCH_ENABLE_EMBEDDINGS=true)")
	}
	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	filters, err := searchFiltersFromRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	vec, err := s.embedder.EmbedOne(ctx, req.Query, s.metrics)
	if err != nil {
		log.Error("embed hybrid query", zap.Error(err))
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	limit := normalizedLimit(int(req.Limit), 12, 50)
	fetch := min(max(limit*4, 40), 200)
	dense, err := SearchByVector(ctx, s.pool, vec, fetch, filters)
	if err != nil {
		log.Error("hybrid vector search", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	lexical, lexicalErr := SearchChunksByContent(ctx, s.pool, req.Query, fetch, filters)
	if lexicalErr != nil {
		log.Warn("hybrid lexical search", zap.Error(lexicalErr))
	}
	maxPerNote := 2
	if filters.DateFrom != nil && filters.DateTo != nil && filters.DateFrom.Equal(*filters.DateTo) {
		// A single-day question commonly targets one daily note; diversity must
		// not hide the rest of that note from broad prompts such as "what did I do".
		maxPerNote = limit
	}
	selected := FuseByChunkID(dense, lexical, limit, maxPerNote)
	expanded, err := ExpandChunkNeighbors(ctx, s.pool, selected, 1)
	if err != nil {
		log.Error("expand hybrid context", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SearchResponse{Hits: hitsToProto(expanded, "")}, nil
}

func searchFiltersFromRequest(req *pb.SearchRequest) (SearchFilters, error) {
	filters := SearchFilters{Kinds: req.Kinds}
	for _, kind := range filters.Kinds {
		if kind != string(KindParagraph) && kind != string(KindTask) {
			return SearchFilters{}, fmt.Errorf("unsupported chunk kind %q", kind)
		}
	}
	parse := func(value, field string) (*time.Time, error) {
		if value == "" {
			return nil, nil
		}
		for _, layout := range []string{"2006-01-02", "02-Jan-2006", "02.01.2006"} {
			if parsed, parseErr := time.Parse(layout, value); parseErr == nil {
				return &parsed, nil
			}
		}
		return nil, fmt.Errorf("%s must be YYYY-MM-DD or DD-Mmm-YYYY", field)
	}
	var err error
	if filters.DateFrom, err = parse(req.DateFrom, "date_from"); err != nil {
		return SearchFilters{}, err
	}
	if filters.DateTo, err = parse(req.DateTo, "date_to"); err != nil {
		return SearchFilters{}, err
	}
	if filters.DateFrom != nil && filters.DateTo != nil && filters.DateFrom.After(*filters.DateTo) {
		return SearchFilters{}, fmt.Errorf("date_from must not be after date_to")
	}
	return filters, nil
}

func normalizedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	return min(value, maximum)
}

func (s *SearchServer) GetNote(ctx context.Context, req *pb.GetNoteRequest) (resp *pb.Note, err error) {
	defer s.metrics.recordRPC(ctx, "GetNote", &err)
	log := applog.With(ctx, logger)

	var note *NoteFull
	switch k := req.Key.(type) {
	case *pb.GetNoteRequest_Id:
		note, err = GetNoteByID(ctx, s.pool, k.Id)
	case *pb.GetNoteRequest_Relpath:
		note, err = GetNoteByRelpath(ctx, s.pool, k.Relpath)
	default:
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	if err != nil {
		log.Error("get note", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	if note == nil {
		return nil, status.Error(codes.NotFound, "note not found")
	}
	return &pb.Note{
		Id:      note.ID,
		Relpath: note.Relpath,
		Name:    note.Name,
		Content: note.Content,
		Mtime:   timestamppb.New(note.Mtime),
	}, nil
}

func (s *SearchServer) Reindex(ctx context.Context, req *pb.ReindexRequest) (resp *pb.ReindexResponse, err error) {
	defer s.metrics.recordRPC(ctx, "Reindex", &err)
	if s.indexer == nil {
		return nil, status.Error(codes.FailedPrecondition, "indexer not configured")
	}
	var stats SyncStats
	var syncErr error
	if req.Force {
		stats, syncErr = s.indexer.ForceReindex(ctx)
	} else {
		stats, syncErr = s.indexer.SyncOnce(ctx)
	}
	if syncErr != nil {
		return nil, status.Error(codes.Internal, syncErr.Error())
	}
	pending, countErr := CountNotesPendingIndex(ctx, s.pool, s.cfg.EmbedModel)
	if countErr != nil {
		return nil, status.Error(codes.Internal, countErr.Error())
	}
	return &pb.ReindexResponse{
		Added:    int32(stats.Added),
		Updated:  int32(stats.Updated),
		Deleted:  int32(stats.Deleted),
		Embedded: int32(stats.Embedded),
		Errors:   int32(stats.Errors),
		Pending:  pending,
	}, nil
}

// Metrics is the search service's metric set. Returned as an opaque pointer so
// callers (main, tests) can construct and pass it without exposing the layout.
type Metrics = searchMetrics

// NewMetrics constructs the metric set wired to the global MeterProvider.
func NewMetrics() *Metrics { return newSearchMetrics() }
