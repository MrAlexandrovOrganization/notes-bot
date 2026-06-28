package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/internal/grpcutil"
	pb "notes-bot/proto/search"
)

// SearchHit is the user-facing result of any search RPC.
type SearchHit struct {
	NoteID    int64
	Relpath   string
	Name      string
	Snippet   string
	Score     float64
	ChunkKind string
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
	conn, err := grpcutil.Dial(host, port)
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
			Relpath:   h.Relpath,
			Name:      h.Name,
			Snippet:   h.Snippet,
			Score:     h.Score,
			ChunkKind: h.ChunkKind,
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
