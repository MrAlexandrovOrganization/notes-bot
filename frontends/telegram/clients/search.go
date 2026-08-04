package clients

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"notes-bot/internal/grpcutil"
	pb "notes-bot/proto/search"
)

// searchCallTimeout is generous because SearchSemantic may trigger Ollama to
// load the embedding model from disk on cold start (5-15s for bge-m3:567m).
const searchCallTimeout = 5 * time.Minute

// SearchHit is the user-facing result of any search RPC.
type SearchHit struct {
	NoteID    int64
	ChunkID   int64
	Relpath   string
	Name      string
	Snippet   string
	Score     float64
	ChunkKind string
	Heading   string
	Ord       int
	Neighbor  bool
	NoteDate  string
	Title     string
	Tags      []string
	Links     []string
}

type SearchOptions struct {
	DateFrom string
	DateTo   string
	Kinds    []string
	NoteIDs  []int64
}

type AskNotesResult struct {
	Answer          string
	Evidence        []*SearchHit
	Searches        []string
	Steps           int
	BudgetExhausted bool
}

// SearchNote is the full content returned by GetNote.
type SearchNote struct {
	ID      int64
	Relpath string
	Name    string
	Content string
	Mtime   time.Time
}

type SearchClient struct {
	conn *grpc.ClientConn
	stub pb.SearchServiceClient
}

func NewSearchClient(host, port string) (*SearchClient, error) {
	// Custom dial — bypasses grpcutil.Dial's 10s default. Semantic search can
	// take longer when Ollama is warming up the embedding model.
	conn, err := grpc.NewClient(
		fmt.Sprintf("%s:%s", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(grpcutil.TimeoutInterceptor(searchCallTimeout)),
	)
	if err != nil {
		return nil, fmt.Errorf("dial search: %w", err)
	}
	return &SearchClient{conn: conn, stub: pb.NewSearchServiceClient(conn)}, nil
}

func (c *SearchClient) Close() {
	c.conn.Close()
}

func protoToHits(resp *pb.SearchResponse) []*SearchHit {
	if resp == nil {
		return nil
	}
	out := make([]*SearchHit, len(resp.Hits))
	for i, h := range resp.Hits {
		out[i] = &SearchHit{
			NoteID:    h.NoteId,
			ChunkID:   h.ChunkId,
			Relpath:   h.Relpath,
			Name:      h.Name,
			Snippet:   h.Snippet,
			Score:     h.Score,
			ChunkKind: h.ChunkKind,
			Heading:   h.HeadingPath,
			Ord:       int(h.Ord),
			Neighbor:  h.Neighbor,
			NoteDate:  h.NoteDate,
			Title:     h.Title,
			Tags:      h.Tags,
			Links:     h.Links,
		}
	}
	return out
}

func (c *SearchClient) SearchByName(ctx context.Context, query string, limit int) ([]*SearchHit, error) {
	resp, err := c.stub.SearchByName(ctx, &pb.SearchRequest{Query: query, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) FindNotes(ctx context.Context, query string, limit int, options SearchOptions) ([]*SearchHit, error) {
	resp, err := c.stub.FindNotes(ctx, &pb.SearchRequest{
		Query: query, Limit: int32(limit), DateFrom: options.DateFrom, DateTo: options.DateTo,
		Kinds: options.Kinds, NoteIds: options.NoteIDs,
	})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) SearchByContent(ctx context.Context, query string, limit int) ([]*SearchHit, error) {
	resp, err := c.stub.SearchByContent(ctx, &pb.SearchRequest{Query: query, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) SearchSemantic(ctx context.Context, query string, limit int) ([]*SearchHit, error) {
	resp, err := c.stub.SearchSemantic(ctx, &pb.SearchRequest{Query: query, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) SearchHybrid(ctx context.Context, query string, limit int, options SearchOptions) ([]*SearchHit, error) {
	resp, err := c.stub.SearchHybrid(ctx, &pb.SearchRequest{
		Query:    query,
		Limit:    int32(limit),
		DateFrom: options.DateFrom,
		DateTo:   options.DateTo,
		Kinds:    options.Kinds,
		NoteIds:  options.NoteIDs,
	})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) SearchProfiles(ctx context.Context, query string, limit int, options SearchOptions) ([]*SearchHit, error) {
	resp, err := c.stub.SearchProfiles(ctx, &pb.SearchRequest{
		Query: query, Limit: int32(limit), DateFrom: options.DateFrom, DateTo: options.DateTo,
		Kinds: options.Kinds, NoteIds: options.NoteIDs,
	})
	if err != nil {
		return nil, err
	}
	return protoToHits(resp), nil
}

func (c *SearchClient) AskNotes(ctx context.Context, question, currentDateTime string, options SearchOptions) (*AskNotesResult, error) {
	resp, err := c.stub.AskNotes(ctx, &pb.AskRequest{
		Question: question, CurrentDatetime: currentDateTime,
		DateFrom: options.DateFrom, DateTo: options.DateTo,
	})
	if err != nil {
		return nil, err
	}
	return &AskNotesResult{
		Answer:          resp.Answer,
		Evidence:        protoToHits(&pb.SearchResponse{Hits: resp.Evidence}),
		Searches:        resp.Searches,
		Steps:           int(resp.Steps),
		BudgetExhausted: resp.BudgetExhausted,
	}, nil
}

func (c *SearchClient) GetNoteByID(ctx context.Context, id int64) (*SearchNote, error) {
	resp, err := c.stub.GetNote(ctx, &pb.GetNoteRequest{Key: &pb.GetNoteRequest_Id{Id: id}})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	return &SearchNote{
		ID:      resp.Id,
		Relpath: resp.Relpath,
		Name:    resp.Name,
		Content: resp.Content,
		Mtime:   resp.Mtime.AsTime(),
	}, nil
}
