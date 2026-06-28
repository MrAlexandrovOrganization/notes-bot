package search

import (
	"context"

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
	pool    *pgxpool.Pool
	cfg     *Config
	indexer *Indexer
	metrics *searchMetrics
}

func NewSearchServer(pool *pgxpool.Pool, cfg *Config, indexer *Indexer, metrics *searchMetrics) *SearchServer {
	return &SearchServer{pool: pool, cfg: cfg, indexer: indexer, metrics: metrics}
}

func hitsToProto(hits []SearchHit, kind string) []*pb.Hit {
	out := make([]*pb.Hit, len(hits))
	for i, h := range hits {
		out[i] = &pb.Hit{
			NoteId:    h.NoteID,
			Relpath:   h.Relpath,
			Name:      h.Name,
			Snippet:   h.Snippet,
			Score:     h.Score,
			ChunkKind: firstNonEmpty(h.ChunkKind, kind),
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
	hits, err := SearchByContent(ctx, s.pool, req.Query, int(req.Limit))
	if err != nil {
		log.Error("search by content", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SearchResponse{Hits: hitsToProto(hits, "")}, nil
}

func (s *SearchServer) SearchSemantic(ctx context.Context, req *pb.SearchRequest) (resp *pb.SearchResponse, err error) {
	defer s.metrics.recordRPC(ctx, "SearchSemantic", &err)
	defer s.metrics.recordSearch(ctx, "semantic", &err)
	if !s.cfg.EnableEmbeddings {
		return nil, status.Error(codes.Unimplemented, "semantic search disabled (set ENABLE_EMBEDDINGS=true)")
	}
	return nil, status.Error(codes.Unimplemented, "semantic search not implemented yet")
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
	stats, syncErr := s.indexer.SyncOnce(ctx)
	if syncErr != nil {
		return nil, status.Error(codes.Internal, syncErr.Error())
	}
	return &pb.ReindexResponse{
		Added:    int32(stats.Added),
		Updated:  int32(stats.Updated),
		Deleted:  int32(stats.Deleted),
		Embedded: int32(stats.Embedded),
	}, nil
}

// Metrics is the search service's metric set. Returned as an opaque pointer so
// callers (main, tests) can construct and pass it without exposing the layout.
type Metrics = searchMetrics

// NewMetrics constructs the metric set wired to the global MeterProvider.
func NewMetrics() *Metrics { return newSearchMetrics() }
